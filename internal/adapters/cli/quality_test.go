package cli

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"v720_go_client/internal/domain"
)

// startMockCameraCapturingAll handshakes, then records every frame the client
// sends afterward while keeping the connection open, so a streaming command can
// run to completion.
func startMockCameraCapturingAll(t *testing.T, devID string) (string, <-chan domain.Frame) {
	t.Helper()
	received := make(chan domain.Frame, 32)
	addr := serveMockCamera(t, func(conn net.Conn) {
		if !mockHandshake(conn, devID, 200) {
			return
		}
		for {
			f, err := readStreamFrame(conn)
			if err != nil {
				return
			}
			select {
			case received <- f:
			default:
			}
		}
	})
	return addr, received
}

// --quality 2 must send the HD grade (502-wrapped code 201, speedGrade 2) before
// the stream-start command.
func TestRun_LiveQualitySendsHDGrade(t *testing.T) {
	const devID = "dev123"
	addr, received := startMockCameraCapturingAll(t, devID)

	app, _, errb := newTestApp()
	code := app.Run(context.Background(), []string{
		"ap", "live", "--addr", addr, "--dev", devID,
		"--connect-timeout", "2s", "--duration", "200ms", "--quality", "2",
	})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %q)", code, errb.String())
	}

	if !waitForQualityCommand(t, received, 201, 2) {
		t.Fatal("device never received the 502/201 speedGrade=2 command")
	}
}

// --quality 0 (the default) must NOT send any quality command.
func TestRun_LiveNoQualityByDefault(t *testing.T) {
	const devID = "dev123"
	addr, received := startMockCameraCapturingAll(t, devID)

	app, _, errb := newTestApp()
	code := app.Run(context.Background(), []string{
		"ap", "live", "--addr", addr, "--dev", devID,
		"--connect-timeout", "2s", "--duration", "200ms",
	})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %q)", code, errb.String())
	}

	// Drain briefly; no frame should be a 201 command.
	deadline := time.After(300 * time.Millisecond)
	for {
		select {
		case f := <-received:
			if isQualityCommand(f, 201, 2) {
				t.Fatal("sent a quality command despite --quality 0")
			}
		case <-deadline:
			return
		}
	}
}

func waitForQualityCommand(t *testing.T, received <-chan domain.Frame, wantInner, wantGrade int) bool {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case f := <-received:
			if isQualityCommand(f, wantInner, wantGrade) {
				return true
			}
		case <-deadline:
			return false
		}
	}
}

// isQualityCommand reports whether f is {"code":502,"content":{"code":<inner>,
// "speedGrade":<grade>,...}}.
func isQualityCommand(f domain.Frame, inner, grade int) bool {
	if f.Type != domain.FrameTypeJSON {
		return false
	}
	var m struct {
		Code    int `json:"code"`
		Content struct {
			Code       int `json:"code"`
			SpeedGrade int `json:"speedGrade"`
		} `json:"content"`
	}
	if err := json.Unmarshal(f.Payload, &m); err != nil {
		return false
	}
	return m.Code == 502 && m.Content.Code == inner && m.Content.SpeedGrade == grade
}
