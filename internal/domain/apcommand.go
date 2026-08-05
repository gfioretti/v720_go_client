package domain

import "maps"

// NewAPDeviceCommand wraps a device-control command (doc §12.5) in the AP-mode
// envelope the camera actually expects:
//
//	{"code":502,"content":{"code":N,"devTarget":"<devTarget>",...params}}
//
// Verified against the decompiled source: every AP device command in OooOOO0
// builds a flat map {"devTarget",..., "code", <param>} and hands it to
// NaxclowApChannel.sendApCommand, which wraps it as {"code":502,"content":...}.
// The doc's original §12.5 table omitted both the 502 wrapper and devTarget.
//
// The inner code and devTarget are authoritative — a stray "code"/"devTarget"
// key in inner.Params cannot shadow them.
func NewAPDeviceCommand(devTarget string, inner Command) Command {
	content := make(map[string]any, len(inner.Params)+2)
	maps.Copy(content, inner.Params)
	content["code"] = uint16(inner.Code)
	content["devTarget"] = devTarget
	return Command{Code: CodeApCommand, Params: map[string]any{"content": content}}
}
