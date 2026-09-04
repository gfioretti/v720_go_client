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
// Only codes whose purpose the doc states outright are named. Several confirmed
// integers are deliberately omitted until their purpose or real value is pinned
// down: 204 (params s/p/url present but purpose unclear per §12.5), 207/211/223/
// 224 (paramless, purpose unclear per §12.5), 299 (likely
// disconnect/unregister — inferred, not confirmed), and the jadx-obscured
// codes instLed/motorState/setRf433Dev/sfq plus two paramless variants, whose
// real integers need an Android Studio lookup (doc §7.1). Do not guess these.
const (
	CodeVideoQuality  Code = 201 // param: speedGrade (int) — video quality/HD grade
	CodeIRLed         Code = 202 // param: IrLed (int) — IR LED on/off
	CodeSDRecordMode  Code = 203 // param: sdMoveMode (int) — SD motion-detect recording mode
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
	CodeMediaProbeOK       Code = 51 // in:  status 200 — probe succeeded
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
