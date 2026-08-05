package domain

import "testing"

// StreamState inner-code values are fixed by doc §12.3 step 8.
func TestStreamState_Values(t *testing.T) {
	tests := []struct {
		s    StreamState
		want int
	}{
		{StreamStop, 0},
		{StreamOpenAudioCloseVideo, 1},
		{StreamCloseAudioOpenVideo, 2},
		{StreamStart, 3},
	}
	for _, tt := range tests {
		if int(tt.s) != tt.want {
			t.Errorf("StreamState = %d, want %d", int(tt.s), tt.want)
		}
	}
}

func TestFrameType_MediaClassification(t *testing.T) {
	tests := []struct {
		ft      FrameType
		isVideo bool
		isAudio bool
	}{
		{FrameTypeMJPEG, true, false},
		{FrameTypeAudioG711, false, true},
		{FrameTypeAudioPCM, false, true},
		{FrameTypeJSON, false, false},
		{FrameTypeHandshakeTrigger, false, false},
		{FrameTypeGSensor, false, false},
	}
	for _, tt := range tests {
		if got := tt.ft.IsVideo(); got != tt.isVideo {
			t.Errorf("%s.IsVideo() = %v, want %v", tt.ft, got, tt.isVideo)
		}
		if got := tt.ft.IsAudio(); got != tt.isAudio {
			t.Errorf("%s.IsAudio() = %v, want %v", tt.ft, got, tt.isAudio)
		}
	}
}
