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

// NewStreamControl wraps the state in the AP-mode 502 content envelope
// ({"code":502,"content":{"code":<state>}}, doc §12.3 step 8 sendApCommand).
func TestNewStreamControl_JSON(t *testing.T) {
	tests := []struct {
		name  string
		state StreamState
		want  string
	}{
		{"start", StreamStart, `{"code":502,"content":{"code":3}}`},
		{"stop", StreamStop, `{"code":502,"content":{"code":0}}`},
		{"audio only", StreamOpenAudioCloseVideo, `{"code":502,"content":{"code":1}}`},
		{"video only", StreamCloseAudioOpenVideo, `{"code":502,"content":{"code":2}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := NewStreamControl(tt.state)
			if cmd.Code != CodeApCommand {
				t.Errorf("Code = %d, want %d", cmd.Code, CodeApCommand)
			}
			got, err := cmd.JSON()
			if err != nil {
				t.Fatalf("JSON() error: %v", err)
			}
			if string(got) != tt.want {
				t.Fatalf("JSON() = %s, want %s", got, tt.want)
			}
		})
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
