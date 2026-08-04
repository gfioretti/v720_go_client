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
