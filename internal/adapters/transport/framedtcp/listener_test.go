package framedtcp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gfioretti/v720_go_client/internal/domain"
)

// A Dial↔Accept pair is a full duplex framed transport: frames flow both
// directions, proving the accepted (server-side) Channel works like the dialed
// one.
func TestListener_AcceptRoundTrip(t *testing.T) {
	ln, err := Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	type accepted struct {
		ch  *Channel
		err error
	}
	accepts := make(chan accepted, 1)
	go func() {
		ch, err := ln.Accept()
		accepts <- accepted{ch, err}
	}()

	client, err := Dial(context.Background(), ln.Addr().String())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	a := <-accepts
	if a.err != nil {
		t.Fatalf("Accept: %v", a.err)
	}
	server := a.ch
	t.Cleanup(func() { _ = server.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// client → server (the camera → coordinator register direction).
	up := domain.Frame{Type: domain.FrameTypeJSON, Sequence: 1, Payload: []byte(`{"code":100}`)}
	if err := client.Send(ctx, up); err != nil {
		t.Fatalf("client.Send: %v", err)
	}
	got, err := server.Receive(ctx)
	if err != nil {
		t.Fatalf("server.Receive: %v", err)
	}
	if !sameFrame(got, up) {
		t.Fatalf("server received %+v, want %+v", got, up)
	}

	// server → client (the coordinator → camera start-stream direction).
	down := domain.Frame{Type: domain.FrameTypeJSON, Sequence: 2, Payload: []byte(`{"code":301}`)}
	if err := server.Send(ctx, down); err != nil {
		t.Fatalf("server.Send: %v", err)
	}
	got, err = client.Receive(ctx)
	if err != nil {
		t.Fatalf("client.Receive: %v", err)
	}
	if !sameFrame(got, down) {
		t.Fatalf("client received %+v, want %+v", got, down)
	}
}

func TestListener_AcceptAfterCloseErrors(t *testing.T) {
	ln, err := Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	_ = ln.Close()
	if _, err := ln.Accept(); err == nil || errors.Is(err, context.Canceled) {
		t.Fatalf("Accept after Close = %v, want a listener-closed error", err)
	}
}
