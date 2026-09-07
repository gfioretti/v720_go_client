package domain

import (
	"testing"
)

func TestLiveConf_Addr(t *testing.T) {
	tests := []struct {
		name string
		conf LiveConf
		want string
	}{
		{"ipv4", LiveConf{Host: "1.2.3.4", TCPPort: 6123}, "1.2.3.4:6123"},
		{"hostname", LiveConf{Host: "relay.example.com", TCPPort: 443}, "relay.example.com:443"},
		{"ipv6 gets bracketed", LiveConf{Host: "::1", TCPPort: 6123}, "[::1]:6123"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.conf.Addr(); got != tt.want {
				t.Errorf("Addr() = %q, want %q", got, tt.want)
			}
		})
	}
}

// The integer values must match the app's P2P_MODE_* constants exactly (doc §5),
// because the media channel compares candidate addresses against them.
func TestConnectionMode_Values(t *testing.T) {
	tests := []struct {
		m    ConnectionMode
		want uint8
	}{
		{ModeDirect, 1},
		{ModeNAT, 2},
		{ModeForward, 3},
		{ModeAP, 4},
	}
	for _, tt := range tests {
		if uint8(tt.m) != tt.want {
			t.Errorf("ConnectionMode = %d, want %d", uint8(tt.m), tt.want)
		}
	}
}

func TestConnectionMode_String(t *testing.T) {
	tests := []struct {
		m    ConnectionMode
		want string
	}{
		{ModeDirect, "Direct"},
		{ModeNAT, "NAT"},
		{ModeForward, "Forward"},
		{ModeAP, "AP"},
		{ConnectionMode(0), "ConnectionMode(0)"},
		{ConnectionMode(9), "ConnectionMode(9)"},
	}
	for _, tt := range tests {
		if got := tt.m.String(); got != tt.want {
			t.Errorf("ConnectionMode(%d).String() = %q, want %q", uint8(tt.m), got, tt.want)
		}
	}
}
