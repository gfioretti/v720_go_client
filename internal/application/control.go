package application

import (
	"context"

	"v720_go_client/internal/domain"
)

// SendCommand sends a device-control command (doc §12.5 — PTZ, IR LED, video
// quality, etc.) to the camera over the AP-mode connection. The command is
// wrapped in the AP-mode envelope with this session's devId as devTarget
// (domain.NewAPDeviceCommand → {"code":502,"content":{"code":N,"devTarget":...}}).
// It must be called after a successful Connect.
//
// It is fire-and-forget: it returns once the frame has been written, and does
// not wait for or parse any device response. Whether the device acknowledges a
// control command (and in what shape) is unconfirmed (doc §12.8); a future slice
// can add response handling once that is pinned down against real hardware.
func (s *APSession) SendCommand(ctx context.Context, cmd domain.Command) error {
	return s.sendControl(ctx, domain.NewAPDeviceCommand(s.devID, cmd))
}
