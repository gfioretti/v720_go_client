package domain

// FrameType is the 16-bit "packet type tag" stored at offset 4 of every wire
// frame's 20-byte header (findings doc §12.3a). It classifies what kind of
// payload the frame carries — JSON control message, MJPEG video, G.711 audio,
// keepalive pong, and so on. The concrete tag constants live alongside this
// type; see doc §12.4 for the authoritative table.
//
// The on-wire encoding is a little-endian int16. Every observed tag value is a
// small non-negative number, so an unsigned 16-bit representation is a faithful
// and convenient carrier.
type FrameType uint16