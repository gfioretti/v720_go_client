package domain

import (
	"bytes"
	"testing"
)

// trailer is 5 arbitrary bytes standing in for the END/FINISH framing suffix
// (4-byte size + 1) that the reassembler strips.
var trailer = []byte{0x09, 0x01, 0x02, 0x03, 0x04}

func videoFrame(flag int8, payload []byte) Frame {
	return Frame{Type: FrameTypeMJPEG, Flag: flag, Payload: payload}
}

// A video frame reassembles from HEAD -> BODY -> END, concatenating payloads and
// stripping END's 5-byte trailer.
func TestMediaReassembler_VideoHeadBodyEnd(t *testing.T) {
	var r MediaReassembler

	if _, ok := r.Push(videoFrame(MsgFlagHead, []byte("aaa"))); ok {
		t.Fatal("HEAD alone should not complete a frame")
	}
	if _, ok := r.Push(videoFrame(MsgFlagBody, []byte("bbb"))); ok {
		t.Fatal("BODY should not complete a frame")
	}
	got, ok := r.Push(videoFrame(MsgFlagEnd, append([]byte("ccc"), trailer...)))
	if !ok {
		t.Fatal("END should complete the frame")
	}
	if got.Kind != MediaVideo || !bytes.Equal(got.Data, []byte("aaabbbccc")) {
		t.Fatalf("frame = %+v, want video 'aaabbbccc'", got)
	}
}

// An audio frame is a single FINISH packet, with its trailer stripped.
func TestMediaReassembler_AudioFinish(t *testing.T) {
	var r MediaReassembler
	got, ok := r.Push(Frame{Type: FrameTypeAudioG711, Flag: MsgFlagFinish, Payload: append([]byte("audio"), trailer...)})
	if !ok || got.Kind != MediaAudio || !bytes.Equal(got.Data, []byte("audio")) {
		t.Fatalf("audio frame = %+v, ok=%v, want audio 'audio'", got, ok)
	}
}

// BODY/END without a preceding HEAD are ignored (no output, no panic).
func TestMediaReassembler_OrphanFragmentsIgnored(t *testing.T) {
	var r MediaReassembler
	if _, ok := r.Push(videoFrame(MsgFlagBody, []byte("x"))); ok {
		t.Error("orphan BODY produced a frame")
	}
	if _, ok := r.Push(videoFrame(MsgFlagEnd, append([]byte("x"), trailer...))); ok {
		t.Error("orphan END produced a frame")
	}
}

// A new HEAD abandons an incomplete previous frame rather than corrupting it.
func TestMediaReassembler_HeadRestartsFrame(t *testing.T) {
	var r MediaReassembler
	r.Push(videoFrame(MsgFlagHead, []byte("stale")))
	r.Push(videoFrame(MsgFlagBody, []byte("junk")))
	// New frame starts; the stale partial is dropped.
	r.Push(videoFrame(MsgFlagHead, []byte("real")))
	got, ok := r.Push(videoFrame(MsgFlagEnd, append([]byte("frame"), trailer...)))
	if !ok || !bytes.Equal(got.Data, []byte("realframe")) {
		t.Fatalf("frame = %q, want 'realframe'", got.Data)
	}
}

// Two frames in a row reassemble independently.
func TestMediaReassembler_ConsecutiveFrames(t *testing.T) {
	var r MediaReassembler
	r.Push(videoFrame(MsgFlagHead, []byte("one")))
	f1, ok1 := r.Push(videoFrame(MsgFlagEnd, trailer)) // END with only the trailer -> no extra bytes
	r.Push(videoFrame(MsgFlagHead, []byte("two")))
	f2, ok2 := r.Push(videoFrame(MsgFlagEnd, trailer))
	if !ok1 || !bytes.Equal(f1.Data, []byte("one")) {
		t.Fatalf("frame 1 = %q", f1.Data)
	}
	if !ok2 || !bytes.Equal(f2.Data, []byte("two")) {
		t.Fatalf("frame 2 = %q", f2.Data)
	}
}

// An END payload shorter than the trailer trims to empty rather than panicking.
func TestMediaReassembler_ShortEndPayload(t *testing.T) {
	var r MediaReassembler
	r.Push(videoFrame(MsgFlagHead, []byte("hdr")))
	got, ok := r.Push(videoFrame(MsgFlagEnd, []byte{1, 2})) // shorter than the 5-byte trailer
	if !ok || !bytes.Equal(got.Data, []byte("hdr")) {
		t.Fatalf("frame = %q, want 'hdr' (short END contributes nothing)", got.Data)
	}
}

// Multiple BODY fragments concatenate in arrival order.
func TestMediaReassembler_MultipleBodyFragments(t *testing.T) {
	var r MediaReassembler
	r.Push(videoFrame(MsgFlagHead, []byte("A")))
	r.Push(videoFrame(MsgFlagBody, []byte("B")))
	r.Push(videoFrame(MsgFlagBody, []byte("C")))
	got, ok := r.Push(videoFrame(MsgFlagEnd, append([]byte("D"), trailer...)))
	if !ok || !bytes.Equal(got.Data, []byte("ABCD")) {
		t.Fatalf("frame = %q, want 'ABCD'", got.Data)
	}
}

// PCM audio (tag 6) FINISH also emits a complete audio frame.
func TestMediaReassembler_PCMAudioFinish(t *testing.T) {
	var r MediaReassembler
	got, ok := r.Push(Frame{Type: FrameTypeAudioPCM, Flag: MsgFlagFinish, Payload: append([]byte("pcm"), trailer...)})
	if !ok || got.Kind != MediaAudio || !bytes.Equal(got.Data, []byte("pcm")) {
		t.Fatalf("pcm frame = %+v, ok=%v", got, ok)
	}
}

// Mismatched flag/type combinations yield no frame: a FINISH on a video tag, and
// a non-FINISH flag on an audio tag.
func TestMediaReassembler_MismatchedFlagsIgnored(t *testing.T) {
	var r MediaReassembler
	if _, ok := r.Push(videoFrame(MsgFlagFinish, append([]byte("x"), trailer...))); ok {
		t.Error("FINISH on a video tag produced a frame")
	}
	if _, ok := r.Push(Frame{Type: FrameTypeAudioG711, Flag: MsgFlagHead, Payload: []byte("x")}); ok {
		t.Error("non-FINISH flag on an audio tag produced a frame")
	}
}

// NewRetransmitConfirm packs received package IDs as 4-byte little-endian into a
// type-605 frame; empty ids yield an empty payload.
func TestNewRetransmitConfirm(t *testing.T) {
	f := NewRetransmitConfirm([]uint32{1, 258})
	if f.Type != FrameTypeRetransmitConfirm {
		t.Errorf("type = %d, want 605", f.Type)
	}
	want := []byte{1, 0, 0, 0, 2, 1, 0, 0} // 1 and 258 (0x0102) little-endian
	if !bytes.Equal(f.Payload, want) {
		t.Errorf("payload = %v, want %v", f.Payload, want)
	}

	empty := NewRetransmitConfirm(nil)
	if len(empty.Payload) != 0 || empty.Type != FrameTypeRetransmitConfirm {
		t.Errorf("empty confirm = %+v", empty)
	}
}

// The msg-flag constants decode as the documented signed sentinels.
func TestMsgFlagValues(t *testing.T) {
	tests := []struct {
		flag int8
		want int8
	}{
		{MsgFlagHead, -6},
		{MsgFlagBody, -5},
		{MsgFlagEnd, -4},
		{MsgFlagFinish, -1},
	}
	for _, tt := range tests {
		if tt.flag != tt.want {
			t.Errorf("flag = %d, want %d", tt.flag, tt.want)
		}
	}
}
