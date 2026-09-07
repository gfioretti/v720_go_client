package relay

import (
	"bytes"
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"v720_go_client/internal/application"
	"v720_go_client/internal/domain"
)

// Compile-time proof the UDP adapter satisfies the channel contract the bundle
// needs (and hence application.RelayChannel).
var _ channel = (*MediaChannel)(nil)

func sameFrame(a, b domain.Frame) bool {
	return a.Type == b.Type && a.Flag == b.Flag && a.Unknown == b.Unknown &&
		a.Opaque == b.Opaque && a.Sequence == b.Sequence && bytes.Equal(a.Payload, b.Payload)
}

// dialTestMedia starts a loopback UDP peer, dials a MediaChannel to it, and
// returns the client channel plus the peer PacketConn so a test can play the
// device. The client's address is learned from its first datagram.
func dialTestMedia(t *testing.T) (*MediaChannel, net.PacketConn) {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen udp: %v", err)
	}
	c, err := DialMedia(context.Background(), pc.LocalAddr().String())
	if err != nil {
		_ = pc.Close()
		t.Fatalf("DialMedia: %v", err)
	}
	t.Cleanup(func() {
		_ = c.Close()
		_ = pc.Close()
	})
	return c, pc
}

func TestMediaChannel_SendEncodesFrameToDatagram(t *testing.T) {
	c, peer := dialTestMedia(t)

	want := domain.Frame{Type: domain.FrameTypeAudioG711, Sequence: 7, Payload: []byte("audio")}
	if err := c.Send(context.Background(), want); err != nil {
		t.Fatalf("Send: %v", err)
	}

	buf := make([]byte, domain.MaxFrameLength)
	_ = peer.SetReadDeadline(time.Now().Add(time.Second))
	n, _, err := peer.ReadFrom(buf)
	if err != nil {
		t.Fatalf("peer ReadFrom: %v", err)
	}
	got, err := domain.Decode(buf[:n])
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !sameFrame(got, want) {
		t.Fatalf("peer received %+v, want %+v", got, want)
	}
}

func TestMediaChannel_ReceiveDecodesDatagram(t *testing.T) {
	c, peer := dialTestMedia(t)

	// Send one datagram so the peer learns the client's address.
	if err := c.Send(context.Background(), domain.Frame{Type: domain.FrameTypeJSON}); err != nil {
		t.Fatalf("priming Send: %v", err)
	}
	buf := make([]byte, domain.MaxFrameLength)
	_ = peer.SetReadDeadline(time.Now().Add(time.Second))
	_, client, err := peer.ReadFrom(buf)
	if err != nil {
		t.Fatalf("peer ReadFrom: %v", err)
	}

	want := domain.Frame{Type: domain.FrameTypeMJPEG, Sequence: 42, Payload: []byte("\xff\xd8jpeg\xff\xd9")}
	if _, err := peer.WriteTo(want.Encode(), client); err != nil {
		t.Fatalf("peer WriteTo: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got, err := c.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if !sameFrame(got, want) {
		t.Fatalf("Receive = %+v, want %+v", got, want)
	}
}

// A malformed datagram is dropped, not fatal: the next valid datagram still
// arrives.
func TestMediaChannel_DropsMalformedDatagram(t *testing.T) {
	c, peer := dialTestMedia(t)

	if err := c.Send(context.Background(), domain.Frame{Type: domain.FrameTypeJSON}); err != nil {
		t.Fatalf("priming Send: %v", err)
	}
	buf := make([]byte, domain.MaxFrameLength)
	_ = peer.SetReadDeadline(time.Now().Add(time.Second))
	_, client, err := peer.ReadFrom(buf)
	if err != nil {
		t.Fatalf("peer ReadFrom: %v", err)
	}

	// Garbage (too short for a header), then a valid frame.
	if _, err := peer.WriteTo([]byte("xx"), client); err != nil {
		t.Fatalf("peer WriteTo garbage: %v", err)
	}
	want := domain.Frame{Type: domain.FrameTypeAudioPCM, Sequence: 1, Payload: []byte("pcm")}
	if _, err := peer.WriteTo(want.Encode(), client); err != nil {
		t.Fatalf("peer WriteTo valid: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got, err := c.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if !sameFrame(got, want) {
		t.Fatalf("Receive = %+v, want %+v (malformed datagram not dropped?)", got, want)
	}
}

// A datagram whose header length field disagrees with the bytes present is
// rejected by Decode and dropped, like any other malformed datagram.
func TestMediaChannel_DropsLengthMismatchDatagram(t *testing.T) {
	c, peer := dialTestMedia(t)

	if err := c.Send(context.Background(), domain.Frame{Type: domain.FrameTypeJSON}); err != nil {
		t.Fatalf("priming Send: %v", err)
	}
	buf := make([]byte, domain.MaxFrameLength)
	_ = peer.SetReadDeadline(time.Now().Add(time.Second))
	_, client, err := peer.ReadFrom(buf)
	if err != nil {
		t.Fatalf("peer ReadFrom: %v", err)
	}

	// A well-formed frame's bytes, but with the length field overstating the
	// payload, then a valid frame.
	bad := domain.Frame{Type: domain.FrameTypeMJPEG, Payload: []byte("abc")}.Encode()
	bad[0] = 100 // claim 100 payload bytes when only 3 are present
	if _, err := peer.WriteTo(bad, client); err != nil {
		t.Fatalf("peer WriteTo bad: %v", err)
	}
	want := domain.Frame{Type: domain.FrameTypeAudioPCM, Sequence: 2, Payload: []byte("ok")}
	if _, err := peer.WriteTo(want.Encode(), client); err != nil {
		t.Fatalf("peer WriteTo valid: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got, err := c.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if !sameFrame(got, want) {
		t.Fatalf("Receive = %+v, want %+v (length-mismatch datagram not dropped?)", got, want)
	}
}

func TestMediaChannel_ReceiveHonoursContextDeadline(t *testing.T) {
	c, _ := dialTestMedia(t)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := c.Receive(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Receive error = %v, want context.DeadlineExceeded", err)
	}
}

func TestMediaChannel_CloseUnblocksReceive(t *testing.T) {
	c, _ := dialTestMedia(t)

	go func() {
		time.Sleep(20 * time.Millisecond)
		_ = c.Close()
	}()
	if _, err := c.Receive(context.Background()); !errors.Is(err, application.ErrTransportClosed) {
		t.Fatalf("Receive after Close = %v, want ErrTransportClosed", err)
	}
}

func TestMediaChannel_SendAfterCloseFails(t *testing.T) {
	c, _ := dialTestMedia(t)
	_ = c.Close()
	if err := c.Send(context.Background(), domain.Frame{}); !errors.Is(err, application.ErrTransportClosed) {
		t.Fatalf("Send after Close = %v, want ErrTransportClosed", err)
	}
}

// Receive on an already-closed channel (closed before the call, not during a
// blocked read) returns the sentinel.
func TestMediaChannel_ReceiveAfterCloseFails(t *testing.T) {
	c, _ := dialTestMedia(t)
	_ = c.Close()
	if _, err := c.Receive(context.Background()); !errors.Is(err, application.ErrTransportClosed) {
		t.Fatalf("Receive after Close = %v, want ErrTransportClosed", err)
	}
}

// Close is idempotent: the second call is a no-op and does not panic (the bundle
// relies on this).
func TestMediaChannel_CloseIsIdempotent(t *testing.T) {
	c, _ := dialTestMedia(t)
	if err := c.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}
