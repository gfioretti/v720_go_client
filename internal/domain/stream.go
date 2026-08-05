package domain

// StreamState is the inner "code" carried inside a CodeApCommand (502) content
// wrapper to signal live-stream state (findings doc §12.3 step 8). It is a
// separate namespace from the top-level JSON Code: these values live in the
// nested "content" object, not the outer message.
type StreamState int

const (
	StreamStop                StreamState = 0 // stop the stream
	StreamOpenAudioCloseVideo StreamState = 1 // open audio, close video
	StreamCloseAudioOpenVideo StreamState = 2 // close audio, open video
	StreamStart               StreamState = 3 // start the stream
)

// NewStreamControl builds the AP-mode live-stream state command
// {"code":502,"content":{"code":<state>}} (doc §12.3 step 8, sendApCommand): a
// CodeApCommand whose content nests the StreamState. In relay mode the same
// content rides inside a 301 wrapper instead; that envelope is a session-layer
// concern, so this builder only produces the AP-mode form.
func NewStreamControl(state StreamState) Command {
	return Command{Code: CodeApCommand, Params: map[string]any{
		"content": map[string]any{"code": int(state)},
	}}
}
