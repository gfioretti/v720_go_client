package relay

import (
	"context"
	"errors"
	"testing"

	"v720_go_client/internal/domain"
)

// fakeChannel is a minimal channel (RelayChannel + Closer) for exercising the
// Transport bundle without real sockets. It records whether Close was called and
// can be made to fail on Close.
type fakeChannel struct {
	closed   bool
	closeErr error
}

func (f *fakeChannel) Send(context.Context, domain.Frame) error      { return nil }
func (f *fakeChannel) Receive(context.Context) (domain.Frame, error) { return domain.Frame{}, nil }
func (f *fakeChannel) Close() error                                  { f.closed = true; return f.closeErr }

func TestTransport_AccessorsReturnTheirChannels(t *testing.T) {
	coord, media, storage := &fakeChannel{}, &fakeChannel{}, &fakeChannel{}
	tr := NewTransport(coord, media, storage)

	if tr.Coordinator() != coord {
		t.Error("Coordinator() returned the wrong channel")
	}
	if tr.Media() != media {
		t.Error("Media() returned the wrong channel")
	}
	if tr.Storage() != storage {
		t.Error("Storage() returned the wrong channel")
	}
}

func TestTransport_CloseClosesAllChannels(t *testing.T) {
	coord, media, storage := &fakeChannel{}, &fakeChannel{}, &fakeChannel{}
	tr := NewTransport(coord, media, storage)

	if err := tr.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !coord.closed || !media.closed || !storage.closed {
		t.Errorf("not all channels closed: coord=%v media=%v storage=%v",
			coord.closed, media.closed, storage.closed)
	}
}

// A failure on one channel's Close still closes the others and is surfaced.
func TestTransport_CloseJoinsErrorsAndClosesAll(t *testing.T) {
	boom := errors.New("media boom")
	coord, media, storage := &fakeChannel{}, &fakeChannel{closeErr: boom}, &fakeChannel{}
	tr := NewTransport(coord, media, storage)

	err := tr.Close()
	if !errors.Is(err, boom) {
		t.Fatalf("Close error = %v, want it to wrap %v", err, boom)
	}
	if !coord.closed || !storage.closed {
		t.Error("a Close failure prevented the other channels from closing")
	}
}

// A nil channel (bundle built before media/storage were dialed) is tolerated by
// Close.
func TestTransport_CloseToleratesNilChannels(t *testing.T) {
	coord := &fakeChannel{}
	tr := NewTransport(coord, nil, nil)

	if err := tr.Close(); err != nil {
		t.Fatalf("Close with nil channels: %v", err)
	}
	if !coord.closed {
		t.Error("coordinator not closed")
	}
}
