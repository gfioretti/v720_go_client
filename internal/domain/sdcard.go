package domain

// SD-card / recording browse & playback command builders (findings doc §12.6).
//
// Unlike device-control commands (§12.5), these are NOT wrapped in the AP-mode
// 502/content envelope: NaxclowApChannel builds each as a flat map and sends it
// directly (sendProtoBeanWithTimeout), so the wire form is a bare type-0 JSON
// frame {"code":N,"target":"<devId>",...}. The `target` field carries the devId
// (the same value device-control commands put in `devTarget`). In P2P/relay mode
// these use a forwardId instead — a session-layer concern not modelled here.
//
// `date` is an integer (e.g. 20260805); `hours` and `minute` are integers
// identifying a recording within the catalogue (see the catalogue XML parser).

// NewSDCardDateList requests the recording catalogue / date list (code 400).
func NewSDCardDateList(target string) Command {
	return Command{Code: CodeSDCardDateList, Params: map[string]any{"target": target}}
}

// NewSDCardFileList requests the file/recording list for one date (code 402).
func NewSDCardFileList(target string, date int) Command {
	return Command{Code: CodeSDCardFileList, Params: map[string]any{
		"target": target,
		"date":   date,
	}}
}

// NewSDCardFileInfo requests info for the AVI recording at date/hours/minute
// (code 404).
func NewSDCardFileInfo(target string, date, hours, minute int) Command {
	return Command{Code: CodeSDCardFileInfo, Params: recordingParams(target, date, hours, minute)}
}

// NewSDCardStartAVI starts streaming the AVI recording at date/hours/minute
// (code 406).
func NewSDCardStartAVI(target string, date, hours, minute int) Command {
	return Command{Code: CodeSDCardStartAVI, Params: recordingParams(target, date, hours, minute)}
}

// NewSDCardStopAVI stops the current AVI playback stream (code 408).
func NewSDCardStopAVI(target string) Command {
	return Command{Code: CodeSDCardStopAVI, Params: map[string]any{"target": target}}
}

// recordingParams builds the field set shared by the per-recording commands
// (404/406/412): target + date + hours + minute.
func recordingParams(target string, date, hours, minute int) map[string]any {
	return map[string]any{
		"target": target,
		"date":   date,
		"hours":  hours,
		"minute": minute,
	}
}
