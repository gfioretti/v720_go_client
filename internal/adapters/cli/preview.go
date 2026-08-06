package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// defaultPlayerCommand plays a raw MJPEG stream arriving on stdin. ffplay is part
// of ffmpeg and reads the concatenated-JPEG stream our reassembler produces.
var defaultPlayerCommand = []string{"ffplay", "-f", "mjpeg", "-i", "-"}

// runAPPreview connects and opens a live video window by streaming the
// reassembled MJPEG into a media player (ffplay by default) until the player is
// closed, --duration elapses, or the process is interrupted.
func (a *App) runAPPreview(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("ap preview", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	addr := fs.String("addr", "", "camera AP-mode address, host:port (required)")
	dev := fs.String("dev", "", "device id / target (required)")
	password := fs.String("password", "", "device password (the sk value)")
	connectTimeout := fs.Duration("connect-timeout", 10*time.Second, "handshake timeout")
	duration := fs.Duration("duration", 0, "how long to preview (0 = until the player is closed)")
	player := fs.String("player", strings.Join(defaultPlayerCommand, " "),
		"player command; the MJPEG stream is written to its stdin")
	debug := fs.Bool("debug", false, "log every frame sent/received to stderr")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *addr == "" || *dev == "" {
		a.errf("v720ctl ap preview: --addr and --dev are required\n")
		return 2
	}
	if fs.NArg() > 0 {
		a.errf("v720ctl ap preview: unexpected arguments: %v\n", fs.Args())
		return 2
	}
	command := strings.Fields(*player)
	if len(command) == 0 {
		a.errf("v720ctl ap preview: --player is empty\n")
		return 2
	}

	// Start the player first: if it can't launch (e.g. ffplay not installed),
	// fail before touching the camera.
	stdin, wait, err := a.Player(ctx, command)
	if err != nil {
		a.errf("v720ctl: start player %q: %v\n", command[0], err)
		if errors.Is(err, exec.ErrNotFound) {
			a.errf("hint: install ffmpeg (which provides ffplay), or pass --player with a viewer that reads MJPEG from stdin\n")
		}
		return 1
	}

	connectCtx, cancel := context.WithTimeout(ctx, *connectTimeout)
	session, transport, err := a.dialAndConnect(connectCtx, *addr, *dev, *password, *debug)
	cancel()
	if err != nil {
		_ = stdin.Close()
		_ = wait()
		a.errf("v720ctl: %v\n", err)
		return 1
	}
	defer func() { _ = transport.Close() }()

	streamCtx := ctx
	if *duration > 0 {
		var stop context.CancelFunc
		streamCtx, stop = context.WithTimeout(ctx, *duration)
		defer stop()
	}

	writer := &frameWriter{mjpeg: stdin}
	a.errf("previewing %s (%s)... close the player window to stop\n", *dev, previewDescription(*duration))
	streamErr := session.StartLiveView(streamCtx, writer, nil)

	// Closing stdin signals end-of-stream so the player can exit; then wait for it.
	_ = stdin.Close()
	_ = wait()

	// A write failure to the player's stdin means the user closed the window —
	// that is the normal way to stop a preview, not an error.
	if streamErr != nil && !isPlayerClosed(streamErr) {
		a.errf("v720ctl: live view: %v\n", streamErr)
		return 1
	}
	a.errf("preview ended: %d frame(s) shown\n", writer.videoN)
	return 0
}

// previewDescription mirrors streamDescription but phrased for the preview.
func previewDescription(d time.Duration) string {
	if d > 0 {
		return "for " + d.String()
	}
	return "live"
}

// isPlayerClosed reports whether err is the broken-pipe / closed-pipe failure we
// get once the player process has exited and we keep writing frames to it.
func isPlayerClosed(err error) bool {
	return errors.Is(err, io.ErrClosedPipe) || errors.Is(err, os.ErrClosed) ||
		strings.Contains(err.Error(), "broken pipe")
}

// launchPlayer is the real PlayerLauncher: it spawns the player as a subprocess,
// wiring our process's stdin pipe to it and routing the player's own output to
// the CLI's stderr so it never corrupts stdout.
func (a *App) launchPlayer(ctx context.Context, command []string) (io.WriteCloser, func() error, error) {
	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	cmd.Stdout = a.Stderr
	cmd.Stderr = a.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, fmt.Errorf("%w", err)
	}
	return stdin, cmd.Wait, nil
}
