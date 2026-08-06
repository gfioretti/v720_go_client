package cli

import (
	"encoding/binary"
	"fmt"
	"os"
)

// wavWriter streams signed-16-bit little-endian PCM samples into a canonical
// 44-byte-header WAV (RIFF/WAVE) file. Because a live capture's total length is
// unknown up front, the two size fields in the header are written as zero at
// creation and patched to their real values on Close by seeking back — so the
// target must be a regular, seekable file (not a pipe).
type wavWriter struct {
	f          *os.File
	sampleRate uint32
	channels   uint16
	dataBytes  uint32
}

const wavHeaderSize = 44

// newWAVWriter creates path and writes a WAV header sized for PCM16 at the given
// sample rate and channel count, leaving the length fields as zero for Close to
// patch. On any error the file is cleaned up and a non-nil error returned.
func newWAVWriter(path string, sampleRate uint32, channels uint16) (*wavWriter, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	w := &wavWriter{f: f, sampleRate: sampleRate, channels: channels}
	if err := w.writeHeader(); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return nil, err
	}
	return w, nil
}

// writeHeader emits the 44-byte RIFF/WAVE/fmt/data header with the two size
// fields (RIFF chunk size, data chunk size) zeroed; Close patches them.
func (w *wavWriter) writeHeader() error {
	var h [wavHeaderSize]byte
	bitsPerSample := uint16(16)
	blockAlign := w.channels * bitsPerSample / 8
	byteRate := w.sampleRate * uint32(blockAlign)

	copy(h[0:4], "RIFF")
	// h[4:8] RIFF chunk size — patched on Close.
	copy(h[8:12], "WAVE")
	copy(h[12:16], "fmt ")
	binary.LittleEndian.PutUint32(h[16:20], 16) // fmt chunk size (PCM)
	binary.LittleEndian.PutUint16(h[20:22], 1)  // audio format 1 = PCM
	binary.LittleEndian.PutUint16(h[22:24], w.channels)
	binary.LittleEndian.PutUint32(h[24:28], w.sampleRate)
	binary.LittleEndian.PutUint32(h[28:32], byteRate)
	binary.LittleEndian.PutUint16(h[32:34], blockAlign)
	binary.LittleEndian.PutUint16(h[34:36], bitsPerSample)
	copy(h[36:40], "data")
	// h[40:44] data chunk size — patched on Close.

	_, err := w.f.Write(h[:])
	return err
}

// WriteSamples appends PCM16 samples as little-endian bytes.
func (w *wavWriter) WriteSamples(samples []int16) error {
	if len(samples) == 0 {
		return nil
	}
	buf := make([]byte, len(samples)*2)
	for i, s := range samples {
		binary.LittleEndian.PutUint16(buf[i*2:], uint16(s))
	}
	if _, err := w.f.Write(buf); err != nil {
		return err
	}
	w.dataBytes += uint32(len(buf))
	return nil
}

// Close patches the two header size fields with the real byte counts and closes
// the file.
func (w *wavWriter) Close() error {
	var sizes [4]byte

	binary.LittleEndian.PutUint32(sizes[:], 36+w.dataBytes)
	if _, err := w.f.WriteAt(sizes[:], 4); err != nil {
		_ = w.f.Close()
		return fmt.Errorf("patch RIFF size: %w", err)
	}
	binary.LittleEndian.PutUint32(sizes[:], w.dataBytes)
	if _, err := w.f.WriteAt(sizes[:], 40); err != nil {
		_ = w.f.Close()
		return fmt.Errorf("patch data size: %w", err)
	}
	return w.f.Close()
}
