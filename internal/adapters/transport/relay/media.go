// Package relay holds the secondary/driven adapters for P2P/cloud-relay mode: the
// UDP live media channel and the Transport bundle that groups the three relay
// channels (TCP coordinator + UDP media + TCP storage) behind the
// application.RelayTransport port. The two TCP channels reuse the shared
// framedtcp adapter; only the UDP media channel is relay-specific.
package relay

import (
	"context"
	"fmt"
	"net"
	"sync"

	"v720_go_client/internal/application"
	"v720_go_client/internal/domain"
)

// MediaChannel is the UDP live audio/video channel (the app's NaxclowLiveChannel
// — see CLAUDE.md naming map). Unlike the TCP channels, UDP already preserves
// datagram boundaries, so each datagram carries exactly one whole frame: the same
// 20-byte header layout written directly into the payload rather than a
// length-prefixed stream (findings doc §12.2a). A background read loop decodes
// datagrams into a buffered channel so Receive can honour context
// cancellation/deadlines without abandoning a blocked socket read; it satisfies
// application.RelayChannel (Send/Receive) plus Close for the owning Transport.
//
// NOTE: the async phase-2 orchestration that drives this channel (candidate
// probing, telemetry, keepalive, fragment reassembly) is deferred until a real
// capture exists — see docs/mode-a-client-design.md. This adapter provides only
// the raw datagram-to-Frame transport.
type MediaChannel struct {
	conn net.Conn

	frames     chan domain.Frame
	readerDone chan struct{}
	closed     chan struct{}
	closeOnce  sync.Once

	errMu   sync.Mutex
	readErr error
}

// DialMedia opens a connected UDP socket to addr (host:port) and starts serving
// frames. ctx bounds only the dial itself; the returned MediaChannel outlives it.
//
// Because the socket is *connected*, it only accepts datagrams from addr. That is
// fine for a single settled media address, but the deferred phase-2 candidate
// probing (doc §12.2 phase 2 step 2) may need to hear from several candidate
// addresses before one wins — its orchestration cannot simply hand this adapter a
// single address up front; it must settle the address first (or use an
// unconnected socket) before this connected transport applies.
func DialMedia(ctx context.Context, addr string) (*MediaChannel, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "udp", addr)
	if err != nil {
		return nil, fmt.Errorf("relay media: dial %s: %w", addr, err)
	}
	c := &MediaChannel{
		conn:       conn,
		frames:     make(chan domain.Frame, 64),
		readerDone: make(chan struct{}),
		closed:     make(chan struct{}),
	}
	go c.readLoop()
	return c, nil
}

// Send encodes the frame into a single UDP datagram and writes it. It honours a
// ctx already cancelled on entry and returns application.ErrTransportClosed once
// the channel is closed. Unlike framedtcp.Send it does not apply ctx's deadline
// to the write: a UDP sendto on a connected socket does not block on the peer, so
// there is no stalled write to bound.
func (c *MediaChannel) Send(ctx context.Context, f domain.Frame) error {
	if c.isClosed() {
		return application.ErrTransportClosed
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := c.conn.Write(f.Encode()); err != nil {
		if c.isClosed() {
			return application.ErrTransportClosed
		}
		return fmt.Errorf("relay media: send: %w", err)
	}
	return nil
}

// Receive returns the next inbound frame, honouring ctx (returning ctx.Err()),
// returning application.ErrTransportClosed after our own Close, and surfacing the
// underlying I/O error if the socket fails.
func (c *MediaChannel) Receive(ctx context.Context) (domain.Frame, error) {
	select {
	case f := <-c.frames:
		return f, nil
	default:
	}
	select {
	case f := <-c.frames:
		return f, nil
	case <-ctx.Done():
		return domain.Frame{}, ctx.Err()
	case <-c.closed:
		return domain.Frame{}, application.ErrTransportClosed
	case <-c.readerDone:
		select {
		case f := <-c.frames:
			return f, nil
		default:
		}
		return domain.Frame{}, c.finalErr()
	}
}

// Close shuts the channel down. Safe to call more than once.
func (c *MediaChannel) Close() error {
	c.closeOnce.Do(func() {
		close(c.closed)
		_ = c.conn.Close()
	})
	return nil
}

func (c *MediaChannel) readLoop() {
	defer close(c.readerDone)
	// One datagram per read; size the buffer to a whole maximum frame.
	buf := make([]byte, domain.MaxFrameLength)
	for {
		n, err := c.conn.Read(buf)
		if err != nil {
			c.errMu.Lock()
			c.readErr = err
			c.errMu.Unlock()
			return
		}
		f, err := domain.Decode(buf[:n])
		if err != nil {
			// A malformed datagram is dropped rather than tearing the channel down:
			// UDP can deliver stray/corrupt packets, and unlike a TCP stream there
			// is no framing to resync — the next datagram stands on its own.
			continue
		}
		select {
		case c.frames <- f:
		case <-c.closed:
			return
		}
	}
}

func (c *MediaChannel) isClosed() bool {
	select {
	case <-c.closed:
		return true
	default:
		return false
	}
}

func (c *MediaChannel) finalErr() error {
	if c.isClosed() {
		return application.ErrTransportClosed
	}
	c.errMu.Lock()
	defer c.errMu.Unlock()
	if c.readErr != nil {
		return fmt.Errorf("relay media: receive: %w", c.readErr)
	}
	return application.ErrTransportClosed
}
