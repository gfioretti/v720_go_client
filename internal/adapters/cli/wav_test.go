package cli

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestWAVWriter_WritesValidHeaderAndData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.wav")
	w, err := newWAVWriter(path, 8000, 1)
	if err != nil {
		t.Fatalf("newWAVWriter: %v", err)
	}
	// Two writes across two calls, to prove dataBytes accumulates.
	if err := w.WriteSamples([]int16{0, -1, 32767, -32768}); err != nil {
		t.Fatalf("WriteSamples: %v", err)
	}
	if err := w.WriteSamples([]int16{100}); err != nil {
		t.Fatalf("WriteSamples: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}

	const nSamples = 5
	wantData := uint32(nSamples * 2)
	if len(b) != wavHeaderSize+int(wantData) {
		t.Fatalf("file size = %d, want %d", len(b), wavHeaderSize+int(wantData))
	}
	if string(b[0:4]) != "RIFF" || string(b[8:12]) != "WAVE" || string(b[12:16]) != "fmt " || string(b[36:40]) != "data" {
		t.Fatalf("chunk ids wrong: %q", b[:40])
	}
	if got := binary.LittleEndian.Uint32(b[4:8]); got != 36+wantData {
		t.Errorf("RIFF size = %d, want %d", got, 36+wantData)
	}
	if got := binary.LittleEndian.Uint16(b[20:22]); got != 1 {
		t.Errorf("audio format = %d, want 1 (PCM)", got)
	}
	if got := binary.LittleEndian.Uint16(b[22:24]); got != 1 {
		t.Errorf("channels = %d, want 1", got)
	}
	if got := binary.LittleEndian.Uint32(b[24:28]); got != 8000 {
		t.Errorf("sample rate = %d, want 8000", got)
	}
	if got := binary.LittleEndian.Uint32(b[28:32]); got != 8000*2 {
		t.Errorf("byte rate = %d, want %d", got, 8000*2)
	}
	if got := binary.LittleEndian.Uint16(b[32:34]); got != 2 {
		t.Errorf("block align = %d, want 2", got)
	}
	if got := binary.LittleEndian.Uint16(b[34:36]); got != 16 {
		t.Errorf("bits per sample = %d, want 16", got)
	}
	if got := binary.LittleEndian.Uint32(b[40:44]); got != wantData {
		t.Errorf("data size = %d, want %d", got, wantData)
	}
	// First sample should be 0, the extreme values round-trip as-is.
	if got := int16(binary.LittleEndian.Uint16(b[wavHeaderSize+4 : wavHeaderSize+6])); got != 32767 {
		t.Errorf("sample[2] = %d, want 32767", got)
	}
}

func TestWAVWriter_EmptyWriteIsNoop(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.wav")
	w, err := newWAVWriter(path, 8000, 1)
	if err != nil {
		t.Fatalf("newWAVWriter: %v", err)
	}
	if err := w.WriteSamples(nil); err != nil {
		t.Fatalf("WriteSamples(nil): %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	b, _ := os.ReadFile(path)
	if len(b) != wavHeaderSize {
		t.Fatalf("file size = %d, want just the %d-byte header", len(b), wavHeaderSize)
	}
	if got := binary.LittleEndian.Uint32(b[40:44]); got != 0 {
		t.Errorf("data size = %d, want 0", got)
	}
}
