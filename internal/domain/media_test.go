package domain

import (
	"bytes"
	"testing"
)

// Anchor values derived from the canonical ITU-T G.711 A-law expansion. The two
// quietest codes decode to ±8; the two loudest to ±32256 (the largest magnitude
// A-law represents, comfortably inside int16).
func TestDecodeALawSample(t *testing.T) {
	tests := []struct {
		in   byte
		want int16
	}{
		{0xD5, 8},      // smallest positive step
		{0x55, -8},     // smallest negative step
		{0xAA, 32256},  // largest positive
		{0x2A, -32256}, // largest negative
	}
	for _, tt := range tests {
		if got := DecodeALawSample(tt.in); got != tt.want {
			t.Errorf("DecodeALawSample(%#02x) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

// A-law is sign-symmetric: flipping the sign bit (0x80) negates the sample.
func TestDecodeALawSample_SignSymmetry(t *testing.T) {
	for a := 0; a < 256; a++ {
		pos := DecodeALawSample(byte(a) | 0x80)
		neg := DecodeALawSample(byte(a) &^ 0x80)
		if pos != -neg {
			t.Fatalf("code %#02x: +side=%d, -side=%d (not symmetric)", a, pos, neg)
		}
	}
}

func TestDecodeALaw_MapsEveryByte(t *testing.T) {
	in := []byte{0xD5, 0x55, 0xAA, 0x2A}
	got := DecodeALaw(in)
	if len(got) != len(in) {
		t.Fatalf("len = %d, want %d", len(got), len(in))
	}
	want := []int16{8, -8, 32256, -32256}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("sample %d = %d, want %d", i, got[i], want[i])
		}
	}
}

func TestDecodeALaw_Empty(t *testing.T) {
	if got := DecodeALaw(nil); len(got) != 0 {
		t.Fatalf("DecodeALaw(nil) = %v, want empty", got)
	}
}

// Every A-law code must decode to a value representable in int16 (no overflow /
// wraparound), i.e. the round-trip through the algorithm stays in range.
func TestDecodeALawSample_AllCodesInRange(t *testing.T) {
	for a := 0; a < 256; a++ {
		v := DecodeALawSample(byte(a))
		if v < -32768 || v > 32767 {
			t.Fatalf("code %#02x decoded to %d, out of int16 range", a, v)
		}
	}
}

// Short helpers to build byte streams with JPEG markers. A "JPEG" here is just
// SOI + some body + EOI — the reassembler cares only about the FFD8/FFD9
// framing, not real JPEG validity.
var (
	soi = []byte{0xFF, 0xD8}
	eoi = []byte{0xFF, 0xD9}
)

func jpeg(body string) []byte {
	out := append([]byte{}, soi...)
	out = append(out, body...)
	return append(out, eoi...)
}

func TestMJPEGReassembler(t *testing.T) {
	tests := []struct {
		name      string
		fragments [][]byte
		want      [][]byte
	}{
		{
			name:      "one whole image in one fragment",
			fragments: [][]byte{jpeg("A")},
			want:      [][]byte{jpeg("A")},
		},
		{
			name:      "image split across two fragments",
			fragments: [][]byte{append(append([]byte{}, soi...), []byte("hel")...), append([]byte("lo"), eoi...)},
			want:      [][]byte{jpeg("hello")},
		},
		{
			name: "SOI/EOI markers split across the fragment boundary",
			// First fragment ends on the lone 0xFF of the SOI; second starts with 0xD8.
			fragments: [][]byte{{0xFF}, append(append([]byte{0xD8}, []byte("x")...), 0xFF), {0xD9}},
			want:      [][]byte{jpeg("x")},
		},
		{
			name:      "two images in a single fragment",
			fragments: [][]byte{append(jpeg("A"), jpeg("BB")...)},
			want:      [][]byte{jpeg("A"), jpeg("BB")},
		},
		{
			name:      "leading garbage before the first SOI is discarded",
			fragments: [][]byte{append([]byte("junk"), jpeg("A")...)},
			want:      [][]byte{jpeg("A")},
		},
		{
			name:      "bytes between EOI and next SOI are discarded",
			fragments: [][]byte{append(append(jpeg("A"), []byte("gap")...), jpeg("B")...)},
			want:      [][]byte{jpeg("A"), jpeg("B")},
		},
		{
			name:      "incomplete trailing image emits nothing until its EOI",
			fragments: [][]byte{append(append([]byte{}, soi...), []byte("partial")...)},
			want:      nil,
		},
		{
			name:      "image spanning three fragments",
			fragments: [][]byte{append(append([]byte{}, soi...), []byte("aa")...), []byte("bb"), append([]byte("cc"), eoi...)},
			want:      [][]byte{jpeg("aabbcc")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var r MJPEGReassembler
			var got [][]byte
			for _, frag := range tt.fragments {
				got = append(got, r.Append(frag)...)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %d images, want %d: %q", len(got), len(tt.want), got)
			}
			for i := range got {
				if !bytes.Equal(got[i], tt.want[i]) {
					t.Errorf("image %d = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// A completed image must not alias the reassembler's internal buffer: mutating a
// returned image, or feeding more data afterward, must not corrupt it.
func TestMJPEGReassembler_ReturnedImagesDoNotAlias(t *testing.T) {
	var r MJPEGReassembler
	imgs := r.Append(append(jpeg("first"), soi...)) // one whole image + start of another
	if len(imgs) != 1 {
		t.Fatalf("got %d images, want 1", len(imgs))
	}
	first := imgs[0]
	// Feeding the rest of the second image must not disturb the first.
	r.Append(append([]byte("second"), eoi...))
	if !bytes.Equal(first, jpeg("first")) {
		t.Fatalf("first image mutated to %q", first)
	}
}
