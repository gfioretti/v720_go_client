package domain

import (
	"encoding/json"
	"testing"
)

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

// NewStreamControl must produce the 502-wrapped envelope the camera expects,
// {"code":502,"content":{"code":<state>,"devTarget":...}} — the exact shape
// OooOOO0.OooOO0o(int,String) → sendApCommand sends (start=3, stop=0).
func TestNewStreamControl(t *testing.T) {
	tests := []struct {
		name  string
		state StreamState
		want  float64
	}{
		{"start", StreamStart, 3},
		{"stop", StreamStop, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, err := NewStreamControl("dev123", tt.state).JSON()
			if err != nil {
				t.Fatalf("JSON() error: %v", err)
			}
			var m map[string]any
			if err := json.Unmarshal(body, &m); err != nil {
				t.Fatalf("unmarshal %q: %v", body, err)
			}
			if m["code"] != float64(502) {
				t.Errorf("outer code = %v, want 502", m["code"])
			}
			content, ok := m["content"].(map[string]any)
			if !ok {
				t.Fatalf("content = %v (%T), want a JSON object", m["content"], m["content"])
			}
			if content["code"] != tt.want {
				t.Errorf("inner code = %v, want %v", content["code"], tt.want)
			}
			if content["devTarget"] != "dev123" {
				t.Errorf("inner devTarget = %v, want dev123", content["devTarget"])
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
