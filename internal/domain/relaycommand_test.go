package domain

import "testing"

// NewRelayDeviceCommand must produce the exact P2P/relay wire form from the
// app's NaxclowServerChannel.sendForwardCommand (doc §12.2 step 10):
//
//	{"code":301,"target":"<devId>","content":{"code":N,...params}}
//
// Unlike AP-mode's 502 wrapper, "target" is at the outer level and "content" is
// the bare inner command. Key order is encoding/json's deterministic alphabetical
// sort at each level.
func TestNewRelayDeviceCommand_JSON(t *testing.T) {
	tests := []struct {
		name  string
		inner Command
		want  string
	}{
		{
			"video quality",
			NewVideoQuality(2),
			`{"code":301,"content":{"code":201,"speedGrade":2},"target":"dev123"}`,
		},
		{
			"IR LED",
			NewIRLed(1),
			`{"code":301,"content":{"IrLed":1,"code":202},"target":"dev123"}`,
		},
		{
			"switch camera",
			NewSwitchCamera(0),
			`{"code":301,"content":{"camId":0,"code":222},"target":"dev123"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := NewRelayDeviceCommand("dev123", tt.inner)
			if cmd.Code != CodeRelayForward {
				t.Errorf("outer Code = %d, want %d", cmd.Code, CodeRelayForward)
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

// A paramless inner command (nil Params) yields content with only the code —
// several §12.5 commands are paramless, so this must not panic or misencode.
func TestNewRelayDeviceCommand_NilInnerParams(t *testing.T) {
	cmd := NewRelayDeviceCommand("dev123", Command{Code: CodeVideoQuality})
	got, err := cmd.JSON()
	if err != nil {
		t.Fatalf("JSON() error: %v", err)
	}
	want := `{"code":301,"content":{"code":201},"target":"dev123"}`
	if string(got) != want {
		t.Fatalf("JSON() = %s, want %s", got, want)
	}
}

// NewRelayDeviceCommand must not mutate the caller's Params map.
func TestNewRelayDeviceCommand_DoesNotAliasInnerParams(t *testing.T) {
	params := map[string]any{"speedGrade": 2}
	inner := Command{Code: CodeVideoQuality, Params: params}
	_ = NewRelayDeviceCommand("dev123", inner)

	if len(params) != 1 {
		t.Fatalf("inner Params was mutated: %v", params)
	}
	if _, spurious := params["code"]; spurious {
		t.Fatal("NewRelayDeviceCommand leaked code into the caller's Params")
	}
}

// The outer code and target are authoritative: stray "code"/"target" keys in the
// source command's Params must not shadow them. A stray "target" belongs to the
// inner content namespace and is meaningless there, but it must never escape to
// the outer level.
func TestNewRelayDeviceCommand_OuterFieldsAuthoritative(t *testing.T) {
	inner := Command{Code: CodeVideoQuality, Params: map[string]any{
		"speedGrade": 1,
		"code":       999,
		"target":     "spoofed",
	}}
	cmd := NewRelayDeviceCommand("real-dev", inner)
	got, err := cmd.JSON()
	if err != nil {
		t.Fatalf("JSON() error: %v", err)
	}
	// Outer code=301 and target=real-dev win; the inner content's own code is the
	// real command code (201), and the stray "target" stays trapped inside content.
	want := `{"code":301,"content":{"code":201,"speedGrade":1,"target":"spoofed"},"target":"real-dev"}`
	if string(got) != want {
		t.Fatalf("JSON() = %s, want %s", got, want)
	}
}
