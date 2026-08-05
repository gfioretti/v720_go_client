package cli

import (
	"bytes"
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"v720_go_client/internal/domain"
)

func TestRun_SetMissingArgs(t *testing.T) {
	app, _, errb := newTestApp()
	// Flags present but no <setting> <value>.
	code := app.Run(context.Background(), []string{"ap", "set", "--addr", "127.0.0.1:9", "--dev", "d"})
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(errb.String(), "expected <setting> <value>") {
		t.Fatalf("stderr = %q", errb.String())
	}
}

func TestRun_SetUnknownSetting(t *testing.T) {
	app, _, errb := newTestApp()
	code := app.Run(context.Background(), []string{"ap", "set", "--addr", "127.0.0.1:9", "--dev", "d", "frobnicate", "1"})
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(errb.String(), "unknown setting") {
		t.Fatalf("stderr = %q", errb.String())
	}
}

func TestRun_SetNonIntegerValue(t *testing.T) {
	app, _, errb := newTestApp()
	code := app.Run(context.Background(), []string{"ap", "set", "--addr", "127.0.0.1:9", "--dev", "d", "quality", "high"})
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(errb.String(), "must be an integer") {
		t.Fatalf("stderr = %q", errb.String())
	}
}

// End-to-end: `ap set quality 2` connects and delivers the command to the mock
// device in exactly the AP-mode wire form (502/content + devTarget).
func TestRun_SetSendsCommandInWireForm(t *testing.T) {
	const devID = "dev123"
	addr, received := startMockCameraCapturingCommand(t, devID)

	app, out, errb := newTestApp()
	code := app.Run(context.Background(), []string{
		"ap", "set", "--addr", addr, "--dev", devID, "quality", "2",
	})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %q)", code, errb.String())
	}
	if !strings.Contains(out.String(), "sent quality=2 to "+devID) {
		t.Fatalf("stdout = %q", out.String())
	}

	var got domain.Frame
	select {
	case got = <-received:
	case <-time.After(2 * time.Second):
		t.Fatal("device never received the command")
	}
	if got.Type != domain.FrameTypeJSON {
		t.Fatalf("received frame type = %s, want JSON", got.Type)
	}
	want, err := domain.NewAPDeviceCommand(devID, domain.NewVideoQuality(2)).JSON()
	if err != nil {
		t.Fatalf("build expected: %v", err)
	}
	if !bytes.Equal(got.Payload, want) {
		t.Fatalf("device received %s, want %s", got.Payload, want)
	}
}

func TestRun_SetMissingRequiredFlags(t *testing.T) {
	app, _, errb := newTestApp()
	// --dev omitted.
	code := app.Run(context.Background(), []string{"ap", "set", "--addr", "127.0.0.1:9", "quality", "2"})
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(errb.String(), "required") {
		t.Fatalf("stderr = %q", errb.String())
	}
}

// startMockCameraCapturingCommand does the handshake, then captures the first
// frame the client sends afterward (the device-control command) onto the
// returned channel.
func startMockCameraCapturingCommand(t *testing.T, devID string) (string, <-chan domain.Frame) {
	t.Helper()
	received := make(chan domain.Frame, 1)
	addr := serveMockCamera(t, func(conn net.Conn) {
		if !mockHandshake(conn, devID, 200) {
			return
		}
		f, err := readStreamFrame(conn)
		if err != nil {
			return
		}
		received <- f
	})
	return addr, received
}
