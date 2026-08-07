package cli

import (
	"context"
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"v720_go_client/internal/application"
)

//go:embed panel.html
var panelHTML string

// runAPServe connects to the camera and serves its live view over HTTP as an
// MJPEG stream, so any browser on the machine can watch it like an IP webcam:
// open the printed URL and the page shows the live feed via <img src=…>. It runs
// until interrupted (Ctrl-C) or the camera stream ends.
func (a *App) runAPServe(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("ap serve", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	addr := fs.String("addr", "", "camera AP-mode address, host:port (required)")
	dev := fs.String("dev", "", "device id / target (required)")
	password := fs.String("password", "", "device password (the sk value)")
	httpAddr := fs.String("http", ":8080", "address to serve the live view on")
	quality := fs.Int("quality", 0, "video grade to request first: 1=SD, 2=HD (0=leave as-is)")
	connectTimeout := fs.Duration("connect-timeout", 10*time.Second, "handshake timeout")
	debug := fs.Bool("debug", false, "log every frame sent/received to stderr")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *addr == "" || *dev == "" {
		a.errf("v720ctl ap serve: --addr and --dev are required\n")
		return 2
	}
	if fs.NArg() > 0 {
		a.errf("v720ctl ap serve: unexpected arguments: %v\n", fs.Args())
		return 2
	}

	// Bind the HTTP port first so a bad --http fails before we touch the camera.
	ln, err := net.Listen("tcp", *httpAddr)
	if err != nil {
		a.errf("v720ctl: listen on %s: %v\n", *httpAddr, err)
		return 1
	}
	defer func() { _ = ln.Close() }()

	connectCtx, cancel := context.WithTimeout(ctx, *connectTimeout)
	session, transport, err := a.dialAndConnect(connectCtx, *addr, *dev, *password, *debug)
	cancel()
	if err != nil {
		a.errf("v720ctl: %v\n", err)
		return 1
	}
	defer func() { _ = transport.Close() }()

	if err := a.applyVideoQuality(ctx, session, *quality); err != nil {
		a.errf("v720ctl: set quality: %v\n", err)
		return 1
	}

	srv := newMJPEGServer()
	srv.session = session // enables the control panel's device commands
	httpSrv := &http.Server{Handler: srv.routes()}

	// The stream feeds the broadcaster; the HTTP server fans it out to browsers.
	// Either finishing (camera drop, or Ctrl-C cancelling ctx) tears down both.
	streamErr := make(chan error, 1)
	go func() { streamErr <- session.StartLiveView(ctx, srv, nil) }()

	serveErr := make(chan error, 1)
	go func() { serveErr <- httpSrv.Serve(ln) }()

	a.outf("control panel at http://localhost%s  (open it in your browser; Ctrl-C to stop)\n", displayPort(ln.Addr(), *httpAddr))

	var rc int
	select {
	case <-ctx.Done():
	case err := <-streamErr:
		if err != nil {
			a.errf("v720ctl: live view: %v\n", err)
			rc = 1
		}
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			a.errf("v720ctl: http server: %v\n", err)
			rc = 1
		}
	}

	shutdownCtx, stop := context.WithTimeout(context.Background(), 2*time.Second)
	defer stop()
	_ = httpSrv.Shutdown(shutdownCtx)
	return rc
}

// displayPort renders the ":port" suffix for the watch URL, preferring the
// actually-bound port (so ":0" resolves to the real one).
func displayPort(bound net.Addr, requested string) string {
	if tcp, ok := bound.(*net.TCPAddr); ok {
		return fmt.Sprintf(":%d", tcp.Port)
	}
	return requested
}

// mjpegBoundary separates JPEG parts in the multipart/x-mixed-replace stream.
const mjpegBoundary = "v720frame"

// mjpegServer fans out live JPEG frames to any number of connected HTTP clients
// and remembers the most recent frame for single-shot snapshots. It implements
// application.VideoFrameHandler via OnVideoFrame.
//
// When session is set, it also serves the control panel: HTTP requests to
// /control are turned into device commands on that session. session may be nil
// (e.g. in stream-only tests), in which case /control reports it is unavailable.
type mjpegServer struct {
	mu      sync.Mutex
	clients map[chan []byte]struct{}
	latest  []byte

	session *application.APSession
	// sendMu serialises control commands so only one goroutine writes to the
	// camera's TCP connection at a time (the live read loop reads concurrently,
	// which is safe; concurrent writers are not).
	sendMu sync.Mutex
}

func newMJPEGServer() *mjpegServer {
	return &mjpegServer{clients: make(map[chan []byte]struct{})}
}

// OnVideoFrame receives a complete JPEG image (StartLiveView has already
// reassembled it) and publishes it. The reassembler hands us a fresh slice per
// frame, so retaining it as the latest snapshot and sharing it with clients is
// safe — it is never mutated.
func (s *mjpegServer) OnVideoFrame(jpeg []byte) error {
	s.mu.Lock()
	s.latest = jpeg
	clients := make([]chan []byte, 0, len(s.clients))
	for ch := range s.clients {
		clients = append(clients, ch)
	}
	s.mu.Unlock()

	for _, ch := range clients {
		// Never block the live loop on a slow browser: drop this frame for it.
		select {
		case ch <- jpeg:
		default:
		}
	}
	return nil
}

func (s *mjpegServer) subscribe() chan []byte {
	ch := make(chan []byte, 4)
	s.mu.Lock()
	s.clients[ch] = struct{}{}
	s.mu.Unlock()
	return ch
}

func (s *mjpegServer) unsubscribe(ch chan []byte) {
	s.mu.Lock()
	delete(s.clients, ch)
	s.mu.Unlock()
}

func (s *mjpegServer) snapshot() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.latest
}

func (s *mjpegServer) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/stream.mjpg", s.handleStream)
	mux.HandleFunc("/snapshot.jpg", s.handleSnapshot)
	mux.HandleFunc("/control", s.handleControl)
	return mux
}

func (s *mjpegServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, panelHTML)
}

// handleControl turns a POST like `set=quality&value=2` into a device command on
// the session (reusing the same deviceSettings map the `ap set` command uses).
// Commands are serialised so concurrent panel clicks can't interleave writes to
// the camera.
func (s *mjpegServer) handleControl(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	if s.session == nil {
		http.Error(w, "control unavailable", http.StatusServiceUnavailable)
		return
	}
	setting := r.FormValue("set")
	build, ok := deviceSettings[setting]
	if !ok {
		http.Error(w, "unknown setting "+setting, http.StatusBadRequest)
		return
	}
	value, err := strconv.Atoi(r.FormValue("value"))
	if err != nil {
		http.Error(w, "value must be an integer", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), sendCommandTimeout)
	defer cancel()
	s.sendMu.Lock()
	err = s.session.SendCommand(ctx, build(value))
	s.sendMu.Unlock()
	if err != nil {
		http.Error(w, "send failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *mjpegServer) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	img := s.snapshot()
	if img == nil {
		http.Error(w, "no frame yet", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(img)
}

func (s *mjpegServer) handleStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	ch := s.subscribe()
	defer s.unsubscribe(ch)

	w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary="+mjpegBoundary)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "close")
	// Flush the response headers now so the client sees the stream has started
	// even before the first frame is published.
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// Send the most recent frame right away so the page isn't blank until the
	// next one arrives.
	if img := s.snapshot(); img != nil {
		if writeMJPEGPart(w, img) != nil {
			return
		}
		flusher.Flush()
	}

	for {
		select {
		case <-r.Context().Done():
			return
		case jpeg := <-ch:
			if writeMJPEGPart(w, jpeg) != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// writeMJPEGPart writes one multipart/x-mixed-replace part: the boundary, the
// JPEG headers, and the image bytes.
func writeMJPEGPart(w io.Writer, jpeg []byte) error {
	if _, err := fmt.Fprintf(w, "--%s\r\nContent-Type: image/jpeg\r\nContent-Length: %d\r\n\r\n", mjpegBoundary, len(jpeg)); err != nil {
		return err
	}
	if _, err := w.Write(jpeg); err != nil {
		return err
	}
	_, err := io.WriteString(w, "\r\n")
	return err
}
