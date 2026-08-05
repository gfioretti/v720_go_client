// Package aptcp is the secondary/driven adapter for AP-mode: the single TCP
// connection (the app's NaxclowApChannel — see CLAUDE.md naming map) over which
// everything, including live A/V, flows when the client is on the camera's own
// Nax_* Wi-Fi hotspot. Channel implements the application.Transport port,
// turning the raw byte stream into whole domain.Frames and back using the
// 20-byte length-prefix header framing of doc §12.3a.
package aptcp

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"v720_go_client/internal/application"
	"v720_go_client/internal/domain"
)

// readBufferSize comfortably spans a maximum-size frame so header+payload reads
// batch into few syscalls; it bounds buffering only, not correctness.
const readBufferSize = 16 * 1024

// Channel is a live AP-mode TCP transport. A background goroutine continuously
// decodes inbound frames into a channel so Receive can honour context
// deadlines/cancellation without abandoning a blocked socket read. Writes are
// serialised by a mutex. It is safe for concurrent use.
type Channel struct {
	conn net.Conn

	frames     chan domain.Frame
	readerDone chan struct{} // closed when the read loop exits
	closed     chan struct{} // closed by Close()
	closeOnce  sync.Once

	writeMu sync.Mutex

	errMu   sync.Mutex
	readErr error // the error that stopped the read loop, if any
}

// Dial opens a TCP connection to addr (host:port) and starts serving frames.
// ctx bounds only the dial itself; the returned Channel outlives it.
func Dial(ctx context.Context, addr string) (*Channel, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("aptcp: dial %s: %w", addr, err)
	}
	c := &Channel{
		conn:       conn,
		frames:     make(chan domain.Frame, 64),
		readerDone: make(chan struct{}),
		closed:     make(chan struct{}),
	}
	go c.readLoop()
	return c, nil
}

// Send encodes the frame and writes it to the socket. It honours a ctx deadline
// (via the socket write deadline) and a ctx already cancelled on entry, and
// returns application.ErrTransportClosed if the channel is (or becomes, via our
// own Close) closed. Note: a ctx with no deadline that is cancelled *during* a
// blocked write is not interrupted mid-write — an inherent net.Conn limitation;
// pass a deadline if you need to bound a stalled write.
func (c *Channel) Send(ctx context.Context, f domain.Frame) error {
	if c.isClosed() {
		return application.ErrTransportClosed
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	data := f.Encode()

	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if dl, ok := ctx.Deadline(); ok {
		_ = c.conn.SetWriteDeadline(dl)
		defer func() { _ = c.conn.SetWriteDeadline(time.Time{}) }()
	}
	if _, err := c.conn.Write(data); err != nil {
		if c.isClosed() {
			return application.ErrTransportClosed
		}
		return fmt.Errorf("aptcp: send: %w", err)
	}
	return nil
}

// Receive returns the next inbound frame, blocking until one arrives. It honours
// ctx (returning ctx.Err()), returns application.ErrTransportClosed after our own
// Close, and surfaces the underlying I/O error if the peer/socket fails.
func (c *Channel) Receive(ctx context.Context) (domain.Frame, error) {
	// Prefer delivering an already-buffered frame over reporting termination.
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
		// The read loop stopped (I/O error or our Close). Drain any frame it
		// delivered just before exiting, otherwise report the terminal error.
		select {
		case f := <-c.frames:
			return f, nil
		default:
		}
		return domain.Frame{}, c.finalErr()
	}
}

// Close shuts the channel down. Safe to call more than once; the first call
// closes the socket and unblocks Receive.
func (c *Channel) Close() error {
	c.closeOnce.Do(func() {
		close(c.closed)
		_ = c.conn.Close()
	})
	return nil
}

func (c *Channel) readLoop() {
	defer close(c.readerDone)
	br := bufio.NewReaderSize(c.conn, readBufferSize)
	for {
		f, err := readFrame(br)
		if err != nil {
			c.errMu.Lock()
			c.readErr = err
			c.errMu.Unlock()
			return
		}
		select {
		case c.frames <- f:
		case <-c.closed:
			return
		}
	}
}

// readFrame reads exactly one length-prefixed frame from r (doc §12.3a): the
// 20-byte header, whose first 4 bytes give the payload length, then that many
// payload bytes, then decodes the whole thing.
func readFrame(r *bufio.Reader) (domain.Frame, error) {
	buf := make([]byte, domain.HeaderSize)
	if _, err := io.ReadFull(r, buf); err != nil {
		return domain.Frame{}, err
	}
	payloadLen := binary.LittleEndian.Uint32(buf[:4])
	// Compare in uint32 (not int) so a hostile/desync length near 0xFFFFFFFF
	// can't wrap negative on a 32-bit build and slip past the guard into a bad
	// allocation. The bound is a small positive constant.
	if payloadLen > uint32(domain.MaxFrameLength-domain.HeaderSize) {
		return domain.Frame{}, fmt.Errorf("aptcp: frame payload length %d exceeds maximum", payloadLen)
	}
	if payloadLen > 0 {
		full := make([]byte, domain.HeaderSize+int(payloadLen))
		copy(full, buf)
		if _, err := io.ReadFull(r, full[domain.HeaderSize:]); err != nil {
			return domain.Frame{}, err
		}
		buf = full
	}
	return domain.Decode(buf)
}

func (c *Channel) isClosed() bool {
	select {
	case <-c.closed:
		return true
	default:
		return false
	}
}

// finalErr reports why Receive can no longer return frames: our own Close maps to
// ErrTransportClosed; otherwise it wraps the read loop's terminal I/O error.
func (c *Channel) finalErr() error {
	if c.isClosed() {
		return application.ErrTransportClosed
	}
	c.errMu.Lock()
	defer c.errMu.Unlock()
	if c.readErr != nil {
		return fmt.Errorf("aptcp: receive: %w", c.readErr)
	}
	return application.ErrTransportClosed
}
