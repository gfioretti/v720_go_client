package cli

import (
	"context"
	"errors"
	"flag"
	"time"

	"github.com/gfioretti/v720_go_client/internal/domain"
)

// apAddress is the camera's fixed AP-mode address (doc §12.3); set-wifi always
// runs over the hotspot, so it is the default --addr.
const apAddress = "192.168.169.1:6123"

// runAPSetWifi provisions the camera onto a home Wi-Fi network so it can then be
// reached in STA mode (see v720ctl scan). It connects over the camera's AP
// hotspot and sends the set-wifi command (doc §12.5 code 204, s=ssid, p=password):
//
//	v720ctl ap set-wifi --ssid <name> --pass <pwd> --dev <devId> [--addr host:port]
//
// It is fire-and-forget: once accepted, the camera reboots onto the target
// network and drops its AP connection, so there is no ack to wait for.
func (a *App) runAPSetWifi(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("ap set-wifi", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	addr := fs.String("addr", apAddress, "camera AP-mode address, host:port")
	dev := fs.String("dev", "", "device id / target (required)")
	password := fs.String("password", "", "the camera's own device password / sk (NOT the home Wi-Fi password)")
	ssid := fs.String("ssid", "", "home Wi-Fi SSID for the camera to join (required)")
	pass := fs.String("pass", "", "home Wi-Fi password (WPA: 8-63 chars; empty for an open network)")
	timeout := fs.Duration("timeout", 10*time.Second, "connect timeout")
	debug := fs.Bool("debug", false, "log every frame sent/received to stderr")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *dev == "" || *ssid == "" {
		a.errf("v720ctl ap set-wifi: --dev and --ssid are required\n")
		return 2
	}
	// Validate the WPA passphrase length up front: because set-wifi is
	// fire-and-forget, a length the camera silently rejects would leave it never
	// joining and never returning to its hotspot — a dead end only discovered
	// later when scan finds nothing. Empty is allowed (open network).
	if n := len(*pass); n != 0 && (n < 8 || n > 63) {
		a.errf("v720ctl ap set-wifi: --pass must be 8-63 characters (WPA), or empty for an open network; got %d\n", n)
		return 2
	}
	if fs.NArg() > 0 {
		a.errf("v720ctl ap set-wifi: unexpected arguments: %v\n", fs.Args())
		return 2
	}

	connectCtx, cancel := context.WithTimeout(ctx, *timeout)
	session, transport, err := a.dialAndConnect(connectCtx, *addr, *dev, *password, *debug)
	cancel()
	if err != nil {
		a.errf("v720ctl: %v\n", err)
		return 1
	}
	defer func() { _ = transport.Close() }()

	sendCtx, cancelSend := context.WithTimeout(ctx, sendCommandTimeout)
	defer cancelSend()
	if err := session.SendCommand(sendCtx, domain.NewSetWifi(*ssid, *pass)); err != nil {
		a.errf("v720ctl: send set-wifi: %v\n", err)
		return 1
	}

	a.outf("sent Wi-Fi config to %s: SSID %q.\n", *dev, *ssid)
	a.outf("the camera will reboot and join %q; its Nax_* hotspot will drop.\n", *ssid)
	a.outf("reconnect to your home Wi-Fi, then run 'v720ctl scan' to find the camera's LAN IP.\n")
	return 0
}
