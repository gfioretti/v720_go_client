package cli

import (
	"context"
	"errors"
	"flag"
	"sort"
	"strconv"
	"strings"
	"time"

	"v720_go_client/internal/domain"
)

// sendCommandTimeout bounds writing a single control command after connecting.
const sendCommandTimeout = 5 * time.Second

// deviceSettings maps a CLI setting name to the domain command constructor for
// it (doc §12.5). Each takes a single integer value.
var deviceSettings = map[string]func(int) domain.Command{
	"quality":      domain.NewVideoQuality, // 201 speedGrade
	"ir":           domain.NewIRLed,        // 202 IrLed
	"motion-alert": domain.NewMotionAlert,  // 205 moveAlert
	"move-grade":   domain.NewMoveGrade,    // 206 moveGrade
	"white-led":    domain.NewWhiteLED,     // 220 ledEI
	"camera":       domain.NewSwitchCamera, // 222 camId
}

func settingNames() string {
	names := make([]string, 0, len(deviceSettings))
	for k := range deviceSettings {
		names = append(names, k)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// runAPSet connects and sends a single device-control command:
//
//	v720ctl ap set <setting> <value> --addr ... --dev ...
func (a *App) runAPSet(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("ap set", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	addr := fs.String("addr", "", "camera AP-mode address, host:port (required)")
	dev := fs.String("dev", "", "device id / target (required)")
	password := fs.String("password", "", "device password (the sk value)")
	timeout := fs.Duration("timeout", 10*time.Second, "connect timeout")
	debug := fs.Bool("debug", false, "log every frame sent/received to stderr")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *addr == "" || *dev == "" {
		a.errf("v720ctl ap set: --addr and --dev are required\n")
		return 2
	}

	rest := fs.Args()
	if len(rest) != 2 {
		a.errf("v720ctl ap set: expected <setting> <value>; settings: %s\n", settingNames())
		return 2
	}
	setting, valueStr := rest[0], rest[1]
	build, ok := deviceSettings[setting]
	if !ok {
		a.errf("v720ctl ap set: unknown setting %q; valid: %s\n", setting, settingNames())
		return 2
	}
	value, err := strconv.Atoi(valueStr)
	if err != nil {
		a.errf("v720ctl ap set: value must be an integer, got %q\n", valueStr)
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
	if err := session.SendCommand(sendCtx, build(value)); err != nil {
		a.errf("v720ctl: send %s: %v\n", setting, err)
		return 1
	}

	a.outf("sent %s=%d to %s\n", setting, value, *dev)
	return 0
}
