package domain

import (
	"encoding/json"
	"maps"
)

// Code is the JSON "code" field carried inside a FrameTypeJSON (tag 0) payload.
// It is the application-level message type within a JSON control frame and is a
// different namespace from the FrameType packet tag. The concrete values come
// from the doc tables §12.5 (device control), §12.6 (SD-card/file transfer),
// and §12.7 (connection/auth).
type Code uint16

// Device control commands (findings doc §12.5). On both AP-mode and P2P/relay
// these carry the same codes/params; the mode-specific envelope (AP-mode's
// {"code":502,"content":{...}} vs relay's {"code":301,...}) is applied by the
// session layer, not here.
//
// Only codes whose purpose the doc states outright are named. Several other
// confirmed integers stay unnamed until we need them; their meanings are now
// resolved via the a9-v720 cross-reference (doc §7.1) but pending re-verification
// against v3.2.8: 207 sdcard-format, 208 ap-mode, 210 instLed, 211 wifi-scan,
// 212 motorState, 216 mirrorFlip, 217 sfq, 218 sdcard-status, 219 rf433,
// 299 reboot. Add them here as features need them.
const (
	CodeVideoQuality  Code = 201 // param: speedGrade (int) — video quality/HD grade
	CodeIRLed         Code = 202 // param: IrLed (int) — IR LED on/off
	CodeSDRecordMode  Code = 203 // param: sdMoveMode (int) — SD motion-detect recording mode
	CodeSetWifi       Code = 204 // params: s (ssid), p (password) — join a home Wi-Fi (STA provisioning)
	CodeMotionAlert   Code = 205 // param: moveAlert (int) — motion-detection alert toggle
	CodeMoveGrade     Code = 206 // param: moveGrade (int) — PTZ move grade/sensitivity
	CodeWhiteLED      Code = 220 // param: ledEI (int) — white/status LED state
	CodeSetApPassword Code = 221 // param: setApPwd (string) — set the camera's AP-mode WiFi password
	CodeSwitchCamera  Code = 222 // param: camId (int) — switch camera (dual-lens devices)
	CodeSendSDP       Code = 230 // params: devTarget, sdp — AP-mode only, no-op on relay
)

// SD-card / file transfer (findings doc §12.6). The same integers are used on
// AP-mode (addressed with target=devId) and P2P/relay (addressed with a
// forwardId). CodeSDCardRegister has no AP-mode equivalent.
const (
	CodeSDCardDateList   Code = 400 // out: request SD-card date list
	CodeSDCardFileList   Code = 402 // out: request file-name list for a date
	CodeSDCardFileSize   Code = 403 // in:  pending XML file size, ahead of type-8 chunks
	CodeSDCardFileInfo   Code = 404 // out: request AVI file info
	CodeSDCardStartAVI   Code = 406 // out: start AVI file stream (playback)
	CodeSDCardStopAVI    Code = 408 // out: stop AVI file stream
	CodeSDCardDeleteFile Code = 412 // out: delete media file
	CodeAlbumInfo        Code = 414 // out: request photo-album info (AP-mode only)
	CodeAlbumPage        Code = 416 // out: request album pictures for a page (AP-mode only)
	CodeAlbumStop        Code = 418 // out: stop album show (AP-mode only)
	CodeSDCardRegister   Code = 110 // out: register the SD-card TCP connection (relay only)
)

// Connection / auth (findings doc §12.7). AP-mode handshake and control.
const (
	CodeTimeSync  Code = 4   // out: params devTarget, unixTimer — time-sync reply
	CodeAuthorize Code = 500 // out: param sk — password/authorize check
	CodeConnect   Code = 501 // out: params target, token, unixTimer — connect/register
	CodeApCommand Code = 502 // out: param content (nested JSON) — live-stream state wrapper

	// CodeBaseInfo requests/carries the device base-info snapshot
	// (CODE_FORWARD_DEV_BASE_INFO, a9-v720). It shares the integer 4 with
	// CodeTimeSync — the protocol reuses code 4 for both; context (a bare
	// time-sync reply vs a 301-wrapped device-command) disambiguates.
	CodeBaseInfo Code = 4

	// StatusOK is the "status" value the protocol uses for success on the
	// coordinator/register acks (e.g. {"code":101,"status":200}).
	StatusOK = 200
)

// Relay-mode TCP coordinator channel (findings doc §12.2 phase 1, the
// NaxclowServerChannel). These codes coordinate the P2P/cloud-relay handshake:
// register against the rendezvous server, obtain forward IDs, exchange candidate
// addresses for NAT traversal, then confirm registration. CodeTimeSync (4) is
// shared with AP-mode (§12.2 step 9). Credential-field mapping is per §12.1a:
// the register "uid" is the app account ID, "token" is the device pwd, "devToken"
// (on codes 10/30/52) is tarPwd.
const (
	CodeRelayReqConnect       Code = 10  // out: report client candidate addresses (reqConnect, TCP variant)
	CodeRelayDeviceCandidates Code = 13  // in:  status + devIp/devPort/devNatIp/devNatPort
	CodeRelayReqForwardID     Code = 30  // out: request a live forward ID (reqForwardId)
	CodeRelayForwardID        Code = 31  // in:  status + forwardId (live)
	CodeRelayReqPlaybackID    Code = 36  // out: request an SD-card/playback forward ID (reqPlaybackId)
	CodeRelayPlaybackID       Code = 37  // in:  status + forwardId (playback)
	CodeRelayDeviceStatus     Code = 52  // out: complete registration (reqSendDeviceStatus)
	CodeRelayDevicePresence   Code = 53  // in:  status 0 offline / 1 online
	CodeRelayRegister         Code = 100 // out: register on the coordinator (reqRegister)
	CodeRelayRegisterStatus   Code = 101 // in:  status 200 ok / 500 fail (P2P register result)
	CodeRelayHousekeeping     Code = 298 // in:  housekeeping message relayed on fresh connection
	CodeRelayForward          Code = 301 // both: device-command wrapper (sendForwardCommand)
)

// Relay-mode UDP media channel (findings doc §12.2 phase 2, the
// NaxclowLiveChannel). This channel has its own code sub-namespace for candidate
// probing and quality telemetry. Keepalive uses packet type tag 102
// (FrameTypePongRelay), which is a FrameType, not a Code.
const (
	CodeMediaCandidate     Code = 21 // in:  ip/port candidate to probe
	CodeMediaProbeReq      Code = 50 // out: data-channel probe request (server→camera, C2D_PROBE_REQ)
	CodeMediaProbeOK       Code = 51 // in:  status 200 — probe succeeded / camera's probe response (D2C_PROBE_RSP)
	CodeMediaQualityReport Code = 54 // out: timerStamp — periodic quality telemetry
	CodeMediaQualityEcho   Code = 55 // in:  timerStamp — drives RTT/loss measurement
)

// Command is a device-control value object: a JSON control message identified by
// a Code plus its parameters. It is pure domain — JSON renders the message body
// but performs no I/O and knows nothing of the AP-mode/relay envelope that wraps
// it. Construct the common commands with the New* helpers below, or build one
// directly for codes without a dedicated constructor.
type Command struct {
	Code   Code
	Params map[string]any
}

// JSON renders the command as its on-wire body, {"code":N, ...params}, suitable
// for a FrameTypeJSON (tag 0) payload. The Code field is authoritative: a "code"
// key in Params never shadows it. Key order is encoding/json's deterministic
// alphabetical sort and is irrelevant to the device.
func (c Command) JSON() ([]byte, error) {
	m := make(map[string]any, len(c.Params)+1)
	maps.Copy(m, c.Params)
	m["code"] = uint16(c.Code)
	return json.Marshal(m)
}

// NewVideoQuality sets the video quality / HD grade (doc §12.5 code 201).
func NewVideoQuality(speedGrade int) Command {
	return Command{Code: CodeVideoQuality, Params: map[string]any{"speedGrade": speedGrade}}
}

// NewIRLed sets the IR LED state (doc §12.5 code 202).
func NewIRLed(irLed int) Command {
	return Command{Code: CodeIRLed, Params: map[string]any{"IrLed": irLed}}
}

// NewMotionAlert toggles motion-detection alerts (doc §12.5 code 205).
func NewMotionAlert(moveAlert int) Command {
	return Command{Code: CodeMotionAlert, Params: map[string]any{"moveAlert": moveAlert}}
}

// NewMoveGrade sets the PTZ move grade/sensitivity (doc §12.5 code 206).
func NewMoveGrade(moveGrade int) Command {
	return Command{Code: CodeMoveGrade, Params: map[string]any{"moveGrade": moveGrade}}
}

// NewWhiteLED sets the white/status LED state (doc §12.5 code 220).
func NewWhiteLED(ledEI int) Command {
	return Command{Code: CodeWhiteLED, Params: map[string]any{"ledEI": ledEI}}
}

// NewSwitchCamera switches the active camera on dual-lens devices (doc §12.5 code 222).
func NewSwitchCamera(camID int) Command {
	return Command{Code: CodeSwitchCamera, Params: map[string]any{"camId": camID}}
}

// NewSetWifi tells the camera to join a home Wi-Fi network (STA-mode
// provisioning): code 204 with s=ssid, p=password (doc §12.5, resolved via the
// a9-v720 cross-reference — its set_wifi sends exactly {"code":204,"s":…,"p":…}).
// Like every AP device command it is 502-wrapped and carries devTarget when sent
// (NewAPDeviceCommand). After the camera accepts it, it reboots onto the target
// network and its AP-mode connection drops — so this is fire-and-forget; do not
// wait for a reply on the AP socket.
func NewSetWifi(ssid, password string) Command {
	return Command{Code: CodeSetWifi, Params: map[string]any{"s": ssid, "p": password}}
}

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

// NewMediaQualityReport builds the periodic quality-telemetry message the client
// sends on the UDP media channel (findings doc §12.2 phase 2 step 3):
//
//	{"code":54,"timerStamp":"<millis>"}
//
// timerStamp is a millisecond timestamp carried as a STRING (the doc shows it
// quoted); callers format it, e.g. strconv.FormatInt(ms, 10). The device echoes
// it back in a {"code":55,...} message to drive its RTT/frame-loss measurement.
func NewMediaQualityReport(timerStamp string) Command {
	return Command{Code: CodeMediaQualityReport, Params: map[string]any{
		"timerStamp": timerStamp,
	}}
}

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
