package cli

import (
	"context"
	"errors"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"
)

// runAPRecord connects and records the live view to a single MP4 with synced
// audio. It captures the raw streams to temporary files — MJPEG video and WAV
// audio — then muxes them with ffmpeg (H.264 + AAC) once recording stops (at
// --duration or on Ctrl-C). The video framerate handed to ffmpeg is *measured*
// from the capture (frames ÷ elapsed) so the video and audio tracks line up
// instead of drifting.
func (a *App) runAPRecord(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("ap record", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	addr := fs.String("addr", "", "camera AP-mode address, host:port (required)")
	dev := fs.String("dev", "", "device id / target (required)")
	password := fs.String("password", "", "device password (the sk value)")
	out := fs.String("out", "", "output .mp4 file (required)")
	connectTimeout := fs.Duration("connect-timeout", 10*time.Second, "handshake timeout")
	duration := fs.Duration("duration", 0, "how long to record (0 = until interrupted)")
	keepTemp := fs.Bool("keep-temp", false, "keep the intermediate .mjpeg/.wav files for debugging")
	debug := fs.Bool("debug", false, "log every frame sent/received to stderr")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *addr == "" || *dev == "" || *out == "" {
		a.errf("v720ctl ap record: --addr, --dev and --out are required\n")
		return 2
	}
	if fs.NArg() > 0 {
		a.errf("v720ctl ap record: unexpected arguments: %v\n", fs.Args())
		return 2
	}

	tmpDir, err := os.MkdirTemp("", "v720rec-")
	if err != nil {
		a.errf("v720ctl: create temp dir: %v\n", err)
		return 1
	}
	if *keepTemp {
		a.errf("keeping intermediate files in %s\n", tmpDir)
	} else {
		defer func() { _ = os.RemoveAll(tmpDir) }()
	}
	videoPath := filepath.Join(tmpDir, "video.mjpeg")
	audioPath := filepath.Join(tmpDir, "audio.wav")

	videoFile, err := os.Create(videoPath)
	if err != nil {
		a.errf("v720ctl: create temp video: %v\n", err)
		return 1
	}
	wav, err := newWAVWriter(audioPath, 8000, 1)
	if err != nil {
		_ = videoFile.Close()
		a.errf("v720ctl: create temp audio: %v\n", err)
		return 1
	}

	connectCtx, cancel := context.WithTimeout(ctx, *connectTimeout)
	session, transport, err := a.dialAndConnect(connectCtx, *addr, *dev, *password, *debug)
	cancel()
	if err != nil {
		_ = videoFile.Close()
		_ = wav.Close()
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

	writer := &frameWriter{mjpeg: videoFile, wav: wav}
	a.outf("recording %s (%s)... Ctrl-C to stop\n", *dev, streamDescription(*duration))
	start := time.Now()
	streamErr := session.StartLiveView(streamCtx, writer, writer)
	elapsed := time.Since(start)

	// Finalise the intermediate files before muxing.
	_ = videoFile.Close()
	_ = wav.Close()

	if streamErr != nil {
		a.errf("v720ctl: live view: %v\n", streamErr)
		return 1
	}
	if writer.videoN == 0 {
		a.errf("v720ctl: no video captured — nothing to record\n")
		return 1
	}

	fps := measuredFPS(writer.videoN, elapsed)
	req := MuxRequest{VideoPath: videoPath, OutPath: *out, FPS: fps}
	if writer.audioN > 0 {
		req.AudioPath = audioPath
	}

	// Mux with a fresh context: the recording context may already be cancelled
	// (that is how Ctrl-C stops capture), but we still want to write out what was
	// recorded. Cap it so a stuck ffmpeg cannot hang forever.
	muxCtx, stop := context.WithTimeout(context.Background(), 5*time.Minute)
	defer stop()
	if err := a.Mux(muxCtx, req); err != nil {
		a.errf("v720ctl: mux to MP4: %v\n", err)
		if errors.Is(err, exec.ErrNotFound) {
			a.errf("hint: install ffmpeg to record MP4 (brew install ffmpeg)\n")
		}
		return 1
	}

	a.outf("recorded %d frame(s) over %s (~%.1f fps)%s -> %s\n",
		writer.videoN, elapsed.Round(100*time.Millisecond), fps, audioNote(writer.audioN), *out)
	return 0
}

// measuredFPS derives the capture's real frame rate, clamped to a sane range so
// a degenerate measurement (e.g. a single frame in a near-zero interval) can't
// produce a nonsensical rate for ffmpeg.
func measuredFPS(frames int, elapsed time.Duration) float64 {
	secs := elapsed.Seconds()
	if secs <= 0 || frames <= 0 {
		return 10 // reasonable default for this camera family
	}
	fps := float64(frames) / secs
	if fps < 1 {
		return 1
	}
	if fps > 120 {
		return 120
	}
	return fps
}

func audioNote(audioN int) string {
	if audioN > 0 {
		return " with audio"
	}
	return " (no audio)"
}

// ffmpegMux is the real Muxer: it invokes ffmpeg to combine the captured MJPEG
// and WAV into an H.264/AAC MP4. The measured framerate is applied to the MJPEG
// input so the muxed video runs at real time and stays in sync with the audio.
func (a *App) ffmpegMux(ctx context.Context, req MuxRequest) error {
	args := []string{
		"-y",
		"-f", "mjpeg", "-framerate", strconv.FormatFloat(req.FPS, 'f', 4, 64), "-i", req.VideoPath,
	}
	if req.AudioPath != "" {
		args = append(args, "-i", req.AudioPath)
	}
	// yuvj422p (MJPEG) -> yuv420p so the MP4 plays everywhere.
	args = append(args, "-c:v", "libx264", "-pix_fmt", "yuv420p")
	if req.AudioPath != "" {
		args = append(args, "-c:a", "aac", "-shortest")
	}
	args = append(args, req.OutPath)

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	cmd.Stdout = a.Stderr
	cmd.Stderr = a.Stderr
	return cmd.Run()
}
