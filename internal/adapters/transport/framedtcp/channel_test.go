package framedtcp

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/gfioretti/v720_go_client/internal/application"
	"github.com/gfioretti/v720_go_client/internal/domain"
)

// Compile-time proof the adapter satisfies the port it implements.
var _ application.Transport = (*Channel)(nil)

// dialTestChannel starts a loopback TCP listener, dials a Channel to it, and
// returns the client Channel plus the accepted server-side conn so a test can
// play the device.
func dialTestChannel(t *testing.T) (*Channel, net.Conn) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	type accepted struct {
		conn net.Conn
		err  error
	}
	accepts := make(chan accepted, 1)
	go func() {
		conn, err := ln.Accept()
		accepts <- accepted{conn, err}
	}()

	c, err := Dial(context.Background(), ln.Addr().String())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	a := <-accepts
	if a.err != nil {
		t.Fatalf("accept: %v", a.err)
	}
	t.Cleanup(func() {
		_ = c.Close()
		_ = a.conn.Close()
	})
	return c, a.conn
}

// readStreamFrame reads one length-prefixed frame off a raw stream, mirroring
// the wire framing (doc §12.3a) so a test's mock device can consume what the
// Channel sent.
func readStreamFrame(t *testing.T, r io.Reader) domain.Frame {
	t.Helper()
	h := make([]byte, domain.HeaderSize)
	if _, err := io.ReadFull(r, h); err != nil {
		t.Fatalf("read header: %v", err)
	}
	n := binary.LittleEndian.Uint32(h[:4])
	buf := make([]byte, domain.HeaderSize+int(n))
	copy(buf, h)
	if _, err := io.ReadFull(r, buf[domain.HeaderSize:]); err != nil {
		t.Fatalf("read payload: %v", err)
	}
	f, err := domain.Decode(buf)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return f
}

func sameFrame(a, b domain.Frame) bool {
	return a.Type == b.Type && a.Flag == b.Flag && a.Unknown == b.Unknown &&
		a.Opaque == b.Opaque && a.Sequence == b.Sequence && bytes.Equal(a.Payload, b.Payload)
}

func TestChannel_SendEncodesFrameToStream(t *testing.T) {
	c, server := dialTestChannel(t)

	want := domain.Frame{Type: domain.FrameTypeJSON, Sequence: 3, Payload: []byte(`{"code":501}`)}
	if err := c.Send(context.Background(), want); err != nil {
		t.Fatalf("Send: %v", err)
	}

	got := readStreamFrame(t, server)
	if !sameFrame(got, want) {
		t.Fatalf("device received %+v, want %+v", got, want)
	}
}

// The core adapter responsibility: reassemble frames from an arbitrarily chunked
// TCP byte stream, including a header split across reads and two frames coalesced
// into one write.
func TestChannel_ReassemblesFramesFromChunkedStream(t *testing.T) {
	c, server := dialTestChannel(t)

	f1 := domain.Frame{Type: domain.FrameTypeJSON, Payload: []byte(`{"a":1}`)}
	f2 := domain.Frame{Type: domain.FrameTypeMJPEG, Sequence: 9, Flag: -4, Payload: []byte("jpegdata")}
	stream := append(f1.Encode(), f2.Encode()...)

	// Dribble the bytes out in awkward chunks whose boundaries fall mid-header
	// (3), mid-f1-payload (24, since f1's payload spans bytes [20,27)), and
	// mid-f2 (past the first frame), so neither header nor payload aligns to a
	// write boundary.
	go func() {
		for _, cut := range [][2]int{{0, 3}, {3, 24}, {24, len(stream)}} {
			_, _ = server.Write(stream[cut[0]:cut[1]])
			time.Sleep(time.Millisecond)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	got1, err := c.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive f1: %v", err)
	}
	got2, err := c.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive f2: %v", err)
	}
	if !sameFrame(got1, f1) {
		t.Errorf("frame 1 = %+v, want %+v", got1, f1)
	}
	if !sameFrame(got2, f2) {
		t.Errorf("frame 2 = %+v, want %+v", got2, f2)
	}
}

func TestChannel_ReceiveHonoursContextDeadline(t *testing.T) {
	c, _ := dialTestChannel(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := c.Receive(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Receive error = %v, want context.DeadlineExceeded", err)
	}
}

func TestChannel_CloseUnblocksReceiveWithSentinel(t *testing.T) {
	c, _ := dialTestChannel(t)

	errCh := make(chan error, 1)
	go func() {
		_, err := c.Receive(context.Background())
		errCh <- err
	}()
	time.Sleep(5 * time.Millisecond)
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("second Close should be a no-op: %v", err)
	}

	select {
	case err := <-errCh:
		if !errors.Is(err, application.ErrTransportClosed) {
			t.Fatalf("Receive after Close = %v, want ErrTransportClosed", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Receive did not unblock after Close")
	}
}

func TestChannel_SendAfterCloseFails(t *testing.T) {
	c, _ := dialTestChannel(t)
	_ = c.Close()
	err := c.Send(context.Background(), domain.Frame{Type: domain.FrameTypeNoop})
	if !errors.Is(err, application.ErrTransportClosed) {
		t.Fatalf("Send after Close = %v, want ErrTransportClosed", err)
	}
}

func TestChannel_RemoteCloseSurfacesToReceive(t *testing.T) {
	c, server := dialTestChannel(t)
	_ = server.Close() // device hangs up

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := c.Receive(ctx)
	// A clean remote hangup surfaces as io.EOF, distinct from our own Close
	// (ErrTransportClosed) and from a timeout — that mapping is the contract.
	if !errors.Is(err, io.EOF) {
		t.Fatalf("Receive after remote close = %v, want io.EOF", err)
	}
	if errors.Is(err, application.ErrTransportClosed) {
		t.Fatalf("Receive error = %v, must not be ErrTransportClosed on a peer hangup", err)
	}
}

// The trickiest concurrency path: a good frame already buffered when the read
// loop then hits a terminal error must still be delivered before the error.
func TestChannel_DrainsBufferedFrameBeforeTerminalError(t *testing.T) {
	c, server := dialTestChannel(t)

	good := domain.Frame{Type: domain.FrameTypeJSON, Sequence: 42, Payload: []byte(`{"ok":1}`)}
	_, _ = server.Write(good.Encode())
	// Then a header claiming an impossible length → the read loop errors out.
	var bad [domain.HeaderSize]byte
	binary.LittleEndian.PutUint32(bad[:4], 0xFFFFFFFF)
	_, _ = server.Write(bad[:])

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	got, err := c.Receive(ctx)
	if err != nil {
		t.Fatalf("first Receive should return the buffered good frame, got err: %v", err)
	}
	if !sameFrame(got, good) {
		t.Fatalf("first Receive = %+v, want %+v", got, good)
	}
	if _, err := c.Receive(ctx); err == nil {
		t.Fatal("second Receive after terminal error returned nil")
	}
}

func TestChannel_ReceiveRejectsOversizeFrame(t *testing.T) {
	c, server := dialTestChannel(t)

	// A header claiming a payload beyond MaxFrameLength must not be honoured
	// (protocol desync / hostile length), and must not trigger a huge allocation.
	var h [domain.HeaderSize]byte
	binary.LittleEndian.PutUint32(h[:4], uint32(domain.MaxFrameLength))
	go func() { _, _ = server.Write(h[:]) }()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := c.Receive(ctx); err == nil {
		t.Fatal("Receive accepted an oversize frame")
	}
}
