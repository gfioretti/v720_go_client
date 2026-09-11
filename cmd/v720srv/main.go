// Command v720srv is the self-hosted server a V720 camera connects out to in STA
// mode, replacing naxclow's cloud. Point the camera's naxclow hostnames at this
// server via DNS (dnsmasq: address=/naxclow.com/<server-ip>); it answers the
// camera's boot REST on :80 (handing back its own host:tcpPort), accepts the
// camera's coordinator session on :6123, and drives the camera to start
// streaming. See docs/self-hosted-server-scope.md.
package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"v720_go_client/internal/adapters/cloudhttp"
	"v720_go_client/internal/adapters/transport/framedtcp"
	"v720_go_client/internal/adapters/transport/relay"
	"v720_go_client/internal/application"
)

func main() {
	host := flag.String("host", "", "this server's LAN IP that the camera connects to (required)")
	httpPort := flag.Int("http-port", 80, "port for the camera boot REST (naxclow uses 80; needs privilege)")
	httpsPort := flag.Int("https-port", 443, "port for the boot REST over TLS, self-signed (0 disables)")
	tcpPort := flag.Int("tcp-port", 6123, "port for the camera coordinator session (TCP) and media (UDP)")
	viewPort := flag.Int("view-port", 8080, "port for the live MJPEG viewer (http://<host>:<view-port>/live)")
	deviceCode := flag.String("device-code", "0800c0000001", "device uid reported to the camera")
	target := flag.String("target", application.DefaultClientTarget, "client target id used in 301 forward wrappers")
	pwd := flag.String("pwd", "deadbeef", "device password expected by the camera")
	verbose := flag.Bool("verbose", false, "log every camera REST call, coordinator frame, and media stats")
	dnsEnabled := flag.Bool("dns", false, "run a built-in DNS sinkhole on :53 (naxclow -> --host, forward the rest); avoids needing Pi-hole/dnsmasq")
	dnsUpstream := flag.String("dns-upstream", "1.1.1.1:53", "upstream resolver for non-naxclow DNS queries")
	flag.Parse()

	if *host == "" {
		fmt.Fprintln(os.Stderr, "v720srv: --host (this server's LAN IP) is required")
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Boot REST (:80) — DNS-redirected naxclow calls land here; getA9ConfCheck
	// hands the camera our host:tcpPort so it opens its coordinator session to us.
	var restHandler http.Handler = cloudhttp.Handler(cloudhttp.Config{
		Host: *host, TCPPort: *tcpPort, DeviceCode: *deviceCode, Pwd: *pwd,
	})
	if *verbose {
		restHandler = logRequests(restHandler, log.Printf)
	}
	httpSrv := &http.Server{Addr: fmt.Sprintf(":%d", *httpPort), Handler: restHandler}
	go func() {
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http server (:%d): %v", *httpPort, err)
		}
	}()

	// Boot REST over TLS (best-effort) — in case the firmware uses HTTPS. Uses a
	// self-signed cert the camera does not verify. A bind failure (e.g. no
	// privilege for :443) is logged, not fatal — plain HTTP still serves.
	var httpsSrv *http.Server
	if *httpsPort > 0 {
		if cert, err := selfSignedCert("v720.naxclow.com", "*.naxclow.com", *host); err != nil {
			log.Printf("v720srv: TLS cert generation failed, HTTPS disabled: %v", err)
		} else {
			httpsSrv = &http.Server{
				Addr:      fmt.Sprintf(":%d", *httpsPort),
				Handler:   restHandler,
				TLSConfig: &tls.Config{Certificates: []tls.Certificate{cert}},
			}
			go func() {
				if err := httpsSrv.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
					log.Printf("v720srv: HTTPS server (:%d) not running: %v", *httpsPort, err)
				}
			}()
		}
	}

	// Coordinator (:6123 TCP) — the camera's session connection.
	ln, err := framedtcp.Listen(fmt.Sprintf(":%d", *tcpPort))
	if err != nil {
		log.Fatalf("coordinator listen: %v", err)
	}

	// Media (:6123 UDP) — the camera's live A/V; reassembled and fanned out to
	// browser viewers.
	mediaSrv, err := relay.ListenMedia(fmt.Sprintf(":%d", *tcpPort))
	if err != nil {
		log.Fatalf("media listen: %v", err)
	}
	hub := newMediaHub()
	go runMedia(ctx, mediaSrv, hub, *verbose, log.Printf)

	// Optional built-in DNS sinkhole (:53) — point the network's DNS here and the
	// camera resolves naxclow to us; everything else is forwarded.
	var dnsConn net.PacketConn
	if *dnsEnabled {
		if ip := net.ParseIP(*host); ip == nil || ip.To4() == nil {
			log.Printf("v720srv: --dns needs --host to be an IPv4 address (got %q); DNS disabled", *host)
		} else if dnsConn, err = net.ListenPacket("udp", ":53"); err != nil {
			log.Printf("v720srv: DNS sinkhole disabled (:53 bind failed: %v)", err)
			dnsConn = nil
		} else {
			go runDNS(ctx, dnsConn, net.ParseIP(*host), *dnsUpstream, *verbose, log.Printf)
			log.Printf("v720srv: DNS sinkhole on :53 (naxclow -> %s, forward -> %s); set this box (%s) as the network's DNS", *host, *dnsUpstream, *host)
		}
	}

	// Viewer HTTP — the live MJPEG stream.
	viewMux := http.NewServeMux()
	viewMux.HandleFunc("/live", hub.handler())
	viewMux.HandleFunc("/snapshot", hub.snapshotHandler())
	viewSrv := &http.Server{Addr: fmt.Sprintf(":%d", *viewPort), Handler: viewMux}
	go func() {
		if err := viewSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("viewer server (:%d): %v", *viewPort, err)
		}
	}()

	go func() {
		<-ctx.Done()
		_ = ln.Close()
		_ = mediaSrv.Close()
		if dnsConn != nil {
			_ = dnsConn.Close()
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutdownCtx)
		if httpsSrv != nil {
			_ = httpsSrv.Shutdown(shutdownCtx)
		}
		// Close (not Shutdown) the viewer: its MJPEG connections are long-lived
		// and would otherwise block Shutdown until the deadline.
		_ = viewSrv.Close()
	}()

	log.Printf("v720srv: REST on :%d, coordinator+media on :%d, viewer on :%d/live (+/snapshot); advertising host %s:%d to cameras",
		*httpPort, *tcpPort, *viewPort, *host, *tcpPort)
	log.Printf("v720srv: point the camera's naxclow DNS at %s (dnsmasq: address=/naxclow.com/%s)", *host, *host)

	serveCameras(ctx, ln, application.NewDeviceGateway(*target), *verbose, log.Printf, nil)
	log.Printf("v720srv: shut down")
}
