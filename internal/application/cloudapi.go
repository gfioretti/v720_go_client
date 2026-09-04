package application

import (
	"context"

	"v720_go_client/internal/domain"
)

// CloudAPI is the driven/secondary port a relay session's setup phase uses to
// obtain the rendezvous addressing + credentials before opening its channels
// (findings doc §5). It abstracts *where* those come from: the real naxclow REST
// backend is one adapter, but the whole point of the port is that a self-hosted
// server the camera has been pointed at can implement it just as well (doc §6).
// It is defined here, in the package that consumes it; adapters satisfy it
// structurally and never import this package.
//
// The methods mirror the app's cloud flow (doc §5 steps 1–4) and are expected to
// be called in order — Login first, then the rest. Auth state (a bearer token in
// the naxclow case) is the adapter's own business and deliberately absent from
// this interface: the application only needs the account UID (for the coordinator
// register message, doc §12.1a) and the LiveConf. A self-hosted adapter is free
// to no-op Login/Watching and simply return a configured Account/LiveConf.
type CloudAPI interface {
	// Login authenticates the app user and returns their account, whose UID the
	// coordinator register message carries as "uid" (doc §12.1a). Adapters retain
	// whatever auth state subsequent calls need.
	Login(ctx context.Context, username, password string) (domain.Account, error)

	// ListDevices returns the cameras bound to the logged-in account (doc §5 step
	// 2). Requires a prior successful Login.
	ListDevices(ctx context.Context) ([]domain.Device, error)

	// Watching validates the device password against the device before a session
	// is allowed (doc §5 step 3). devicesCode is the device ID (same value as
	// domain.Device.ID / GetA9VideoConf's devID — the doc just names it
	// differently per endpoint); pwd is the user-configured device password.
	Watching(ctx context.Context, devicesCode, pwd string) error

	// GetA9VideoConf returns the addressing + credential bundle for a device (doc
	// §5 step 4), used to bring up the relay channels.
	GetA9VideoConf(ctx context.Context, devID string) (domain.LiveConf, error)
}
