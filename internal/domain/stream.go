package domain

// StreamState is the inner "code" carried inside a CodeApCommand (502) content
// wrapper to control or report live-stream state (findings doc §12.3 step 8).
//
// These are BIDIRECTIONAL, not inbound-only as an earlier static reading claimed:
//   - The client SENDS StreamStart to begin live view and StreamStop to end it.
//     Confirmed against the decompiled app: OooOOO0.OooOO0o(int,String) builds
//     {"devTarget","code":<state>} and hands it to sendApCommand (the 502
//     wrapper); the UI calls setLiveStreamMode(devId, 3) to start streaming
//     (ApRotateDeviceActivity/UdpDeviceActivity) and setLiveStreamMode(devId, 0)
//     to stop (VideoPlaybackActivity). Build the outbound command with
//     NewStreamControl.
//   - The device also SENDS these back as notifications: inner 3 → stream started,
//     0 → stopped, 1 → "open audio, close video", 2 → "close audio, open video".
//
// This is a separate namespace from the top-level JSON Code and from the
// device-control content codes.
type StreamState int

const (
	StreamStop                StreamState = 0 // stop the stream
	StreamOpenAudioCloseVideo StreamState = 1 // open audio, close video
	StreamCloseAudioOpenVideo StreamState = 2 // close audio, open video
	StreamStart               StreamState = 3 // start the stream
)

// NewStreamControl builds the AP-mode live-stream control command the client
// sends to start or stop streaming:
//
//	{"code":502,"content":{"code":<state>,"devTarget":"<devTarget>"}}
//
// Verified against the decompiled source (OooOOO0.OooOO0o(int,String) →
// NaxclowApChannel.sendApCommand): pass StreamStart to begin live view — the app
// sends exactly this in response to the device's post-handshake ready
// notification — and StreamStop to end it. This supersedes the doc's earlier
// "stream-state codes are inbound-only / live view auto-starts" reading, which a
// real-camera capture disproved (the device streams nothing until it receives
// this command).
func NewStreamControl(devTarget string, state StreamState) Command {
	return Command{Code: CodeApCommand, Params: map[string]any{
		"content": map[string]any{
			"code":      uint16(state),
			"devTarget": devTarget,
		},
	}}
}
