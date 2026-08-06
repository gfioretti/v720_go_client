package cli

import (
	"context"
	"strings"
	"testing"

	"v720_go_client/internal/domain"
)

func TestFrameDetail(t *testing.T) {
	// JSON control frames show their (truncated) body.
	json := domain.Frame{Type: domain.FrameTypeJSON, Payload: []byte(`{"code":500}`)}
	if got := frameDetail(json); got != ` {"code":500}` {
		t.Errorf("frameDetail(JSON) = %q", got)
	}
	// Binary/media frames show nothing (payload not dumped).
	if got := frameDetail(domain.Frame{Type: domain.FrameTypeMJPEG, Payload: []byte("\xff\xd8jpeg")}); got != "" {
		t.Errorf("frameDetail(MJPEG) = %q, want empty", got)
	}
	// Long JSON is truncated.
	long := domain.Frame{Type: domain.FrameTypeJSON, Payload: []byte("{" + strings.Repeat("a", debugPayloadMax+50) + "}")}
	if got := frameDetail(long); !strings.Contains(got, "...(truncated)") {
		t.Errorf("frameDetail(long) not truncated: %q", got)
	}
}

// End-to-end: --debug logs the handshake frames (sent and received) to stderr.
func TestRun_ConnectDebugLogsFrames(t *testing.T) {
	const devID = "dev123"
	addr := startMockCamera(t, devID, 200)

	app, _, errb := newTestApp()
	code := app.Run(context.Background(), []string{
		"ap", "connect", "--debug", "--addr", addr, "--dev", devID, "--timeout", "2s",
	})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %q)", code, errb.String())
	}

	logs := errb.String()
	// The empty type-115 trigger is sent, and the 501 register JSON body appears.
	for _, want := range []string{
		"-> SEND HandshakeTrigger",
		`"token":"NaxclowToken"`,
		"<- RECV",
	} {
		if !strings.Contains(logs, want) {
			t.Fatalf("debug log missing %q:\n%s", want, logs)
		}
	}
}

// Without --debug, no frame logging appears.
func TestRun_ConnectNoDebugByDefault(t *testing.T) {
	const devID = "dev123"
	addr := startMockCamera(t, devID, 200)

	app, _, errb := newTestApp()
	if code := app.Run(context.Background(), []string{
		"ap", "connect", "--addr", addr, "--dev", devID, "--timeout", "2s",
	}); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if strings.Contains(errb.String(), "SEND") || strings.Contains(errb.String(), "RECV") {
		t.Fatalf("unexpected debug output without --debug: %q", errb.String())
	}
}
