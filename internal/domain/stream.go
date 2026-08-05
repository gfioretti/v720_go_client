package domain

// StreamState is the inner "code" the device reports inside a CodeApCommand (502)
// content wrapper to signal live-stream state (findings doc §12.3 step 8). These
// are INBOUND notifications only: the AP-mode client does not send them to
// control streaming (live view auto-starts after the handshake, and stops when
// the client closes the connection — see application.APSession.StartLiveView).
// They exist here so a future inbound handler can interpret a stream-state
// notification the device pushes. This is a separate namespace from the
// top-level JSON Code and from the device-control content codes.
type StreamState int

const (
	StreamStop                StreamState = 0 // stop the stream
	StreamOpenAudioCloseVideo StreamState = 1 // open audio, close video
	StreamCloseAudioOpenVideo StreamState = 2 // close audio, open video
	StreamStart               StreamState = 3 // start the stream
)
