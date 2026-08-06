package application

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"v720_go_client/internal/domain"
)

const (
	testDevID    = "dev123"
	testPassword = "s3cret"
)

// jsonControlFrame builds an inbound FrameTypeJSON frame carrying the given
// control message, mimicking what the device sends back during the handshake.
func jsonControlFrame(t *testing.T, m map[string]any) domain.Frame {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal control frame: %v", err)
	}
	return domain.Frame{Type: domain.FrameTypeJSON, Payload: b}
}

// decodeControl parses an outbound JSON frame's payload into a generic map so a
// test can assert on the fields the client sent.
func decodeControl(t *testing.T, f domain.Frame) map[string]any {
	t.Helper()
	if f.Type != domain.FrameTypeJSON {
		t.Fatalf("frame type = %s, want JSON", f.Type)
	}
	var m map[string]any
	if err := json.Unmarshal(f.Payload, &m); err != nil {
		t.Fatalf("unmarshal payload %q: %v", f.Payload, err)
	}
	return m
}

// happyPathResponses pre-loads the device's successful handshake replies, in the
// order the client expects to receive them (doc §12.3): the type-115 echo, then
// the 501 registration confirmation. The 500 authorize is fire-and-forget — the
// device is not expected to (and a real one does not) reply to it — so there is
// no auth response to pre-load.
func happyPathResponses(t *testing.T, ft *fakeTransport) {
	ft.push(domain.Frame{Type: domain.FrameTypeHandshakeTrigger})
	ft.push(jsonControlFrame(t, map[string]any{"code": 501, "status": 200, "target": testDevID}))
}

func TestAPSession_Connect_HappyPath(t *testing.T) {
	ft := newFakeTransport()
	s := NewAPSession(ft, testDevID, testPassword)
	fixed := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) // offset 0 → unixTimer == Unix()
	s.now = func() time.Time { return fixed }
	happyPathResponses(t, ft)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.Connect(ctx); err != nil {
		t.Fatalf("Connect() error: %v", err)
	}

	sent := ft.sentFrames()
	if len(sent) != 4 {
		t.Fatalf("sent %d frames, want 4: %+v", len(sent), sent)
	}

	// Frame 0 — bare type-115 handshake trigger, no body (doc §12.3 step 2).
	if sent[0].Type != domain.FrameTypeHandshakeTrigger {
		t.Errorf("frame 0 type = %s, want HandshakeTrigger", sent[0].Type)
	}
	if len(sent[0].Payload) != 0 {
		t.Errorf("frame 0 payload = %q, want empty", sent[0].Payload)
	}

	// Frame 1 — 501 register with the hardcoded NaxclowToken (doc §12.3 step 4).
	reg := decodeControl(t, sent[1])
	if reg["code"] != float64(501) || reg["target"] != testDevID ||
		reg["token"] != "NaxclowToken" || reg["unixTimer"] != float64(fixed.Unix()) {
		t.Errorf("register frame = %v", reg)
	}

	// Frame 2 — time-sync code 4 (doc §12.3 step 6).
	sync := decodeControl(t, sent[2])
	if sync["code"] != float64(4) || sync["devTarget"] != testDevID ||
		sync["unixTimer"] != float64(fixed.Unix()) {
		t.Errorf("time-sync frame = %v", sync)
	}

	// Frame 3 — 500 authorize with the password in the "sk" field, sent
	// best-effort because a password is configured (doc §12.3 step 7).
	auth := decodeControl(t, sent[3])
	if auth["code"] != float64(500) || auth["sk"] != testPassword {
		t.Errorf("authorize frame = %v", auth)
	}
}

// A camera with no password (e.g. freshly reset): Connect must NOT send the 500
// authorize (a real no-password camera never answers it) and must succeed on the
// 115→501→time-sync handshake alone.
func TestAPSession_Connect_NoPasswordSkipsAuthorize(t *testing.T) {
	ft := newFakeTransport()
	s := NewAPSession(ft, testDevID, "") // no password
	s.now = func() time.Time { return time.Unix(0, 0).UTC() }
	ft.push(domain.Frame{Type: domain.FrameTypeHandshakeTrigger})
	ft.push(jsonControlFrame(t, map[string]any{"code": 501, "status": 200, "target": testDevID}))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.Connect(ctx); err != nil {
		t.Fatalf("Connect() error: %v", err)
	}

	sent := ft.sentFrames()
	if len(sent) != 3 {
		t.Fatalf("sent %d frames, want 3 (trigger, register, time-sync — no authorize): %+v", len(sent), sent)
	}
	for _, f := range sent {
		if f.Type == domain.FrameTypeJSON {
			if m := decodeControl(t, f); m["code"] == float64(500) {
				t.Fatalf("sent a 500 authorize despite no password: %v", m)
			}
		}
	}
}

func TestAPSession_UnixTimer_AddsTimezoneOffset(t *testing.T) {
	ft := newFakeTransport()
	s := NewAPSession(ft, testDevID, testPassword)

	// A fixed zone two hours east of UTC: unixTimer = Unix() + 7200 (doc §12.3 step 4).
	zone := time.FixedZone("test+2", 2*3600)
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, zone)
	s.now = func() time.Time { return base }

	if got, want := s.unixTimer(), base.Unix()+7200; got != want {
		t.Fatalf("unixTimer() = %d, want %d", got, want)
	}
}

func TestAPSession_Connect_RegistrationRejected(t *testing.T) {
	ft := newFakeTransport()
	s := NewAPSession(ft, testDevID, testPassword)
	ft.push(domain.Frame{Type: domain.FrameTypeHandshakeTrigger})
	ft.push(jsonControlFrame(t, map[string]any{"code": 501, "status": 500, "target": testDevID}))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := s.Connect(ctx)
	if !errors.Is(err, ErrHandshakeRegister) {
		t.Fatalf("Connect() error = %v, want ErrHandshakeRegister", err)
	}
}

func TestAPSession_Connect_TimesOutWaitingForEcho(t *testing.T) {
	ft := newFakeTransport()
	s := NewAPSession(ft, testDevID, testPassword)
	// No frames pushed: the device never echoes, so awaiting the echo must
	// surface the context deadline rather than block forever.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := s.Connect(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Connect() error = %v, want context.DeadlineExceeded", err)
	}
}

func TestAPSession_Connect_SkipsUnrelatedInboundFrames(t *testing.T) {
	ft := newFakeTransport()
	s := NewAPSession(ft, testDevID, testPassword)
	s.now = func() time.Time { return time.Unix(0, 0).UTC() }

	// Interleave noise the handshake must ignore: a keepalive-ish Noop before
	// the echo, and an unrelated control code (device-presence 53) before the
	// registration confirmation.
	ft.push(domain.Frame{Type: domain.FrameTypeNoop})
	ft.push(domain.Frame{Type: domain.FrameTypeHandshakeTrigger})
	ft.push(jsonControlFrame(t, map[string]any{"code": 53, "status": 1}))
	ft.push(jsonControlFrame(t, map[string]any{"code": 501, "status": 200, "target": testDevID}))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.Connect(ctx); err != nil {
		t.Fatalf("Connect() error: %v", err)
	}
}

func TestAPSession_Connect_SkipsMalformedJSONFrame(t *testing.T) {
	ft := newFakeTransport()
	s := NewAPSession(ft, testDevID, testPassword)
	s.now = func() time.Time { return time.Unix(0, 0).UTC() }

	ft.push(domain.Frame{Type: domain.FrameTypeHandshakeTrigger})
	// A JSON-tagged frame whose body is not valid JSON must be skipped, not fatal.
	ft.push(domain.Frame{Type: domain.FrameTypeJSON, Payload: []byte("{not json")})
	ft.push(jsonControlFrame(t, map[string]any{"code": 501, "status": 200, "target": testDevID}))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.Connect(ctx); err != nil {
		t.Fatalf("Connect() error: %v", err)
	}
}

func TestAPSession_Connect_MissingStatusIsRejection(t *testing.T) {
	ft := newFakeTransport()
	s := NewAPSession(ft, testDevID, testPassword)
	s.now = func() time.Time { return time.Unix(0, 0).UTC() }

	ft.push(domain.Frame{Type: domain.FrameTypeHandshakeTrigger})
	// A 501 ack with no "status" field defaults to 0, which is not statusOK and
	// must be treated as a rejected registration.
	ft.push(jsonControlFrame(t, map[string]any{"code": 501, "target": testDevID}))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.Connect(ctx); !errors.Is(err, ErrHandshakeRegister) {
		t.Fatalf("Connect() error = %v, want ErrHandshakeRegister", err)
	}
}

func TestAPSession_Connect_TransportClosed(t *testing.T) {
	ft := newFakeTransport()
	s := NewAPSession(ft, testDevID, testPassword)
	_ = ft.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := s.Connect(ctx)
	if !errors.Is(err, ErrTransportClosed) {
		t.Fatalf("Connect() error = %v, want ErrTransportClosed", err)
	}
}
