package relay

import (
	"fmt"
	"net"

	"v720_go_client/internal/domain"
)

// MediaServer is the server side of the UDP media channel: it binds a UDP socket
// the camera streams its live A/V to (in STA mode, after the coordinator FSM
// starts the stream), and sends per-frame retransmit-confirm ACKs back to it.
// Unlike MediaChannel (a connected client socket), this is an *unconnected*
// listener — it learns the camera's address from each datagram and replies to it.
//
// One datagram carries one whole 20-byte-header frame (findings doc §12.2a).
// Malformed datagrams are dropped, not fatal.
type MediaServer struct {
	conn net.PacketConn
	buf  []byte
}

// ListenMedia binds the UDP media socket on addr (e.g. ":6123").
func ListenMedia(addr string) (*MediaServer, error) {
	conn, err := net.ListenPacket("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("relay media server: listen %s: %w", addr, err)
	}
	return &MediaServer{conn: conn, buf: make([]byte, domain.MaxFrameLength)}, nil
}

// Receive blocks for the next valid media frame, returning it and the camera's
// address (for addressing ACKs). Malformed datagrams are skipped; it returns an
// error only on a real socket failure or once Close is called.
func (s *MediaServer) Receive() (domain.Frame, net.Addr, error) {
	for {
		n, addr, err := s.conn.ReadFrom(s.buf)
		if err != nil {
			return domain.Frame{}, nil, fmt.Errorf("relay media server: receive: %w", err)
		}
		f, err := domain.Decode(s.buf[:n])
		if err != nil {
			continue // stray/corrupt datagram; keep listening
		}
		return f, addr, nil
	}
}

// Send transmits a frame (e.g. a retransmit-confirm ACK) to addr.
func (s *MediaServer) Send(f domain.Frame, addr net.Addr) error {
	if _, err := s.conn.WriteTo(f.Encode(), addr); err != nil {
		return fmt.Errorf("relay media server: send: %w", err)
	}
	return nil
}

// LocalAddr reports the bound address (useful when binding to :0).
func (s *MediaServer) LocalAddr() net.Addr { return s.conn.LocalAddr() }

// Close stops the server and unblocks a pending Receive.
func (s *MediaServer) Close() error { return s.conn.Close() }
