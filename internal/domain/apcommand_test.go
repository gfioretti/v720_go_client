package domain

import "testing"

// NewAPDeviceCommand must produce the exact AP-mode wire form verified against
// the decompiled OooOOO0/NaxclowApChannel.sendApCommand:
//
//	{"code":502,"content":{"code":N,"devTarget":"<devId>",...params}}
//
// (doc §12.5, corrected). Key order is encoding/json's deterministic alphabetical
// sort at each level.
func TestNewAPDeviceCommand_JSON(t *testing.T) {
	tests := []struct {
		name  string
		inner Command
		want  string
	}{
		{
			"video quality",
			NewVideoQuality(2),
			`{"code":502,"content":{"code":201,"devTarget":"dev123","speedGrade":2}}`,
		},
		{
			"IR LED",
			NewIRLed(1),
			`{"code":502,"content":{"IrLed":1,"code":202,"devTarget":"dev123"}}`,
		},
		{
			"switch camera",
			NewSwitchCamera(0),
			`{"code":502,"content":{"camId":0,"code":222,"devTarget":"dev123"}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := NewAPDeviceCommand("dev123", tt.inner)
			if cmd.Code != CodeApCommand {
				t.Errorf("outer Code = %d, want %d", cmd.Code, CodeApCommand)
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

// NewAPDeviceCommand must not mutate the caller's Params map.
func TestNewAPDeviceCommand_DoesNotAliasInnerParams(t *testing.T) {
	params := map[string]any{"speedGrade": 2}
	inner := Command{Code: CodeVideoQuality, Params: params}
	_ = NewAPDeviceCommand("dev123", inner)

	if len(params) != 1 {
		t.Fatalf("inner Params was mutated: %v", params)
	}
	if _, spurious := params["devTarget"]; spurious {
		t.Fatal("NewAPDeviceCommand leaked devTarget into the caller's Params")
	}
}

// The inner code and devTarget are authoritative: stray "code"/"devTarget" keys
// in the source command's Params must not shadow them.
func TestNewAPDeviceCommand_InnerFieldsAuthoritative(t *testing.T) {
	inner := Command{Code: CodeVideoQuality, Params: map[string]any{
		"speedGrade": 1,
		"code":       999,
		"devTarget":  "spoofed",
	}}
	cmd := NewAPDeviceCommand("real-dev", inner)
	got, err := cmd.JSON()
	if err != nil {
		t.Fatalf("JSON() error: %v", err)
	}
	want := `{"code":502,"content":{"code":201,"devTarget":"real-dev","speedGrade":1}}`
	if string(got) != want {
		t.Fatalf("JSON() = %s, want %s", got, want)
	}
}
