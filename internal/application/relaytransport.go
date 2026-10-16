package application

import (
	"context"

	"github.com/gfioretti/v720_go_client/internal/domain"
)

// RelayChannel moves whole domain.Frames over one relay-mode channel. It is the
// Transport contract minus Close — the owning RelayTransport controls lifecycle
// for all channels at once. Framing a TCP byte stream (or reading a UDP
// datagram) into individual frames is the adapter's job, so every value crossing
// this port is a complete frame, exactly as with Transport.
type RelayChannel interface {
	// Send transmits one frame, honouring ctx and returning ErrTransportClosed
	// once the owning transport is closed.
	Send(ctx context.Context, f domain.Frame) error

	// Receive blocks until the next inbound frame arrives, honouring ctx. It
	// returns ErrTransportClosed once closed and no more frames will arrive. As
	// with Transport.Receive, an implementation may return an already-buffered
	// frame in preference to reporting cancellation, so a steady-stream caller
	// should also check ctx itself.
	Receive(ctx context.Context) (domain.Frame, error)
}

// RelayTransport is the driven/secondary port a RelaySession uses to reach the
// three channels of P2P/cloud-relay mode (findings doc §12.1/§12.2): a TCP
// coordinator (registration/coordination), a UDP media channel (live A/V), and a
// TCP storage channel (SD-card/playback). Bundling them behind one port — rather
// than three bare Transports — makes relay's shape explicit and gives the session
// a single Close for the whole session's worth of sockets.
//
// It is defined here, in the package that consumes it; adapters satisfy it
// structurally and never import this package. The AP-mode single-channel
// Transport port is separate and unaffected.
type RelayTransport interface {
	// Coordinator is the TCP control/coordination channel — phase 1 of the
	// handshake and, thereafter, device commands wrapped in code 301 (doc §12.2
	// phase 1).
	Coordinator() RelayChannel

	// Media is the UDP live audio/video channel — phase 2 (doc §12.2 phase 2).
	Media() RelayChannel

	// Storage is the TCP SD-card/playback channel. Wired for completeness; only
	// exercised once SD-card-over-relay is built.
	Storage() RelayChannel

	// Close shuts down all three channels. It is safe to call more than once.
	Close() error
}
