package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"v720_go_client/internal/adapters/transport/relay"
	"v720_go_client/internal/domain"
)

// mediaHub fans reassembled JPEG frames out to any number of HTTP viewers. A slow
// viewer drops frames rather than stalling ingest.
type mediaHub struct {
	mu   sync.Mutex
	subs map[chan []byte]struct{}
}

func newMediaHub() *mediaHub { return &mediaHub{subs: make(map[chan []byte]struct{})} }

func (h *mediaHub) publish(jpeg []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- jpeg:
		default: // viewer is behind; drop this frame for it
		}
	}
}

func (h *mediaHub) subscribe() chan []byte {
	ch := make(chan []byte, 1)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *mediaHub) unsubscribe(ch chan []byte) {
	h.mu.Lock()
	delete(h.subs, ch)
	close(ch)
	h.mu.Unlock()
}

// handler streams the live video as multipart/x-mixed-replace MJPEG — open it in
// a browser (or ffplay) to watch.
func (h *mediaHub) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ch := h.subscribe()
		defer h.unsubscribe(ch)

		w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary=frame")
		flusher, _ := w.(http.Flusher)
		// Flush the stream headers immediately so the viewer starts rendering
		// before the first frame arrives.
		if flusher != nil {
			flusher.Flush()
		}
		for {
			select {
			case <-r.Context().Done():
				return
			case jpeg := <-ch:
				if _, err := fmt.Fprintf(w, "--frame\r\nContent-Type: image/jpeg\r\nContent-Length: %d\r\n\r\n", len(jpeg)); err != nil {
					return
				}
				if _, err := w.Write(jpeg); err != nil {
					return
				}
				if _, err := w.Write([]byte("\r\n")); err != nil {
					return
				}
				if flusher != nil {
					flusher.Flush()
				}
			}
		}
	}
}

// runMedia ingests the camera's UDP media: it answers the data-channel probe,
// reassembles fragmented frames, ACKs each completed video frame, and publishes
// the JPEG to viewers. It returns when the media server is closed or ctx is done.
func runMedia(ctx context.Context, srv *relay.MediaServer, hub *mediaHub, verbose bool, logf func(string, ...any)) {
	var re domain.MediaReassembler
	var frameIDs []uint32 // package ids accumulated for the current frame's ACK
	var videoFrames, audioFrames uint64

	for {
		f, addr, err := srv.Receive()
		if err != nil {
			if ctx.Err() == nil {
				logf("media: receive stopped: %v", err)
			}
			return
		}

		// Data-channel probe: the camera sends code 51, we answer code 50.
		if f.Type == domain.FrameTypeJSON {
			if isProbe(f) {
				if verbose {
					logf("media: probe from %s, answering", addr)
				}
				if reply, err := probeReply(); err == nil {
					_ = srv.Send(reply, addr)
				}
			}
			continue
		}

		// Track package ids for the ACK, scoped to the current *video* frame:
		// reset at each HEAD, and don't accumulate audio ids (which would grow
		// unbounded and mis-attribute to a later video frame's ACK).
		if f.Type == domain.FrameTypeMJPEG {
			if f.Flag == domain.MsgFlagHead {
				frameIDs = frameIDs[:0]
			}
			frameIDs = append(frameIDs, f.Sequence)
		}

		frame, ok := re.Push(f)
		if !ok {
			continue
		}
		if frame.Kind == domain.MediaVideo {
			hub.publish(frame.Data)
			_ = srv.Send(domain.NewRetransmitConfirm(frameIDs), addr)
			frameIDs = frameIDs[:0]
			videoFrames++
			if verbose && (videoFrames == 1 || videoFrames%30 == 0) {
				logf("media: %d video frames (last %d bytes), %d audio frames", videoFrames, len(frame.Data), audioFrames)
			}
		} else {
			// Audio frames are reassembled but not yet sinked.
			audioFrames++
		}
	}
}

func isProbe(f domain.Frame) bool {
	var m struct {
		Code domain.Code `json:"code"`
	}
	return json.Unmarshal(f.Payload, &m) == nil && m.Code == domain.CodeMediaProbeOK
}

func probeReply() (domain.Frame, error) {
	body, err := domain.Command{Code: domain.CodeMediaProbeReq}.JSON()
	if err != nil {
		return domain.Frame{}, err
	}
	return domain.Frame{Type: domain.FrameTypeJSON, Payload: body}, nil
}
