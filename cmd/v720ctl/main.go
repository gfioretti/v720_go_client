// Command v720ctl is the composition root for the independent V720 / A9 Mini
// camera client: it wires the concrete adapters to the application layer and
// hands control to the CLI.
package main

import (
	"context"
	"os"
	"os/signal"

	"v720_go_client/internal/adapters/cli"
)

func main() {
	// Cancel the context on Ctrl-C so in-flight work (and future long-running
	// commands like live-view) shuts down cleanly.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	app := cli.NewApp(os.Stdout, os.Stderr)
	os.Exit(app.Run(ctx, os.Args[1:]))
}
