package domain

import "testing"

func TestServerCommands_JSON(t *testing.T) {
	tests := []struct {
		name string
		cmd  Command
		want string
	}{
		{
			"register ack",
			NewRegisterAck(),
			`{"code":101,"status":200}`,
		},
		{
			"device status online",
			NewDeviceStatus(true),
			`{"code":53,"status":1}`,
		},
		{
			"device status offline",
			NewDeviceStatus(false),
			`{"code":53,"status":0}`,
		},
		{
			"base info request (301-wrapped code 4)",
			NewBaseInfoRequest("tgt", 1700000000),
			`{"code":301,"content":{"code":4,"unixTimer":1700000000},"target":"tgt"}`,
		},
		{
			"open video (301-wrapped stream-start code 3)",
			NewRelayStreamControl("tgt", StreamStart),
			`{"code":301,"content":{"code":3},"target":"tgt"}`,
		},
		{
			"stop stream (301-wrapped code 0)",
			NewRelayStreamControl("tgt", StreamStop),
			`{"code":301,"content":{"code":0},"target":"tgt"}`,
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
