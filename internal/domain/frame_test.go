package domain

import (
	"bytes"
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

func TestFrame_DecodeErrors(t *testing.T) {
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