package relay

import (
	"net"
	"testing"
	"time"

	"github.com/gfioretti/v720_go_client/internal/domain"
)

// The server receives a camera's framed datagram (learning its address) and can
// send an ACK back to it.
func TestMediaServer_ReceiveAndAck(t *testing.T) {
	srv, err := ListenMedia("127.0.0.1:0")
	if err != nil {
		t.Fatalf("ListenMedia: %v", err)
	}
	defer func() { _ = srv.Close() }()

	// A "camera" socket that streams to the server.
	cam, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("camera listen: %v", err)
	}
	defer func() { _ = cam.Close() }()

	video := domain.Frame{Type: domain.FrameTypeMJPEG, Flag: domain.MsgFlagEnd, Sequence: 7, Payload: []byte("jpegdata")}
	if _, err := cam.WriteTo(video.Encode(), srv.LocalAddr()); err != nil {
		t.Fatalf("camera send: %v", err)
	}

	got, addr, err := srv.Receive()
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if got.Type != domain.FrameTypeMJPEG || got.Sequence != 7 || string(got.Payload) != "jpegdata" {
		t.Fatalf("received %+v", got)
	}

	// ACK back to the learned camera address.
	if err := srv.Send(domain.NewRetransmitConfirm([]uint32{7}), addr); err != nil {
		t.Fatalf("Send ack: %v", err)
	}
	buf := make([]byte, domain.MaxFrameLength)
	_ = cam.SetReadDeadline(time.Now().Add(time.Second))
	n, _, err := cam.ReadFrom(buf)
	if err != nil {
		t.Fatalf("camera read ack: %v", err)
	}
	ack, err := domain.Decode(buf[:n])
	if err != nil {
		t.Fatalf("decode ack: %v", err)
	}
	if ack.Type != domain.FrameTypeRetransmitConfirm {
		t.Fatalf("ack type = %d, want 605", ack.Type)
	}
}

// A malformed datagram is skipped; the next valid frame still arrives.
func TestMediaServer_SkipsMalformed(t *testing.T) {
	srv, err := ListenMedia("127.0.0.1:0")
	if err != nil {
		t.Fatalf("ListenMedia: %v", err)
	}
	defer func() { _ = srv.Close() }()

	cam, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("camera listen: %v", err)
	}
	defer func() { _ = cam.Close() }()

	if _, err := cam.WriteTo([]byte("xx"), srv.LocalAddr()); err != nil { // too short for a header
		t.Fatalf("camera send garbage: %v", err)
	}
	valid := domain.Frame{Type: domain.FrameTypeAudioG711, Flag: domain.MsgFlagFinish, Payload: []byte("pcm")}
	if _, err := cam.WriteTo(valid.Encode(), srv.LocalAddr()); err != nil {
		t.Fatalf("camera send valid: %v", err)
	}

	got, _, err := srv.Receive()
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if got.Type != domain.FrameTypeAudioG711 {
		t.Fatalf("received %+v, want the valid frame (malformed not skipped?)", got)
	}
}

// Close unblocks a pending Receive.
func TestMediaServer_CloseUnblocksReceive(t *testing.T) {
	srv, err := ListenMedia("127.0.0.1:0")
	if err != nil {
		t.Fatalf("ListenMedia: %v", err)
	}
	go func() {
		time.Sleep(20 * time.Millisecond)
		_ = srv.Close()
	}()
	if _, _, err := srv.Receive(); err == nil {
		t.Fatal("Receive after Close = nil error, want a closed error")
	}
}
