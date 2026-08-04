package domain

import "testing"

// The numeric values are fixed by the packet-type-tag table in findings doc
// §12.4; this guards against a typo silently reassigning a tag.
func TestFrameType_Values(t *testing.T) {
	tests := []struct {
		ft   FrameType
		want uint16
	}{
		{FrameTypeJSON, 0},
		{FrameTypeMJPEG, 1},
		{FrameTypeAudioG711, 4},
		{FrameTypeAudioPCM, 6},
		{FrameTypeMiscFile, 7},
		{FrameTypeSDCardXMLChunk, 8},
		{FrameTypeGSensor, 10},
		{FrameTypeNoop, 100},
		{FrameTypePongAP, 101},
		{FrameTypePongRelay, 102},
		{FrameTypeHandshakeTrigger, 115},
	}
	for _, tt := range tests {
		if uint16(tt.ft) != tt.want {
			t.Errorf("FrameType = %d, want %d", uint16(tt.ft), tt.want)
		}
	}
}

func TestFrameType_String(t *testing.T) {
	tests := []struct {
		ft   FrameType
		want string
	}{
		{FrameTypeJSON, "JSON"},
		{FrameTypeMJPEG, "MJPEG"},
		{FrameTypeAudioG711, "AudioG711"},
		{FrameTypeAudioPCM, "AudioPCM"},
		{FrameTypeMiscFile, "MiscFile"},
		{FrameTypeSDCardXMLChunk, "SDCardXMLChunk"},
		{FrameTypeGSensor, "GSensor"},
		{FrameTypeNoop, "Noop"},
		{FrameTypePongAP, "PongAP"},
		{FrameTypePongRelay, "PongRelay"},
		{FrameTypeHandshakeTrigger, "HandshakeTrigger"},
		// An unmapped tag falls back to a numeric rendering rather than a panic.
		{FrameType(999), "FrameType(999)"},
	}
	for _, tt := range tests {
		if got := tt.ft.String(); got != tt.want {
			t.Errorf("FrameType(%d).String() = %q, want %q", uint16(tt.ft), got, tt.want)
		}
	}
}
