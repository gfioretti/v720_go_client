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

	"v720_go_client/internal/adapters/transport/framedtcp"
	"v720_go_client/internal/application"
	"v720_go_client/internal/domain"
)

// Dialer opens a Transport to the given address. It is a seam: the real App uses
// the framedtcp adapter, while tests inject a fake to exercise the command wiring
// without a socket.
type Dialer func(ctx context.Context, addr string) (application.Transport, error)

// PlayerLauncher starts a media-player subprocess (e.g. ffplay) and returns a
// writer for its stdin, into which the caller streams MJPEG, plus a wait function
// to call once stdin is closed. It is a seam: the real App spawns a process,
// while tests inject a fake that captures the streamed bytes.
type PlayerLauncher func(ctx context.Context, command []string) (stdin io.WriteCloser, wait func() error, err error)

// MuxRequest describes a video+audio muxing job for a Muxer.
type MuxRequest struct {
	VideoPath string  // captured MJPEG file (concatenated JPEGs)
	AudioPath string  // captured WAV file; empty when no audio was recorded
	OutPath   string  // destination .mp4
	FPS       float64 // measured video frame rate, so the tracks stay aligned
}

// Muxer combines the captured MJPEG video and WAV audio into a single MP4. It is
// a seam: the real App shells out to ffmpeg, while tests inject a fake that
// records the request without spawning anything.
type Muxer func(ctx context.Context, req MuxRequest) error

// App holds the CLI's I/O sinks, its transport dialer, its media-player
// launcher, and its muxer. Construct it with NewApp for the real wiring.
type App struct {
	Stdout io.Writer
	Stderr io.Writer
	Dial   Dialer
	Player PlayerLauncher
	Mux    Muxer
}

// NewApp returns an App wired to the real AP-mode TCP transport, a real
// process-spawning media-player launcher, and a real ffmpeg-based muxer.
func NewApp(stdout, stderr io.Writer) *App {
	a := &App{Stdout: stdout, Stderr: stderr, Dial: dialAPTCP}
	a.Player = a.launchPlayer
	a.Mux = a.ffmpegMux
	return a
}

func dialAPTCP(ctx context.Context, addr string) (application.Transport, error) {
	c, err := framedtcp.Dial(ctx, addr)
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
  v720ctl ap preview --addr <host:port> --dev <devId> [--password <pwd>] [--player <cmd>] [--duration <dur>]
  v720ctl ap serve   --addr <host:port> --dev <devId> [--password <pwd>] [--http :8080]
  v720ctl ap record  --addr <host:port> --dev <devId> [--password <pwd>] --out <file.mp4> [--duration <dur>] [--quality 1|2]
  v720ctl ap set --addr <host:port> --dev <devId> [--password <pwd>] <setting> <value>
  v720ctl ap sd-list --addr <host:port> --dev <devId> [--password <pwd>]

Commands:
  ap connect   Connect over the camera's AP-mode Wi-Fi hotspot and run the handshake.
  ap live      Connect, then stream live view. --mjpeg emits a raw MJPEG stream for live viewing
               (pipe '--mjpeg -' into: ffplay -f mjpeg -i -), --out saves frames as .jpg, and
               --audio-out saves the G.711 audio as a playable .wav.
  ap preview   Connect, then open a live video window by piping the stream into a player
               (default: ffplay -f mjpeg -i -). Override the player with --player.
  ap serve     Connect, then serve a browser control panel over HTTP (default :8080): live
               MJPEG video plus buttons for HD/SD, IR, LED, motion alert, camera and snapshot.
  ap record    Connect, then record live view to a single MP4 with synced audio
               (H.264 + AAC via ffmpeg). Stops at --duration or on Ctrl-C.
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
		a.errf("v720ctl ap: expected a subcommand (connect, live, preview, serve, record, set, sd-list)\n\n%s", usage)
		return 2
	}
	switch args[0] {
	case "connect":
		return a.runAPConnect(ctx, args[1:])
	case "live":
		return a.runAPLive(ctx, args[1:])
	case "preview":
		return a.runAPPreview(ctx, args[1:])
	case "serve":
		return a.runAPServe(ctx, args[1:])
	case "record":
		return a.runAPRecord(ctx, args[1:])
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

// applyVideoQuality sends the video quality / HD grade command (doc §12.5 code
// 201) when quality > 0, so a streaming command can request HD within the same
// session before the stream starts. Grade 1 = SD/smooth, 2 = HD (the app's own
// values). quality <= 0 is a no-op (leave the camera's current grade untouched).
func (a *App) applyVideoQuality(ctx context.Context, session *application.APSession, quality int) error {
	if quality <= 0 {
		return nil
	}
	c, cancel := context.WithTimeout(ctx, sendCommandTimeout)
	defer cancel()
	return session.SendCommand(c, domain.NewVideoQuality(quality))
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
