package domain

import "maps"

// NewRelayDeviceCommand wraps a device-control command (doc §12.5) in the
// P2P/cloud-relay envelope the coordinator channel expects:
//
//	{"code":301,"target":"<devTarget>","content":{"code":N,...params}}
//
// This is the relay analogue of AP-mode's 502 wrapper (see NewAPDeviceCommand),
// read from the app's NaxclowServerChannel.sendForwardCommand (doc §12.2 step
// 10). Note the structural difference from AP-mode: here "target" sits at the
// OUTER level alongside code 301, and "content" is the bare inner command — the
// inner map carries no devTarget of its own. (AP-mode instead pushes devTarget
// INSIDE content.)
//
// The outer code and target are authoritative — stray "code"/"target" keys in
// inner.Params cannot shadow them — and the caller's Params map is never mutated.
func NewRelayDeviceCommand(devTarget string, inner Command) Command {
	content := make(map[string]any, len(inner.Params)+1)
	maps.Copy(content, inner.Params)
	content["code"] = uint16(inner.Code)
	return Command{Code: CodeRelayForward, Params: map[string]any{
		"target":  devTarget,
		"content": content,
	}}
}
