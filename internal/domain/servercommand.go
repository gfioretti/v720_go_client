package domain

// Server-side control messages: what the self-hosted server SENDS to a camera
// that has connected out to it in STA mode (findings doc §6; cross-referenced
// against a9-v720's v720_sta.py). These are the mirror of the client's coordinator
// messages — the camera plays the role the app's device does, and the server
// plays naxclow's coordinator. a9-v720 is an earlier firmware revision, so the
// exact bring-up sequence is provisional pending real-camera verification.

// NewRegisterAck is the server's reply to the camera's register request (code
// 100): {"code":101,"status":200} (CODE_S2_REGISTER_RSP). Sent bare (type-0
// JSON), not 301-wrapped.
func NewRegisterAck() Command {
	return Command{Code: CodeRelayRegisterStatus, Params: map[string]any{"status": StatusOK}}
}

// NewDeviceStatus tells the camera the server considers it online/offline:
// {"code":53,"status":1|0} (CODE_S2_DEVICE_STATUS). Sent bare.
func NewDeviceStatus(online bool) Command {
	status := 0
	if online {
		status = 1
	}
	return Command{Code: CodeRelayDevicePresence, Params: map[string]any{"status": status}}
}

// NewBaseInfoRequest asks the camera for its base info (battery, version,
// settings): the 301-forward wrapper around {"code":4,"unixTimer":…} (inner code
// 4 = CODE_FORWARD_DEV_BASE_INFO), addressed to target. The camera replies with
// its device-state snapshot, after which the server starts the stream.
func NewBaseInfoRequest(target string, unixTimer int64) Command {
	return NewRelayDeviceCommand(target, Command{
		Code:   CodeBaseInfo,
		Params: map[string]any{"unixTimer": unixTimer},
	})
}

// NewRelayStreamControl is the relay/server-side stream control: the 301-forward
// wrapper around {"code":<state>} (e.g. StreamStart=3 = CODE_FORWARD_OPEN_A_OPEN_V
// to begin streaming, StreamStop=0 to end). This is the server-to-camera analogue
// of the AP-mode NewStreamControl (which uses the 502 wrapper instead).
func NewRelayStreamControl(target string, state StreamState) Command {
	return NewRelayDeviceCommand(target, Command{Code: Code(state)})
}
