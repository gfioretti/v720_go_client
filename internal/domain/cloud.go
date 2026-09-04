package domain

import (
	"net"
	"strconv"
)

// The types here are pure value objects describing the cloud/rendezvous inputs a
// relay session needs before it can open its channels (findings doc §5, §12.1a).
// They carry no behaviour beyond trivial derived values and know nothing of how
// they were obtained — the real naxclow REST backend and a self-hosted server are
// interchangeable sources (see the application-layer CloudAPI port).

// Account is the logged-in app user's identity. Only the account ID is modelled,
// because that is all the relay handshake consumes: the coordinator register
// message's "uid" field is this UID, not the device ID (doc §12.1a).
type Account struct {
	// UID is the app account's user ID, sent as "uid" in the coordinator register
	// (doc §12.2 phase 1 step 2 / §12.1a).
	UID string
}

// Device is one camera bound to an Account, as returned by the device-list step
// (doc §5 step 2). ID is the devicesCode / devId used to address the device
// throughout the protocol.
type Device struct {
	ID     string
	Name   string
	Online bool
}

// LiveConf is the addressing + credential bundle the relay session needs to bring
// up its channels — the app's DeviceLiveBean from getA9VideoConf (doc §5 step 4).
// Per §12.1a: Pwd populates the register "token" field and Domain the third
// register field, while TarPwd populates the "devToken" field on codes 10/30/52.
type LiveConf struct {
	// Host and TCPPort address the coordinator/rendezvous server (not necessarily
	// the camera itself), where the phase-1 TCP handshake runs (doc §12.2 phase 1
	// step 1).
	Host    string
	TCPPort int

	Pwd    string // device password → coordinator register "token"
	TarPwd string // → "devToken" on codes 10/30/52
	Domain string // → the third coordinator register field
}

// Addr is the "host:port" dial string for the coordinator channel, formed safely
// for IPv6 hosts via net.JoinHostPort.
func (c LiveConf) Addr() string {
	return net.JoinHostPort(c.Host, strconv.Itoa(c.TCPPort))
}
