package cli

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"v720_go_client/internal/domain"
)

func newTestApp() (*App, *bytes.Buffer, *bytes.Buffer) {
	var out, errb bytes.Buffer
	app := NewApp(&out, &errb)
	return app, &out, &errb
}

func TestRun_NoArgsShowsUsage(t *testing.T) {
	app, _, errb := newTestApp()
	if code := app.Run(context.Background(), nil); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(errb.String(), "Usage:") {
		t.Fatalf("stderr missing usage: %q", errb.String())
	}
}

func TestRun_UnknownCommand(t *testing.T) {
	app, _, errb := newTestApp()
	if code := app.Run(context.Background(), []string{"frobnicate"}); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(errb.String(), "unknown command") {
		t.Fatalf("stderr = %q", errb.String())
	}
}

func TestRun_Help(t *testing.T) {
	app, out, _ := newTestApp()
	if code := app.Run(context.Background(), []string{"help"}); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(out.String(), "ap connect") {
		t.Fatalf("stdout = %q", out.String())
	}
}

func TestRun_ApWithoutSubcommand(t *testing.T) {
	app, _, errb := newTestApp()
	if code := app.Run(context.Background(), []string{"ap"}); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(errb.String(), "expected a subcommand") {
		t.Fatalf("stderr = %q", errb.String())
	}
}

func TestRun_ConnectMissingRequiredFlags(t *testing.T) {
	app, _, errb := newTestApp()
	// --dev omitted.
	code := app.Run(context.Background(), []string{"ap", "connect", "--addr", "127.0.0.1:9"})
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(errb.String(), "required") {
		t.Fatalf("stderr = %q", errb.String())
	}
}

func TestRun_ConnectDialFailureExits1(t *testing.T) {
	app, _, errb := newTestApp()
	// Port 1 on loopback refuses; a short timeout keeps the test quick.
	code := app.Run(context.Background(), []string{
		"ap", "connect", "--addr", "127.0.0.1:1", "--dev", "d", "--timeout", "500ms",
	})
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errb.String(), "connect") {
		t.Fatalf("stderr = %q", errb.String())
	}
}

func TestRun_ConnectUnknownFlagExits2(t *testing.T) {
	app, _, errb := newTestApp()
	code := app.Run(context.Background(), []string{
		"ap", "connect", "--addr", "127.0.0.1:9", "--dev", "d", "--bogus",
	})
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(errb.String(), "not defined") {
		t.Fatalf("stderr = %q", errb.String())
	}
}

// End-to-end: the real CLI, over the real aptcp adapter, completes the AP-mode
// handshake against a mock device — exercising the whole vertical slice.
func TestRun_ConnectHandshakeSucceeds(t *testing.T) {
	const devID = "dev123"
	addr := startMockCamera(t, devID, 200)

	app, out, errb := newTestApp()
	code := app.Run(context.Background(), []string{
		"ap", "connect", "--addr", addr, "--dev", devID, "--password", "s3cret", "--timeout", "2s",
	})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %q)", code, errb.String())
	}
	if !strings.Contains(out.String(), "connected to "+devID) {
		t.Fatalf("stdout = %q", out.String())
	}
}

// A device that rejects the 501 registration must surface as a runtime failure
// (exit 1), not a usage error.
func TestRun_ConnectHandshakeRejectedExits1(t *testing.T) {
	const devID = "dev123"
	addr := startMockCamera(t, devID, 403)

	app, _, errb := newTestApp()
	code := app.Run(context.Background(), []string{
		"ap", "connect", "--addr", addr, "--dev", devID, "--timeout", "2s",
	})
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errb.String(), "handshake failed") {
		t.Fatalf("stderr = %q", errb.String())
	}
}

// --- mock camera + stream frame codec (test-only) ---

// mockHandshake plays the device side of the AP-mode handshake (doc §12.3) on
// conn, replying to the 501 registration with registerStatus. It reports whether
// the handshake completed successfully (registerStatus 200 and no I/O error).
func mockHandshake(conn net.Conn, devID string, registerStatus int) bool {
	// step 3: echo the type-115 trigger.
	if _, err := readStreamFrame(conn); err != nil {
		return false
	}
	if err := writeStreamFrame(conn, domain.Frame{Type: domain.FrameTypeHandshakeTrigger}); err != nil {
		return false
	}
	// step 5: respond to the 501 registration.
	if _, err := readStreamFrame(conn); err != nil {
		return false
	}
	_ = writeStreamFrame(conn, jsonControl(map[string]any{"code": 501, "status": registerStatus, "target": devID}))
	if registerStatus != 200 {
		return false // client aborts; nothing more to exchange.
	}
	// step 6: consume the time-sync (code 4), no reply.
	if _, err := readStreamFrame(conn); err != nil {
		return false
	}
	// step 7: confirm the 500 authorization.
	if _, err := readStreamFrame(conn); err != nil {
		return false
	}
	_ = writeStreamFrame(conn, jsonControl(map[string]any{"code": 500, "status": 200}))
	return true
}

// startMockCamera listens on loopback and plays the device side of the handshake
// for one connection, returning the listen address.
func startMockCamera(t *testing.T, devID string, registerStatus int) string {
	t.Helper()
	return serveMockCamera(t, func(conn net.Conn) {
		if !mockHandshake(conn, devID, registerStatus) {
			return
		}
		// Keep the conn open until the client hangs up so late reads aren't cut off.
		_, _ = io.Copy(io.Discard, conn)
	})
}

// startMockCameraStreaming does the handshake, then — since AP-mode live view
// auto-starts and the client sends no start command — streams the given media
// frames immediately, then drains until the client hangs up.
func startMockCameraStreaming(t *testing.T, devID string, media []domain.Frame) string {
	t.Helper()
	return serveMockCamera(t, func(conn net.Conn) {
		if !mockHandshake(conn, devID, 200) {
			return
		}
		// AP-mode live view auto-starts after the handshake — the client sends
		// no start command — so stream the media frames straight away.
		for _, f := range media {
			if err := writeStreamFrame(conn, f); err != nil {
				return
			}
		}
		_, _ = io.Copy(io.Discard, conn)
	})
}

// serveMockCamera starts a one-shot loopback listener and runs handle on the
// accepted connection, returning the listen address.
func serveMockCamera(t *testing.T, handle func(net.Conn)) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		handle(conn)
	}()

	return ln.Addr().String()
}

// drain reads and discards from conn until the client hangs up, keeping the
// connection open so the client's final reads aren't cut short.
func drain(conn net.Conn) {
	_, _ = io.Copy(io.Discard, conn)
}

func jsonControl(m map[string]any) domain.Frame {
	b, _ := json.Marshal(m)
	return domain.Frame{Type: domain.FrameTypeJSON, Payload: b}
}

func writeStreamFrame(conn net.Conn, f domain.Frame) error {
	_, err := conn.Write(f.Encode())
	return err
}

func readStreamFrame(conn net.Conn) (domain.Frame, error) {
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	h := make([]byte, domain.HeaderSize)
	if _, err := io.ReadFull(conn, h); err != nil {
		return domain.Frame{}, err
	}
	n := binary.LittleEndian.Uint32(h[:4])
	buf := make([]byte, domain.HeaderSize+int(n))
	copy(buf, h)
	if _, err := io.ReadFull(conn, buf[domain.HeaderSize:]); err != nil {
		return domain.Frame{}, err
	}
	return domain.Decode(buf)
}
