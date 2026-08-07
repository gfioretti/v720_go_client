package domain

import "testing"

func intp(v int) *int { return &v }

func TestParseDeviceMessage_DeviceInfoSnapshot(t *testing.T) {
	// The snapshot the camera sends right after the handshake (content code 4).
	payload := []byte(`{"code":502,"content":{"code":4,"devPower":100,"IrLed":1,"speedGrade":1,"mirrorFlip":0,"wifiName":"Francesinhas","version":"202503081631"}}`)
	st, ok := ParseDeviceMessage(payload)
	if !ok {
		t.Fatal("ParseDeviceMessage ok=false, want true for a device-info message")
	}
	if st.SpeedGrade == nil || *st.SpeedGrade != 1 {
		t.Errorf("SpeedGrade = %v, want 1", st.SpeedGrade)
	}
	if st.IrLed == nil || *st.IrLed != 1 {
		t.Errorf("IrLed = %v, want 1", st.IrLed)
	}
	if st.DevPower == nil || *st.DevPower != 100 {
		t.Errorf("DevPower = %v, want 100", st.DevPower)
	}
	if st.WifiName != "Francesinhas" {
		t.Errorf("WifiName = %q", st.WifiName)
	}
	if st.Version != "202503081631" {
		t.Errorf("Version = %q", st.Version)
	}
	// A field the message didn't carry stays nil.
	if st.MoveAlert != nil {
		t.Errorf("MoveAlert = %v, want nil (absent)", st.MoveAlert)
	}
}

func TestParseDeviceMessage_Rejects(t *testing.T) {
	tests := []struct {
		name    string
		payload string
	}{
		{"stream-state start", `{"code":502,"content":{"code":3}}`},
		{"stream-state stop", `{"code":502,"content":{"code":0}}`},
		{"not 502", `{"code":501,"status":200}`},
		{"no content", `{"code":502}`},
		{"malformed", `{not json`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, ok := ParseDeviceMessage([]byte(tt.payload)); ok {
				t.Errorf("ok=true, want false for %s", tt.name)
			}
		})
	}
}

func TestDeviceState_Merge(t *testing.T) {
	base := DeviceState{SpeedGrade: intp(1), IrLed: intp(1), WifiName: "cam", DevPower: intp(90)}
	// A later partial update: HD on, motion alert on; omits everything else.
	base.Merge(DeviceState{SpeedGrade: intp(2), MoveAlert: intp(1)})

	if base.SpeedGrade == nil || *base.SpeedGrade != 2 {
		t.Errorf("SpeedGrade = %v, want 2 (updated)", base.SpeedGrade)
	}
	if base.MoveAlert == nil || *base.MoveAlert != 1 {
		t.Errorf("MoveAlert = %v, want 1 (added)", base.MoveAlert)
	}
	// Fields the update omitted must be preserved, not cleared.
	if base.IrLed == nil || *base.IrLed != 1 {
		t.Errorf("IrLed = %v, want 1 (preserved)", base.IrLed)
	}
	if base.WifiName != "cam" {
		t.Errorf("WifiName = %q, want preserved", base.WifiName)
	}
	if base.DevPower == nil || *base.DevPower != 90 {
		t.Errorf("DevPower = %v, want 90 (preserved)", base.DevPower)
	}
}
