package main

import (
	"context"
	"encoding/json"
	"net/http"

	"v720_go_client/internal/domain"
)

// logRequests wraps an http.Handler to log every request line — useful for
// seeing exactly which boot REST calls the camera makes (and in what order).
func logRequests(h http.Handler, logf func(string, ...any)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logf("REST %s %s", r.Method, r.URL.Path)
		h.ServeHTTP(w, r)
	})
}

// loggingChannel decorates a coordinator RelayChannel to log every frame in and
// out (with the JSON code when applicable), so the bring-up FSM's exchange with
// the camera is visible under --verbose.
type loggingChannel struct {
	inner interface {
		Send(context.Context, domain.Frame) error
		Receive(context.Context) (domain.Frame, error)
		Close() error
	}
	logf func(string, ...any)
}

func (c *loggingChannel) Send(ctx context.Context, f domain.Frame) error {
	logFrame(c.logf, "TX", f)
	return c.inner.Send(ctx, f)
}

func (c *loggingChannel) Receive(ctx context.Context) (domain.Frame, error) {
	f, err := c.inner.Receive(ctx)
	if err == nil {
		logFrame(c.logf, "RX", f)
	}
	return f, err
}

func (c *loggingChannel) Close() error { return c.inner.Close() }

// logFrame logs one coordinator frame, decoding the JSON "code" when the frame
// carries a JSON payload.
func logFrame(logf func(string, ...any), dir string, f domain.Frame) {
	if f.Type == domain.FrameTypeJSON && len(f.Payload) > 0 {
		var m struct {
			Code domain.Code `json:"code"`
		}
		if json.Unmarshal(f.Payload, &m) == nil {
			logf("coord %s JSON code=%d (%d bytes)", dir, m.Code, len(f.Payload))
			return
		}
	}
	logf("coord %s type=%s (%d bytes)", dir, f.Type, len(f.Payload))
}
