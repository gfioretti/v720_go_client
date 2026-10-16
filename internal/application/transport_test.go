package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gfioretti/v720_go_client/internal/domain"
)

func TestFakeTransport_SendRecordsFrames(t *testing.T) {
	ft := newFakeTransport()
	ctx := context.Background()

	if err := ft.Send(ctx, domain.Frame{Type: domain.FrameTypeHandshakeTrigger}); err != nil {
		t.Fatalf("Send() error: %v", err)
	}
	if err := ft.Send(ctx, domain.Frame{Type: domain.FrameTypeJSON, Payload: []byte("x")}); err != nil {
		t.Fatalf("Send() error: %v", err)
	}

	sent := ft.sentFrames()
	if len(sent) != 2 {
		t.Fatalf("sentFrames len = %d, want 2", len(sent))
	}
	if sent[0].Type != domain.FrameTypeHandshakeTrigger || sent[1].Type != domain.FrameTypeJSON {
		t.Fatalf("unexpected sent frames: %+v", sent)
	}
}

func TestFakeTransport_ReceiveReturnsPushedFrames(t *testing.T) {
	ft := newFakeTransport()
	ft.push(domain.Frame{Type: domain.FrameTypePongAP, Sequence: 7})

	got, err := ft.Receive(context.Background())
	if err != nil {
		t.Fatalf("Receive() error: %v", err)
	}
	if got.Type != domain.FrameTypePongAP || got.Sequence != 7 {
		t.Fatalf("Receive() = %+v, want type PongAP seq 7", got)
	}
}

func TestFakeTransport_ReceiveHonoursContextCancellation(t *testing.T) {
	ft := newFakeTransport()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := ft.Receive(ctx) // nothing pushed → blocks until ctx expires
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Receive() error = %v, want context.DeadlineExceeded", err)
	}
}

func TestFakeTransport_CloseUnblocksReceive(t *testing.T) {
	ft := newFakeTransport()

	errCh := make(chan error, 1)
	go func() {
		_, err := ft.Receive(context.Background())
		errCh <- err
	}()

	// Give the goroutine a moment to park in Receive, then close.
	time.Sleep(5 * time.Millisecond)
	if err := ft.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}

	select {
	case err := <-errCh:
		if !errors.Is(err, ErrTransportClosed) {
			t.Fatalf("Receive() after Close error = %v, want ErrTransportClosed", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Receive() did not unblock after Close")
	}
}

func TestFakeTransport_SendHonoursCancelledContext(t *testing.T) {
	ft := newFakeTransport()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := ft.Send(ctx, domain.Frame{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Send() with cancelled ctx = %v, want context.Canceled", err)
	}
	if len(ft.sentFrames()) != 0 {
		t.Fatal("Send() recorded a frame despite a cancelled context")
	}
}

func TestFakeTransport_ReceiveDrainsBufferedFramesAfterClose(t *testing.T) {
	ft := newFakeTransport()
	ft.push(domain.Frame{Type: domain.FrameTypeJSON, Sequence: 1})
	ft.push(domain.Frame{Type: domain.FrameTypeJSON, Sequence: 2})
	_ = ft.Close()

	// Both buffered frames must still be delivered in order before closure is reported.
	for want := uint32(1); want <= 2; want++ {
		got, err := ft.Receive(context.Background())
		if err != nil {
			t.Fatalf("Receive() #%d error: %v", want, err)
		}
		if got.Sequence != want {
			t.Fatalf("Receive() #%d seq = %d, want %d", want, got.Sequence, want)
		}
	}
	if _, err := ft.Receive(context.Background()); !errors.Is(err, ErrTransportClosed) {
		t.Fatalf("Receive() after drain = %v, want ErrTransportClosed", err)
	}
}

func TestFakeTransport_ConcurrentSendReceive(t *testing.T) {
	ft := newFakeTransport()
	ctx := context.Background()

	const n = 100
	done := make(chan struct{})
	go func() {
		for i := range n {
			got, err := ft.Receive(ctx)
			if err != nil {
				t.Errorf("Receive() error: %v", err)
				break
			}
			if got.Sequence != uint32(i) {
				t.Errorf("Receive() seq = %d, want %d", got.Sequence, i)
			}
		}
		close(done)
	}()

	for i := range n {
		if err := ft.Send(ctx, domain.Frame{Sequence: uint32(i)}); err != nil {
			t.Fatalf("Send() error: %v", err)
		}
		ft.push(domain.Frame{Sequence: uint32(i)})
	}
	<-done
}

func TestFakeTransport_SendAfterCloseFails(t *testing.T) {
	ft := newFakeTransport()
	if err := ft.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}
	if err := ft.Close(); err != nil {
		t.Fatalf("second Close() should be a no-op, got: %v", err)
	}

	err := ft.Send(context.Background(), domain.Frame{})
	if !errors.Is(err, ErrTransportClosed) {
		t.Fatalf("Send() after Close error = %v, want ErrTransportClosed", err)
	}
}
