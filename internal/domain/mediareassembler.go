package domain

// UDP media fragment reassembly (findings doc §12.2 phase 2; resolved via
// a9-v720's v720_sta.py). The camera's live A/V arrives as 20-byte-header UDP
// frames whose flag byte (Frame.Flag, offset 6) is a fragment marker. A video
// (MJPEG) frame spans HEAD → BODY* → END packets; an audio (G.711) frame is a
// single FINISH packet. This is distinct from MJPEGReassembler, which delimits a
// continuous *TCP* byte stream by JPEG markers (AP-mode) — here UDP preserves
// datagram boundaries and the camera marks fragments explicitly.
//
// The msg-flag values are the a9-v720 PROTOCOL_MSG_FLAG_* constants as signed
// bytes (250→-6, 251→-5, 252→-4, 255→-1) — matching the doc's observed sentinels.
const (
	MsgFlagHead   int8 = -6 // 250: first fragment of a video frame
	MsgFlagBody   int8 = -5 // 251: a middle fragment
	MsgFlagEnd    int8 = -4 // 252: last fragment; its payload carries a trailing 5-byte suffix (4-byte size + 1)
	MsgFlagFinish int8 = -1 // 255: a complete, unfragmented frame (audio); also strips the 5-byte suffix
)

// mediaTrailer is the number of trailing bytes on END/FINISH payloads that are
// framing metadata (a 4-byte little-endian frame size plus one byte), not media
// data — stripped before the frame is emitted (a9-v720 uses payload[:-5]).
const mediaTrailer = 5

// MediaKind distinguishes a reassembled video frame from an audio frame.
type MediaKind int

const (
	MediaVideo MediaKind = iota // a complete JPEG image
	MediaAudio                  // a complete G.711 audio frame
)

// MediaFrame is a fully reassembled media unit ready to decode/sink.
type MediaFrame struct {
	Kind MediaKind
	Data []byte
}

// MediaReassembler reassembles the camera's fragmented UDP media into whole
// frames. Feed it every media frame in arrival order via Push; it returns a
// complete MediaFrame (ok=true) when a video frame's END or an audio FINISH
// arrives. It is a plain value object with no I/O; a single instance handles one
// stream and is not safe for concurrent use.
//
// Reassembly follows arrival order (as a9-v720 does): on a LAN the camera's
// packets are effectively ordered, and the retransmit-confirm ACK covers loss.
// Out-of-order UDP delivery is not reordered here — a limitation shared with the
// reference and acceptable for the LAN media path.
type MediaReassembler struct {
	video      []byte
	assembling bool
}

// Push feeds one inbound media frame. It returns a reassembled MediaFrame and
// true when a frame completes; otherwise the zero MediaFrame and false.
func (r *MediaReassembler) Push(f Frame) (MediaFrame, bool) {
	switch f.Type {
	case FrameTypeMJPEG:
		return r.pushVideo(f)
	case FrameTypeAudioG711, FrameTypeAudioPCM:
		if f.Flag == MsgFlagFinish {
			// Copy so the emitted frame never aliases the caller's payload — a
			// self-contained guarantee independent of how the transport owns it.
			return MediaFrame{Kind: MediaAudio, Data: append([]byte(nil), trimTrailer(f.Payload)...)}, true
		}
	}
	return MediaFrame{}, false
}

func (r *MediaReassembler) pushVideo(f Frame) (MediaFrame, bool) {
	switch f.Flag {
	case MsgFlagHead:
		// Begin a new frame, discarding any partial one (a dropped END).
		r.video = append(r.video[:0], f.Payload...)
		r.assembling = true
	case MsgFlagBody:
		if r.assembling {
			r.video = append(r.video, f.Payload...)
		}
	case MsgFlagEnd:
		if r.assembling {
			r.video = append(r.video, trimTrailer(f.Payload)...)
			out := MediaFrame{Kind: MediaVideo, Data: append([]byte(nil), r.video...)}
			r.video = r.video[:0]
			r.assembling = false
			return out, true
		}
	}
	return MediaFrame{}, false
}

// trimTrailer drops the framing trailer (4-byte size + 1) from an END/FINISH
// payload, returning just the media bytes. A payload shorter than the trailer is
// returned empty rather than panicking.
func trimTrailer(payload []byte) []byte {
	if len(payload) < mediaTrailer {
		return nil
	}
	return payload[:len(payload)-mediaTrailer]
}
