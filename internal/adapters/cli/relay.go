package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"time"

	"v720_go_client/internal/adapters/naxclowapi"
	"v720_go_client/internal/adapters/transport/relay"
	"v720_go_client/internal/application"
)

// newCloudAPI is the real CloudFactory: a naxclowapi client for baseURL.
func newCloudAPI(baseURL string) application.CloudAPI {
	return naxclowapi.New(baseURL, nil)
}

func (a *App) runRelay(ctx context.Context, args []string) int {
	if len(args) == 0 {
		a.errf("v720ctl relay: expected a subcommand (connect)\n\n%s", usage)
		return 2
	}
	switch args[0] {
	case "connect":
		return a.runRelayConnect(ctx, args[1:])
	default:
		a.errf("v720ctl relay: unknown subcommand %q\n\n%s", args[0], usage)
		return 2
	}
}

// runRelayConnect wires the whole Mode A setup path end to end: cloud rendezvous
// (login → watching → getA9VideoConf) followed by the phase-1 coordinator
// handshake. It stops after registration — live A/V over the UDP media channel is
// the deferred phase-2 work (see docs/mode-a-client-design.md).
func (a *App) runRelayConnect(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("relay connect", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	user := fs.String("user", "", "cloud account phone/username (required)")
	pass := fs.String("pass", "", "cloud account password (required)")
	dev := fs.String("dev", "", "device id / devicesCode (required)")
	devPass := fs.String("dev-pass", "", "device password, validated via the watching call")
	baseURL := fs.String("base-url", naxclowapi.DefaultBaseURL, "cloud REST base URL; point at a self-hosted server to bypass naxclow")
	timeout := fs.Duration("timeout", 20*time.Second, "overall timeout (cloud calls + dial + handshake)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *user == "" || *pass == "" || *dev == "" {
		a.errf("v720ctl relay connect: --user, --pass and --dev are required\n")
		return 2
	}
	if fs.NArg() > 0 {
		a.errf("v720ctl relay connect: unexpected arguments: %v\n", fs.Args())
		return 2
	}

	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()

	session, transport, err := a.relayConnect(ctx, *baseURL, *user, *pass, *dev, *devPass)
	if err != nil {
		a.errf("v720ctl: %v\n", err)
		return 1
	}
	defer func() { _ = transport.Close() }()

	a.outf("relay registered: device %s via coordinator\n", *dev)
	a.outf("  live forwardId:     %s\n", session.LiveForwardID())
	a.outf("  playback forwardId: %s\n", session.PlaybackForwardID())
	a.outf("note: live A/V over the UDP media channel is not yet implemented (phase 2 deferred).\n")
	return 0
}

// relayConnect performs the cloud rendezvous and the phase-1 coordinator
// handshake, returning the registered session and the transport (which the caller
// must Close). Only the coordinator channel is dialed here; the media/storage
// channels belong to the deferred phase-2 work. On failure the transport, if
// opened, is closed.
func (a *App) relayConnect(ctx context.Context, baseURL, user, pass, dev, devPass string) (*application.RelaySession, application.RelayTransport, error) {
	cloud := a.Cloud(baseURL)

	account, err := cloud.Login(ctx, user, pass)
	if err != nil {
		return nil, nil, fmt.Errorf("cloud login: %w", err)
	}
	if err := cloud.Watching(ctx, dev, devPass); err != nil {
		return nil, nil, fmt.Errorf("cloud watching: %w", err)
	}
	conf, err := cloud.GetA9VideoConf(ctx, dev)
	if err != nil {
		return nil, nil, fmt.Errorf("cloud getA9VideoConf: %w", err)
	}

	coord, err := a.Dial(ctx, conf.Addr())
	if err != nil {
		return nil, nil, fmt.Errorf("dial coordinator %s: %w", conf.Addr(), err)
	}
	// Only the coordinator is needed for phase 1: RelaySession.Connect uses
	// Coordinator() exclusively and never touches Media()/Storage(), and the
	// bundle's Close tolerates the nil channels. Media/storage are dialed later,
	// once phase 2 is built.
	transport := relay.NewTransport(coord, nil, nil)

	// Client candidate addresses are left empty: real discovery (STUN / the media
	// socket bind) is part of the deferred phase-2 work (see ClientCandidates).
	session := application.NewRelaySession(transport, account, conf, dev, application.ClientCandidates{})
	if err := session.Connect(ctx); err != nil {
		_ = transport.Close()
		return nil, nil, fmt.Errorf("relay handshake failed: %w", err)
	}
	return session, transport, nil
}
