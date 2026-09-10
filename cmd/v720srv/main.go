// Command v720srv is the self-hosted server a V720 camera connects out to in STA
// mode, replacing naxclow's cloud. Point the camera's naxclow hostnames at this
// server via DNS (dnsmasq: address=/naxclow.com/<server-ip>); it answers the
// camera's boot REST on :80 (handing back its own host:tcpPort), accepts the
// camera's coordinator session on :6123, and drives the camera to start
// streaming. See docs/self-hosted-server-scope.md.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"v720_go_client/internal/adapters/cloudhttp"
	"v720_go_client/internal/adapters/transport/framedtcp"
	"v720_go_client/internal/application"
)

func main() {
	host := flag.String("host", "", "this server's LAN IP that the camera connects to (required)")
	httpPort := flag.Int("http-port", 80, "port for the camera boot REST (naxclow uses 80; needs privilege)")
	tcpPort := flag.Int("tcp-port", 6123, "port for the camera coordinator session")
	deviceCode := flag.String("device-code", "0800c0000001", "device uid reported to the camera")
	target := flag.String("target", application.DefaultClientTarget, "client target id used in 301 forward wrappers")
	pwd := flag.String("pwd", "deadbeef", "device password expected by the camera")
	flag.Parse()

	if *host == "" {
		fmt.Fprintln(os.Stderr, "v720srv: --host (this server's LAN IP) is required")
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Boot REST (:80) — DNS-redirected naxclow calls land here; getA9ConfCheck
	// hands the camera our host:tcpPort so it opens its coordinator session to us.
	httpSrv := &http.Server{
		Addr: fmt.Sprintf(":%d", *httpPort),
		Handler: cloudhttp.Handler(cloudhttp.Config{
			Host: *host, TCPPort: *tcpPort, DeviceCode: *deviceCode, Pwd: *pwd,
		}),
	}
	go func() {
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http server (:%d): %v", *httpPort, err)
		}
	}()

	// Coordinator (:6123) — the camera's session connection.
	ln, err := framedtcp.Listen(fmt.Sprintf(":%d", *tcpPort))
	if err != nil {
		log.Fatalf("coordinator listen: %v", err)
	}

	go func() {
		<-ctx.Done()
		_ = ln.Close()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutdownCtx)
	}()

	log.Printf("v720srv: REST on :%d, coordinator on :%d, advertising host %s:%d to cameras",
		*httpPort, *tcpPort, *host, *tcpPort)
	log.Printf("v720srv: point the camera's naxclow DNS at %s (dnsmasq: address=/naxclow.com/%s)", *host, *host)

	serveCameras(ctx, ln, application.NewDeviceGateway(*target), log.Printf, nil)
	log.Printf("v720srv: shut down")
}
