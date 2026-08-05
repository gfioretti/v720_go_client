package application

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"v720_go_client/internal/domain"
)

type audioRec struct {
	codec domain.FrameType
	data  []byte
}

// recordingSink implements both VideoFrameHandler and AudioFrameHandler, records
// what it receives, and closes done once stopAfter frames have arrived so a test
// can deterministically know when to cancel.
type recordingSink struct {
	mu       sync.Mutex
	videos   [][]byte
	audios   []audioRec
	videoErr error // if set, OnVideoFrame returns it
	audioErr error // if set, OnAudioFrame returns it

	stopAfter int
	n         int
	done      chan struct{}
	doneOnce  sync.Once
}

func newRecordingSink(stopAfter int) *recordingSink {
	return &recordingSink{stopAfter: stopAfter, done: make(chan struct{})}
}

func (r *recordingSink) OnVideoFrame(data []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.videoErr != nil {
		return r.videoErr
	}
	r.videos = append(r.videos, bytes.Clone(data))
	r.tick()
	return nil
}

func (r *recordingSink) OnAudioFrame(codec domain.FrameType, data []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.audioErr != nil {
		return r.audioErr
	}
	r.audios = append(r.audios, audioRec{codec, bytes.Clone(data)})
	r.tick()
	return nil
}

func (r *recordingSink) tick() {
	r.n++
	if r.stopAfter > 0 && r.n >= r.stopAfter {
		r.doneOnce.Do(func() { close(r.done) })
	}
}

func (r *recordingSink) counts() (videos, audios int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.videos), len(r.audios)
}

func TestStartLiveView_DispatchesMediaUntilCancel(t *testing.T) {
	ft := newFakeTransport()
	s := NewAPSession(ft, testDevID, testPassword)
	sink := newRecordingSink(3)

	// One frame of each media kind, plus noise the loop must ignore.
	ft.push(domain.Frame{Type: domain.FrameTypeJSON, Payload: []byte(`{"code":298}`)})
	ft.push(domain.Frame{Type: domain.FrameTypeMJPEG, Payload: []byte("jpeg")})
	ft.push(domain.Frame{Type: domain.FrameTypeNoop})
	ft.push(domain.Frame{Type: domain.FrameTypeAudioG711, Payload: []byte("g711")})
	ft.push(domain.Frame{Type: domain.FrameTypeAudioPCM, Payload: []byte("pcm")})

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- s.StartLiveView(ctx, sink, sink) }()

	select {
	case <-sink.done:
	case <-time.After(time.Second):
		t.Fatal("sink did not receive all frames")
	}
	cancel()

	if err := <-errCh; err != nil {
		t.Fatalf("StartLiveView returned %v, want nil on cancel", err)
	}

	if v, a := sink.counts(); v != 1 || a != 2 {
		t.Fatalf("received %d video / %d audio, want 1 / 2", v, a)
	}
	if !bytes.Equal(sink.videos[0], []byte("jpeg")) {
		t.Errorf("video payload = %q", sink.videos[0])
	}
	if sink.audios[0].codec != domain.FrameTypeAudioG711 || sink.audios[1].codec != domain.FrameTypeAudioPCM {
		t.Errorf("audio codecs = %v", sink.audios)
	}

	// The session sent exactly a start then a stop control command.
	sent := ft.sentFrames()
	if len(sent) != 2 {
		t.Fatalf("sent %d frames, want 2 (start, stop): %+v", len(sent), sent)
	}
	assertPayload(t, sent[0], domain.NewStreamControl(domain.StreamStart))
	assertPayload(t, sent[1], domain.NewStreamControl(domain.StreamStop))
}

func assertPayload(t *testing.T, f domain.Frame, cmd domain.Command) {
	t.Helper()
	if f.Type != domain.FrameTypeJSON {
		t.Fatalf("frame type = %s, want JSON", f.Type)
	}
	want, err := cmd.JSON()
	if err != nil {
		t.Fatalf("encode expected: %v", err)
	}
	if !bytes.Equal(f.Payload, want) {
		t.Fatalf("payload = %s, want %s", f.Payload, want)
	}
}

func TestStartLiveView_NilHandlersDropMedia(t *testing.T) {
	ft := newFakeTransport()
	s := NewAPSession(ft, testDevID, testPassword)

	ft.push(domain.Frame{Type: domain.FrameTypeMJPEG, Payload: []byte("jpeg")})
	ft.push(domain.Frame{Type: domain.FrameTypeAudioG711, Payload: []byte("g711")})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	// Both handlers nil: frames must be silently dropped, no panic, clean stop.
	if err := s.StartLiveView(ctx, nil, nil); err != nil {
		t.Fatalf("StartLiveView = %v, want nil", err)
	}
}

func TestStartLiveView_StartCommandFailsIfTransportClosed(t *testing.T) {
	ft := newFakeTransport()
	s := NewAPSession(ft, testDevID, testPassword)
	_ = ft.Close()

	err := s.StartLiveView(context.Background(), newRecordingSink(0), nil)
	if !errors.Is(err, ErrTransportClosed) {
		t.Fatalf("StartLiveView = %v, want ErrTransportClosed", err)
	}
	// A failed start must not emit a stray stop (the defer is registered only
	// after the start succeeds).
	if n := len(ft.sentFrames()); n != 0 {
		t.Fatalf("sent %d frames after a failed start, want 0", n)
	}
}

// cancelOnFirstSink cancels the live-view context the instant it gets a video
// frame, letting a test prove cancellation is observed even with more frames
// already buffered in the transport.
type cancelOnFirstSink struct {
	cancel context.CancelFunc
	count  int
}

func (c *cancelOnFirstSink) OnVideoFrame(data []byte) error {
	c.count++
	c.cancel()
	return nil
}

func TestStartLiveView_ObservesCancelWithFramesBuffered(t *testing.T) {
	ft := newFakeTransport()
	s := NewAPSession(ft, testDevID, testPassword)
	ctx, cancel := context.WithCancel(context.Background())
	sink := &cancelOnFirstSink{cancel: cancel}

	// Two frames buffered up front; the first cancels. Without the loop's own
	// ctx check, the drain-first transport would still hand over the second.
	ft.push(domain.Frame{Type: domain.FrameTypeMJPEG, Payload: []byte("f1")})
	ft.push(domain.Frame{Type: domain.FrameTypeMJPEG, Payload: []byte("f2")})

	if err := s.StartLiveView(ctx, sink, nil); err != nil {
		t.Fatalf("StartLiveView = %v, want nil", err)
	}
	if sink.count != 1 {
		t.Fatalf("dispatched %d frames after cancel-on-first, want 1", sink.count)
	}
}

func TestStartLiveView_AudioHandlerErrorStopsStream(t *testing.T) {
	ft := newFakeTransport()
	s := NewAPSession(ft, testDevID, testPassword)
	sinkErr := errors.New("codec failure")
	sink := &recordingSink{audioErr: sinkErr, done: make(chan struct{})}

	ft.push(domain.Frame{Type: domain.FrameTypeAudioG711, Payload: []byte("g711")})

	err := s.StartLiveView(context.Background(), sink, sink)
	if !errors.Is(err, sinkErr) {
		t.Fatalf("StartLiveView = %v, want it to wrap the audio-handler error", err)
	}
	sent := ft.sentFrames()
	if len(sent) != 2 {
		t.Fatalf("sent %d frames, want start+stop", len(sent))
	}
	assertPayload(t, sent[1], domain.NewStreamControl(domain.StreamStop))
}

func TestStartLiveView_SinkErrorStopsStream(t *testing.T) {
	ft := newFakeTransport()
	s := NewAPSession(ft, testDevID, testPassword)
	sinkErr := errors.New("disk full")
	sink := &recordingSink{videoErr: sinkErr, done: make(chan struct{})}

	ft.push(domain.Frame{Type: domain.FrameTypeMJPEG, Payload: []byte("jpeg")})

	err := s.StartLiveView(context.Background(), sink, sink)
	if !errors.Is(err, sinkErr) {
		t.Fatalf("StartLiveView = %v, want it to wrap the sink error", err)
	}
	// A stop command should still have gone out after the error.
	sent := ft.sentFrames()
	if len(sent) != 2 {
		t.Fatalf("sent %d frames, want start+stop", len(sent))
	}
	assertPayload(t, sent[1], domain.NewStreamControl(domain.StreamStop))
}

func TestStartLiveView_TransportErrorPropagates(t *testing.T) {
	ft := newFakeTransport()
	s := NewAPSession(ft, testDevID, testPassword)

	errCh := make(chan error, 1)
	go func() { errCh <- s.StartLiveView(context.Background(), newRecordingSink(0), nil) }()

	// No frames, then the connection drops: not a ctx cancel, so it must surface.
	time.Sleep(10 * time.Millisecond)
	_ = ft.Close()

	select {
	case err := <-errCh:
		if !errors.Is(err, ErrTransportClosed) {
			t.Fatalf("StartLiveView = %v, want ErrTransportClosed", err)
		}
	case <-time.After(time.Second):
		t.Fatal("StartLiveView did not return after transport close")
	}
}
