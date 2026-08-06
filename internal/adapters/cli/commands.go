// Package cli is the primary/driving adapter: it parses command-line arguments
// and drives the application layer (application.APSession) to talk to the
// camera. It is the outside world calling in, so it depends inward on the
// application layer and the transport adapter it wires up, never the reverse.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"v720_go_client/internal/adapters/transport/aptcp"
	"v720_go_client/internal/application"
)

// Dialer opens a Transport to the given address. It is a seam: the real App uses
// the aptcp adapter, while tests inject a fake to exercise the command wiring
// without a socket.
type Dialer func(ctx context.Context, addr string) (application.Transport, error)

// App holds the CLI's I/O sinks and its transport dialer. Construct it with
// NewApp for the real wiring.
type App struct {
	Stdout io.Writer
	Stderr io.Writer
	Dial   Dialer
}

// NewApp returns an App wired to the real AP-mode TCP transport.
func NewApp(stdout, stderr io.Writer) *App {
	return &App{Stdout: stdout, Stderr: stderr, Dial: dialAPTCP}
}

func dialAPTCP(ctx context.Context, addr string) (application.Transport, error) {
	c, err := aptcp.Dial(ctx, addr)
	if err != nil {
		return nil, err // return an untyped nil interface, not a nil *Channel
	}
	return c, nil
}

// outf/errf are the single choke points for CLI output. A write to stdout/stderr
// can only fail for reasons a CLI cannot act on (closed pipe), so the error is
// deliberately discarded here, in one documented place, rather than at every call.
func (a *App) outf(format string, args ...any) { _, _ = fmt.Fprintf(a.Stdout, format, args...) }
func (a *App) errf(format string, args ...any) { _, _ = fmt.Fprintf(a.Stderr, format, args...) }

const usage = `v720ctl — independent client for the V720 / A9 Mini WiFi camera

Usage:
  v720ctl ap connect --addr <host:port> --dev <devId> [--password <pwd>] [--timeout <dur>]
  v720ctl ap live    --addr <host:port> --dev <devId> [--password <pwd>] [--mjpeg -|<file>] [--out <dir>] [--audio-out <file.wav>] [--duration <dur>]
  v720ctl ap set --addr <host:port> --dev <devId> [--password <pwd>] <setting> <value>
  v720ctl ap sd-list --addr <host:port> --dev <devId> [--password <pwd>]

Commands:
  ap connect   Connect over the camera's AP-mode Wi-Fi hotspot and run the handshake.
  ap live      Connect, then stream live view. --mjpeg emits a raw MJPEG stream for live viewing
               (pipe '--mjpeg -' into: ffplay -f mjpeg -i -), --out saves frames as .jpg, and
               --audio-out saves the G.711 audio as a playable .wav.
  ap set       Connect, then send one device-control command (settings: camera, ir, motion-alert, move-grade, quality, white-led).
  ap sd-list   Connect, then list the SD-card recording catalogue (dates → hours → minutes).
`

// Run dispatches args (excluding the program name) and returns a process exit
// code: 0 success, 1 runtime failure, 2 usage error.
func (a *App) Run(ctx context.Context, args []string) int {
	if len(args) == 0 {
		a.errf("%s", usage)
		return 2
	}
	switch args[0] {
	case "ap":
		return a.runAP(ctx, args[1:])
	case "help", "-h", "--help":
		a.outf("%s", usage)
		return 0
	default:
		a.errf("v720ctl: unknown command %q\n\n%s", args[0], usage)
		return 2
	}
}

func (a *App) runAP(ctx context.Context, args []string) int {
	if len(args) == 0 {
		a.errf("v720ctl ap: expected a subcommand (connect, live, set, sd-list)\n\n%s", usage)
		return 2
	}
	switch args[0] {
	case "connect":
		return a.runAPConnect(ctx, args[1:])
	case "live":
		return a.runAPLive(ctx, args[1:])
	case "set":
		return a.runAPSet(ctx, args[1:])
	case "sd-list":
		return a.runAPSDList(ctx, args[1:])
	default:
		a.errf("v720ctl ap: unknown subcommand %q\n\n%s", args[0], usage)
		return 2
	}
}

// dialAndConnect dials the transport and runs the AP-mode handshake, returning
// the connected session and its transport (which the caller must Close). ctx
// should carry the connect deadline. When debug is set, the transport is wrapped
// so every frame is logged to stderr. On failure the transport is closed here.
func (a *App) dialAndConnect(ctx context.Context, addr, dev, password string, debug bool) (*application.APSession, application.Transport, error) {
	transport, err := a.Dial(ctx, addr)
	if err != nil {
		return nil, nil, fmt.Errorf("connect: %w", err)
	}
	if debug {
		transport = newDebugTransport(transport, a.errf)
	}
	session := application.NewAPSession(transport, dev, password)
	if err := session.Connect(ctx); err != nil {
		_ = transport.Close()
		return nil, nil, fmt.Errorf("handshake failed: %w", err)
	}
	return session, transport, nil
}

func (a *App) runAPConnect(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("ap connect", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	addr := fs.String("addr", "", "camera AP-mode address, host:port (required)")
	dev := fs.String("dev", "", "device id / target (required)")
	password := fs.String("password", "", "device password (the sk value)")
	timeout := fs.Duration("timeout", 10*time.Second, "overall connect timeout (dial + handshake)")
	debug := fs.Bool("debug", false, "log every frame sent/received to stderr")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0 // an explicit -h/--help is not a usage error
		}
		return 2 // flag package already printed the error to a.Stderr
	}
	if *addr == "" || *dev == "" {
		a.errf("v720ctl ap connect: --addr and --dev are required\n")
		return 2
	}
	if fs.NArg() > 0 {
		a.errf("v720ctl ap connect: unexpected arguments: %v\n", fs.Args())
		return 2
	}

	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()

	_, transport, err := a.dialAndConnect(ctx, *addr, *dev, *password, *debug)
	if err != nil {
		a.errf("v720ctl: %v\n", err)
		return 1
	}
	defer func() { _ = transport.Close() }()

	a.outf("connected to %s at %s\n", *dev, *addr)
	return 0
}
