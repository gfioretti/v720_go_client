package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"v720_go_client/internal/domain"
)

// apToken is the hardcoded literal the client sends as the 501 register token in
// AP-mode. It is not derived from anything device-specific (findings doc §12.3
// step 4).
const apToken = "NaxclowToken"

// statusOK is the "status" value the device reports for a successful control
// exchange (doc §12.3 steps 5 and 7).
const statusOK = 200

// Errors surfaced by the AP-mode handshake, distinguishable with errors.Is.
var (
	// ErrHandshakeRegister means the device rejected the 501 registration.
	ErrHandshakeRegister = errors.New("application: AP-mode registration rejected by device")
	// ErrHandshakeAuth means the device rejected the 500 password authorization.
	ErrHandshakeAuth = errors.New("application: AP-mode authorization rejected by device")
)

// APSession drives a local AP-mode (camera's own Wi-Fi hotspot) session over a
// single Transport. It is the application-layer counterpart of the app's
// OooOOO0 session (see CLAUDE.md naming map) and implements the AP-mode
// handshake from doc §12.3. It will grow to satisfy the Session port as
// live-view and control use cases are added; for now it owns the handshake.
type APSession struct {
	transport Transport
	devID     string
	password  string

	// now supplies the current time for the unixTimer fields; overridable in
	// tests for deterministic timestamps. Defaults to time.Now.
	now func() time.Time
}

// NewAPSession returns an APSession that talks to the camera over transport,
// authenticating as devID with the shared password (the "sk"/pwd value).
func NewAPSession(transport Transport, devID, password string) *APSession {
	return &APSession{
		transport: transport,
		devID:     devID,
		password:  password,
		now:       time.Now,
	}
}

// Connect performs the AP-mode handshake end to end (doc §12.3 steps 2–7):
// send the empty type-115 trigger, await the device's echo, register with the
// NaxclowToken, send the time-sync, then authorize with the password. It
// returns ErrHandshakeRegister / ErrHandshakeAuth if the device rejects a step,
// or the transport/context error if the connection drops or ctx expires.
func (s *APSession) Connect(ctx context.Context) error {
	// Step 2: announce ourselves with a bare type-115 frame (no JSON body).
	if err := s.transport.Send(ctx, domain.Frame{Type: domain.FrameTypeHandshakeTrigger}); err != nil {
		return fmt.Errorf("ap handshake: send trigger: %w", err)
	}

	// Step 3: the device echoes a type-115 frame back before we may register.
	if err := s.awaitFrameType(ctx, domain.FrameTypeHandshakeTrigger); err != nil {
		return fmt.Errorf("ap handshake: await echo: %w", err)
	}

	// The register and time-sync messages share one timestamp (doc §12.3 steps 4/6).
	ts := s.unixTimer()

	// Step 4: register with the hardcoded token.
	register := domain.Command{Code: domain.CodeConnect, Params: map[string]any{
		"target":    s.devID,
		"token":     apToken,
		"unixTimer": ts,
	}}
	if err := s.sendControl(ctx, register); err != nil {
		return fmt.Errorf("ap handshake: send register: %w", err)
	}

	// Step 5: registration confirmation.
	if status, err := s.awaitCode(ctx, domain.CodeConnect); err != nil {
		return fmt.Errorf("ap handshake: await register ack: %w", err)
	} else if status != statusOK {
		return fmt.Errorf("%w: status %d", ErrHandshakeRegister, status)
	}

	// Step 6: time-sync (no acknowledgement expected).
	timeSync := domain.Command{Code: domain.CodeTimeSync, Params: map[string]any{
		"devTarget": s.devID,
		"unixTimer": ts,
	}}
	if err := s.sendControl(ctx, timeSync); err != nil {
		return fmt.Errorf("ap handshake: send time-sync: %w", err)
	}

	// Step 7: authorize with the password in the "sk" field.
	authorize := domain.Command{Code: domain.CodeAuthorize, Params: map[string]any{
		"sk": s.password,
	}}
	if err := s.sendControl(ctx, authorize); err != nil {
		return fmt.Errorf("ap handshake: send authorize: %w", err)
	}
	if status, err := s.awaitCode(ctx, domain.CodeAuthorize); err != nil {
		return fmt.Errorf("ap handshake: await authorize ack: %w", err)
	} else if status != statusOK {
		return fmt.Errorf("%w: status %d", ErrHandshakeAuth, status)
	}

	return nil
}

// unixTimer produces the timestamp the AP-mode messages carry: unix epoch
// seconds plus the local timezone's offset east of UTC, in seconds (doc §12.3
// step 4).
func (s *APSession) unixTimer() int64 {
	t := s.now()
	_, offset := t.Zone()
	return t.Unix() + int64(offset)
}

// sendControl serialises a Command to JSON and sends it as a FrameTypeJSON frame.
func (s *APSession) sendControl(ctx context.Context, cmd domain.Command) error {
	body, err := cmd.JSON()
	if err != nil {
		return fmt.Errorf("encode command %d: %w", cmd.Code, err)
	}
	return s.transport.Send(ctx, domain.Frame{Type: domain.FrameTypeJSON, Payload: body})
}

// awaitFrameType receives frames until one of the wanted packet type tag arrives,
// discarding any other frames (keepalives, noise) in between. ctx bounds the wait.
func (s *APSession) awaitFrameType(ctx context.Context, want domain.FrameType) error {
	for {
		f, err := s.transport.Receive(ctx)
		if err != nil {
			return err
		}
		if f.Type == want {
			return nil
		}
	}
}

// apControlMessage is the subset of an inbound JSON control frame the handshake
// inspects.
type apControlMessage struct {
	Code   domain.Code `json:"code"`
	Status int         `json:"status"`
}

// awaitCode receives frames until a JSON control frame carrying the wanted code
// arrives, then returns its status. Non-JSON frames and JSON frames with other
// codes are skipped (e.g. an interleaved device-presence or keepalive message).
// ctx bounds the wait.
func (s *APSession) awaitCode(ctx context.Context, want domain.Code) (int, error) {
	for {
		f, err := s.transport.Receive(ctx)
		if err != nil {
			return 0, err
		}
		if f.Type != domain.FrameTypeJSON {
			continue
		}
		var msg apControlMessage
		if err := json.Unmarshal(f.Payload, &msg); err != nil {
			// A malformed/partial JSON frame is not the ack we're waiting for; skip it.
			continue
		}
		if msg.Code == want {
			return msg.Status, nil
		}
	}
}
