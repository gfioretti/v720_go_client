package cli

import (
	"bytes"
	"context"
	"io"
	"os/exec"
	"strings"
	"testing"

	"v720_go_client/internal/domain"
)

// fakePlayer records the launch command and captures everything streamed to the
// player's stdin, standing in for a real ffplay subprocess.
type fakePlayer struct {
	command []string
	stdin   bytes.Buffer
	waited  bool
	err     error // if set, launching fails with this error
}

func (p *fakePlayer) launch(_ context.Context, command []string) (io.WriteCloser, func() error, error) {
	if p.err != nil {
		return nil, nil, p.err
	}
	p.command = command
	return nopWriteCloser{&p.stdin}, func() error { p.waited = true; return nil }, nil
}

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

func TestRun_PreviewStreamsToPlayer(t *testing.T) {
	const devID = "dev123"
	jpeg := []byte("\xff\xd8preview\xff\xd9")
	addr := startMockCameraStreaming(t, devID, []domain.Frame{
		{Type: domain.FrameTypeMJPEG, Payload: jpeg},
	})

	fp := &fakePlayer{}
	app, out, errb := newTestApp()
	app.Player = fp.launch

	code := app.Run(context.Background(), []string{
		"ap", "preview", "--addr", addr, "--dev", devID,
		"--connect-timeout", "2s", "--duration", "250ms",
	})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %q)", code, errb.String())
	}
	// Default player command and the streamed image both must be right.
	if strings.Join(fp.command, " ") != "ffplay -f mjpeg -i -" {
		t.Errorf("player command = %v, want default ffplay", fp.command)
	}
	if !bytes.Equal(fp.stdin.Bytes(), jpeg) {
		t.Errorf("streamed %q to player, want %q", fp.stdin.Bytes(), jpeg)
	}
	if !fp.waited {
		t.Errorf("player was not waited on")
	}
	// The video stream must not leak onto our stdout (it belongs to the player).
	if out.Len() != 0 {
		t.Errorf("stdout = %q, want empty", out.String())
	}
}

func TestRun_PreviewCustomPlayer(t *testing.T) {
	const devID = "dev123"
	addr := startMockCameraStreaming(t, devID, nil)

	fp := &fakePlayer{}
	app, _, errb := newTestApp()
	app.Player = fp.launch

	code := app.Run(context.Background(), []string{
		"ap", "preview", "--addr", addr, "--dev", devID,
		"--connect-timeout", "2s", "--duration", "150ms", "--player", "mpv --demuxer=mjpeg -",
	})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %q)", code, errb.String())
	}
	if strings.Join(fp.command, " ") != "mpv --demuxer=mjpeg -" {
		t.Errorf("player command = %v, want the custom one", fp.command)
	}
}

func TestRun_PreviewPlayerNotFound(t *testing.T) {
	fp := &fakePlayer{err: &exec.Error{Name: "ffplay", Err: exec.ErrNotFound}}
	app, _, errb := newTestApp()
	app.Player = fp.launch

	code := app.Run(context.Background(), []string{
		"ap", "preview", "--addr", "127.0.0.1:1", "--dev", "d",
	})
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errb.String(), "install ffmpeg") {
		t.Errorf("stderr = %q, want an install hint", errb.String())
	}
}

func TestRun_PreviewRequiresAddrAndDev(t *testing.T) {
	app, _, errb := newTestApp()
	app.Player = (&fakePlayer{}).launch
	if code := app.Run(context.Background(), []string{"ap", "preview", "--addr", "x:1"}); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(errb.String(), "required") {
		t.Errorf("stderr = %q", errb.String())
	}
}
