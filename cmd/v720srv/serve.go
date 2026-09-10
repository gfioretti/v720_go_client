package main

import (
	"context"

	"v720_go_client/internal/adapters/transport/framedtcp"
	"v720_go_client/internal/application"
)

// serveCameras accepts camera coordinator connections and runs each through the
// bring-up FSM. Each accepted camera is handled in its own goroutine. It returns
// when the listener is closed (or ctx is done). onRegistered, if non-nil, is
// called for each camera that completes bring-up.
func serveCameras(
	ctx context.Context,
	ln *framedtcp.Listener,
	gw *application.DeviceGateway,
	logf func(string, ...any),
	onRegistered func(application.Registration),
) {
	for {
		ch, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return // shutting down
			}
			logf("accept stopped: %v", err)
			return
		}
		go handleCamera(ctx, ch, gw, logf, onRegistered)
	}
}

// handleCamera runs one camera through bring-up, then holds the coordinator
// connection open (draining it) so the camera stays registered. Live A/V arrives
// on the separate UDP media channel (not yet wired here).
func handleCamera(
	ctx context.Context,
	ch *framedtcp.Channel,
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
