package domain

import (
	"encoding/json"
	"testing"
)

// Code integers are fixed by the doc tables §12.5 (device control), §12.6
// (SD-card/file), and §12.7 (connection/auth). Guard against typos.
func TestCode_Values(t *testing.T) {
	tests := []struct {
		c    Code
		want uint16
	}{
		// §12.5 device control
		{CodeVideoQuality, 201},
		{CodeIRLed, 202},
		{CodeSDRecordMode, 203},
		{CodeSetWifi, 204},
		{CodeMotionAlert, 205},
		{CodeMoveGrade, 206},
		{CodeWhiteLED, 220},
		{CodeSetApPassword, 221},
		{CodeSwitchCamera, 222},
		{CodeSendSDP, 230},
		// §12.6 SD-card / file transfer
		{CodeSDCardDateList, 400},
		{CodeSDCardFileList, 402},
		{CodeSDCardFileSize, 403},
		{CodeSDCardFileInfo, 404},
		{CodeSDCardStartAVI, 406},
		{CodeSDCardStopAVI, 408},
		{CodeSDCardDeleteFile, 412},
		{CodeAlbumInfo, 414},
		{CodeAlbumPage, 416},
		{CodeAlbumStop, 418},
		{CodeSDCardRegister, 110},
		// §12.7 connection / auth
		{CodeTimeSync, 4},
		{CodeAuthorize, 500},
		{CodeConnect, 501},
		{CodeApCommand, 502},
		// §12.2 phase 1 relay coordinator channel
		{CodeRelayReqConnect, 10},
		{CodeRelayDeviceCandidates, 13},
		{CodeRelayReqForwardID, 30},
		{CodeRelayForwardID, 31},
		{CodeRelayReqPlaybackID, 36},
		{CodeRelayPlaybackID, 37},
		{CodeRelayDeviceStatus, 52},
		{CodeRelayDevicePresence, 53},
		{CodeRelayRegister, 100},
		{CodeRelayRegisterStatus, 101},
		{CodeRelayHousekeeping, 298},
		{CodeRelayForward, 301},
		// §12.2 phase 2 relay media channel
		{CodeMediaCandidate, 21},
		{CodeMediaProbeOK, 51},
		{CodeMediaQualityReport, 54},
		{CodeMediaQualityEcho, 55},
	}
	for _, tt := range tests {
		if uint16(tt.c) != tt.want {
			t.Errorf("Code = %d, want %d", uint16(tt.c), tt.want)
		}
	}
}

// Command.JSON emits a type-0 JSON body of {"code":N, ...params}. Key order is
// encoding/json's deterministic alphabetical sort and is irrelevant to the
// device (it parses JSON), but pinning exact bytes here catches param-name and
// value-encoding regressions. Constructors are the device-control commands from
// §12.5 whose parameter names/types the doc states outright.
func TestCommand_Constructors(t *testing.T) {
	tests := []struct {
		name string
		cmd  Command
		want string
	}{
		{"video quality", NewVideoQuality(2), `{"code":201,"speedGrade":2}`},
		{"IR LED", NewIRLed(1), `{"IrLed":1,"code":202}`},
		{"motion alert", NewMotionAlert(0), `{"code":205,"moveAlert":0}`},
		{"move grade", NewMoveGrade(3), `{"code":206,"moveGrade":3}`},
		{"white LED", NewWhiteLED(1), `{"code":220,"ledEI":1}`},
		{"switch camera", NewSwitchCamera(1), `{"camId":1,"code":222}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.cmd.JSON()
			if err != nil {
				t.Fatalf("JSON() error: %v", err)
			}
			if string(got) != tt.want {
				t.Fatalf("JSON() = %s, want %s", got, tt.want)
			}
		})
	}
}

// A caller-supplied "code" key in Params must never shadow the command's own
// Code — the Code field is authoritative.
func TestCommand_JSONCodeIsAuthoritative(t *testing.T) {
	cmd := Command{Code: CodeVideoQuality, Params: map[string]any{"code": 999, "speedGrade": 1}}
	got, err := cmd.JSON()
	if err != nil {
		t.Fatalf("JSON() error: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(got, &parsed); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
	if parsed["code"] != float64(201) {
		t.Fatalf("code = %v, want 201 (Code field must win over Params)", parsed["code"])
	}
}

// A command with no extra params still emits a valid {"code":N} body.
func TestCommand_JSONNoParams(t *testing.T) {
	got, err := Command{Code: CodeAuthorize}.JSON()
	if err != nil {
		t.Fatalf("JSON() error: %v", err)
	}
	if string(got) != `{"code":500}` {
		t.Fatalf("JSON() = %s, want {\"code\":500}", got)
	}
}

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
		{
			"set wifi",
			NewSetWifi("HomeNet", "secret8"),
			`{"code":502,"content":{"code":204,"devTarget":"dev123","p":"secret8","s":"HomeNet"}}`,
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

// NewMediaQualityReport emits the exact {"code":54,"timerStamp":"<millis>"} shape
// (doc §12.2 phase 2 step 3), with timerStamp as a JSON string.
func TestNewMediaQualityReport_JSON(t *testing.T) {
	cmd := NewMediaQualityReport("1757200000123")
	if cmd.Code != CodeMediaQualityReport {
		t.Errorf("Code = %d, want %d", cmd.Code, CodeMediaQualityReport)
	}
	got, err := cmd.JSON()
	if err != nil {
		t.Fatalf("JSON() error: %v", err)
	}
	want := `{"code":54,"timerStamp":"1757200000123"}`
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

// SD-card commands are sent BARE (not 502-wrapped, unlike device control) as
// {"code":N,"target":"<devId>",...} type-0 frames — verified against
// NaxclowApChannel's direct sendProtoBeanWithTimeout calls (doc §12.6). Field
// sets per code confirmed from the source. Key order is encoding/json's
// alphabetical sort.
func TestSDCardCommands_JSON(t *testing.T) {
	const dev = "dev123"
	tests := []struct {
		name string
		cmd  Command
		want string
	}{
		{
			"date list (400)",
			NewSDCardDateList(dev),
			`{"code":400,"target":"dev123"}`,
		},
		{
			"file list for date (402)",
			NewSDCardFileList(dev, 20260805),
			`{"code":402,"date":20260805,"target":"dev123"}`,
		},
		{
			"file info (404)",
			NewSDCardFileInfo(dev, 20260805, 14, 30),
			`{"code":404,"date":20260805,"hours":14,"minute":30,"target":"dev123"}`,
		},
		{
			"start AVI stream (406)",
			NewSDCardStartAVI(dev, 20260805, 14, 30),
			`{"code":406,"date":20260805,"hours":14,"minute":30,"target":"dev123"}`,
		},
		{
			"stop AVI stream (408)",
			NewSDCardStopAVI(dev),
			`{"code":408,"target":"dev123"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.cmd.JSON()
			if err != nil {
				t.Fatalf("JSON() error: %v", err)
			}
			if string(got) != tt.want {
				t.Fatalf("JSON() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestSDCardCommands_Codes(t *testing.T) {
	const dev = "d"
	checks := []struct {
		cmd  Command
		code Code
	}{
		{NewSDCardDateList(dev), CodeSDCardDateList},
		{NewSDCardFileList(dev, 1), CodeSDCardFileList},
		{NewSDCardFileInfo(dev, 1, 2, 3), CodeSDCardFileInfo},
		{NewSDCardStartAVI(dev, 1, 2, 3), CodeSDCardStartAVI},
		{NewSDCardStopAVI(dev), CodeSDCardStopAVI},
	}
	for _, c := range checks {
		if c.cmd.Code != c.code {
			t.Errorf("Code = %d, want %d", c.cmd.Code, c.code)
		}
	}
}

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
