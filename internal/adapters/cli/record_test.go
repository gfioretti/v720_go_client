package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gfioretti/v720_go_client/internal/domain"
)

// fakeMux records the mux request and inspects the intermediate files while they
// still exist (the command deletes them after Mux returns), then writes a stub
// MP4 so the command sees success.
type fakeMux struct {
	called     bool
	req        MuxRequest
	videoBytes int64
	audioBytes int64
	err        error
}

func (m *fakeMux) mux(_ context.Context, req MuxRequest) error {
	m.called = true
	m.req = req
	if fi, err := os.Stat(req.VideoPath); err == nil {
		m.videoBytes = fi.Size()
	}
	if req.AudioPath != "" {
		if fi, err := os.Stat(req.AudioPath); err == nil {
			m.audioBytes = fi.Size()
		}
	}
	if m.err != nil {
		return m.err
	}
	return os.WriteFile(req.OutPath, []byte("stub-mp4"), 0o644)
}

func TestRun_RecordCapturesVideoAndAudioThenMuxes(t *testing.T) {
	const devID = "dev123"
	media := []domain.Frame{
		{Type: domain.FrameTypeMJPEG, Payload: []byte("\xff\xd8frame1\xff\xd9")},
		{Type: domain.FrameTypeMJPEG, Payload: []byte("\xff\xd8frame2\xff\xd9")},
		{Type: domain.FrameTypeAudioG711, Payload: []byte{0xD5, 0x55, 0xAA, 0x2A}},
	}
	addr := startMockCameraStreaming(t, devID, media)

	fm := &fakeMux{}
	app, out, errb := newTestApp()
	app.Mux = fm.mux

	outPath := filepath.Join(t.TempDir(), "rec.mp4")
	code := app.Run(context.Background(), []string{
		"ap", "record", "--addr", addr, "--dev", devID,
		"--connect-timeout", "2s", "--duration", "250ms", "--out", outPath,
	})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %q)", code, errb.String())
	}
	if !fm.called {
		t.Fatal("muxer was not called")
	}
	if fm.req.OutPath != outPath {
		t.Errorf("mux OutPath = %q, want %q", fm.req.OutPath, outPath)
	}
	if fm.req.AudioPath == "" {
		t.Error("mux AudioPath empty, want the captured WAV (audio was streamed)")
	}
	if fm.req.FPS <= 0 {
		t.Errorf("mux FPS = %v, want > 0", fm.req.FPS)
	}
	// Two complete JPEGs -> the temp .mjpeg holds both frames' bytes.
	if fm.videoBytes == 0 {
		t.Error("temp video file was empty")
	}
	// 4 A-law samples -> 8 PCM bytes + the 44-byte WAV header.
	if fm.audioBytes != 44+8 {
		t.Errorf("temp audio size = %d, want %d", fm.audioBytes, 44+8)
	}
	// The stub MP4 the fake muxer wrote must be present.
	if b, err := os.ReadFile(outPath); err != nil || string(b) != "stub-mp4" {
		t.Errorf("output file = %q, err %v", b, err)
	}
	if !strings.Contains(out.String(), "with audio") {
		t.Errorf("stdout = %q, want it to mention audio", out.String())
	}
}

func TestRun_RecordVideoOnlyOmitsAudioInput(t *testing.T) {
	const devID = "dev123"
	media := []domain.Frame{
		{Type: domain.FrameTypeMJPEG, Payload: []byte("\xff\xd8only\xff\xd9")},
	}
	addr := startMockCameraStreaming(t, devID, media)

	fm := &fakeMux{}
	app, out, errb := newTestApp()
	app.Mux = fm.mux

	outPath := filepath.Join(t.TempDir(), "novoice.mp4")
	code := app.Run(context.Background(), []string{
		"ap", "record", "--addr", addr, "--dev", devID,
		"--connect-timeout", "2s", "--duration", "200ms", "--out", outPath,
	})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %q)", code, errb.String())
	}
	if fm.req.AudioPath != "" {
		t.Errorf("mux AudioPath = %q, want empty (no audio streamed)", fm.req.AudioPath)
	}
	if !strings.Contains(out.String(), "no audio") {
		t.Errorf("stdout = %q, want it to note no audio", out.String())
	}
}

func TestRun_RecordRequiresOut(t *testing.T) {
	app, _, errb := newTestApp()
	code := app.Run(context.Background(), []string{"ap", "record", "--addr", "x:1", "--dev", "d"})
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(errb.String(), "required") {
		t.Errorf("stderr = %q", errb.String())
	}
}

func TestRun_RecordFfmpegMissing(t *testing.T) {
	const devID = "dev123"
	addr := startMockCameraStreaming(t, devID, []domain.Frame{
		{Type: domain.FrameTypeMJPEG, Payload: []byte("\xff\xd8x\xff\xd9")},
	})
	fm := &fakeMux{err: &exec.Error{Name: "ffmpeg", Err: exec.ErrNotFound}}
	app, _, errb := newTestApp()
	app.Mux = fm.mux

	outPath := filepath.Join(t.TempDir(), "rec.mp4")
	code := app.Run(context.Background(), []string{
		"ap", "record", "--addr", addr, "--dev", devID,
		"--connect-timeout", "2s", "--duration", "150ms", "--out", outPath,
	})
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errb.String(), "install ffmpeg") {
		t.Errorf("stderr = %q, want an ffmpeg install hint", errb.String())
	}
}
