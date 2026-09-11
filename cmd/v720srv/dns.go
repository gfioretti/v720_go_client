package main

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"strings"
	"time"
)

// A minimal DNS sinkhole (RFC 1035), hand-rolled to keep the project
// dependency-free. It answers A queries for naxclow domains with the server's IP
// (so the camera connects out to us) and forwards every other query upstream (so
// the rest of the network's DNS keeps working). This lets the user point the
// Fritz!Box's local DNS at the server box instead of standing up Pi-hole/dnsmasq.

// naxclowSuffixes are the base domains sinkholed to the server.
var naxclowSuffixes = []string{"naxclow.com", "naxclow.com.cn"}

func isNaxclow(name string) bool {
	n := strings.ToLower(strings.TrimSuffix(name, "."))
	for _, base := range naxclowSuffixes {
		if n == base || strings.HasSuffix(n, "."+base) {
			return true
		}
	}
	return false
}

const (
	dnsTypeA    = 1
	dnsTypeAAAA = 28
)

// parseQuestion reads the first question's name and qtype from a DNS message.
// Question names never use compression, so a simple label walk suffices.
func parseQuestion(msg []byte) (name string, qtype uint16, ok bool) {
	if len(msg) < 12 || binary.BigEndian.Uint16(msg[4:6]) < 1 {
		return "", 0, false
	}
	off := 12
	var labels []string
	for {
		if off >= len(msg) {
			return "", 0, false
		}
		n := int(msg[off])
		off++
		if n == 0 {
			break
		}
		if n > 63 || off+n > len(msg) {
			return "", 0, false
		}
		labels = append(labels, string(msg[off:off+n]))
		off += n
	}
	if off+4 > len(msg) {
		return "", 0, false
	}
	return strings.Join(labels, "."), binary.BigEndian.Uint16(msg[off : off+2]), true
}

// questionEnd returns the offset just past the (single) question section. It is
// only sound on a message parseQuestion accepted; it clamps to len(msg) so a
// stray caller can never drive a slice out of bounds.
func questionEnd(msg []byte) int {
	off := 12
	for off < len(msg) {
		n := int(msg[off])
		if n == 0 {
			off++
			break
		}
		off += n + 1
	}
	end := off + 4 // qtype (2) + qclass (2)
	if end > len(msg) {
		return len(msg)
	}
	return end
}

// responseHeader turns the copied query header into a response: QR=1, AA=1, RA=1,
// RCODE=0, preserving the opcode/RD bits, and sets the section counts.
func responseHeader(resp []byte, ancount uint16) {
	resp[2] = resp[2] | 0x84                 // QR + AA (preserve opcode, RD, TC)
	resp[3] = 0x80                           // RA=1, RCODE=0
	binary.BigEndian.PutUint16(resp[4:6], 1) // QDCOUNT: we only echo the first question
	binary.BigEndian.PutUint16(resp[6:8], ancount)
	binary.BigEndian.PutUint16(resp[8:10], 0)  // NSCOUNT
	binary.BigEndian.PutUint16(resp[10:12], 0) // ARCOUNT
}

// buildAResponse answers a query with a single A record pointing at ip.
func buildAResponse(query []byte, ip net.IP) []byte {
	ip4 := ip.To4()
	if ip4 == nil {
		return buildEmptyResponse(query)
	}
	qend := questionEnd(query)
	resp := make([]byte, qend, qend+16)
	copy(resp, query[:qend])
	responseHeader(resp, 1)
	// answer: name pointer to the question (0xC00C), type A, class IN, TTL 60,
	// RDLENGTH 4, the IPv4 address.
	resp = append(resp,
		0xC0, 0x0C, // name pointer to offset 12
		0x00, 0x01, // TYPE A
		0x00, 0x01, // CLASS IN
		0x00, 0x00, 0x00, 0x3C, // TTL 60
		0x00, 0x04, // RDLENGTH 4
		ip4[0], ip4[1], ip4[2], ip4[3],
	)
	return resp
}

// buildEmptyResponse answers with NOERROR and no records (NODATA) — used for a
// sinkholed non-A query (e.g. AAAA) so the camera falls back to our A record
// rather than resolving the real address.
func buildEmptyResponse(query []byte) []byte {
	qend := questionEnd(query)
	resp := make([]byte, qend)
	copy(resp, query[:qend])
	responseHeader(resp, 0)
	return resp
}

// forwardDNS relays a query to an upstream resolver and returns its response.
func forwardDNS(query []byte, upstream string) ([]byte, error) {
	conn, err := net.DialTimeout("udp", upstream, 3*time.Second)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := conn.Write(query); err != nil {
		return nil, err
	}
	buf := make([]byte, 1500)
	n, err := conn.Read(buf)
	if err != nil {
		return nil, err
	}
	// Sanity-check the transaction id matches the query (cheap anti-spoof hygiene
	// on a fresh per-query socket).
	if n < 2 || len(query) < 2 || buf[0] != query[0] || buf[1] != query[1] {
		return nil, errors.New("dns: upstream response id mismatch")
	}
	return buf[:n], nil
}

// runDNS serves the sinkhole on conn until it is closed (or ctx is done).
func runDNS(ctx context.Context, conn net.PacketConn, sinkIP net.IP, upstream string, verbose bool, logf func(string, ...any)) {
	buf := make([]byte, 1500)
	for {
		n, addr, err := conn.ReadFrom(buf)
		if err != nil {
			if ctx.Err() == nil {
				logf("dns: stopped: %v", err)
			}
			return
		}
		query := append([]byte(nil), buf[:n]...)
		go handleDNS(conn, query, addr, sinkIP, upstream, verbose, logf)
	}
}

func handleDNS(conn net.PacketConn, query []byte, addr net.Addr, sinkIP net.IP, upstream string, verbose bool, logf func(string, ...any)) {
	name, qtype, ok := parseQuestion(query)
	if !ok {
		return
	}
	if isNaxclow(name) {
		if verbose {
			logf("dns: sinkhole %s (type %d) -> %s", name, qtype, sinkIP)
		}
		if qtype == dnsTypeA {
			_, _ = conn.WriteTo(buildAResponse(query, sinkIP), addr)
		} else {
			_, _ = conn.WriteTo(buildEmptyResponse(query), addr)
		}
		return
	}
	if resp, err := forwardDNS(query, upstream); err == nil {
		_, _ = conn.WriteTo(resp, addr)
	}
}
