package cli

import (
	"context"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gfioretti/v720_go_client/internal/domain"
)

func TestHostsInCIDR(t *testing.T) {
	tests := []struct {
		cidr      string
		wantCount int
		spotFirst string
		spotLast  string
	}{
		{"192.168.1.0/24", 254, "192.168.1.1", "192.168.1.254"}, // excludes .0 and .255
		{"10.0.0.0/30", 2, "10.0.0.1", "10.0.0.2"},              // excludes network+broadcast
		{"192.168.1.5/32", 1, "192.168.1.5", "192.168.1.5"},     // single host
		{"192.168.1.4/31", 2, "192.168.1.4", "192.168.1.5"},     // both usable
	}
	for _, tt := range tests {
		t.Run(tt.cidr, func(t *testing.T) {
			hosts, err := hostsInCIDR(tt.cidr)
			if err != nil {
				t.Fatalf("hostsInCIDR(%q) error: %v", tt.cidr, err)
			}
			if len(hosts) != tt.wantCount {
				t.Fatalf("got %d hosts, want %d", len(hosts), tt.wantCount)
			}
			if hosts[0] != tt.spotFirst || hosts[len(hosts)-1] != tt.spotLast {
				t.Errorf("range = [%s .. %s], want [%s .. %s]", hosts[0], hosts[len(hosts)-1], tt.spotFirst, tt.spotLast)
			}
		})
	}
}

func TestHostsInCIDR_Errors(t *testing.T) {
	for _, cidr := range []string{"not-a-cidr", "2001:db8::/64", "10.0.0.0/8"} {
		if _, err := hostsInCIDR(cidr); err == nil {
			t.Errorf("hostsInCIDR(%q) = nil error, want error", cidr)
		}
	}
}

func TestScanHosts_ReturnsHitsInOrder(t *testing.T) {
	hosts := []string{"10.0.0.1", "10.0.0.2", "10.0.0.3", "10.0.0.4"}
	// Probe answers "camera" only for .2 and .4.
	probe := func(_ context.Context, addr string, _ time.Duration) bool {
		return strings.HasPrefix(addr, "10.0.0.2:") || strings.HasPrefix(addr, "10.0.0.4:")
	}
	got := scanHosts(context.Background(), hosts, 6123, time.Second, 8, probe)
	want := []string{"10.0.0.2", "10.0.0.4"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("scanHosts = %v, want %v", got, want)
	}
}

// serveHandshakeEcho plays just the devId-free part of the handshake: read one
// frame, echo a type-115 frame. That is what probeCamera checks.
func serveHandshakeEcho(t *testing.T) string {
	t.Helper()
	return serveMockCamera(t, func(conn net.Conn) {
		if _, err := readStreamFrame(conn); err != nil {
			return
		}
		_ = writeStreamFrame(conn, domain.Frame{Type: domain.FrameTypeHandshakeTrigger})
	})
}

func TestProbeCamera(t *testing.T) {
	// A camera that echoes the handshake trigger is detected.
	if !probeCamera(context.Background(), serveHandshakeEcho(t), time.Second) {
		t.Error("probeCamera did not detect a handshake-echoing camera")
	}

	// A listener that accepts then closes without echoing is not a camera.
	silent := serveMockCamera(t, func(conn net.Conn) { _ = conn.Close() })
	if probeCamera(context.Background(), silent, 500*time.Millisecond) {
		t.Error("probeCamera falsely detected a silent listener")
	}

	// A closed port is not a camera.
	if probeCamera(context.Background(), "127.0.0.1:1", 500*time.Millisecond) {
		t.Error("probeCamera falsely detected a closed port")
	}
}

// End-to-end: the scan command finds a loopback mock camera addressed via a /32.
func TestRun_ScanFindsCamera(t *testing.T) {
	addr := serveHandshakeEcho(t)
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split addr: %v", err)
	}

	app, out, errb := newTestApp()
	code := app.Run(context.Background(), []string{
		"scan", "--subnet", host + "/32", "--port", portStr, "--timeout", "2s",
	})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %q)", code, errb.String())
	}
	if !strings.Contains(out.String(), "camera found: "+net.JoinHostPort(host, portStr)) {
		t.Fatalf("stdout = %q, want a 'camera found' line", out.String())
	}
}

func TestRun_ScanNoneFound(t *testing.T) {
	app, out, _ := newTestApp()
	// A /32 pointed at a closed loopback port finds nothing but is not an error.
	code := app.Run(context.Background(), []string{
		"scan", "--subnet", "127.0.0.1/32", "--port", strconv.Itoa(1), "--timeout", "300ms",
	})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(out.String(), "no cameras found") {
		t.Fatalf("stdout = %q, want 'no cameras found'", out.String())
	}
}

func TestRun_ScanBadSubnetExits2(t *testing.T) {
	app, _, errb := newTestApp()
	if code := app.Run(context.Background(), []string{"scan", "--subnet", "nonsense"}); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(errb.String(), "invalid subnet") {
		t.Fatalf("stderr = %q", errb.String())
	}
}

func TestRun_ScanUsageErrorsExit2(t *testing.T) {
	cases := [][]string{
		{"scan", "--subnet", "127.0.0.1/32", "--concurrency", "0"}, // concurrency < 1
		{"scan", "--subnet", "127.0.0.1/32", "extra-arg"},          // trailing args
	}
	for _, args := range cases {
		app, _, _ := newTestApp()
		if code := app.Run(context.Background(), args); code != 2 {
			t.Errorf("%v: exit code = %d, want 2", args, code)
		}
	}
}
