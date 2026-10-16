package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gfioretti/v720_go_client/internal/domain"
)

var (
	relayAccount    = domain.Account{UID: "user-42"}
	relayConf       = domain.LiveConf{Host: "1.2.3.4", TCPPort: 6123, Pwd: "devpwd", TarPwd: "tarpwd", Domain: "dom-x"}
	relayCandidates = ClientCandidates{IP: "10.0.0.5", Port: 5000, NatIP: "203.0.113.9", NatPort: 40000}
)

// pushPhase1Responses pre-loads the coordinator's happy-path replies in the order
// the client awaits them (doc §12.2 phase 1): device candidates (13), live
// forwardId (31), playback forwardId (37), registration OK (101).
func pushPhase1Responses(t *testing.T, ch *fakeTransport) {
	t.Helper()
	ch.push(jsonControlFrame(t, map[string]any{
		"code": 13, "status": 200,
		"devIp": "192.168.1.9", "devPort": 8000,
		"devNatIp": "198.51.100.7", "devNatPort": 9000,
	}))
	ch.push(jsonControlFrame(t, map[string]any{"code": 31, "status": 200, "forwardId": "live-fwd"}))
	ch.push(jsonControlFrame(t, map[string]any{"code": 37, "status": 200, "forwardId": "pb-fwd"}))
	ch.push(jsonControlFrame(t, map[string]any{"code": 101, "status": 200}))
}

func newConnectedRelaySession(t *testing.T) (*RelaySession, *fakeRelayTransport) {
	t.Helper()
	rt := newFakeRelayTransport()
	s := NewRelaySession(rt, relayAccount, relayConf, testDevID, relayCandidates)
	pushPhase1Responses(t, rt.coordinator)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.Connect(ctx); err != nil {
		t.Fatalf("Connect() error: %v", err)
	}
	return s, rt
}

func TestRelaySession_Connect_HappyPath(t *testing.T) {
	s, rt := newConnectedRelaySession(t)

	sent := rt.coordinator.sentFrames()
	if len(sent) != 6 {
		t.Fatalf("sent %d coordinator frames, want 6: %+v", len(sent), sent)
	}

	// Frame 0 — bare frame preceding register (doc §12.2 phase 1 step 2).
	if sent[0].Type != domain.FrameTypeJSON || len(sent[0].Payload) != 0 {
		t.Errorf("frame 0 = %+v, want empty JSON frame", sent[0])
	}

	// Frame 1 — register (100): uid=account, token=pwd, domain (§12.1a).
	reg := decodeControl(t, sent[1])
	if reg["code"] != float64(100) || reg["uid"] != relayAccount.UID ||
		reg["token"] != relayConf.Pwd || reg["domain"] != relayConf.Domain {
		t.Errorf("register frame = %v", reg)
	}

	// Frame 2 — reqForwardId (30): our cli addr, the device addr from the code-13
	// response, devTarget, devToken=tarPwd.
	fwd := decodeControl(t, sent[2])
	if fwd["code"] != float64(30) || fwd["cliIp"] != relayCandidates.IP ||
		fwd["cliPort"] != float64(relayCandidates.Port) || fwd["devIp"] != "192.168.1.9" ||
		fwd["devPort"] != float64(8000) || fwd["devTarget"] != testDevID ||
		fwd["devToken"] != relayConf.TarPwd {
		t.Errorf("reqForwardId frame = %v", fwd)
	}

	// Frame 3 — reqPlaybackId (36): just devTarget.
	pb := decodeControl(t, sent[3])
	if pb["code"] != float64(36) || pb["devTarget"] != testDevID {
		t.Errorf("reqPlaybackId frame = %v", pb)
	}

	// Frame 4 — reqConnect (10): all four client candidates + devTarget/devToken.
	conn := decodeControl(t, sent[4])
	if conn["code"] != float64(10) || conn["cliIp"] != relayCandidates.IP ||
		conn["cliPort"] != float64(relayCandidates.Port) || conn["cliNatIp"] != relayCandidates.NatIP ||
		conn["cliNatPort"] != float64(relayCandidates.NatPort) || conn["devTarget"] != testDevID ||
		conn["devToken"] != relayConf.TarPwd {
		t.Errorf("reqConnect frame = %v", conn)
	}

	// Frame 5 — reqSendDeviceStatus (52): devTarget, devToken, status.
	ds := decodeControl(t, sent[5])
	if ds["code"] != float64(52) || ds["devTarget"] != testDevID || ds["devToken"] != relayConf.TarPwd {
		t.Errorf("device status frame = %v", ds)
	}

	// Forward IDs captured for later phases.
	if s.LiveForwardID() != "live-fwd" {
		t.Errorf("LiveForwardID() = %q, want live-fwd", s.LiveForwardID())
	}
	if s.PlaybackForwardID() != "pb-fwd" {
		t.Errorf("PlaybackForwardID() = %q, want pb-fwd", s.PlaybackForwardID())
	}
}

// Interleaved presence (53), housekeeping (298), and time-sync (4) frames, plus a
// stray non-JSON frame, must be skipped while awaiting each expected response.
func TestRelaySession_Connect_SkipsInterleavedFrames(t *testing.T) {
	rt := newFakeRelayTransport()
	s := NewRelaySession(rt, relayAccount, relayConf, testDevID, relayCandidates)

	ch := rt.coordinator
	ch.push(domain.Frame{Type: domain.FrameTypeNoop}) // non-JSON noise
	ch.push(jsonControlFrame(t, map[string]any{"code": 298}))
	ch.push(jsonControlFrame(t, map[string]any{
		"code": 13, "status": 200, "devIp": "192.168.1.9", "devPort": 8000,
	}))
	ch.push(jsonControlFrame(t, map[string]any{"code": 53, "status": 1}))
	ch.push(jsonControlFrame(t, map[string]any{"code": 31, "status": 200, "forwardId": "live-fwd"}))
	ch.push(jsonControlFrame(t, map[string]any{"code": 37, "status": 200, "forwardId": "pb-fwd"}))
	ch.push(jsonControlFrame(t, map[string]any{"code": 4, "unixTimer": 12345}))
	ch.push(jsonControlFrame(t, map[string]any{"code": 101, "status": 200}))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.Connect(ctx); err != nil {
		t.Fatalf("Connect() error: %v", err)
	}
	if s.LiveForwardID() != "live-fwd" || s.PlaybackForwardID() != "pb-fwd" {
		t.Errorf("forward IDs = %q/%q", s.LiveForwardID(), s.PlaybackForwardID())
	}
}

// A {"code":101,"status":500} rejection surfaces as ErrRelayRegister.
func TestRelaySession_Connect_RegisterRejected(t *testing.T) {
	rt := newFakeRelayTransport()
	s := NewRelaySession(rt, relayAccount, relayConf, testDevID, relayCandidates)

	ch := rt.coordinator
	ch.push(jsonControlFrame(t, map[string]any{"code": 13, "status": 200, "devIp": "192.168.1.9", "devPort": 8000}))
	ch.push(jsonControlFrame(t, map[string]any{"code": 31, "status": 200, "forwardId": "live-fwd"}))
	ch.push(jsonControlFrame(t, map[string]any{"code": 37, "status": 200, "forwardId": "pb-fwd"}))
	ch.push(jsonControlFrame(t, map[string]any{"code": 101, "status": 500}))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := s.Connect(ctx)
	if !errors.Is(err, ErrRelayRegister) {
		t.Fatalf("Connect() error = %v, want ErrRelayRegister", err)
	}
}

// A non-200 forwardId response fails the handshake before registration.
func TestRelaySession_Connect_ForwardIDFailed(t *testing.T) {
	rt := newFakeRelayTransport()
	s := NewRelaySession(rt, relayAccount, relayConf, testDevID, relayCandidates)

	ch := rt.coordinator
	ch.push(jsonControlFrame(t, map[string]any{"code": 13, "status": 200, "devIp": "192.168.1.9", "devPort": 8000}))
	ch.push(jsonControlFrame(t, map[string]any{"code": 31, "status": 500}))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.Connect(ctx); err == nil {
		t.Fatal("Connect() error = nil, want forwardId failure")
	}
}

// A dropped coordinator connection mid-handshake propagates the transport error.
func TestRelaySession_Connect_TransportClosed(t *testing.T) {
	rt := newFakeRelayTransport()
	s := NewRelaySession(rt, relayAccount, relayConf, testDevID, relayCandidates)
	rt.coordinator.Close() // no responses will ever arrive

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.Connect(ctx); !errors.Is(err, ErrTransportClosed) {
		t.Fatalf("Connect() error = %v, want ErrTransportClosed", err)
	}
}
