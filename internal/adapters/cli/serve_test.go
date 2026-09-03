package cli

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"v720_go_client/internal/domain"
)

// syncBuffer is a goroutine-safe io.Writer, needed because the serve command
// writes to Stdout from its own goroutine while the test reads it.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// freePort reserves and releases a loopback port, returning "127.0.0.1:PORT".
func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	return addr
}

// /control without a session (stream-only server) reports it is unavailable, and
// rejects bad input, without panicking.
func TestMJPEGServer_ControlValidation(t *testing.T) {
	ts := httptest.NewServer(newMJPEGServer().routes()) // no session
	defer ts.Close()

	// No session -> 503.
	resp, _ := http.PostForm(ts.URL+"/control", url.Values{"set": {"quality"}, "value": {"2"}})
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("no-session status = %d, want 503", resp.StatusCode)
	}
	resp.Body.Close()

	// Wrong method -> 405.
	resp2, _ := http.Get(ts.URL + "/control")
	if resp2.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET /control status = %d, want 405", resp2.StatusCode)
	}
	resp2.Body.Close()
}

// The viewer page must embed and reference the stream.
func TestMJPEGServer_PanelReferencesStream(t *testing.T) {
	ts := httptest.NewServer(newMJPEGServer().routes())
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `src="/stream.mjpg"`) {
		t.Errorf("panel missing the stream image")
	}
	if !strings.Contains(string(body), `data-set="quality"`) {
		t.Errorf("panel missing the quality control")
	}
}

// End-to-end: a POST to /control on the real `ap serve` command reaches the
// camera as the corresponding device command.
func TestRun_ServeControlReachesCamera(t *testing.T) {
	const devID = "dev123"
	camAddr, received := startMockCameraCapturingAll(t, devID)
	httpAddr := freePort(t)

	app := NewApp(&syncBuffer{}, &syncBuffer{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan int, 1)
	go func() {
		done <- app.Run(ctx, []string{
			"ap", "serve", "--addr", camAddr, "--dev", devID,
			"--connect-timeout", "2s", "--http", httpAddr,
		})
	}()

	// Wait for the panel to come up.
	base := "http://" + httpAddr
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if resp, err := http.Get(base + "/"); err == nil {
			_ = resp.Body.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	resp, err := http.PostForm(base+"/control", url.Values{"set": {"quality"}, "value": {"2"}})
	if err != nil {
		t.Fatalf("POST /control: %v", err)
	}
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("/control status = %d, want 204", resp.StatusCode)
	}
	resp.Body.Close()

	if !waitForQualityCommand(t, received, 201, 2) {
		t.Fatal("camera never received the 502/201 speedGrade=2 command from /control")
	}

	cancel()
	<-done
}

// End-to-end: the real `ap serve` command connects to a (mock) camera, and a
// browser-style HTTP GET of /snapshot.jpg returns the streamed frame.
func TestRun_ServeEndToEnd(t *testing.T) {
	const devID = "dev123"
	jpeg := []byte("\xff\xd8servedframe\xff\xd9")
	camAddr := startMockCameraStreaming(t, devID, []domain.Frame{
		{Type: domain.FrameTypeMJPEG, Payload: jpeg},
	})
	httpAddr := freePort(t)

	app := NewApp(&syncBuffer{}, &syncBuffer{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan int, 1)
	go func() {
		done <- app.Run(ctx, []string{
			"ap", "serve", "--addr", camAddr, "--dev", devID,
			"--connect-timeout", "2s", "--http", httpAddr,
		})
	}()

	// Poll the snapshot endpoint until the frame has propagated.
	url := "http://" + httpAddr + "/snapshot.jpg"
	var body []byte
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil && resp.StatusCode == 200 {
			body, _ = io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			break
		}
		if resp != nil {
			_ = resp.Body.Close()
		}
		time.Sleep(20 * time.Millisecond)
	}
	if string(body) != string(jpeg) {
		t.Fatalf("snapshot = %q, want %q", body, jpeg)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("serve did not shut down after ctx cancel")
	}
}

func TestMJPEGServer_IndexServesViewerPage(t *testing.T) {
	ts := httptest.NewServer(newMJPEGServer().routes())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("content-type = %q, want text/html", ct)
	}
	if !strings.Contains(string(body), `src="/stream.mjpg"`) {
		t.Errorf("index page missing the stream <img>: %q", body)
	}
}

func TestMJPEGServer_SnapshotBeforeAndAfterFrame(t *testing.T) {
	srv := newMJPEGServer()
	ts := httptest.NewServer(srv.routes())
	defer ts.Close()

	// Before any frame: 503.
	resp, err := http.Get(ts.URL + "/snapshot.jpg")
	if err != nil {
		t.Fatalf("GET snapshot: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 before any frame", resp.StatusCode)
	}

	// After a frame: 200 image/jpeg with the exact bytes.
	jpeg := []byte("\xff\xd8snap\xff\xd9")
	_ = srv.OnVideoFrame(jpeg)
	resp2, err := http.Get(ts.URL + "/snapshot.jpg")
	if err != nil {
		t.Fatalf("GET snapshot 2: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != 200 {
		t.Fatalf("status = %d, want 200 after a frame", resp2.StatusCode)
	}
	if ct := resp2.Header.Get("Content-Type"); ct != "image/jpeg" {
		t.Errorf("content-type = %q, want image/jpeg", ct)
	}
	got, _ := io.ReadAll(resp2.Body)
	if string(got) != string(jpeg) {
		t.Errorf("snapshot = %q, want %q", got, jpeg)
	}
}

func TestMJPEGServer_StreamDeliversFrames(t *testing.T) {
	srv := newMJPEGServer()
	ts := httptest.NewServer(srv.routes())
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/stream.mjpg", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET stream: %v", err)
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "multipart/x-mixed-replace") {
		t.Fatalf("content-type = %q, want multipart/x-mixed-replace", ct)
	}

	// Publish a frame after the client has connected; it must arrive as a part.
	jpeg := []byte("\xff\xd8streamed\xff\xd9")
	go func() {
		time.Sleep(20 * time.Millisecond)
		_ = srv.OnVideoFrame(jpeg)
	}()

	// Read the multipart body until the image bytes appear (boundary + headers +
	// image) or a deadline hits.
	buf := make([]byte, 0, 256)
	r := bufio.NewReader(resp.Body)
	deadline := time.Now().Add(1500 * time.Millisecond)
	for !strings.Contains(string(buf), "streamed") && time.Now().Before(deadline) {
		b, err := r.ReadByte()
		if err != nil {
			break
		}
		buf = append(buf, b)
	}
	got := string(buf)
	if !strings.Contains(got, "--"+mjpegBoundary) {
		t.Errorf("stream missing boundary marker: %q", got)
	}
	if !strings.Contains(got, "Content-Type: image/jpeg") {
		t.Errorf("stream missing per-part JPEG header: %q", got)
	}
	if !strings.Contains(got, "streamed") {
		t.Errorf("stream missing the image bytes: %q", got)
	}
}

// End-to-end: when the camera drops the connection mid-stream, `ap serve` must
// transparently reconnect and keep the panel serving frames — not exit. The mock
// camera streams frame A, drops, then (on the client's reconnect) streams frame
// B; the panel must end up serving frame B.
func TestRun_ServeReconnectsAfterDrop(t *testing.T) {
	const devID = "dev123"
	frameA := []byte("\xff\xd8sessionA\xff\xd9")
	frameB := []byte("\xff\xd8sessionB\xff\xd9")
	camAddr := startMockCameraStreamingSessions(t, devID, [][]domain.Frame{
		{{Type: domain.FrameTypeMJPEG, Payload: frameA}},
		{{Type: domain.FrameTypeMJPEG, Payload: frameB}},
	})
	httpAddr := freePort(t)

	out, errOut := &syncBuffer{}, &syncBuffer{}
	app := NewApp(out, errOut)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan int, 1)
	go func() {
		done <- app.Run(ctx, []string{
			"ap", "serve", "--addr", camAddr, "--dev", devID,
			"--connect-timeout", "2s", "--http", httpAddr,
		})
	}()

	// Poll the snapshot endpoint until it returns frame B — the frame only the
	// SECOND camera session streams. Seeing it proves serve survived the drop and
	// resumed streaming on a fresh connection.
	url := "http://" + httpAddr + "/snapshot.jpg"
	var body []byte
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil && resp.StatusCode == 200 {
			body, _ = io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if string(body) == string(frameB) {
				break
			}
		} else if resp != nil {
			_ = resp.Body.Close()
		}
		time.Sleep(20 * time.Millisecond)
	}
	if string(body) != string(frameB) {
		t.Fatalf("snapshot = %q, want frame B %q (reconnect did not resume streaming)", body, frameB)
	}

	// The drop and recovery must be narrated: a "connection lost" warning and an
	// "attempting to reconnect" line on stderr, and a "streaming resumed" line on
	// stdout once it's back.
	if errLog := errOut.String(); !strings.Contains(errLog, "camera connection lost") {
		t.Errorf("stderr missing connection-lost warning; got:\n%s", errLog)
	}
	if errLog := errOut.String(); !strings.Contains(errLog, "attempting to reconnect") {
		t.Errorf("stderr missing reconnect-attempt log; got:\n%s", errLog)
	}
	if outLog := out.String(); !strings.Contains(outLog, "streaming resumed") {
		t.Errorf("stdout missing resume log; got:\n%s", outLog)
	}

	cancel()
	select {
	case rc := <-done:
		if rc != 0 {
			t.Fatalf("serve exit code = %d, want 0", rc)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serve did not shut down after ctx cancel")
	}
}

// startMockCameraStreamingSessions plays the device side of several SEQUENTIAL
// connections. For each session it accepts a connection, runs the handshake, and
// streams that session's frames; every session but the last then closes the
// connection to simulate the camera dropping mid-stream, so the client must
// reconnect for the next Accept to be served. The last session stays open until
// the client hangs up.
func startMockCameraStreamingSessions(t *testing.T, devID string, sessions [][]domain.Frame) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for i, media := range sessions {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			if mockHandshake(conn, devID, 200) {
				for _, f := range media {
					if err := writeStreamFrame(conn, f); err != nil {
						break
					}
				}
				if i == len(sessions)-1 {
					drain(conn) // keep the final session open
				}
			}
			_ = conn.Close() // non-last: drop, prompting the client to reconnect
		}
	}()

	return ln.Addr().String()
}

// A slow client that never reads must not block OnVideoFrame (the live loop).
func TestMJPEGServer_SlowClientDoesNotBlockPublish(t *testing.T) {
	srv := newMJPEGServer()
	ch := srv.subscribe()
	defer srv.unsubscribe(ch)

	// The channel buffers 4; publishing many more than that must still return
	// promptly (extra frames are dropped for this client, not blocked on).
	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			_ = srv.OnVideoFrame([]byte("\xff\xd8x\xff\xd9"))
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("OnVideoFrame blocked on a non-reading client")
	}
}
