package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"v720_go_client/internal/domain"
)

// ErrRelayRegister means the coordinator rejected the P2P registration (a
// {"code":101,"status":500} response — the app's onError(3)/CODE_P2P_REGISTER_FAIL).
// Distinguishable with errors.Is.
var ErrRelayRegister = errors.New("application: relay registration rejected by coordinator")

// ClientCandidates are the client's own candidate addresses reported to the
// coordinator during phase 1 (the cli*/cliNat* fields on codes 30 and 10, doc
// §12.2 phase 1 steps 4/6), so the coordinator can tell the device where to reach
// us for NAT traversal.
//
// UNCONFIRMED how these are really discovered: the app learns them from its own
// socket bind plus a STUN-style exchange, and the exact relationship to the
// phase-2 UDP candidate probe (codes 21/51) is not pinned down (doc §12.2 phase 2
// step 2, §12.8). For now they are supplied to the session explicitly; real
// discovery arrives with the UDP media channel (phase 2 / the media adapter).
type ClientCandidates struct {
	IP      string // → cliIp
	Port    int    // → cliPort
	NatIP   string // → cliNatIp
	NatPort int    // → cliNatPort
}

// deviceCandidates are the two device addresses the coordinator returns in its
// code-13 response (doc §12.2 phase 1 step 3). Per the doc's note, the field
// names read "backwards": devNatIp/devNatPort is the address checked against
// P2P_MODE_DIRECT and devIp/devPort against P2P_MODE_NAT — captured verbatim here,
// not rationalised.
type deviceCandidates struct {
	IP      string
	Port    int
	NatIP   string
	NatPort int
}

// RelaySession drives a P2P/cloud-relay session (findings doc §12.2) over a
// RelayTransport. It is the application-layer counterpart of the app's
// C3048OooOo0O session (see CLAUDE.md naming map). This slice implements phase 1
// — the TCP coordinator handshake (register → forward IDs → candidate exchange →
// registration confirmation); phase 2 (UDP media bring-up) follows.
//
// Like APSession, it receives already-resolved inputs — the account, the
// getA9VideoConf LiveConf, the device ID — rather than calling CloudAPI itself;
// a thin use case wires CloudAPI to the session at the composition root.
type RelaySession struct {
	transport  RelayTransport
	account    domain.Account
	conf       domain.LiveConf
	devID      string
	candidates ClientCandidates

	// Populated by Connect for use by later phases:
	liveForwardID     string           // from code 31 — threads through the media channel
	playbackForwardID string           // from code 37 — registers the storage channel
	devCandidates     deviceCandidates // from code 13 — where to reach the device
}

// NewRelaySession returns a RelaySession addressing devID over transport, using
// the credentials resolved from the cloud rendezvous (account.UID, and conf's
// pwd/tarPwd/domain per doc §12.1a). candidates are the client's own addresses to
// advertise during the handshake.
func NewRelaySession(transport RelayTransport, account domain.Account, conf domain.LiveConf, devID string, candidates ClientCandidates) *RelaySession {
	return &RelaySession{
		transport:  transport,
		account:    account,
		conf:       conf,
		devID:      devID,
		candidates: candidates,
	}
}

// LiveForwardID and PlaybackForwardID expose the forward IDs the coordinator
// assigned during Connect, for the media and storage channels respectively.
func (s *RelaySession) LiveForwardID() string     { return s.liveForwardID }
func (s *RelaySession) PlaybackForwardID() string { return s.playbackForwardID }

// Connect runs the phase-1 coordinator handshake (doc §12.2 phase 1):
//
//	→ bare frame, then register (100: uid/token=pwd/domain)
//	← 13   device candidate addresses
//	→ 30   request live forward ID          ← 31 forwardId
//	→ 36   request playback forward ID       ← 37 forwardId
//	→ 10   report client candidate addresses
//	→ 52   complete registration
//	← 101  status 200 → registered; 500 → ErrRelayRegister
//
// Interleaved presence/housekeeping/time-sync frames (codes 53/298/4) are
// skipped while awaiting each expected response. It returns ErrRelayRegister if
// the coordinator rejects registration, or the transport/context error if the
// connection drops or ctx expires.
func (s *RelaySession) Connect(ctx context.Context) error {
	coord := s.transport.Coordinator()

	// Step 2: a bare frame precedes the register request (the app sends an empty
	// C3050OooOoO() first). Its packet type tag is not pinned by the doc (§12.2
	// step 2); we send the zero value, type 0 (FrameTypeJSON), with no payload —
	// UNCONFIRMED, but the most plausible default.
	if err := coord.Send(ctx, domain.Frame{}); err != nil {
		return fmt.Errorf("relay handshake: send bare frame: %w", err)
	}

	// Step 2: register. Field mapping per §12.1a: uid = app account UID,
	// token = device pwd, and the third field is domain. The third field's JSON
	// KEY is still jadx-obscured (doc §12.8, inferred to be "domain") — UNCONFIRMED.
	register := domain.Command{Code: domain.CodeRelayRegister, Params: map[string]any{
		"uid":    s.account.UID,
		"token":  s.conf.Pwd,
		"domain": s.conf.Domain,
	}}
	if err := s.send(ctx, coord, register); err != nil {
		return fmt.Errorf("relay handshake: send register: %w", err)
	}

	// Step 3: device candidate addresses.
	cand, err := s.await(ctx, coord, domain.CodeRelayDeviceCandidates)
	if err != nil {
		return fmt.Errorf("relay handshake: await device candidates: %w", err)
	}
	s.devCandidates = deviceCandidates{
		IP: cand.DevIP, Port: cand.DevPort,
		NatIP: cand.DevNatIP, NatPort: cand.DevNatPort,
	}

	// Step 4: request a live forward ID.
	reqForward := domain.Command{Code: domain.CodeRelayReqForwardID, Params: map[string]any{
		"cliIp":     s.candidates.IP,
		"cliPort":   s.candidates.Port,
		"devIp":     s.devCandidates.IP,
		"devPort":   s.devCandidates.Port,
		"devTarget": s.devID,
		"devToken":  s.conf.TarPwd,
	}}
	if err := s.send(ctx, coord, reqForward); err != nil {
		return fmt.Errorf("relay handshake: send reqForwardId: %w", err)
	}
	fwd, err := s.await(ctx, coord, domain.CodeRelayForwardID)
	if err != nil {
		return fmt.Errorf("relay handshake: await forwardId: %w", err)
	}
	if fwd.Status != statusOK {
		return fmt.Errorf("relay handshake: forwardId status %d", fwd.Status)
	}
	s.liveForwardID = fwd.ForwardID

	// Step 5: request a playback (SD-card) forward ID.
	reqPlayback := domain.Command{Code: domain.CodeRelayReqPlaybackID, Params: map[string]any{
		"devTarget": s.devID,
	}}
	if err := s.send(ctx, coord, reqPlayback); err != nil {
		return fmt.Errorf("relay handshake: send reqPlaybackId: %w", err)
	}
	pb, err := s.await(ctx, coord, domain.CodeRelayPlaybackID)
	if err != nil {
		return fmt.Errorf("relay handshake: await playback forwardId: %w", err)
	}
	if pb.Status != statusOK {
		return fmt.Errorf("relay handshake: playback forwardId status %d", pb.Status)
	}
	s.playbackForwardID = pb.ForwardID

	// Step 6: report our own candidate addresses.
	reqConnect := domain.Command{Code: domain.CodeRelayReqConnect, Params: map[string]any{
		"cliIp":      s.candidates.IP,
		"cliPort":    s.candidates.Port,
		"cliNatIp":   s.candidates.NatIP,
		"cliNatPort": s.candidates.NatPort,
		"devTarget":  s.devID,
		"devToken":   s.conf.TarPwd,
	}}
	if err := s.send(ctx, coord, reqConnect); err != nil {
		return fmt.Errorf("relay handshake: send reqConnect: %w", err)
	}

	// Step 7: complete registration. The "status" value is shown in the doc but
	// its meaning is not pinned (doc §12.2 phase 1 step 7) — 1 ("ready/online") is
	// a placeholder, UNCONFIRMED.
	deviceStatus := domain.Command{Code: domain.CodeRelayDeviceStatus, Params: map[string]any{
		"devTarget": s.devID,
		"devToken":  s.conf.TarPwd,
		"status":    1,
	}}
	if err := s.send(ctx, coord, deviceStatus); err != nil {
		return fmt.Errorf("relay handshake: send device status: %w", err)
	}

	// Step 8: registration confirmation.
	reg, err := s.await(ctx, coord, domain.CodeRelayRegisterStatus)
	if err != nil {
		return fmt.Errorf("relay handshake: await registration: %w", err)
	}
	if reg.Status != statusOK {
		return fmt.Errorf("%w: status %d", ErrRelayRegister, reg.Status)
	}
	return nil
}

// send serialises a Command and writes it as a FrameTypeJSON frame on ch.
func (s *RelaySession) send(ctx context.Context, ch RelayChannel, cmd domain.Command) error {
	body, err := cmd.JSON()
	if err != nil {
		return fmt.Errorf("encode command %d: %w", cmd.Code, err)
	}
	return ch.Send(ctx, domain.Frame{Type: domain.FrameTypeJSON, Payload: body})
}

// relayControlMessage is the union of coordinator-channel JSON fields the
// handshake reads across its several response codes.
type relayControlMessage struct {
	Code       domain.Code `json:"code"`
	Status     int         `json:"status"`
	DevIP      string      `json:"devIp"`
	DevPort    int         `json:"devPort"`
	DevNatIP   string      `json:"devNatIp"`
	DevNatPort int         `json:"devNatPort"`
	ForwardID  string      `json:"forwardId"`
}

// await receives JSON control frames on ch until one carries the wanted code,
// then returns it. Non-JSON frames and JSON frames with other codes (interleaved
// presence/housekeeping/time-sync) are skipped. ctx bounds the wait.
//
// Known edge: a coordinator that fast-fails with a terminal {"code":101,
// "status":500} *before* the client reaches the await-101 step would have that
// 101 skipped here, so Connect would block until ctx expires rather than
// returning ErrRelayRegister promptly. Per §12.2 the 101 arrives last (step 8),
// so the normal path is unaffected; harden this only if a real coordinator is
// observed rejecting early.
func (s *RelaySession) await(ctx context.Context, ch RelayChannel, want domain.Code) (relayControlMessage, error) {
	for {
		f, err := ch.Receive(ctx)
		if err != nil {
			return relayControlMessage{}, err
		}
		if f.Type != domain.FrameTypeJSON {
			continue
		}
		var msg relayControlMessage
		if err := json.Unmarshal(f.Payload, &msg); err != nil {
			continue
		}
		if msg.Code == want {
			return msg, nil
		}
	}
}
