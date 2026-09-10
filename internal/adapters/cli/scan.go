package cli

import (
	"context"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"v720_go_client/internal/adapters/transport/framedtcp"
	"v720_go_client/internal/domain"
)

// defaultProbeTimeout bounds each per-host probe; a camera on the LAN answers the
// handshake echo well within this, and closed/absent hosts fail (refused) or hit
// it (filtered) quickly.
const defaultProbeTimeout = 400 * time.Millisecond

// runScan finds V720 cameras on the local network by probing a subnet for the
// AP/direct TCP port and confirming each open host speaks the camera handshake.
// It is the "locate the camera" step for reaching a camera in STA mode (joined to
// the home Wi-Fi) rather than via its own hotspot — the camera is reachable by a
// direct TCP connection using the same 20-byte frame protocol (findings doc
// §12.3a). Discovery is by port+handshake probe: the app has no broadcast
// discovery, so the camera's LAN IP must be found this way (or from the router).
func (a *App) runScan(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("scan", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	subnet := fs.String("subnet", "", "CIDR to scan, e.g. 192.168.1.0/24 (default: derived from a local interface)")
	port := fs.Int("port", 6123, "camera TCP port to probe")
	timeout := fs.Duration("timeout", defaultProbeTimeout, "per-host probe timeout")
	concurrency := fs.Int("concurrency", 128, "maximum concurrent probes")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() > 0 {
		a.errf("v720ctl scan: unexpected arguments: %v\n", fs.Args())
		return 2
	}
	if *concurrency < 1 {
		a.errf("v720ctl scan: --concurrency must be >= 1\n")
		return 2
	}

	cidr := *subnet
	if cidr == "" {
		derived, err := defaultSubnet()
		if err != nil {
			a.errf("v720ctl scan: could not derive a local subnet (%v); pass --subnet\n", err)
			return 2
		}
		cidr = derived
	}
	hosts, err := hostsInCIDR(cidr)
	if err != nil {
		a.errf("v720ctl scan: %v\n", err)
		return 2
	}

	a.outf("scanning %d hosts on %s (port %d)...\n", len(hosts), cidr, *port)
	found := scanHosts(ctx, hosts, *port, *timeout, *concurrency, probeCamera)
	if len(found) == 0 {
		a.outf("no cameras found\n")
		return 0
	}
	for _, ip := range found {
		a.outf("camera found: %s\n", net.JoinHostPort(ip, strconv.Itoa(*port)))
	}
	return 0
}

// probeFunc probes one address for a camera; it is the seam scanHosts fans out
// over, so tests can substitute a deterministic probe.
type probeFunc func(ctx context.Context, addr string, timeout time.Duration) bool

// probeCamera reports whether addr speaks the camera handshake: it opens the
// framed TCP connection, sends the bare type-115 trigger, and expects the device
// to echo a type-115 frame (findings doc §12.3 steps 2-3). This confirmation
// needs no device id, so it identifies a camera we don't yet have credentials
// for. Any dial/IO error or a non-115 reply means "not a camera".
func probeCamera(ctx context.Context, addr string, timeout time.Duration) bool {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ch, err := framedtcp.Dial(ctx, addr)
	if err != nil {
		return false
	}
	defer func() { _ = ch.Close() }()

	if err := ch.Send(ctx, domain.Frame{Type: domain.FrameTypeHandshakeTrigger}); err != nil {
		return false
	}
	f, err := ch.Receive(ctx)
	if err != nil {
		return false
	}
	return f.Type == domain.FrameTypeHandshakeTrigger
}

// scanHosts probes every host:port concurrently (bounded by concurrency) and
// returns the hosts that answered, preserving input order.
func scanHosts(ctx context.Context, hosts []string, port int, timeout time.Duration, concurrency int, probe probeFunc) []string {
	found := make([]bool, len(hosts))
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for i, h := range hosts {
		if ctx.Err() != nil {
			break // cancelled (e.g. Ctrl-C): stop launching further probes
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, addr string) {
			defer wg.Done()
			defer func() { <-sem }()
			if probe(ctx, addr, timeout) {
				found[i] = true
			}
		}(i, net.JoinHostPort(h, strconv.Itoa(port)))
	}
	wg.Wait()

	var out []string
	for i, ok := range found {
		if ok {
			out = append(out, hosts[i])
		}
	}
	return out
}

// maxHostBits caps a scan at a /16 (65 536 addresses) so a typo like /8 can't
// launch a multi-million-host sweep.
const maxHostBits = 16

// hostsInCIDR enumerates the probeable IPv4 addresses in cidr. For a /24 and
// larger it excludes the network and broadcast addresses; for /31 and /32 it
// returns every address (there are no reserved ones to skip).
func hostsInCIDR(cidr string) ([]string, error) {
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, fmt.Errorf("invalid subnet %q: %w", cidr, err)
	}
	network := ipnet.IP.To4()
	if network == nil {
		return nil, fmt.Errorf("only IPv4 subnets are supported: %s", cidr)
	}
	ones, bits := ipnet.Mask.Size()
	hostBits := bits - ones
	if hostBits > maxHostBits {
		return nil, fmt.Errorf("subnet %s is too large (/%d); use /%d or smaller", cidr, ones, bits-maxHostBits)
	}

	start := binary.BigEndian.Uint32(network)
	count := uint32(1) << uint(hostBits)
	skipEnds := hostBits >= 2 // only /30 and larger have distinct network+broadcast to skip

	out := make([]string, 0, count)
	for i := uint32(0); i < count; i++ {
		if skipEnds && (i == 0 || i == count-1) {
			continue
		}
		var b [4]byte
		binary.BigEndian.PutUint32(b[:], start+i)
		out = append(out, net.IP(b[:]).String())
	}
	return out, nil
}

// defaultSubnet derives a CIDR to scan from the host's private IPv4 interfaces,
// preferring the NARROWEST subnet (longest prefix). That favours a real LAN
// (typically /24) over a wide virtual bridge (e.g. Docker's 172.17.0.0/16 or a
// VPN), which would otherwise turn into a slow, wrong sweep.
func defaultSubnet() (string, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "", err
	}
	best := ""
	bestOnes := -1
	for _, addr := range addrs {
		ipnet, ok := addr.(*net.IPNet)
		if !ok {
			continue
		}
		ip4 := ipnet.IP.To4()
		if ip4 == nil || ip4.IsLoopback() || !ip4.IsPrivate() {
			continue
		}
		ones, _ := ipnet.Mask.Size()
		if ones > bestOnes {
			bestOnes, best = ones, ipnet.String()
		}
	}
	if best == "" {
		return "", errors.New("no private IPv4 interface found")
	}
	return best, nil
}
