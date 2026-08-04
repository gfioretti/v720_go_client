package application

import (
	"context"
	"sync"

	"v720_go_client/internal/domain"
)

// fakeTransport is an in-memory Transport for exercising Session logic without a
// real socket. Tests script inbound frames with push and inspect what the
// session emitted with sentFrames. It is safe for concurrent use so a test can
// drive a handshake from one goroutine while feeding responses from another.
type fakeTransport struct {
	mu   sync.Mutex
	sent []domain.Frame

	inbound   chan domain.Frame
	done      chan struct{}
	closeOnce sync.Once
}

// Compile-time proof the fake satisfies the port it stands in for.
var _ Transport = (*fakeTransport)(nil)

func newFakeTransport() *fakeTransport {
	return &fakeTransport{
		inbound: make(chan domain.Frame, 32),
		done:    make(chan struct{}),
	}
}

func (f *fakeTransport) Send(ctx context.Context, fr domain.Frame) error {
	// Check ctx directly rather than via a select with a default clause: a
	// default would race an already-fired ctx.Done() and honour cancellation
	// only ~half the time.
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-f.done:
		return ErrTransportClosed
	default:
	}
	f.mu.Lock()
	f.sent = append(f.sent, fr)
	f.mu.Unlock()
	return nil
}

func (f *fakeTransport) Receive(ctx context.Context) (domain.Frame, error) {
	// Drain a buffered frame in preference to reporting cancellation/closure, so
	// Receive keeps returning queued frames until inbound is empty — matching the
	// port's "closed and no more frames will arrive" contract.
	select {
	case fr := <-f.inbound:
		return fr, nil
	default:
	}
	select {
	case fr := <-f.inbound:
		return fr, nil
	case <-ctx.Done():
		return domain.Frame{}, ctx.Err()
	case <-f.done:
		return domain.Frame{}, ErrTransportClosed
	}
}

func (f *fakeTransport) Close() error {
	f.closeOnce.Do(func() { close(f.done) })
	return nil
}

// push queues a frame to be returned by a subsequent Receive call. It is
// close-safe: once the transport is closed it drops the frame rather than
// blocking forever on a channel no Receive is draining. (Buffer capacity is 32;
// tests script well under that.)
func (f *fakeTransport) push(fr domain.Frame) {
	select {
	case f.inbound <- fr:
	case <-f.done:
	}
}

// sentFrames returns a copy of every frame the code under test has sent so far.
func (f *fakeTransport) sentFrames() []domain.Frame {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]domain.Frame(nil), f.sent...)
}
