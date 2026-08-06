package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strings"
	"time"

	"v720_go_client/internal/domain"
)

// runAPSDList connects and prints the SD-card recording catalogue:
//
//	v720ctl ap sd-list --addr ... --dev ...
func (a *App) runAPSDList(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("ap sd-list", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	addr := fs.String("addr", "", "camera AP-mode address, host:port (required)")
	dev := fs.String("dev", "", "device id / target (required)")
	password := fs.String("password", "", "device password (the sk value)")
	connectTimeout := fs.Duration("connect-timeout", 10*time.Second, "handshake timeout")
	timeout := fs.Duration("timeout", 30*time.Second, "catalogue fetch timeout")
	debug := fs.Bool("debug", false, "log every frame sent/received to stderr")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *addr == "" || *dev == "" {
		a.errf("v720ctl ap sd-list: --addr and --dev are required\n")
		return 2
	}
	if fs.NArg() > 0 {
		a.errf("v720ctl ap sd-list: unexpected arguments: %v\n", fs.Args())
		return 2
	}

	connectCtx, cancel := context.WithTimeout(ctx, *connectTimeout)
	session, transport, err := a.dialAndConnect(connectCtx, *addr, *dev, *password, *debug)
	cancel()
	if err != nil {
		a.errf("v720ctl: %v\n", err)
		return 1
	}
	defer func() { _ = transport.Close() }()

	listCtx, cancelList := context.WithTimeout(ctx, *timeout)
	defer cancelList()
	catalogue, err := session.ListRecordings(listCtx)
	if err != nil {
		a.errf("v720ctl: list recordings: %v\n", err)
		return 1
	}

	a.printCatalogue(catalogue)
	return 0
}

func (a *App) printCatalogue(c domain.Catalogue) {
	if len(c.Days) == 0 {
		a.outf("no recordings on SD card\n")
		return
	}
	for _, day := range c.Days {
		a.outf("%s\n", day.Date)
		for _, h := range day.Hours {
			a.outf("  %02d: %s\n", h.Hour, formatMinutes(h.Minutes))
		}
	}
}

func formatMinutes(minutes []int) string {
	if len(minutes) == 0 {
		return "(none)"
	}
	parts := make([]string, len(minutes))
	for i, m := range minutes {
		parts[i] = fmt.Sprintf("%02d", m)
	}
	return strings.Join(parts, " ")
}
