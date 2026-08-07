package domain

import "encoding/json"

// DeviceState is the subset of the camera's reported settings the client tracks
// for display. The device pushes these inside 502-wrapped control messages: a
// near-complete snapshot right after the handshake (content code 4), then
// incremental updates as individual settings change.
//
// Integer fields are pointers so "not reported" (nil) is distinct from a real
// zero value — that lets successive partial messages be merged without a later
// message that omits a field clobbering an earlier known value. The JSON tags
// match the camera's own field names so a message's content unmarshals straight
// into this struct, and the same struct serialises back out for a UI.
type DeviceState struct {
	SpeedGrade *int   `json:"speedGrade,omitempty"` // video grade (201): 1=SD, 2=HD
	IrLed      *int   `json:"IrLed,omitempty"`      // IR LED (202)
	MoveAlert  *int   `json:"moveAlert,omitempty"`  // motion-detection alert (205)
	LedEI      *int   `json:"ledEI,omitempty"`      // white/status LED (220)
	MirrorFlip *int   `json:"mirrorFlip,omitempty"` // image mirror/flip
	DevPower   *int   `json:"devPower,omitempty"`   // battery percentage
	WifiName   string `json:"wifiName,omitempty"`   // camera's Wi-Fi SSID
	Version    string `json:"version,omitempty"`    // firmware version
}

// ParseDeviceMessage extracts a DeviceState from a raw inbound JSON control frame
// payload. It reports ok=true only for AP-mode device messages — a 502 wrapper
// (CodeApCommand) whose inner content is a settings/status message, i.e. not one
// of the stream-state notifications (inner code 0–3, handled elsewhere). For any
// other frame it returns ok=false and callers should ignore it.
func ParseDeviceMessage(payload []byte) (DeviceState, bool) {
	var env struct {
		Code    int             `json:"code"`
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(payload, &env); err != nil {
		return DeviceState{}, false
	}
	if env.Code != int(CodeApCommand) || len(env.Content) == 0 {
		return DeviceState{}, false
	}
	var inner struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(env.Content, &inner); err != nil {
		return DeviceState{}, false
	}
	// 0–3 are live-stream state notifications (stop / audio-video toggles /
	// start), not device settings.
	if inner.Code >= 0 && inner.Code <= 3 {
		return DeviceState{}, false
	}
	var st DeviceState
	if err := json.Unmarshal(env.Content, &st); err != nil {
		return DeviceState{}, false
	}
	return st, true
}

// Merge overlays the non-empty fields of other onto d, so a stream of partial
// device messages accumulates into the latest known full state.
func (d *DeviceState) Merge(other DeviceState) {
	if other.SpeedGrade != nil {
		d.SpeedGrade = other.SpeedGrade
	}
	if other.IrLed != nil {
		d.IrLed = other.IrLed
	}
	if other.MoveAlert != nil {
		d.MoveAlert = other.MoveAlert
	}
	if other.LedEI != nil {
		d.LedEI = other.LedEI
	}
	if other.MirrorFlip != nil {
		d.MirrorFlip = other.MirrorFlip
	}
	if other.DevPower != nil {
		d.DevPower = other.DevPower
	}
	if other.WifiName != "" {
		d.WifiName = other.WifiName
	}
	if other.Version != "" {
		d.Version = other.Version
	}
}
