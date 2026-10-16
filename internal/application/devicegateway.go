package application

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gfioretti/v720_go_client/internal/domain"
)

// DefaultClientTarget is the client-target id the server puts in its 301 forward
// wrappers. a9-v720 uses a fixed constant here (unrelated to the device uid) and
// the camera accepts it; the value is UNCONFIRMED to matter, so it is
// configurable with this default.
const DefaultClientTarget = "00112233445566778899aabbccddeeff"

// Registration is what the camera announces when it connects out to the server
// in STA mode: its device uid and token (findings doc §6; from the camera's
// code-100 register request).
type Registration struct {
	UID   string
	Token string
}

// DeviceGateway is the server-side counterpart of RelaySession: it drives a
// camera that has connected out to the self-hosted server (STA mode) through the
// bring-up sequence that makes it start streaming. It mirrors, from the
// coordinator's side, the client's relay handshake — cross-referenced against
// a9-v720's v720_sta.py. a9-v720 reflects an earlier firmware revision, so the
// sequence is provisional pending real-camera verification.
type DeviceGateway struct {
	target string
	now    func() time.Time
}

// NewDeviceGateway returns a gateway that addresses the camera's 301 forward
// wrappers to target (use DefaultClientTarget unless a specific value is needed).
func NewDeviceGateway(target string) *DeviceGateway {
	return &DeviceGateway{target: target, now: time.Now}
}

// Bringup runs the TCP control FSM over the camera's coordinator connection
// (findings doc §6, from a9-v720's v720_sta.py):
//
//	← 100  register {uid, token}
//	→ 101  register ack {status:200}
//	→ 53   device status {status:1}
//	→ 301  base-info request {content:{code:4, unixTimer}}
//	← …    camera's base-info / device-state reply
//	→ 301  open video {content:{code:3}}  → camera starts streaming
//
// It returns the camera's Registration. Interleaved heartbeats and other frames
// are skipped while awaiting each expected message. The UDP data-channel probe
// and media ingestion are handled separately (the media path). ctx bounds the
// whole sequence.
func (g *DeviceGateway) Bringup(ctx context.Context, coord RelayChannel) (Registration, error) {
	reg, err := g.awaitRegister(ctx, coord)
	if err != nil {
		return Registration{}, fmt.Errorf("gateway: await register: %w", err)
	}

	if err := g.send(ctx, coord, domain.NewRegisterAck()); err != nil {
		return Registration{}, fmt.Errorf("gateway: send register ack: %w", err)
	}
	if err := g.send(ctx, coord, domain.NewDeviceStatus(true)); err != nil {
		return Registration{}, fmt.Errorf("gateway: send device status: %w", err)
	}

	if err := g.send(ctx, coord, domain.NewBaseInfoRequest(g.target, g.now().Unix())); err != nil {
		return Registration{}, fmt.Errorf("gateway: send base-info request: %w", err)
	}
	// Await the camera's base-info reply before starting the stream (a9-v720
	// starts live only after this).
	if err := g.awaitBaseInfoReply(ctx, coord); err != nil {
		return Registration{}, fmt.Errorf("gateway: await base-info reply: %w", err)
	}

	if err := g.send(ctx, coord, domain.NewRelayStreamControl(g.target, domain.StreamStart)); err != nil {
		return Registration{}, fmt.Errorf("gateway: send open-video: %w", err)
	}
	return reg, nil
}

// registerMessage is the subset of the camera's code-100 register frame the
// gateway reads.
type registerMessage struct {
	Code  domain.Code `json:"code"`
	UID   string      `json:"uid"`
	Token string      `json:"token"`
}

// awaitRegister receives JSON frames until the camera's code-100 register arrives,
// then returns its uid/token. Non-JSON frames (heartbeats) and other codes are
// skipped.
func (g *DeviceGateway) awaitRegister(ctx context.Context, coord RelayChannel) (Registration, error) {
	for {
		f, err := coord.Receive(ctx)
		if err != nil {
			return Registration{}, err
		}
		if f.Type != domain.FrameTypeJSON {
			continue
		}
		var msg registerMessage
		if err := json.Unmarshal(f.Payload, &msg); err != nil {
			continue
		}
		if msg.Code == domain.CodeRelayRegister {
			return Registration{UID: msg.UID, Token: msg.Token}, nil
		}
	}
}

// awaitBaseInfoReply waits for the camera's reply to the base-info request,
// skipping interleaved non-JSON frames (heartbeats) exactly as awaitRegister
// does. We accept any JSON code as the trigger rather than validating its shape —
// UNCONFIRMED which exact code the reply carries.
func (g *DeviceGateway) awaitBaseInfoReply(ctx context.Context, coord RelayChannel) error {
	for {
		f, err := coord.Receive(ctx)
		if err != nil {
			return err
		}
		if f.Type == domain.FrameTypeJSON {
			return nil
		}
	}
}

// send serialises a Command and writes it as a FrameTypeJSON frame on ch.
func (g *DeviceGateway) send(ctx context.Context, ch RelayChannel, cmd domain.Command) error {
	body, err := cmd.JSON()
	if err != nil {
		return fmt.Errorf("encode command %d: %w", cmd.Code, err)
	}
	return ch.Send(ctx, domain.Frame{Type: domain.FrameTypeJSON, Payload: body})
}
