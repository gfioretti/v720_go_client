package application

import (
	"encoding/json"

	"github.com/gfioretti/v720_go_client/internal/domain"
)

// This file holds the spec-pinned primitives of the relay UDP media channel
// (findings doc §12.2 phase 2): the candidate-probe result → ConnectionMode
// decision, and the parsers/builders for the channel's control messages. They
// are pure and unit-tested here; the async orchestration that consumes them —
// the probe/telemetry/keepalive loops and the fragment reassembler — is
// deliberately deferred until a real traffic capture exists, because the doc
// leaves the reassembly sentinels and timing behaviour unpinned (see
// docs/mode-a-client-design.md).

// Addr is a candidate IP:port on the media channel. Comparison is by value.
type Addr struct {
	IP   string
	Port int
}

// classifyConnectionMode maps the confirmed responding candidate address to the
// ConnectionMode it represents, by matching it against the three known
// candidates in the app's ladder order (doc §12.2 phase 2 step 2, §5):
//
//   - direct  = the device's devNatIp/devNatPort — checked against P2P_MODE_DIRECT
//   - nat     = the device's devIp/devPort — checked against P2P_MODE_NAT
//     (yes, that pairing reads backwards from the field names; it is read
//     verbatim from the app in doc §12.2 phase 1 step 3, not rationalised)
//   - forward = the relay/forward address — P2P_MODE_FORWARD
//
// ok is false when the responding address matches none of the three, so the
// caller can decide how to treat an unexpected address rather than this silently
// inventing a mode. Candidates are expected to be non-zero; matching proceeds in
// direct→nat→forward order, so if two candidates were equal the earlier (more
// direct) mode wins.
func classifyConnectionMode(responding, direct, nat, forward Addr) (domain.ConnectionMode, bool) {
	// A zero responding address can never be a real confirmed candidate; reject
	// it up front so it cannot spuriously match a zero-valued (unpopulated)
	// candidate slot.
	if responding == (Addr{}) {
		return 0, false
	}
	switch responding {
	case direct:
		return domain.ModeDirect, true
	case nat:
		return domain.ModeNAT, true
	case forward:
		return domain.ModeForward, true
	default:
		return 0, false
	}
}

// mediaControlMessage is the subset of the UDP media channel's JSON control
// fields the phase-2 primitives read (codes 21 and 51, doc §12.2 phase 2 step 2).
type mediaControlMessage struct {
	Code   domain.Code `json:"code"`
	Status int         `json:"status"`
	IP     string      `json:"ip"`
	Port   int         `json:"port"`
}

// parseMediaCandidate extracts the candidate address from an inbound
// {"code":21,"ip":...,"port":...} media-channel frame (doc §12.2 phase 2 step 2).
// ok is false for any frame that is not a well-formed code-21 JSON message.
func parseMediaCandidate(f domain.Frame) (Addr, bool) {
	msg, ok := decodeMediaControl(f)
	if !ok || msg.Code != domain.CodeMediaCandidate {
		return Addr{}, false
	}
	// A code-21 frame missing a usable ip/port is not a probeable candidate;
	// reject it here rather than letting an empty address reach the prober.
	if msg.IP == "" || msg.Port <= 0 {
		return Addr{}, false
	}
	return Addr{IP: msg.IP, Port: msg.Port}, true
}

// isMediaProbeOK reports whether f is a {"code":51,"status":200} probe-success
// confirmation (doc §12.2 phase 2 step 2).
func isMediaProbeOK(f domain.Frame) bool {
	msg, ok := decodeMediaControl(f)
	return ok && msg.Code == domain.CodeMediaProbeOK && msg.Status == statusOK
}

func decodeMediaControl(f domain.Frame) (mediaControlMessage, bool) {
	if f.Type != domain.FrameTypeJSON {
		return mediaControlMessage{}, false
	}
	var msg mediaControlMessage
	if err := json.Unmarshal(f.Payload, &msg); err != nil {
		return mediaControlMessage{}, false
	}
	return msg, true
}

// relayKeepalivePing is the bare ping frame the client sends on writer-idle over
// the media channel (doc §12.2 phase 2 step 4); the device replies with a
// type-102 frame (domain.FrameTypePongRelay), which the deferred keepalive loop
// uses to reset its timeout. The ping's packet type tag is not pinned by the doc
// (as with the coordinator's bare frame); the zero value (type 0, no payload) is
// used — UNCONFIRMED.
func relayKeepalivePing() domain.Frame {
	return domain.Frame{}
}

// isRelayPong reports whether f is the media-channel keepalive pong (packet type
// tag 102, doc §12.2 phase 2 step 4 / §12.4).
func isRelayPong(f domain.Frame) bool {
	return f.Type == domain.FrameTypePongRelay
}
