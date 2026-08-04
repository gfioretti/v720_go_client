package domain

import "fmt"

// FrameType is the 16-bit "packet type tag" stored at offset 4 of every wire
// frame's 20-byte header (findings doc §12.3a). It classifies what kind of
// payload the frame carries — JSON control message, MJPEG video, G.711 audio,
// keepalive pong, and so on. The concrete tag constants live alongside this
// type; see doc §12.4 for the authoritative table.
//
// The on-wire encoding is a little-endian int16. Every observed tag value is a
// small non-negative number, so an unsigned 16-bit representation is a faithful
// and convenient carrier.
type FrameType uint16

// Packet type tags, from the authoritative table in findings doc §12.4. These
// classify the payload of a frame; they are distinct from the JSON "code" field
// carried inside FrameTypeJSON payloads (see Code in command.go).
const (
	FrameTypeJSON           FrameType = 0  // JSON command/response payload
	FrameTypeMJPEG          FrameType = 1  // MJPEG video frame
	FrameTypeAudioG711      FrameType = 4  // G.711 A-law audio frame
	FrameTypeAudioPCM       FrameType = 6  // PCM audio frame
	FrameTypeMiscFile       FrameType = 7  // misc/file-related queue
	FrameTypeSDCardXMLChunk FrameType = 8  // SD-card XML file chunk (AP-mode)
	FrameTypeGSensor        FrameType = 10 // G-sensor/orientation data (3× int16 LE)
	FrameTypeNoop           FrameType = 100
	FrameTypePongAP         FrameType = 101 // keepalive pong, AP-mode
	FrameTypePongRelay      FrameType = 102 // keepalive pong, P2P/relay
	// FrameTypeHandshakeTrigger opens the AP-mode handshake: the client sends an
	// empty type-115 frame and the device echoes it back (doc §12.3 steps 2–4).
	FrameTypeHandshakeTrigger FrameType = 115
)

// String renders the tag by name for logging, falling back to a numeric form
// for any value not in the §12.4 table.
func (ft FrameType) String() string {
	switch ft {
	case FrameTypeJSON:
		return "JSON"
	case FrameTypeMJPEG:
		return "MJPEG"
	case FrameTypeAudioG711:
		return "AudioG711"
	case FrameTypeAudioPCM:
		return "AudioPCM"
	case FrameTypeMiscFile:
		return "MiscFile"
	case FrameTypeSDCardXMLChunk:
		return "SDCardXMLChunk"
	case FrameTypeGSensor:
		return "GSensor"
	case FrameTypeNoop:
		return "Noop"
	case FrameTypePongAP:
		return "PongAP"
	case FrameTypePongRelay:
		return "PongRelay"
	case FrameTypeHandshakeTrigger:
		return "HandshakeTrigger"
	default:
		return fmt.Sprintf("FrameType(%d)", uint16(ft))
	}
}