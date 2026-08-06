package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"v720_go_client/internal/domain"
)

// --mjpeg - streams the raw concatenated JPEG frames to stdout (for piping into
// a viewer), keeping human-readable status on stderr so the stream stays clean.
func TestRun_LiveMJPEGStreamsToStdout(t *testing.T) {
	const devID = "dev123"
	f1 := []byte("\xff\xd8AAA\xff\xd9")
	f2 := []byte("\xff\xd8BBB\xff\xd9")
	media := []domain.Frame{
		{Type: domain.FrameTypeMJPEG, Payload: f1},
		{Type: domain.FrameTypeMJPEG, Payload: f2},
	}
	addr := startMockCameraStreaming(t, devID, media)

	app, out, errb := newTestApp()
	code := app.Run(context.Background(), []string{
		"ap", "live", "--addr", addr, "--dev", devID,
		"--connect-timeout", "2s", "--duration", "250ms", "--mjpeg", "-",
	})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %q)", code, errb.String())
	}
	if got, want := out.Bytes(), append(append([]byte{}, f1...), f2...); !bytes.Equal(got, want) {
		t.Fatalf("stdout MJPEG stream = %q, want %q", got, want)
	}
	// Status text must be on stderr, not mixed into the binary stream.
	if !strings.Contains(errb.String(), "streaming from") {
		t.Fatalf("status not on stderr: %q", errb.String())
	}
}

func TestRun_LiveMJPEGStreamsToFile(t *testing.T) {
	const devID = "dev123"
	f1 := []byte("\xff\xd8jpeg1\xff\xd9")
	media := []domain.Frame{{Type: domain.FrameTypeMJPEG, Payload: f1}}
	addr := startMockCameraStreaming(t, devID, media)

	path := filepath.Join(t.TempDir(), "live.mjpeg")
	app, out, errb := newTestApp()
	code := app.Run(context.Background(), []string{
		"ap", "live", "--addr", addr, "--dev", devID,
		"--connect-timeout", "2s", "--duration", "250ms", "--mjpeg", path,
	})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %q)", code, errb.String())
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read mjpeg file: %v", err)
	}
	if !bytes.Equal(got, f1) {
		t.Fatalf("mjpeg file = %q, want %q", got, f1)
	}
	// When streaming to a file (not stdout), status stays on stdout.
	if !strings.Contains(out.String(), "streaming from") {
		t.Fatalf("status = %q", out.String())
	}
}

// Both sinks at once: --mjpeg writes the concatenated stream and --out writes
// the individual .jpg.
func TestRun_LiveMJPEGAndOutTogether(t *testing.T) {
	const devID = "dev123"
	jpg := []byte("\xff\xd8jpeg\xff\xd9")
	addr := startMockCameraStreaming(t, devID, []domain.Frame{{Type: domain.FrameTypeMJPEG, Payload: jpg}})

	dir := t.TempDir()
	streamPath := filepath.Join(dir, "live.mjpeg")
	framesDir := filepath.Join(dir, "frames")
	app, _, errb := newTestApp()
	code := app.Run(context.Background(), []string{
		"ap", "live", "--addr", addr, "--dev", devID,
		"--connect-timeout", "2s", "--duration", "250ms", "--mjpeg", streamPath, "--out", framesDir,
	})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %q)", code, errb.String())
	}
	if got, _ := os.ReadFile(streamPath); !bytes.Equal(got, jpg) {
		t.Fatalf("mjpeg file = %q, want %q", got, jpg)
	}
	if got, err := os.ReadFile(filepath.Join(framesDir, "frame-000001.jpg")); err != nil || !bytes.Equal(got, jpg) {
		t.Fatalf("frame file = %q err=%v, want %q", got, err, jpg)
	}
}

// A --mjpeg target that can't be created exits 1 before any connection.
func TestRun_LiveMJPEGCreateFailure(t *testing.T) {
	app, _, errb := newTestApp()
	badPath := filepath.Join(t.TempDir(), "nonexistent-dir", "live.mjpeg") // parent missing
	code := app.Run(context.Background(), []string{
		"ap", "live", "--addr", "127.0.0.1:1", "--dev", "d", "--mjpeg", badPath,
	})
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errb.String(), "create mjpeg file") {
		t.Fatalf("stderr = %q", errb.String())
	}
}

// Under --mjpeg -, even a connect failure must keep its error off stdout so the
// piped stream is never polluted.
func TestRun_LiveMJPEGStdoutStaysCleanOnError(t *testing.T) {
	app, out, errb := newTestApp()
	code := app.Run(context.Background(), []string{
		"ap", "live", "--addr", "127.0.0.1:1", "--dev", "d", "--connect-timeout", "300ms", "--mjpeg", "-",
	})
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if out.Len() != 0 {
		t.Fatalf("stdout not empty on error under --mjpeg -: %q", out.String())
	}
	if !strings.Contains(errb.String(), "connect") {
		t.Fatalf("error not on stderr: %q", errb.String())
	}
}

func TestRun_LiveMissingRequiredFlags(t *testing.T) {
	app, _, errb := newTestApp()
	if code := app.Run(context.Background(), []string{"ap", "live", "--addr", "127.0.0.1:9"}); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(errb.String(), "required") {
		t.Fatalf("stderr = %q", errb.String())
	}
}

func TestRun_ApUnknownSubcommand(t *testing.T) {
	app, _, errb := newTestApp()
	if code := app.Run(context.Background(), []string{"ap", "wat"}); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(errb.String(), "unknown subcommand") {
		t.Fatalf("stderr = %q", errb.String())
	}
}

// End-to-end: connect + stream, counting frames, over the real aptcp adapter.
func TestRun_LiveCountsFrames(t *testing.T) {
	const devID = "dev123"
	// Each video frame is a complete single-fragment JPEG so the reassembler emits
	// two whole images.
	media := []domain.Frame{
		{Type: domain.FrameTypeMJPEG, Payload: []byte("\xff\xd8jpeg1\xff\xd9")},
		{Type: domain.FrameTypeMJPEG, Payload: []byte("\xff\xd8jpeg2\xff\xd9")},
		{Type: domain.FrameTypeAudioG711, Payload: []byte("g711")},
	}
	addr := startMockCameraStreaming(t, devID, media)

	app, out, errb := newTestApp()
	code := app.Run(context.Background(), []string{
		"ap", "live", "--addr", addr, "--dev", devID, "--connect-timeout", "2s", "--duration", "250ms",
	})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %q)", code, errb.String())
	}
	if !strings.Contains(out.String(), "2 video frame(s), 1 audio frame(s)") {
		t.Fatalf("stdout = %q", out.String())
	}
}

// End-to-end: MJPEG frames are written to --out as sequential .jpg files.
func TestRun_LiveWritesFramesToDir(t *testing.T) {
	const devID = "dev123"
	jpeg := []byte("\xff\xd8\xff\xe0JFIF-ish\xff\xd9")
	media := []domain.Frame{{Type: domain.FrameTypeMJPEG, Payload: jpeg}}
	addr := startMockCameraStreaming(t, devID, media)

	dir := t.TempDir()
	app, out, errb := newTestApp()
	code := app.Run(context.Background(), []string{
		"ap", "live", "--addr", addr, "--dev", devID,
		"--connect-timeout", "2s", "--duration", "250ms", "--out", dir,
	})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %q)", code, errb.String())
	}

	got, err := os.ReadFile(filepath.Join(dir, "frame-000001.jpg"))
	if err != nil {
		t.Fatalf("reading written frame: %v", err)
	}
	if !bytes.Equal(got, jpeg) {
		t.Fatalf("written frame = %q, want %q", got, jpeg)
	}
	if !strings.Contains(out.String(), "written to "+dir) {
		t.Fatalf("stdout = %q", out.String())
	}
}
