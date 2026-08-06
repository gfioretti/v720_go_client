package domain

import (
	"bytes"
	"testing"
)

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
