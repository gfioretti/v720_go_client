package cli

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gfioretti/v720_go_client/internal/domain"
)

func TestRecorder_CaptureAndFinish(t *testing.T) {
	var r recorder
	out := "out.mp4"
	if err := r.start(out); err != nil {
		t.Fatalf("start: %v", err)
	}
	if !r.isActive() {
		t.Fatal("not active after start")
	}
	if err := r.start("again.mp4"); err == nil {
		t.Fatal("second start should fail while recording")
	}

	r.onVideo([]byte("\xff\xd8a\xff\xd9"))
	r.onVideo([]byte("\xff\xd8b\xff\xd9"))
	r.onAudio(domain.FrameTypeAudioG711, []byte{0xD5, 0x55})
	r.onAudio(domain.FrameTypeAudioPCM, []byte{1, 2}) // not G711 -> ignored

	fin, err := r.finish()
	if err != nil {
		t.Fatalf("finish: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(fin.dir) })

	if r.isActive() {
		t.Fatal("still active after finish")
	}
	if fin.videoN != 2 {
		t.Errorf("videoN = %d, want 2", fin.videoN)
	}
	if fin.req.OutPath != out {
		t.Errorf("OutPath = %q, want %q", fin.req.OutPath, out)
	}
	if fin.req.AudioPath == "" {
		t.Error("AudioPath empty despite G711 audio captured")
	}
	if fin.req.FPS <= 0 {
		t.Errorf("FPS = %v, want > 0", fin.req.FPS)
	}
	if fi, err := os.Stat(fin.req.VideoPath); err != nil || fi.Size() == 0 {
		t.Errorf("temp video missing/empty: %v", err)
	}
	// Frames after finishing are ignored, and finishing twice errors.
	r.onVideo([]byte("\xff\xd8c\xff\xd9"))
	if _, err := r.finish(); err == nil {
		t.Error("finish twice should error")
	}
}

func TestMJPEGServer_RecordUnavailableWithoutSession(t *testing.T) {
	ts := httptest.NewServer(newMJPEGServer().routes()) // no session/mux
	defer ts.Close()
	resp, _ := http.Post(ts.URL+"/record?action=start", "", nil)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 without a session", resp.StatusCode)
	}
	resp.Body.Close()
}

// startMockCameraStreamingRepeating streams frame continuously so frames arrive
// during a recording window.
func startMockCameraStreamingRepeating(t *testing.T, devID string, frame domain.Frame, interval time.Duration) string {
	t.Helper()
	return serveMockCamera(t, func(conn net.Conn) {
		if !mockHandshake(conn, devID, 200) {
			return
		}
		for {
			if err := writeStreamFrame(conn, frame); err != nil {
				return
			}
			time.Sleep(interval)
		}
	})
}

// End-to-end: start/stop a recording via /record on the real serve command; the
// captured frames must reach the muxer and the response report the output.
func TestRun_ServeRecordEndToEnd(t *testing.T) {
	const devID = "dev123"
	frame := domain.Frame{Type: domain.FrameTypeMJPEG, Payload: []byte("\xff\xd8rec\xff\xd9")}
	camAddr := startMockCameraStreamingRepeating(t, devID, frame, 15*time.Millisecond)
	httpAddr := freePort(t)

	app := NewApp(&syncBuffer{}, &syncBuffer{})
	// Fake muxer: record nothing to disk, just succeed (no shared state read by the
	// test — assertions use the HTTP response body).
	app.Mux = func(_ context.Context, _ MuxRequest) error { return nil }

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan int, 1)
	go func() {
		done <- app.Run(ctx, []string{
			"ap", "serve", "--addr", camAddr, "--dev", devID,
			"--connect-timeout", "2s", "--http", httpAddr,
		})
	}()

	base := "http://" + httpAddr
	waitForHTTP(t, base+"/")

	// Start.
	if got := postRecord(t, base, "start"); got["recording"] != true {
		t.Fatalf("start response = %v, want recording:true", got)
	}
	// Let some frames land inside the recording window.
	time.Sleep(150 * time.Millisecond)
	// Stop.
	stop := postRecord(t, base, "stop")
	if stop["recording"] != false {
		t.Errorf("stop response recording = %v, want false", stop["recording"])
	}
	if f, _ := stop["file"].(string); f == "" {
		t.Errorf("stop response missing file: %v", stop)
	}
	if n, _ := stop["frames"].(float64); n <= 0 {
		t.Errorf("stop response frames = %v, want > 0 (nothing captured)", stop["frames"])
	}

	cancel()
	<-done
}

func waitForHTTP(t *testing.T, url string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if resp, err := http.Get(url); err == nil {
			_ = resp.Body.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("server at %s never came up", url)
}

func postRecord(t *testing.T, base, action string) map[string]any {
	t.Helper()
	resp, err := http.Post(base+"/record?action="+action, "", nil)
	if err != nil {
		t.Fatalf("POST /record?action=%s: %v", action, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/record?action=%s status = %d, want 200", action, resp.StatusCode)
	}
	var m map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		t.Fatalf("decode /record response: %v", err)
	}
	return m
}
