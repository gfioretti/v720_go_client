package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gfioretti/v720_go_client/internal/domain"
)

// End-to-end: `ap set-wifi` connects over the hotspot and delivers the code-204
// provisioning command in the exact AP wire form (502/content + devTarget).
func TestRun_SetWifiSendsCommand(t *testing.T) {
	const devID = "dev123"
	addr, received := startMockCameraCapturingCommand(t, devID)

	app, out, errb := newTestApp()
	code := app.Run(context.Background(), []string{
		"ap", "set-wifi", "--addr", addr, "--dev", devID, "--ssid", "HomeNet", "--pass", "secret12",
	})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %q)", code, errb.String())
	}
	if !strings.Contains(out.String(), `SSID "HomeNet"`) {
		t.Fatalf("stdout = %q", out.String())
	}

	var got domain.Frame
	select {
	case got = <-received:
	case <-time.After(2 * time.Second):
		t.Fatal("device never received the set-wifi command")
	}
	want, err := domain.NewAPDeviceCommand(devID, domain.NewSetWifi("HomeNet", "secret12")).JSON()
	if err != nil {
		t.Fatalf("build expected: %v", err)
	}
	if got.Type != domain.FrameTypeJSON || !bytes.Equal(got.Payload, want) {
		t.Fatalf("device received %s (type %s), want %s", got.Payload, got.Type, want)
	}
}

func TestRun_SetWifiMissingRequiredFlags(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"no ssid", []string{"ap", "set-wifi", "--dev", "d", "--pass", "secret12"}},
		{"no dev", []string{"ap", "set-wifi", "--ssid", "HomeNet", "--pass", "secret12"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app, _, errb := newTestApp()
			if code := app.Run(context.Background(), tc.args); code != 2 {
				t.Fatalf("exit code = %d, want 2", code)
			}
			if !strings.Contains(errb.String(), "required") {
				t.Fatalf("stderr = %q", errb.String())
			}
		})
	}
}

// A WPA password of an invalid length is rejected client-side (exit 2) rather
// than silently sent — important because set-wifi is fire-and-forget.
func TestRun_SetWifiRejectsBadPasswordLength(t *testing.T) {
	app, _, errb := newTestApp()
	code := app.Run(context.Background(), []string{
		"ap", "set-wifi", "--dev", "d", "--ssid", "HomeNet", "--pass", "short",
	})
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(errb.String(), "8-63") {
		t.Fatalf("stderr = %q", errb.String())
	}
}
