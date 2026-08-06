package domain

import "testing"

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
