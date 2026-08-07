package application

import (
	"context"
	"errors"
	"fmt"

	"v720_go_client/internal/domain"
)

// VideoFrameHandler receives one complete MJPEG image per call (a full JPEG,
// SOI…EOI). StartLiveView reassembles these from the fragmented wire frames
// before dispatching — the camera splits each image across several
// FrameTypeMJPEG frames (doc §12.4), so a handler never sees a partial image. It
// is a driven port: the caller (CLI, recorder, GUI) implements it. A returned
// error stops live view.
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
// JSON control/stream-state notifications, keepalive pongs, no-ops — are ignored,
// except that inbound device-settings messages are delivered to the handler set
// via SetDeviceStateHandler (if any), so a UI can reflect the live device state.
//
// Cancelling ctx is the normal way to stop; StartLiveView then returns nil and
// the caller closes the transport to end the stream (which is also how the app
// stops AP-mode live view). Any other outcome (transport failure, or a handler
// returning an error) is returned wrapped.
func (s *APSession) StartLiveView(ctx context.Context, video VideoFrameHandler, audio AudioFrameHandler) error {
	if err := s.sendControl(ctx, domain.NewStreamControl(s.devID, domain.StreamStart)); err != nil {
		return fmt.Errorf("live view: start stream: %w", err)
	}

	// video arrives as fragments; reassemble whole JPEGs before dispatching.
	var video2jpeg domain.MJPEGReassembler

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
			// Feed the fragment even when video is nil is pointless, so gate on it;
			// each fragment may complete zero, one, or several images.
			if video != nil {
				for _, img := range video2jpeg.Append(f.Payload) {
					if err := video.OnVideoFrame(img); err != nil {
						return fmt.Errorf("live view: video handler: %w", err)
					}
				}
			}
		case f.Type.IsAudio():
			if audio != nil {
				if err := audio.OnAudioFrame(f.Type, f.Payload); err != nil {
					return fmt.Errorf("live view: audio handler: %w", err)
				}
			}
		default:
			// Surface the camera's own settings/status messages (502-wrapped
			// device messages) to a registered observer, so a UI can reflect the
			// live device state. Other control/keepalive frames have nothing to
			// dispatch.
			if s.onDeviceState != nil && f.Type == domain.FrameTypeJSON {
				if st, ok := domain.ParseDeviceMessage(f.Payload); ok {
					s.onDeviceState(st)
				}
			}
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
