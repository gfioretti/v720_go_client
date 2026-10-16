package application

import (
	"testing"

	"github.com/gfioretti/v720_go_client/internal/domain"
)

func TestClassifyConnectionMode(t *testing.T) {
	direct := Addr{IP: "198.51.100.7", Port: 9000}  // devNatIp/devNatPort → DIRECT
	nat := Addr{IP: "192.168.1.9", Port: 8000}      // devIp/devPort → NAT
	forward := Addr{IP: "203.0.113.1", Port: 12000} // relay → FORWARD

	tests := []struct {
		name       string
		responding Addr
		wantMode   domain.ConnectionMode
		wantOK     bool
	}{
		{"direct match", direct, domain.ModeDirect, true},
		{"nat match", nat, domain.ModeNAT, true},
		{"forward match", forward, domain.ModeForward, true},
		{"no match", Addr{IP: "10.10.10.10", Port: 1}, 0, false},
		{"zero responding rejected", Addr{}, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mode, ok := classifyConnectionMode(tt.responding, direct, nat, forward)
			if mode != tt.wantMode || ok != tt.wantOK {
				t.Errorf("classifyConnectionMode = (%s, %v), want (%s, %v)",
					mode, ok, tt.wantMode, tt.wantOK)
			}
		})
	}
}

// Ladder order: when direct and nat candidates coincide, the more-direct mode
// wins.
func TestClassifyConnectionMode_LadderOrder(t *testing.T) {
	same := Addr{IP: "1.1.1.1", Port: 100}
	mode, ok := classifyConnectionMode(same, same, same, Addr{IP: "2.2.2.2", Port: 200})
	if mode != domain.ModeDirect || !ok {
		t.Errorf("got (%s, %v), want (Direct, true)", mode, ok)
	}
}

func TestParseMediaCandidate(t *testing.T) {
	f := jsonControlFrame(t, map[string]any{"code": 21, "ip": "192.168.1.9", "port": 8000})
	addr, ok := parseMediaCandidate(f)
	if !ok || addr != (Addr{IP: "192.168.1.9", Port: 8000}) {
		t.Fatalf("parseMediaCandidate = (%+v, %v), want ({192.168.1.9 8000}, true)", addr, ok)
	}

	// Wrong code, wrong frame type, and malformed payload all yield ok=false.
	if _, ok := parseMediaCandidate(jsonControlFrame(t, map[string]any{"code": 51, "status": 200})); ok {
		t.Error("parseMediaCandidate accepted a non-21 frame")
	}
	if _, ok := parseMediaCandidate(domain.Frame{Type: domain.FrameTypeMJPEG}); ok {
		t.Error("parseMediaCandidate accepted a non-JSON frame")
	}
	if _, ok := parseMediaCandidate(domain.Frame{Type: domain.FrameTypeJSON, Payload: []byte("{not json")}); ok {
		t.Error("parseMediaCandidate accepted malformed JSON")
	}
	// A code-21 frame with no usable ip/port is not a probeable candidate.
	if _, ok := parseMediaCandidate(jsonControlFrame(t, map[string]any{"code": 21, "port": 8000})); ok {
		t.Error("parseMediaCandidate accepted an empty IP")
	}
	if _, ok := parseMediaCandidate(jsonControlFrame(t, map[string]any{"code": 21, "ip": "192.168.1.9"})); ok {
		t.Error("parseMediaCandidate accepted a zero port")
	}
}

func TestIsMediaProbeOK(t *testing.T) {
	if !isMediaProbeOK(jsonControlFrame(t, map[string]any{"code": 51, "status": 200})) {
		t.Error("isMediaProbeOK rejected a valid 51/200 frame")
	}
	if isMediaProbeOK(jsonControlFrame(t, map[string]any{"code": 51, "status": 500})) {
		t.Error("isMediaProbeOK accepted a non-200 status")
	}
	if isMediaProbeOK(jsonControlFrame(t, map[string]any{"code": 21, "ip": "x", "port": 1})) {
		t.Error("isMediaProbeOK accepted a non-51 code")
	}
}

func TestRelayPongAndPing(t *testing.T) {
	if !isRelayPong(domain.Frame{Type: domain.FrameTypePongRelay}) {
		t.Error("isRelayPong rejected a type-102 frame")
	}
	if isRelayPong(domain.Frame{Type: domain.FrameTypePongAP}) {
		t.Error("isRelayPong accepted the AP-mode pong (type 101)")
	}
	if ping := relayKeepalivePing(); ping.Type != domain.FrameTypeJSON || len(ping.Payload) != 0 {
		t.Errorf("relayKeepalivePing = %+v, want empty type-0 frame", ping)
	}
}
