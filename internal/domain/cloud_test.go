package domain

import "testing"

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
