package domain

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

// The golden bytes in these tests are hand-built from the 20-byte header table
// in findings doc §12.3a:
//
//	offset 0  size 4  payload length   int32  little-endian
//	offset 4  size 2  packet type tag  int16  little-endian
//	offset 6  size 1  flag byte        int8
//	offset 7  size 1  unknown byte
//	offset 8  size 8  opaque field
//	offset 16 size 4  sequence/counter int32  little-endian
//	offset 20 size N  payload

func TestFrame_Encode(t *testing.T) {
	tests := []struct {
		name  string
		frame Frame
		want  []byte
	}{
		{
			// The AP-mode handshake opens with a bare type-115 frame that has
			// no JSON body — just the 20-byte header, all fields zero except
			// the type tag (doc §12.3 step 2).
			name:  "empty type-115 handshake trigger",
			frame: Frame{Type: 115},
			want: []byte{
				0x00, 0x00, 0x00, 0x00, // length = 0
				0x73, 0x00, // type = 115
				0x00,                                           // flag
				0x00,                                           // unknown
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // opaque
				0x00, 0x00, 0x00, 0x00, // sequence = 0
			},
		},
		{
			// A fully-populated frame exercising every header field, including
			// a signed-negative flag byte (the fragment sentinel -4 from doc
			// §12.2 phase 2 step 6) and a multi-byte little-endian sequence.
			name: "all fields populated with payload",
			frame: Frame{
				Type:     1, // MJPEG video
				Flag:     -4,
				Unknown:  0x2a,
				Opaque:   [8]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08},
				Sequence: 0x04030201,
				Payload:  []byte{0xde, 0xad, 0xbe, 0xef},
			},
			want: []byte{
				0x04, 0x00, 0x00, 0x00, // length = 4
				0x01, 0x00, // type = 1
				0xfc,                                           // flag = -4
				0x2a,                                           // unknown
				0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, // opaque
				0x01, 0x02, 0x03, 0x04, // sequence = 0x04030201
				0xde, 0xad, 0xbe, 0xef, // payload
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.frame.Encode()
			if !bytes.Equal(got, tt.want) {
				t.Fatalf("Encode() =\n%x\nwant\n%x", got, tt.want)
			}
		})
	}
}

func TestFrame_Decode(t *testing.T) {
	// Round-trip every populated field back out of the wire bytes.
	raw := []byte{
		0x04, 0x00, 0x00, 0x00, // length = 4
		0x01, 0x00, // type = 1
		0xfc,                                           // flag = -4
		0x2a,                                           // unknown
		0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, // opaque
		0x01, 0x02, 0x03, 0x04, // sequence = 0x04030201
		0xde, 0xad, 0xbe, 0xef, // payload
	}

	got, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode() unexpected error: %v", err)
	}

	want := Frame{
		Type:     1,
		Flag:     -4,
		Unknown:  0x2a,
		Opaque:   [8]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08},
		Sequence: 0x04030201,
		Payload:  []byte{0xde, 0xad, 0xbe, 0xef},
	}

	if got.Type != want.Type || got.Flag != want.Flag || got.Unknown != want.Unknown ||
		got.Opaque != want.Opaque || got.Sequence != want.Sequence ||
		!bytes.Equal(got.Payload, want.Payload) {
		t.Fatalf("Decode() = %+v, want %+v", got, want)
	}
}

func TestFrame_DecodeEmptyPayload(t *testing.T) {
	raw := []byte{
		0x00, 0x00, 0x00, 0x00,
		0x73, 0x00, // type = 115
		0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
	}

	got, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode() unexpected error: %v", err)
	}
	if got.Type != 115 {
		t.Fatalf("Type = %d, want 115", got.Type)
	}
	if len(got.Payload) != 0 {
		t.Fatalf("Payload len = %d, want 0", len(got.Payload))
	}
}

func TestFrame_EncodeDecodeRoundTrip(t *testing.T) {
	original := Frame{
		Type:     102,
		Flag:     -1,
		Unknown:  0xff,
		Opaque:   [8]byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff, 0x11, 0x22},
		Sequence: 0xdeadbeef,
		Payload:  []byte("hello camera"),
	}

	decoded, err := Decode(original.Encode())
	if err != nil {
		t.Fatalf("Decode(Encode()) error: %v", err)
	}
	if decoded.Type != original.Type || decoded.Flag != original.Flag ||
		decoded.Unknown != original.Unknown || decoded.Opaque != original.Opaque ||
		decoded.Sequence != original.Sequence || !bytes.Equal(decoded.Payload, original.Payload) {
		t.Fatalf("round trip mismatch:\n got %+v\nwant %+v", decoded, original)
	}
}

// Type tags in §12.4 are all ≤ 115, so their high byte is always zero and a
// byte-swapped/big-endian bug at offset 4–5 would go unnoticed. A synthetic
// two-byte tag locks the field's little-endian order independently of the real
// (single-byte) tag values.
func TestFrame_TypeTagByteOrder(t *testing.T) {
	f := Frame{Type: 0x0102}
	got := f.Encode()
	if got[4] != 0x02 || got[5] != 0x01 {
		t.Fatalf("type tag bytes = %#x %#x, want 0x02 0x01 (little-endian)", got[4], got[5])
	}

	decoded, err := Decode(got)
	if err != nil {
		t.Fatalf("Decode() error: %v", err)
	}
	if decoded.Type != 0x0102 {
		t.Fatalf("Type = %#x, want 0x0102", decoded.Type)
	}
}

// Decode must copy the payload, not alias the caller's buffer: a transport
// adapter is free to reuse its read buffer for the next frame, and a future
// "optimization" to slice into the source would pass every value-equality test
// while introducing a use-after-reuse bug. Mutating the source after decoding
// must not disturb the returned payload.
func TestFrame_DecodeDoesNotAliasSource(t *testing.T) {
	raw := []byte{
		0x04, 0x00, 0x00, 0x00,
		0x01, 0x00,
		0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
		0xde, 0xad, 0xbe, 0xef,
	}

	got, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode() error: %v", err)
	}

	// Scribble over the source payload region; the decoded frame must be immune.
	for i := HeaderSize; i < len(raw); i++ {
		raw[i] = 0x00
	}
	if want := []byte{0xde, 0xad, 0xbe, 0xef}; !bytes.Equal(got.Payload, want) {
		t.Fatalf("payload aliased source: got %x, want %x", got.Payload, want)
	}
}

func TestFrame_DecodeMaxLengthBoundary(t *testing.T) {
	// Exactly MaxFrameLength bytes must decode cleanly (the size check is an
	// upper bound, not exclusive of the maximum itself).
	atMax := make([]byte, MaxFrameLength)
	binary.LittleEndian.PutUint32(atMax[0:4], uint32(MaxFrameLength-HeaderSize))
	if _, err := Decode(atMax); err != nil {
		t.Fatalf("Decode() of exactly MaxFrameLength bytes errored: %v", err)
	}
}

func TestFrame_DecodeErrors(t *testing.T) {
	// One byte past MaxFrameLength; a valid length field isolates the size check.
	tooLarge := make([]byte, MaxFrameLength+1)
	binary.LittleEndian.PutUint32(tooLarge[0:4], uint32(MaxFrameLength+1-HeaderSize))

	tests := []struct {
		name string
		raw  []byte
		want error
	}{
		{
			name: "shorter than header",
			raw:  make([]byte, 19),
			want: ErrShortFrame,
		},
		{
			name: "exceeds maximum frame length",
			raw:  tooLarge,
			want: ErrFrameTooLarge,
		},
		{
			name: "length field exceeds available bytes",
			raw: []byte{
				0x10, 0x00, 0x00, 0x00, // claims 16 payload bytes...
				0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00,
				0xde, 0xad, // ...but only 2 are present
			},
			want: ErrLengthMismatch,
		},
		{
			name: "trailing bytes beyond declared length",
			raw: []byte{
				0x01, 0x00, 0x00, 0x00, // claims 1 payload byte...
				0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00,
				0xde, 0xad, // ...but 2 are present
			},
			want: ErrLengthMismatch,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Decode(tt.raw)
			if !errors.Is(err, tt.want) {
				t.Fatalf("Decode() error = %v, want %v", err, tt.want)
			}
		})
	}
}