package application

import (
	"context"
	"errors"
	"fmt"

	"v720_go_client/internal/domain"
)

// VideoFrameHandler receives live video frame payloads. The confirmed payload is
// MJPEG (a JPEG image per frame, doc §12.4). It is a driven port: the caller
// (CLI, recorder, GUI) implements it. A returned error stops live view.
type VideoFrameHandler interface {
	OnVideoFrame(data []byte) error
}

// AudioFrameHandler receives live audio frame payloads, tagged with the codec
// (domain.FrameTypeAudioG711 or domain.FrameTypeAudioPCM, doc §12.4). A returned
// error stops live view.
type AudioFrameHandler interface {
	OnAudioFrame(codec domain.FrameType, data []byte) error
}

// StartLiveView starts the AP-mode live stream and consumes it, dispatching
// inbound media frames to the handlers until the caller cancels ctx or an error
// occurs. It must be called after a successful Connect.
//
// It first SENDS a stream-start command — {"code":502,"content":{"code":3,
// "devTarget":...}}, i.e. the app's setLiveStreamMode(devId, 3). This is
// required: the device streams nothing on its own after the handshake. An earlier
// reading held that AP-mode live view auto-started (the type-115 trigger being
// the request to stream); a real-camera capture disproved it — the handshake
// completed but zero frames arrived until this command was sent. The app itself
// sends it in response to the device's post-handshake ready notification; we send
// it proactively once Connect has finished, which over the ordered TCP stream
// reaches the device after the time-sync.
//
// Either handler may be nil, in which case frames of that kind are dropped (e.g.
// pass a nil AudioFrameHandler for a video-only consumer). Non-media frames —
// JSON control/stream-state notifications, keepalive pongs, no-ops — are ignored.
//
// Cancelling ctx is the normal way to stop; StartLiveView then returns nil and
// the caller closes the transport to end the stream (which is also how the app
// stops AP-mode live view). Any other outcome (transport failure, or a handler
// returning an error) is returned wrapped.
func (s *APSession) StartLiveView(ctx context.Context, video VideoFrameHandler, audio AudioFrameHandler) error {
	if err := s.sendControl(ctx, domain.NewStreamControl(s.devID, domain.StreamStart)); err != nil {
		return fmt.Errorf("live view: start stream: %w", err)
	}

	for {
		// Observe cancellation between frames independently of Receive. A
		// Transport may deliver an already-buffered frame in preference to
		// reporting ctx (see the Transport.Receive contract); under a steady
		// stream a frame is almost always ready, so relying on Receive alone
		// could leave a cancelled StartLiveView spinning until the stream stops.
		if err := ctx.Err(); err != nil {
			return nil
		}

		f, err := s.transport.Receive(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil // caller asked to stop
			}
			return fmt.Errorf("live view: receive: %w", err)
		}

		switch {
		case f.Type.IsVideo():
			if video != nil {
				if err := video.OnVideoFrame(f.Payload); err != nil {
					return fmt.Errorf("live view: video handler: %w", err)
				}
			}
		case f.Type.IsAudio():
			if audio != nil {
				if err := audio.OnAudioFrame(f.Type, f.Payload); err != nil {
					return fmt.Errorf("live view: audio handler: %w", err)
				}
			}
		default:
			// Control/keepalive/stream-state notifications during streaming —
			// nothing to dispatch.
			//
			// Outbound keepalive is deliberately NOT implemented yet: doc §12.3
			// step 9 describes a writer-idle ping plus a tag-101 pong response,
			// but the client ping tag (and the inbound tag that should trigger
			// the pong) are unconfirmed (§12.8) and must not be guessed into the
			// wire protocol. A long write-idle stream may therefore be dropped by
			// the camera until a live capture pins the tag down.
		}
	}
}
