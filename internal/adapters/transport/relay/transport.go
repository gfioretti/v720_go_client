package relay

import (
	"context"
	"errors"
	"io"

	"github.com/gfioretti/v720_go_client/internal/adapters/transport/framedtcp"
	"github.com/gfioretti/v720_go_client/internal/application"
)

// channel is what the bundle needs from each relay channel: the application's
// whole-frame RelayChannel contract plus a Closer, so Transport.Close can shut
// them all down. Both *framedtcp.Channel (TCP coordinator/storage) and
// *MediaChannel (UDP) satisfy it.
type channel interface {
	application.RelayChannel
	io.Closer
}

// Transport bundles the three relay-mode channels behind the
// application.RelayTransport port (findings doc §12.1/§12.2): a TCP coordinator,
// a UDP media channel, and a TCP storage channel.
type Transport struct {
	coordinator channel
	media       channel
	storage     channel
}

// Compile-time proof the bundle satisfies the port.
var _ application.RelayTransport = (*Transport)(nil)

// NewTransport bundles three already-dialed channels.
//
// NOTE on dial ordering: the media and storage channels' addresses are only known
// after the phase-1 coordinator handshake (the device candidates / forward IDs),
// so in a live session the coordinator is dialed first, phase 1 runs, and only
// then are media/storage dialed and this bundle constructed. That orchestration
// is part of the deferred phase-2 wiring (see docs/mode-a-client-design.md); this
// constructor just groups the channels and owns their shared lifecycle.
func NewTransport(coordinator, media, storage channel) *Transport {
	return &Transport{coordinator: coordinator, media: media, storage: storage}
}

// DialCoordinator and DialStorage open the two TCP relay channels via the shared
// framedtcp adapter (the framing is identical across channels, doc §12.2a). They
// are thin helpers so callers need not import framedtcp directly.
func DialCoordinator(ctx context.Context, addr string) (*framedtcp.Channel, error) {
	return framedtcp.Dial(ctx, addr)
}

func DialStorage(ctx context.Context, addr string) (*framedtcp.Channel, error) {
	return framedtcp.Dial(ctx, addr)
}

func (t *Transport) Coordinator() application.RelayChannel { return t.coordinator }
func (t *Transport) Media() application.RelayChannel       { return t.media }
func (t *Transport) Storage() application.RelayChannel     { return t.storage }

// Close shuts down all three channels, returning any errors joined together so a
// failure on one still attempts the others.
func (t *Transport) Close() error {
	return errors.Join(
		closeIf(t.coordinator),
		closeIf(t.media),
		closeIf(t.storage),
	)
}

// closeIf closes c when present, tolerating a nil channel (e.g. a bundle built
// before media/storage were dialed).
func closeIf(c channel) error {
	if c == nil {
		return nil
	}
	return c.Close()
}
