// Package application holds the V720 client's use cases and the ports
// (interfaces) they depend on. It depends only on the domain layer and on the
// ports it defines for itself — never on concrete adapters. Dependency
// direction is strictly inward: adapters → application → domain.
package application

import (
	"context"
	"errors"

	"github.com/gfioretti/v720_go_client/internal/domain"
)

// ErrTransportClosed is returned by a Transport whose connection has been closed,
// so callers can distinguish a deliberate shutdown from other I/O failures with
// errors.Is.
var ErrTransportClosed = errors.New("application: transport closed")

// Transport is the driven/secondary port a Session implementation uses to move
// domain.Frames to and from the network, without caring whether that is
// AP-mode's single TCP connection or relay-mode's TCP-coordinator-plus-UDP-media
// split (findings doc §12.1). It is defined here, in the package that consumes
// it; adapters satisfy it structurally, by implementing these methods, and never
// import this package.
//
// The transport deals only in whole frames: framing a TCP byte stream into
// individual frames (via the length-prefix header, doc §12.3a) is the adapter's
// responsibility, so every value crossing this port is a complete frame.
type Transport interface {
	// Send transmits one frame. It honours ctx for cancellation/deadlines and
	// returns ErrTransportClosed if the transport has been closed.
	Send(ctx context.Context, f domain.Frame) error

	// Receive blocks until the next inbound frame is available, then returns it.
	// It honours ctx for cancellation/deadlines (returning ctx.Err()) when no
	// frame is ready. Implementations may, however, return an already-buffered
	// frame in preference to reporting cancellation, so a caller looping over a
	// steady stream should also check ctx itself to observe cancellation
	// promptly. It returns ErrTransportClosed once the transport is closed and no
	// more frames will arrive.
	Receive(ctx context.Context) (domain.Frame, error)

	// Close shuts the transport down. It is safe to call more than once; the
	// first call performs the shutdown and subsequent calls are no-ops.
	Close() error
}
