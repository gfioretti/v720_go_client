package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"v720_go_client/internal/domain"
)

// runAPLive connects, then streams live view until --duration elapses or the
// process is interrupted, dispatching MJPEG frames (and counting audio) via a
// frameWriter.
func (a *App) runAPLive(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("ap live", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	addr := fs.String("addr", "", "camera AP-mode address, host:port (required)")
	dev := fs.String("dev", "", "device id / target (required)")
	password := fs.String("password", "", "device password (the sk value)")
	connectTimeout := fs.Duration("connect-timeout", 10*time.Second, "handshake timeout")
	duration := fs.Duration("duration", 0, "how long to stream (0 = until interrupted)")
	outDir := fs.String("out", "", "directory to write MJPEG frames as .jpg (empty = count only)")
	debug := fs.Bool("debug", false, "log every frame sent/received to stderr")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *addr == "" || *dev == "" {
		a.errf("v720ctl ap live: --addr and --dev are required\n")
		return 2
	}
	if fs.NArg() > 0 {
		a.errf("v720ctl ap live: unexpected arguments: %v\n", fs.Args())
		return 2
	}

	if *outDir != "" {
		if err := os.MkdirAll(*outDir, 0o755); err != nil {
			a.errf("v720ctl: create output dir: %v\n", err)
			return 1
		}
	}

	// The handshake is bounded by its own deadline; the stream is not (it runs
	// until --duration or the parent ctx, e.g. Ctrl-C).
	connectCtx, cancel := context.WithTimeout(ctx, *connectTimeout)
	session, transport, err := a.dialAndConnect(connectCtx, *addr, *dev, *password, *debug)
	cancel()
	if err != nil {
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

	writer := &frameWriter{dir: *outDir}
	a.outf("streaming from %s (%s)...\n", *dev, streamDescription(*duration))
	if err := session.StartLiveView(streamCtx, writer, writer); err != nil {
		a.errf("v720ctl: live view: %v\n", err)
		return 1
	}

	a.outf("stopped: %d video frame(s), %d audio frame(s)%s\n",
		writer.videoN, writer.audioN, writer.destinationNote())
	return 0
}

func streamDescription(d time.Duration) string {
	if d > 0 {
		return "for " + d.String()
	}
	return "until interrupted"
}

// frameWriter implements the live-view handler ports. Its methods are called
// sequentially from the single StartLiveView loop, so it needs no locking. When
// dir is set it writes each MJPEG frame as a sequentially-numbered .jpg;
// otherwise it just counts. Audio frames are counted (decoding is future work).
type frameWriter struct {
	dir    string
	videoN int
	audioN int
}

func (w *frameWriter) OnVideoFrame(data []byte) error {
	w.videoN++
	if w.dir == "" {
		return nil
	}
	name := filepath.Join(w.dir, fmt.Sprintf("frame-%06d.jpg", w.videoN))
	if err := os.WriteFile(name, data, 0o644); err != nil {
		return fmt.Errorf("write frame %d: %w", w.videoN, err)
	}
	return nil
}

func (w *frameWriter) OnAudioFrame(_ domain.FrameType, _ []byte) error {
	w.audioN++
	return nil
}

func (w *frameWriter) destinationNote() string {
	if w.dir != "" {
		return " written to " + w.dir
	}
	return ""
}
