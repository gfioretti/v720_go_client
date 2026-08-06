package domain

// G.711 A-law audio decoding. The camera's tag-4 audio frames (FrameTypeAudioG711,
// doc §12.4) carry G.711 A-law: one byte per sample, 8 kHz mono. A-law is a
// logarithmic companding of 13-bit linear PCM into 8 bits; decoding expands each
// byte back to a signed 16-bit linear sample, which is what WAV/PCM sinks want.
//
// This is a pure, allocation-light signal transform with no I/O — decoding logic
// lives in the domain; writing the samples to a file or an audio device is an
// adapter concern.

// DecodeALawSample expands one G.711 A-law byte to a signed 16-bit linear PCM
// sample, following the canonical ITU-T G.711 A-law expansion. The odd-bit toggle
// (^0x55) undoes A-law's even-bit inversion; the high nibble selects an exponent
// segment and the low nibble the mantissa.
func DecodeALawSample(a byte) int16 {
	a ^= 0x55
	mantissa := int(a&0x0F) << 4
	segment := (int(a) & 0x70) >> 4

	switch segment {
	case 0:
		mantissa += 8
	case 1:
		mantissa += 0x108
	default:
		mantissa += 0x108
		mantissa <<= segment - 1
	}

	// The top bit is the sign: set → positive, clear → negative.
	if a&0x80 != 0 {
		return int16(mantissa)
	}
	return int16(-mantissa)
}

// DecodeALaw expands a buffer of G.711 A-law bytes into linear PCM16 samples, one
// per input byte. It returns a freshly allocated slice; nil/empty input yields an
// empty slice.
func DecodeALaw(alaw []byte) []int16 {
	if len(alaw) == 0 {
		return nil
	}
	pcm := make([]int16, len(alaw))
	for i, b := range alaw {
		pcm[i] = DecodeALawSample(b)
	}
	return pcm
}
