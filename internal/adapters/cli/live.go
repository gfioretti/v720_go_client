package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"v720_go_client/internal/domain"
)

// runAPLive connects, then streams live view until --duration elapses or the
// process is interrupted, dispatching MJPEG frames (and counting audio) via a
// frameWriter. With --mjpeg it emits a raw MJPEG stream (concatenated JPEG
// frames) for live viewing; with --out it also writes each frame as a .jpg.
func (a *App) runAPLive(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("ap live", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	addr := fs.String("addr", "", "camera AP-mode address, host:port (required)")
	dev := fs.String("dev", "", "device id / target (required)")
	password := fs.String("password", "", "device password (the sk value)")
	connectTimeout := fs.Duration("connect-timeout", 10*time.Second, "handshake timeout")
	duration := fs.Duration("duration", 0, "how long to stream (0 = until interrupted)")
	outDir := fs.String("out", "", "directory to write MJPEG frames as .jpg (empty = don't)")
	mjpeg := fs.String("mjpeg", "", "write a raw MJPEG stream here for live viewing; '-' = stdout\n"+
		"\t(watch live: v720ctl ap live ... --mjpeg - | ffplay -f mjpeg -i -)")
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

	// Open the MJPEG stream sink. When it's stdout, human-readable status must go
	// to stderr instead so it doesn't corrupt the binary stream a viewer reads.
	mjpegW, closeMJPEG, status, rc := a.openMJPEGSink(*mjpeg)
	if rc != 0 {
		return rc
	}
	defer closeMJPEG()

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

	writer := &frameWriter{dir: *outDir, mjpeg: mjpegW}
	status("streaming from %s (%s)...\n", *dev, streamDescription(*duration))
	if err := session.StartLiveView(streamCtx, writer, writer); err != nil {
		a.errf("v720ctl: live view: %v\n", err)
		return 1
	}

	status("stopped: %d video frame(s), %d audio frame(s)%s\n",
		writer.videoN, writer.audioN, writer.destinationNote())
	return 0
}

// openMJPEGSink resolves the --mjpeg target into a writer, a close function, and
// the status-line printer to use (stderr when the stream owns stdout). rc is
// non-zero only on failure. When mjpeg is empty there is no stream sink.
func (a *App) openMJPEGSink(mjpeg string) (w io.Writer, closeFn func(), status func(string, ...any), rc int) {
	switch mjpeg {
	case "":
		return nil, func() {}, a.outf, 0
	case "-":
		// Stream to stdout; keep status off stdout so the stream stays clean.
		return a.Stdout, func() {}, a.errf, 0
	default:
		f, err := os.Create(mjpeg)
		if err != nil {
			a.errf("v720ctl: create mjpeg file: %v\n", err)
			return nil, func() {}, a.outf, 1
		}
		return f, func() { _ = f.Close() }, a.outf, 0
	}
}

func streamDescription(d time.Duration) string {
	if d > 0 {
		return "for " + d.String()
	}
	return "until interrupted"
}

// frameWriter implements the live-view handler ports. Its methods are called
// sequentially from the single StartLiveView loop, so it needs no locking. It
// optionally appends each MJPEG frame to a raw MJPEG stream (mjpeg) and/or writes
// it as a sequentially-numbered .jpg (dir); audio frames are counted (decoding is
// future work).
type frameWriter struct {
	dir    string
	mjpeg  io.Writer
	videoN int
	audioN int
}

func (w *frameWriter) OnVideoFrame(data []byte) error {
	w.videoN++
	// A concatenation of complete JPEG frames is a valid MJPEG stream: each frame
	// carries its own SOI/EOI markers, which the viewer's demuxer splits on.
	if w.mjpeg != nil {
		if _, err := w.mjpeg.Write(data); err != nil {
			return fmt.Errorf("write mjpeg stream: %w", err)
		}
	}
	if w.dir != "" {
		name := filepath.Join(w.dir, fmt.Sprintf("frame-%06d.jpg", w.videoN))
		if err := os.WriteFile(name, data, 0o644); err != nil {
			return fmt.Errorf("write frame %d: %w", w.videoN, err)
		}
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
