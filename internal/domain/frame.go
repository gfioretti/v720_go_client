package domain

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// HeaderSize is the fixed length, in bytes, of every wire frame's header.
// Every channel — AP-mode's single TCP connection and relay-mode's TCP
// coordinator / TCP storage / UDP media channels — uses this identical 20-byte
// header (findings doc §12.2a/§12.3a).
const HeaderSize = 20

// MaxFrameLength mirrors the official app's Netty LengthFieldBasedFrameDecoder
// maxFrameLength (doc §12.3a): a whole frame — header plus payload — never
// exceeds this many bytes on the wire.
const MaxFrameLength = 8092

// Frame is the unified 20-byte-header wire packet used by every V720 channel.
// It is a pure domain value object: Encode/Decode move it to and from its
// on-wire byte layout and perform no I/O. The byte layout is fixed by doc
// §12.3a:
//
//	offset 0  size 4  payload length    int32  little-endian (derived from Payload)
//	offset 4  size 2  packet type tag   int16  little-endian → Type
//	offset 6  size 1  flag byte         int8               → Flag
//	offset 7  size 1  unknown byte                         → Unknown
//	offset 8  size 8  opaque field                         → Opaque
//	offset 16 size 4  sequence/counter  int32  little-endian → Sequence
//	offset 20 size N  payload                               → Payload
type Frame struct {
	// Type is the packet type tag at offset 4 (doc §12.4).
	Type FrameType

	// Flag is the byte at offset 6. Its full meaning is unconfirmed (doc §12.8),
	// but on the UDP media path it acts as a fragment/frame-boundary marker with
	// observed signed sentinel values -1, -4, -5, -6 (doc §12.2 phase 2 step 6),
	// so it is modelled as a signed int8 to preserve those values faithfully.
	Flag int8

	// Unknown is the byte at offset 7. Purpose unconfirmed (doc §12.8).
	Unknown byte

	// Opaque is the 8-byte field at offsets 8–15. Purpose unconfirmed; the best
	// current guess is a session-token / forwardId fragment (doc §12.8).
	Opaque [8]byte

	// Sequence is the counter at offset 16, keyed on by the UDP jitter buffer for
	// reassembly/reordering (doc §12.2 phase 2 step 6).
	Sequence uint32

	// Payload is the N bytes following the header. Its length is what gets
	// written into the offset-0 length field on Encode.
	Payload []byte
}

// Sentinel errors returned by Decode, so callers can branch with errors.Is.
var (
	// ErrShortFrame means the input is smaller than the fixed 20-byte header.
	ErrShortFrame = errors.New("domain: frame shorter than 20-byte header")

	// ErrLengthMismatch means the offset-0 length field does not match the number
	// of payload bytes actually present after the header.
	ErrLengthMismatch = errors.New("domain: frame length field does not match payload size")

	// ErrFrameTooLarge means the whole frame exceeds MaxFrameLength.
	ErrFrameTooLarge = errors.New("domain: frame exceeds maximum length")
)

// Encode serialises the frame into its on-wire byte representation: the 20-byte
// header followed by the payload. The offset-0 length field is written from the
// actual Payload length, so callers never set it by hand.
func (f Frame) Encode() []byte {
	buf := make([]byte, HeaderSize+len(f.Payload))

	binary.LittleEndian.PutUint32(buf[0:4], uint32(len(f.Payload)))
	binary.LittleEndian.PutUint16(buf[4:6], uint16(f.Type))
	buf[6] = byte(f.Flag)
	buf[7] = f.Unknown
	copy(buf[8:16], f.Opaque[:])
	binary.LittleEndian.PutUint32(buf[16:20], f.Sequence)
	copy(buf[HeaderSize:], f.Payload)

	return buf
}

// Decode parses exactly one complete frame out of data: a 20-byte header
// followed by precisely the number of payload bytes the header declares. It
// does not perform stream framing — splitting a TCP byte stream into individual
// frames is the transport adapter's job (via a length-prefix framer); by the
// time bytes reach Decode they must already be a single whole frame.
//
// It returns ErrShortFrame if data is smaller than the header, ErrLengthMismatch
// if the declared payload length does not match the bytes present, and
// ErrFrameTooLarge if the frame exceeds MaxFrameLength.
func Decode(data []byte) (Frame, error) {
	if len(data) < HeaderSize {
		return Frame{}, fmt.Errorf("%w: got %d bytes", ErrShortFrame, len(data))
	}
	if len(data) > MaxFrameLength {
		return Frame{}, fmt.Errorf("%w: %d > %d", ErrFrameTooLarge, len(data), MaxFrameLength)
	}

	payloadLen := binary.LittleEndian.Uint32(data[0:4])
	if int64(payloadLen) != int64(len(data)-HeaderSize) {
		return Frame{}, fmt.Errorf("%w: header declares %d, have %d",
			ErrLengthMismatch, payloadLen, len(data)-HeaderSize)
	}

	f := Frame{
		Type:     FrameType(binary.LittleEndian.Uint16(data[4:6])),
		Flag:     int8(data[6]),
		Unknown:  data[7],
		Sequence: binary.LittleEndian.Uint32(data[16:20]),
	}
	copy(f.Opaque[:], data[8:16])

	if payloadLen > 0 {
		// Copy so the returned Frame does not alias the caller's buffer, which a
		// transport adapter is free to reuse for the next read.
		f.Payload = make([]byte, payloadLen)
		copy(f.Payload, data[HeaderSize:])
	}

	return f, nil
}

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
	// FrameTypeRetransmitConfirm is the server's per-frame media ACK on the UDP
	// media path (a9-v720 P2P_UDP_CMD_RETRANSMISSION_CONFIRM): its payload is the
	// list of received package IDs, 4 bytes little-endian each, so the camera
	// knows which packets not to retransmit.
	FrameTypeRetransmitConfirm FrameType = 605
)

// IsVideo reports whether the tag carries a video frame. Only MJPEG (tag 1) is
// confirmed in practice; a distinct H.264 tag may exist but has not been
// observed (doc §12.4 note).
func (ft FrameType) IsVideo() bool {
	return ft == FrameTypeMJPEG
}

// IsAudio reports whether the tag carries an audio frame: G.711 A-law (tag 4) or
// PCM (tag 6), per doc §12.4.
func (ft FrameType) IsAudio() bool {
	return ft == FrameTypeAudioG711 || ft == FrameTypeAudioPCM
}

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
