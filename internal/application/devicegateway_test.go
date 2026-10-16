package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gfioretti/v720_go_client/internal/domain"
)

const gatewayTarget = "00112233445566778899aabbccddeeff"

func newTestGateway() *DeviceGateway {
	g := NewDeviceGateway(gatewayTarget)
	g.now = func() time.Time { return time.Unix(1700000000, 0) }
	return g
}

func TestDeviceGateway_Bringup_HappyPath(t *testing.T) {
	ch := newFakeTransport()
	// The camera registers, then (after the base-info request) replies with its
	// base-info snapshot.
	ch.push(jsonControlFrame(t, map[string]any{"code": 100, "uid": "cam-uid", "token": "cam-tok"}))
	ch.push(jsonControlFrame(t, map[string]any{
		"code": 301, "target": gatewayTarget, "content": map[string]any{"code": 4, "devPower": 88},
	}))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	reg, err := newTestGateway().Bringup(ctx, ch)
	if err != nil {
		t.Fatalf("Bringup: %v", err)
	}
	if reg.UID != "cam-uid" || reg.Token != "cam-tok" {
		t.Fatalf("registration = %+v, want {cam-uid cam-tok}", reg)
	}

	sent := ch.sentFrames()
	if len(sent) != 4 {
		t.Fatalf("sent %d frames, want 4: %+v", len(sent), sent)
	}
	// Frame 0 — register ack.
	if ack := decodeControl(t, sent[0]); ack["code"] != float64(101) || ack["status"] != float64(200) {
		t.Errorf("register ack = %v", ack)
	}
	// Frame 1 — device status online.
	if ds := decodeControl(t, sent[1]); ds["code"] != float64(53) || ds["status"] != float64(1) {
		t.Errorf("device status = %v", ds)
	}
	// Frame 2 — base-info request, 301-wrapped code 4.
	bi := decodeControl(t, sent[2])
	biContent, _ := bi["content"].(map[string]any)
	if bi["code"] != float64(301) || bi["target"] != gatewayTarget ||
		biContent["code"] != float64(4) || biContent["unixTimer"] != float64(1700000000) {
		t.Errorf("base-info request = %v", bi)
	}
	// Frame 3 — open video, 301-wrapped code 3 (StreamStart).
	ov := decodeControl(t, sent[3])
	ovContent, _ := ov["content"].(map[string]any)
	if ov["code"] != float64(301) || ov["target"] != gatewayTarget || ovContent["code"] != float64(3) {
		t.Errorf("open video = %v", ov)
	}
}

// A heartbeat (non-JSON) and an unrelated JSON frame before the register are
// skipped while awaiting registration.
func TestDeviceGateway_Bringup_SkipsNoiseBeforeRegister(t *testing.T) {
	ch := newFakeTransport()
	ch.push(domain.Frame{Type: domain.FrameTypeNoop}) // heartbeat
	ch.push(jsonControlFrame(t, map[string]any{"code": 999}))
	ch.push(jsonControlFrame(t, map[string]any{"code": 100, "uid": "u", "token": "t"}))
	ch.push(jsonControlFrame(t, map[string]any{"code": 301, "content": map[string]any{"code": 4}}))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	reg, err := newTestGateway().Bringup(ctx, ch)
	if err != nil {
		t.Fatalf("Bringup: %v", err)
	}
	if reg.UID != "u" {
		t.Fatalf("registration UID = %q, want u", reg.UID)
	}
}

// A heartbeat (non-JSON) arriving between the base-info request and the reply is
// skipped, not mistaken for the reply — otherwise open-video would be sent
// prematurely and the FSM would desync by one frame.
func TestDeviceGateway_Bringup_SkipsHeartbeatBeforeBaseInfoReply(t *testing.T) {
	ch := newFakeTransport()
	ch.push(jsonControlFrame(t, map[string]any{"code": 100, "uid": "u", "token": "t"}))
	ch.push(domain.Frame{Type: domain.FrameTypeNoop}) // heartbeat between request and reply
	ch.push(jsonControlFrame(t, map[string]any{"code": 301, "content": map[string]any{"code": 4}}))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := newTestGateway().Bringup(ctx, ch); err != nil {
		t.Fatalf("Bringup: %v", err)
	}
	// The open-video (301/code 3) must still be the last frame sent.
	sent := ch.sentFrames()
	if len(sent) != 4 {
		t.Fatalf("sent %d frames, want 4", len(sent))
	}
	ov := decodeControl(t, sent[3])
	ovContent, _ := ov["content"].(map[string]any)
	if ov["code"] != float64(301) || ovContent["code"] != float64(3) {
		t.Fatalf("last frame = %v, want open-video 301/3", ov)
	}
}

// A dropped connection before the camera registers propagates the transport error.
func TestDeviceGateway_Bringup_TransportClosed(t *testing.T) {
	ch := newFakeTransport()
	ch.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := newTestGateway().Bringup(ctx, ch); !errors.Is(err, ErrTransportClosed) {
		t.Fatalf("Bringup = %v, want ErrTransportClosed", err)
	}
}
