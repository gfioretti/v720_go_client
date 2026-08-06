package domain

import "bytes"

// JPEG start-of-image / end-of-image markers. In a conformant JPEG stream these
// two-byte sequences only ever appear as real markers: within entropy-coded data
// a literal 0xFF is byte-stuffed as 0xFF 0x00, and restart markers are 0xFFD0–D7,
// so a raw 0xFFD8 or 0xFFD9 is unambiguously SOI or EOI. That is what lets us
// delimit images by scanning for these byte pairs.
var (
	jpegSOI = []byte{0xFF, 0xD8}
	jpegEOI = []byte{0xFF, 0xD9}
)

// MJPEGReassembler reconstructs complete JPEG images from the fragmented MJPEG
// payloads the camera streams. On the wire each video frame (FrameTypeMJPEG)
// carries only a slice of a JPEG image, and a single image spans several frames.
//
// This mirrors how the official app decodes AP-mode video: its session does no
// reassembly of its own — it hands each raw payload to the native decoder as a
// continuous byte stream, which splits images on JPEG SOI/EOI markers. Feed each
// fragment payload, in the order received (TCP guarantees that order), to Append;
// it returns any images that fragment completed. Confirmed against a real camera:
// concatenating the fragments and splitting on FFD8…FFD9 yields whole 640×480
// JPEGs.
//
// The zero value is a ready-to-use reassembler. It is not safe for concurrent
// use; drive it from a single goroutine (as StartLiveView's read loop does).
type MJPEGReassembler struct {
	// buf holds bytes from the current, not-yet-complete image. It always starts
	// at an SOI once one has been seen; bytes arriving before the first SOI (or
	// between an EOI and the next SOI) are discarded.
	buf []byte
	// started reports whether buf begins at an SOI. Until the first SOI is seen,
	// incoming bytes are dropped except for a possible trailing 0xFF that might be
	// the first half of an SOI straddling a fragment boundary.
	started bool
}

// Append feeds one fragment payload into the reassembler and returns every
// complete JPEG image (each a full SOI…EOI byte slice) that this fragment
// finished. It returns nil when the fragment only extends an in-progress image.
// Returned slices are copies and never alias the reassembler's internal buffer,
// so callers may retain or mutate them freely.
func (r *MJPEGReassembler) Append(fragment []byte) [][]byte {
	r.buf = append(r.buf, fragment...)

	var images [][]byte
	for {
		if !r.started {
			// Look for the start of an image, discarding any leading garbage.
			i := bytes.Index(r.buf, jpegSOI)
			if i < 0 {
				// No SOI yet. Keep only a trailing 0xFF (a possible split SOI);
				// everything else before a start marker is noise.
				r.buf = keepTrailingFF(r.buf)
				return images
			}
			r.buf = r.buf[i:]
			r.started = true
		}

		// We're positioned at an SOI; find this image's EOI after it.
		j := bytes.Index(r.buf[len(jpegSOI):], jpegEOI)
		if j < 0 {
			return images // image not finished yet
		}
		end := len(jpegSOI) + j + len(jpegEOI)

		img := make([]byte, end)
		copy(img, r.buf[:end])
		images = append(images, img)

		r.buf = r.buf[end:]
		r.started = false
	}
}

// Reset drops any partially-accumulated image, readying the reassembler for a
// fresh stream (e.g. after a reconnect).
func (r *MJPEGReassembler) Reset() {
	r.buf = nil
	r.started = false
}

// keepTrailingFF returns just the final byte of b when it is 0xFF (the possible
// first half of an SOI marker split across a fragment boundary), otherwise an
// empty slice. It avoids retaining pre-SOI garbage while not losing a dangling
// marker byte.
func keepTrailingFF(b []byte) []byte {
	if len(b) > 0 && b[len(b)-1] == 0xFF {
		return []byte{0xFF}
	}
	return nil
}
