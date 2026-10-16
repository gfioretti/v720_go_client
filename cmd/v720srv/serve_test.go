package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/gfioretti/v720_go_client/internal/adapters/transport/framedtcp"
	"github.com/gfioretti/v720_go_client/internal/application"
	"github.com/gfioretti/v720_go_client/internal/domain"
)

// discardf is a no-op logger for tests: the serveCameras/runMedia goroutines
// outlive the test, so passing t.Logf would panic ("Log after test completed")
// on a late log call. Production wiring uses log.Printf, which is always safe.
func discardf(string, ...any) {}

func jsonFrame(t *testing.T, m map[string]any) domain.Frame {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return domain.Frame{Type: domain.FrameTypeJSON, Payload: b}
}

func decode(t *testing.T, f domain.Frame) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(f.Payload, &m); err != nil {
		t.Fatalf("unmarshal %q: %v", f.Payload, err)
	}
	return m
}

// End-to-end over loopback: a camera connects to the coordinator, and
// serveCameras drives it through bring-up to the start-stream command.
func TestServeCameras_BringsUpConnectingCamera(t *testing.T) {
	ln, err := framedtcp.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	registered := make(chan application.Registration, 1)
	go serveCameras(ctx, ln, application.NewDeviceGateway("tgt"), false, discardf, func(r application.Registration) {
		registered <- r
	})

	// Play the camera side.
	cam, err := framedtcp.Dial(ctx, ln.Addr().String())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = cam.Close() }()

	if err := cam.Send(ctx, jsonFrame(t, map[string]any{"code": 100, "uid": "cam-uid", "token": "tok"})); err != nil {
		t.Fatalf("camera send register: %v", err)
	}
	// Read the server's register-ack, device-status, base-info request.
	for i := 0; i < 3; i++ {
		if _, err := cam.Receive(ctx); err != nil {
			t.Fatalf("camera receive %d: %v", i, err)
		}
	}
	// Reply to base-info, then read the open-video command.
	if err := cam.Send(ctx, jsonFrame(t, map[string]any{"code": 301, "content": map[string]any{"code": 4}})); err != nil {
		t.Fatalf("camera send base-info reply: %v", err)
	}
	ov, err := cam.Receive(ctx)
	if err != nil {
		t.Fatalf("camera receive open-video: %v", err)
	}
	m := decode(t, ov)
	content, _ := m["content"].(map[string]any)
	if m["code"] != float64(301) || content["code"] != float64(3) {
		t.Fatalf("expected open-video 301/3, got %v", m)
	}

	select {
	case reg := <-registered:
		if reg.UID != "cam-uid" {
			t.Fatalf("registered UID = %q, want cam-uid", reg.UID)
		}
	case <-time.After(time.Second):
		t.Fatal("onRegistered never fired")
	}
}
