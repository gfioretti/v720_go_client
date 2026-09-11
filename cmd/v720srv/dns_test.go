package main

import (
	"context"
	"encoding/binary"
	"net"
	"strings"
	"testing"
	"time"
)

func TestIsNaxclow(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"v720.naxclow.com", true},
		{"v720.p2p.naxclow.com", true},
		{"naxclow.com", true},
		{"logo.naxclow.com.cn", true},
		{"v720.naxclow.com.", true}, // trailing dot
		{"NAXCLOW.COM", true},       // case-insensitive
		{"google.com", false},
		{"notnaxclow.com", false}, // must be a real suffix, not a substring
		{"naxclow.com.evil.com", false},
	}
	for _, tt := range tests {
		if got := isNaxclow(tt.name); got != tt.want {
			t.Errorf("isNaxclow(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

// dnsQuery encodes a minimal DNS query for name/qtype.
func dnsQuery(name string, qtype uint16) []byte {
	msg := make([]byte, 12)
	binary.BigEndian.PutUint16(msg[0:2], 0x1234) // ID
	msg[2] = 0x01                                // RD
	binary.BigEndian.PutUint16(msg[4:6], 1)      // QDCOUNT
	for _, label := range strings.Split(name, ".") {
		msg = append(msg, byte(len(label)))
		msg = append(msg, label...)
	}
	msg = append(msg, 0) // root label
	tail := make([]byte, 4)
	binary.BigEndian.PutUint16(tail[0:2], qtype)
	binary.BigEndian.PutUint16(tail[2:4], 1) // CLASS IN
	return append(msg, tail...)
}

func TestParseQuestion(t *testing.T) {
	name, qtype, ok := parseQuestion(dnsQuery("v720.naxclow.com", dnsTypeA))
	if !ok || name != "v720.naxclow.com" || qtype != dnsTypeA {
		t.Fatalf("parseQuestion = (%q, %d, %v)", name, qtype, ok)
	}
	if _, _, ok := parseQuestion([]byte{0, 1, 2}); ok {
		t.Error("parseQuestion accepted a truncated message")
	}
}

func TestBuildAResponse(t *testing.T) {
	query := dnsQuery("v720.naxclow.com", dnsTypeA)
	resp := buildAResponse(query, net.ParseIP("192.168.1.5"))

	if resp[2]&0x80 == 0 {
		t.Error("QR bit not set on response")
	}
	if binary.BigEndian.Uint16(resp[6:8]) != 1 {
		t.Fatalf("ANCOUNT = %d, want 1", binary.BigEndian.Uint16(resp[6:8]))
	}
	// The question round-trips, and the A record's RDATA is the sink IP.
	if name, _, ok := parseQuestion(resp); !ok || name != "v720.naxclow.com" {
		t.Fatalf("question in response = %q, %v", name, ok)
	}
	rdata := resp[len(resp)-4:]
	if net.IP(rdata).String() != "192.168.1.5" {
		t.Fatalf("A record RDATA = %v, want 192.168.1.5", net.IP(rdata))
	}
}

func TestBuildEmptyResponse(t *testing.T) {
	resp := buildEmptyResponse(dnsQuery("v720.naxclow.com", dnsTypeAAAA))
	if resp[2]&0x80 == 0 || binary.BigEndian.Uint16(resp[6:8]) != 0 {
		t.Fatalf("empty response: QR/ANCOUNT wrong: %x", resp[:8])
	}
}

// End-to-end: naxclow A queries are sinkholed; everything else is forwarded to
// the configured upstream.
func TestRunDNS_SinkholeAndForward(t *testing.T) {
	// A fake upstream that answers any query with an A record for 9.9.9.9.
	upstream, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("upstream listen: %v", err)
	}
	defer func() { _ = upstream.Close() }()
	go func() {
		b := make([]byte, 1500)
		for {
			n, addr, err := upstream.ReadFrom(b)
			if err != nil {
				return
			}
			_, _ = upstream.WriteTo(buildAResponse(b[:n], net.ParseIP("9.9.9.9")), addr)
		}
	}()

	dnsConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("dns listen: %v", err)
	}
	defer func() { _ = dnsConn.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go runDNS(ctx, dnsConn, net.ParseIP("192.168.1.5"), upstream.LocalAddr().String(), false, discardf)

	client, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("client listen: %v", err)
	}
	defer func() { _ = client.Close() }()

	ask := func(name string) net.IP {
		t.Helper()
		if _, err := client.WriteTo(dnsQuery(name, dnsTypeA), dnsConn.LocalAddr()); err != nil {
			t.Fatalf("client write: %v", err)
		}
		buf := make([]byte, 1500)
		_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, _, err := client.ReadFrom(buf)
		if err != nil {
			t.Fatalf("client read: %v", err)
		}
		return net.IP(buf[n-4 : n])
	}

	if ip := ask("v720.naxclow.com"); ip.String() != "192.168.1.5" {
		t.Errorf("naxclow resolved to %v, want the sink 192.168.1.5", ip)
	}
	if ip := ask("example.com"); ip.String() != "9.9.9.9" {
		t.Errorf("example.com resolved to %v, want the forwarded 9.9.9.9", ip)
	}
}
