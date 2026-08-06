package domain

import "testing"

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
