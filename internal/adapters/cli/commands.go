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

Commands:
  ap connect   Connect over the camera's AP-mode Wi-Fi hotspot and run the handshake.
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
	if len(args) == 0 || args[0] != "connect" {
		a.errf("v720ctl ap: expected \"connect\"\n\n%s", usage)
		return 2
	}
	return a.runAPConnect(ctx, args[1:])
}

func (a *App) runAPConnect(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("ap connect", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	addr := fs.String("addr", "", "camera AP-mode address, host:port (required)")
	dev := fs.String("dev", "", "device id / target (required)")
	password := fs.String("password", "", "device password (the sk value)")
	timeout := fs.Duration("timeout", 10*time.Second, "overall connect timeout (dial + handshake)")
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

	transport, err := a.Dial(ctx, *addr)
	if err != nil {
		a.errf("v720ctl: connect: %v\n", err)
		return 1
	}
	defer func() { _ = transport.Close() }()

	session := application.NewAPSession(transport, *dev, *password)
	if err := session.Connect(ctx); err != nil {
		a.errf("v720ctl: handshake failed: %v\n", err)
		return 1
	}

	a.outf("connected to %s at %s\n", *dev, *addr)
	return 0
}
