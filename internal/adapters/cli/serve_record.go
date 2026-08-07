package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"v720_go_client/internal/domain"
)

// recorder captures the live streams to temporary MJPEG + WAV files while active,
// letting the serve command start and stop an MP4 recording from the browser
// without interrupting the live view. It is safe for concurrent use: the live
// read loop tees frames in via onVideo/onAudio while HTTP handlers start/finish.
type recorder struct {
	mu      sync.Mutex
	active  bool
	dir     string
	video   *os.File
	wav     *wavWriter
	out     string
	started time.Time
	videoN  int
	audioN  int
}

func (r *recorder) isActive() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.active
}

// start begins a recording writing to temp files, targeting out as the final MP4.
func (r *recorder) start(out string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.active {
		return errors.New("already recording")
	}
	dir, err := os.MkdirTemp("", "v720rec-")
	if err != nil {
		return err
	}
	video, err := os.Create(filepath.Join(dir, "video.mjpeg"))
	if err != nil {
		_ = os.RemoveAll(dir)
		return err
	}
	wav, err := newWAVWriter(filepath.Join(dir, "audio.wav"), 8000, 1)
	if err != nil {
		_ = video.Close()
		_ = os.RemoveAll(dir)
		return err
	}
	r.active, r.dir, r.video, r.wav = true, dir, video, wav
	r.out, r.started, r.videoN, r.audioN = out, time.Now(), 0, 0
	return nil
}

func (r *recorder) onVideo(jpeg []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.active {
		return
	}
	if _, err := r.video.Write(jpeg); err == nil {
		r.videoN++
	}
}

func (r *recorder) onAudio(codec domain.FrameType, data []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.active || codec != domain.FrameTypeAudioG711 {
		return
	}
	if err := r.wav.WriteSamples(domain.DecodeALaw(data)); err == nil {
		r.audioN++
	}
}

// recording is the finalised outcome of a capture, ready to mux.
type recording struct {
	req     MuxRequest
	dir     string // temp dir to remove after muxing
	videoN  int
	elapsed time.Duration
}

// finish stops the active recording, closes the temp files, and returns what is
// needed to mux (and then clean up). The measured framerate keeps the muxed
// tracks aligned, exactly as the standalone `ap record` command does.
func (r *recorder) finish() (recording, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.active {
		return recording{}, errors.New("not recording")
	}
	_ = r.video.Close()
	_ = r.wav.Close()
	elapsed := time.Since(r.started)
	req := MuxRequest{
		VideoPath: filepath.Join(r.dir, "video.mjpeg"),
		OutPath:   r.out,
		FPS:       measuredFPS(r.videoN, elapsed),
	}
	if r.audioN > 0 {
		req.AudioPath = filepath.Join(r.dir, "audio.wav")
	}
	out := recording{req: req, dir: r.dir, videoN: r.videoN, elapsed: elapsed}

	r.active, r.dir, r.video, r.wav = false, "", nil, nil
	return out, nil
}

// handleRecord starts/stops an MP4 recording, or reports status:
//
//	POST /record?action=start[&file=name.mp4]  -> {"recording":true,"file":...}
//	POST /record?action=stop                   -> {"recording":false,"file":...,"frames":N,"seconds":X}
//	GET  /record                               -> {"recording":bool}
func (s *mjpegServer) handleRecord(w http.ResponseWriter, r *http.Request) {
	if s.session == nil || s.mux == nil {
		http.Error(w, "recording unavailable", http.StatusServiceUnavailable)
		return
	}
	switch r.FormValue("action") {
	case "": // status query
		writeJSON(w, map[string]any{"recording": s.rec.isActive()})
	case "start":
		out := recordingOutPath(r.FormValue("file"))
		if err := s.rec.start(out); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		writeJSON(w, map[string]any{"recording": true, "file": out})
	case "stop":
		fin, err := s.rec.finish()
		if err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		muxCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		muxErr := s.mux(muxCtx, fin.req)
		_ = os.RemoveAll(fin.dir)
		if muxErr != nil {
			http.Error(w, "mux failed: "+muxErr.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{
			"recording": false, "file": fin.req.OutPath,
			"frames": fin.videoN, "seconds": fin.elapsed.Round(time.Second).Seconds(),
		})
	default:
		http.Error(w, "unknown action", http.StatusBadRequest)
	}
}

// recordingOutPath resolves the output filename. A caller-supplied name is
// reduced to its base (no directory traversal) and defaulted to .mp4; otherwise a
// timestamped name in the current directory is used.
func recordingOutPath(file string) string {
	if file != "" {
		name := filepath.Base(file)
		if filepath.Ext(name) == "" {
			name += ".mp4"
		}
		return name
	}
	return fmt.Sprintf("v720-%s.mp4", time.Now().Format("20060102-150405"))
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
