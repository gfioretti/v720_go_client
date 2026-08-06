package cli

import (
	"context"

	"v720_go_client/internal/application"
	"v720_go_client/internal/domain"
)

// debugPayloadMax caps how many bytes of a JSON control payload the debug log
// prints, so a large control message can't flood the terminal.
const debugPayloadMax = 512

// debugTransport wraps a Transport and logs every frame sent and received. It is
// an observability decorator at the wire boundary: the application layer stays
// unaware of logging, and it only activates when the user passes --debug. Binary
// media payloads (video/audio/AVI) are logged by type and length only, never
// dumped; JSON control frames log their (truncated) body, which is the useful
// part when diagnosing a real-camera handshake.
type debugTransport struct {
	inner application.Transport
	logf  func(format string, args ...any)
}

// Compile-time proof the decorator still satisfies the port.
var _ application.Transport = (*debugTransport)(nil)

func newDebugTransport(inner application.Transport, logf func(string, ...any)) *debugTransport {
	return &debugTransport{inner: inner, logf: logf}
}

func (d *debugTransport) Send(ctx context.Context, f domain.Frame) error {
	d.logf("debug: -> SEND %s len=%d seq=%d%s\n", f.Type, len(f.Payload), f.Sequence, frameDetail(f))
	err := d.inner.Send(ctx, f)
	if err != nil {
		d.logf("debug: -> SEND error: %v\n", err)
	}
	return err
}

func (d *debugTransport) Receive(ctx context.Context) (domain.Frame, error) {
	f, err := d.inner.Receive(ctx)
	if err != nil {
		d.logf("debug: <- RECV error: %v\n", err)
		return f, err
	}
	d.logf("debug: <- RECV %s len=%d seq=%d%s\n", f.Type, len(f.Payload), f.Sequence, frameDetail(f))
	return f, nil
}

func (d *debugTransport) Close() error {
	return d.inner.Close()
}

// frameDetail returns a short suffix for the log line: the JSON body for JSON
// control frames (truncated to debugPayloadMax), and nothing for media/binary
// frames — their payloads are not human-readable and can be large.
func frameDetail(f domain.Frame) string {
	if f.Type != domain.FrameTypeJSON || len(f.Payload) == 0 {
		return ""
	}
	body := string(f.Payload)
	if len(body) > debugPayloadMax {
		body = body[:debugPayloadMax] + "...(truncated)"
	}
	return " " + body
}
