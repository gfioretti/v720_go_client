package cli

import (
	"context"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"

	"v720_go_client/internal/application"
	"v720_go_client/internal/domain"
)

// fakeCloud is a scriptable CloudAPI for the relay CLI tests. It returns a
// LiveConf pointing at a mock coordinator whose host:port the test supplies.
type fakeCloud struct {
	conf     domain.LiveConf
	loginErr error
}

func (f *fakeCloud) Login(context.Context, string, string) (domain.Account, error) {
	return domain.Account{UID: "user-1"}, f.loginErr
}
func (f *fakeCloud) ListDevices(context.Context) ([]domain.Device, error) { return nil, nil }
func (f *fakeCloud) Watching(context.Context, string, string) error       { return nil }
func (f *fakeCloud) GetA9VideoConf(context.Context, string) (domain.LiveConf, error) {
	return f.conf, nil
}

// serveMockCoordinator plays the device side of the relay phase-1 coordinator
// handshake (doc §12.2 phase 1) for one connection, replying to registration with
// regStatus, and returns the listen address.
func serveMockCoordinator(t *testing.T, regStatus int) string {
	t.Helper()
	return serveMockCamera(t, func(conn net.Conn) {
		read := func() bool {
			_, err := readStreamFrame(conn)
			return err == nil
		}
		// bare frame, then register (100).
		if !read() || !read() {
			return
		}
		_ = writeStreamFrame(conn, jsonControl(map[string]any{
			"code": 13, "status": 200,
			"devIp": "1.1.1.1", "devPort": 8000, "devNatIp": "2.2.2.2", "devNatPort": 9000,
		}))
		// reqForwardId (30) → 31.
		if !read() {
			return
		}
		_ = writeStreamFrame(conn, jsonControl(map[string]any{"code": 31, "status": 200, "forwardId": "live-fwd"}))
		// reqPlaybackId (36) → 37.
		if !read() {
			return
		}
		_ = writeStreamFrame(conn, jsonControl(map[string]any{"code": 37, "status": 200, "forwardId": "pb-fwd"}))
		// reqConnect (10) and reqSendDeviceStatus (52), then registration status.
		if !read() || !read() {
			return
		}
		_ = writeStreamFrame(conn, jsonControl(map[string]any{"code": 101, "status": regStatus}))
		_, _ = io.Copy(io.Discard, conn)
	})
}

// confFor turns a host:port listen address into the LiveConf the fake cloud hands
// back, so the CLI dials the mock coordinator.
func confFor(t *testing.T, addr string) domain.LiveConf {
	t.Helper()
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split addr %q: %v", addr, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("port %q: %v", portStr, err)
	}
	return domain.LiveConf{Host: host, TCPPort: port, Pwd: "p", TarPwd: "tp", Domain: "d"}
}

// End-to-end: the real CLI drives a fake cloud + the real framedtcp coordinator
// against a mock device, completing relay phase-1 registration.
func TestRun_RelayConnectSucceeds(t *testing.T) {
	addr := serveMockCoordinator(t, 200)
	app, out, errb := newTestApp()
	app.Cloud = func(string) application.CloudAPI { return &fakeCloud{conf: confFor(t, addr)} }

	code := app.Run(context.Background(), []string{
		"relay", "connect", "--user", "alice", "--pass", "pw", "--dev", "dev123", "--timeout", "2s",
	})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %q)", code, errb.String())
	}
	if !strings.Contains(out.String(), "live-fwd") || !strings.Contains(out.String(), "pb-fwd") {
		t.Fatalf("stdout missing forward IDs: %q", out.String())
	}
}

// A coordinator that rejects registration (101/500) surfaces as exit 1.
func TestRun_RelayConnectHandshakeRejectedExits1(t *testing.T) {
	addr := serveMockCoordinator(t, 500)
	app, _, errb := newTestApp()
	app.Cloud = func(string) application.CloudAPI { return &fakeCloud{conf: confFor(t, addr)} }

	code := app.Run(context.Background(), []string{
		"relay", "connect", "--user", "a", "--pass", "b", "--dev", "d", "--timeout", "2s",
	})
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errb.String(), "handshake failed") {
		t.Fatalf("stderr = %q", errb.String())
	}
}

// A cloud login failure surfaces as exit 1 before any dialing.
func TestRun_RelayConnectLoginFailureExits1(t *testing.T) {
	app, _, errb := newTestApp()
	app.Cloud = func(string) application.CloudAPI {
		return &fakeCloud{loginErr: errors.New("bad credentials")}
	}
	code := app.Run(context.Background(), []string{
		"relay", "connect", "--user", "a", "--pass", "b", "--dev", "d", "--timeout", "2s",
	})
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errb.String(), "login") {
		t.Fatalf("stderr = %q", errb.String())
	}
}

func TestRun_RelayConnectMissingFlagsExits2(t *testing.T) {
	app, _, errb := newTestApp()
	code := app.Run(context.Background(), []string{"relay", "connect", "--user", "a"})
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(errb.String(), "required") {
		t.Fatalf("stderr = %q", errb.String())
	}
}

func TestRun_RelayUnknownSubcommandExits2(t *testing.T) {
	app, _, errb := newTestApp()
	if code := app.Run(context.Background(), []string{"relay", "frobnicate"}); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(errb.String(), "unknown subcommand") {
		t.Fatalf("stderr = %q", errb.String())
	}
}

func TestRun_RelayWithoutSubcommandExits2(t *testing.T) {
	app, _, errb := newTestApp()
	if code := app.Run(context.Background(), []string{"relay"}); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(errb.String(), "expected a subcommand") {
		t.Fatalf("stderr = %q", errb.String())
	}
}
