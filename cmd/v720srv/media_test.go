package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gfioretti/v720_go_client/internal/adapters/transport/relay"
	"github.com/gfioretti/v720_go_client/internal/domain"
)

func mjpegFrame(flag int8, seq uint32, payload []byte) domain.Frame {
	return domain.Frame{Type: domain.FrameTypeMJPEG, Flag: flag, Sequence: seq, Payload: payload}
}

// End-to-end media: a camera streams HEAD/BODY/END video fragments; runMedia
// reassembles them, publishes the JPEG to a viewer, and ACKs the frame.
func TestRunMedia_ReassemblesPublishesAndAcks(t *testing.T) {
	srv, err := relay.ListenMedia("127.0.0.1:0")
	if err != nil {
		t.Fatalf("ListenMedia: %v", err)
	}
	defer func() { _ = srv.Close() }()

	hub := newMediaHub()
	sub := hub.subscribe()
	defer hub.unsubscribe(sub)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go runMedia(ctx, srv, hub, false, discardf)

	cam, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("camera listen: %v", err)
	}
	defer func() { _ = cam.Close() }()

	trailer := []byte{0, 0, 0, 0, 0}
	for _, f := range []domain.Frame{
		mjpegFrame(domain.MsgFlagHead, 1, []byte("aa")),
		mjpegFrame(domain.MsgFlagBody, 2, []byte("bb")),
		mjpegFrame(domain.MsgFlagEnd, 3, append([]byte("cc"), trailer...)),
	} {
		if _, err := cam.WriteTo(f.Encode(), srv.LocalAddr()); err != nil {
			t.Fatalf("camera send: %v", err)
		}
	}

	select {
	case jpeg := <-sub:
		if string(jpeg) != "aabbcc" {
			t.Fatalf("published JPEG = %q, want aabbcc", jpeg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no JPEG published")
	}

	// The camera should receive a retransmit-confirm ACK for the frame.
	buf := make([]byte, domain.MaxFrameLength)
	_ = cam.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, err := cam.ReadFrom(buf)
	if err != nil {
		t.Fatalf("camera read ack: %v", err)
	}
	ack, err := domain.Decode(buf[:n])
	if err != nil || ack.Type != domain.FrameTypeRetransmitConfirm {
		t.Fatalf("ack = %+v, err=%v, want type 605", ack, err)
	}
}

// The retransmit-confirm ACK lists only the current video frame's package ids —
// interleaved audio frames must not accumulate into it (regression: unbounded
// frameIDs growth + mis-attribution).
func TestRunMedia_AckScopedToVideoFrame(t *testing.T) {
	srv, err := relay.ListenMedia("127.0.0.1:0")
	if err != nil {
		t.Fatalf("ListenMedia: %v", err)
	}
	defer func() { _ = srv.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go runMedia(ctx, srv, newMediaHub(), false, discardf)

	cam, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("camera listen: %v", err)
	}
	defer func() { _ = cam.Close() }()

	trailer := []byte{0, 0, 0, 0, 0}
	// An audio frame (seq 5) first, then a two-fragment video frame (seq 10,11).
	send := func(f domain.Frame) {
		if _, err := cam.WriteTo(f.Encode(), srv.LocalAddr()); err != nil {
			t.Fatalf("send: %v", err)
		}
	}
	send(domain.Frame{Type: domain.FrameTypeAudioG711, Flag: domain.MsgFlagFinish, Sequence: 5, Payload: append([]byte("a"), trailer...)})
	send(mjpegFrame(domain.MsgFlagHead, 10, []byte("hh")))
	send(mjpegFrame(domain.MsgFlagEnd, 11, append([]byte("ee"), trailer...)))

	buf := make([]byte, domain.MaxFrameLength)
	_ = cam.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, err := cam.ReadFrom(buf)
	if err != nil {
		t.Fatalf("read ack: %v", err)
	}
	ack, err := domain.Decode(buf[:n])
	if err != nil || ack.Type != domain.FrameTypeRetransmitConfirm {
		t.Fatalf("ack = %+v, err=%v", ack, err)
	}
	ids := make([]uint32, len(ack.Payload)/4)
	for i := range ids {
		ids[i] = binary.LittleEndian.Uint32(ack.Payload[i*4:])
	}
	if len(ids) != 2 || ids[0] != 10 || ids[1] != 11 {
		t.Fatalf("ack ids = %v, want [10 11] (audio seq 5 must be excluded)", ids)
	}
}

// mediaHub tolerates concurrent publish / subscribe / unsubscribe (run under -race).
func TestMediaHub_ConcurrentPubSubUnsub(t *testing.T) {
	hub := newMediaHub()
	var wg sync.WaitGroup

	for i := 0; i < 15; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ch := hub.subscribe()
			time.Sleep(time.Millisecond)
			hub.unsubscribe(ch)
		}()
	}
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				hub.publish([]byte("x"))
			}
		}()
	}
	wg.Wait()
}

// /snapshot returns 503 before any frame, then the latest JPEG once published.
func TestSnapshotHandler(t *testing.T) {
	hub := newMediaHub()
	h := hub.snapshotHandler()

	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/snapshot", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("before any frame: status = %d, want 503", rec.Code)
	}

	hub.publish([]byte("\xff\xd8jpeg\xff\xd9"))
	rec = httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/snapshot", nil))
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("after frame: status=%d type=%q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if rec.Body.String() != "\xff\xd8jpeg\xff\xd9" {
		t.Fatalf("snapshot body = %q", rec.Body.String())
	}
}

// The data-channel probe: the camera's code-51 gets a code-50 reply.
func TestRunMedia_AnswersProbe(t *testing.T) {
	srv, err := relay.ListenMedia("127.0.0.1:0")
	if err != nil {
		t.Fatalf("ListenMedia: %v", err)
	}
	defer func() { _ = srv.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go runMedia(ctx, srv, newMediaHub(), false, discardf)

	cam, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("camera listen: %v", err)
	}
	defer func() { _ = cam.Close() }()

	probe, _ := domain.Command{Code: domain.CodeMediaProbeOK}.JSON()
	if _, err := cam.WriteTo((domain.Frame{Type: domain.FrameTypeJSON, Payload: probe}).Encode(), srv.LocalAddr()); err != nil {
		t.Fatalf("camera send probe: %v", err)
	}

	buf := make([]byte, domain.MaxFrameLength)
	_ = cam.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, err := cam.ReadFrom(buf)
	if err != nil {
		t.Fatalf("camera read probe reply: %v", err)
	}
	reply, err := domain.Decode(buf[:n])
	if err != nil {
		t.Fatalf("decode reply: %v", err)
	}
	var m struct {
		Code domain.Code `json:"code"`
	}
	if json.Unmarshal(reply.Payload, &m) != nil || m.Code != domain.CodeMediaProbeReq {
		t.Fatalf("probe reply = %s, want code 50", reply.Payload)
	}
}
