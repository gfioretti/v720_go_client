package domain

import "fmt"

// ConnectionMode is the connection mode a session ultimately achieves. The
// official app models a classic P2P-camera connection ladder (findings doc §5,
// from NaxclowRtcEngine's P2P_MODE_* constants): try a direct connection first,
// fall back to STUN-assisted NAT traversal, then fall back to relaying through
// naxclow's forward server. AP-mode is the separate mode used when the phone is
// on the camera's own Wi-Fi hotspot.
//
// The integer values match the app's constants exactly (P2P_MODE_DIRECT=1,
// _NAT=2, _FORWARD=3, _AP=4), because the relay media channel compares the
// responding candidate address against these to decide which mode it reached
// (doc §12.2 phase 2 step 2).
type ConnectionMode uint8

const (
	// ModeDirect is a direct LAN/WAN connection to the device (P2P_MODE_DIRECT=1).
	ModeDirect ConnectionMode = 1
	// ModeNAT is a STUN-assisted NAT-traversal connection (P2P_MODE_NAT=2).
	ModeNAT ConnectionMode = 2
	// ModeForward relays through naxclow's forward server (P2P_MODE_FORWARD=3).
	ModeForward ConnectionMode = 3
	// ModeAP is the camera's own Wi-Fi hotspot / local-direct mode (P2P_MODE_AP=4).
	ModeAP ConnectionMode = 4
)

// String renders the mode by name for logging, falling back to a numeric form
// for any value outside the §5 ladder.
func (m ConnectionMode) String() string {
	switch m {
	case ModeDirect:
		return "Direct"
	case ModeNAT:
		return "NAT"
	case ModeForward:
		return "Forward"
	case ModeAP:
		return "AP"
	default:
		return fmt.Sprintf("ConnectionMode(%d)", uint8(m))
	}
}
