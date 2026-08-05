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
	media := []domain.Frame{
		{Type: domain.FrameTypeMJPEG, Payload: []byte("jpeg1")},
		{Type: domain.FrameTypeMJPEG, Payload: []byte("jpeg2")},
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
