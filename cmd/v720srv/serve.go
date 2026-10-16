package main

import (
	"context"

	"github.com/gfioretti/v720_go_client/internal/adapters/transport/framedtcp"
	"github.com/gfioretti/v720_go_client/internal/application"
	"github.com/gfioretti/v720_go_client/internal/domain"
)

// coordChannel is the coordinator connection handleCamera drives: the framed
// duplex transport (Send/Receive/Close). Both *framedtcp.Channel and the verbose
// *loggingChannel wrapper satisfy it.
type coordChannel interface {
	Send(context.Context, domain.Frame) error
	Receive(context.Context) (domain.Frame, error)
	Close() error
}

// serveCameras accepts camera coordinator connections and runs each through the
// bring-up FSM. Each accepted camera is handled in its own goroutine. It returns
// when the listener is closed (or ctx is done). onRegistered, if non-nil, is
// called for each camera that completes bring-up. When verbose, every coordinator
// frame is logged.
func serveCameras(
	ctx context.Context,
	ln *framedtcp.Listener,
	gw *application.DeviceGateway,
	verbose bool,
	logf func(string, ...any),
	onRegistered func(application.Registration),
) {
	for {
		accepted, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return // shutting down
			}
			logf("accept stopped: %v", err)
			return
		}
		var ch coordChannel = accepted
		if verbose {
			ch = &loggingChannel{inner: accepted, logf: logf}
		}
		go handleCamera(ctx, ch, gw, logf, onRegistered)
	}
}

// handleCamera runs one camera through bring-up, then holds the coordinator
// connection open (draining it) so the camera stays registered. Live A/V arrives
// on the separate UDP media channel.
func handleCamera(
	ctx context.Context,
	ch coordChannel,
	gw *application.DeviceGateway,
	logf func(string, ...any),
	onRegistered func(application.Registration),
) {
	defer func() { _ = ch.Close() }()

	reg, err := gw.Bringup(ctx, ch)
	if err != nil {
		logf("camera bring-up failed: %v", err)
		return
	}
	logf("camera registered: uid=%q; sent start-stream", reg.UID)
	if onRegistered != nil {
		onRegistered(reg)
	}

	// Hold the coordinator connection open, draining inbound frames (heartbeats,
	// device messages) until the camera disconnects or we shut down.
	for {
		if _, err := ch.Receive(ctx); err != nil {
			logf("camera uid=%q disconnected: %v", reg.UID, err)
			return
		}
	}
}
