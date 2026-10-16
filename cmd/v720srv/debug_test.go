package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gfioretti/v720_go_client/internal/domain"
)

// capture collects formatted log lines.
type capture struct{ lines []string }

func (c *capture) logf(format string, args ...any) {
	c.lines = append(c.lines, fmt.Sprintf(format, args...))
}
func (c *capture) joined() string { return strings.Join(c.lines, "\n") }

// fakeInner is a minimal coordinator channel for exercising loggingChannel.
type fakeInner struct {
	sent []domain.Frame
	recv domain.Frame
}

func (f *fakeInner) Send(_ context.Context, fr domain.Frame) error {
	f.sent = append(f.sent, fr)
	return nil
}
func (f *fakeInner) Receive(context.Context) (domain.Frame, error) { return f.recv, nil }
func (f *fakeInner) Close() error                                  { return nil }

func TestLogFrame_JSONAndBinary(t *testing.T) {
	var c capture
	logFrame(c.logf, "RX", domain.Frame{Type: domain.FrameTypeJSON, Payload: []byte(`{"code":100}`)})
	logFrame(c.logf, "TX", domain.Frame{Type: domain.FrameTypeMJPEG, Payload: []byte("jpeg")})
	out := c.joined()
	if !strings.Contains(out, "RX JSON code=100") {
		t.Errorf("missing JSON code log: %q", out)
	}
	if !strings.Contains(out, "TX type=MJPEG") {
		t.Errorf("missing binary type log: %q", out)
	}
}

// loggingChannel forwards to its inner channel and logs both directions.
func TestLoggingChannel_ForwardsAndLogs(t *testing.T) {
	var c capture
	inner := &fakeInner{recv: domain.Frame{Type: domain.FrameTypeJSON, Payload: []byte(`{"code":101}`)}}
	lc := &loggingChannel{inner: inner, logf: c.logf}

	if err := lc.Send(context.Background(), domain.Frame{Type: domain.FrameTypeJSON, Payload: []byte(`{"code":53}`)}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(inner.sent) != 1 {
		t.Fatal("Send did not forward to inner")
	}
	got, err := lc.Receive(context.Background())
	if err != nil || got.Type != domain.FrameTypeJSON {
		t.Fatalf("Receive = %+v, %v", got, err)
	}
	out := c.joined()
	if !strings.Contains(out, "TX JSON code=53") || !strings.Contains(out, "RX JSON code=101") {
		t.Errorf("expected TX+RX logs, got %q", out)
	}
}

func TestLogRequests_LogsPath(t *testing.T) {
	var c capture
	h := logRequests(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }), c.logf)
	req := httptest.NewRequest(http.MethodGet, "/app/api/ApiServer/getA9ConfCheck", nil)
	h.ServeHTTP(httptest.NewRecorder(), req)
	if !strings.Contains(c.joined(), "REST GET /app/api/ApiServer/getA9ConfCheck") {
		t.Errorf("request not logged: %q", c.joined())
	}
}
