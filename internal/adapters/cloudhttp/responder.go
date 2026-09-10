// Package cloudhttp is the HTTP side of the self-hosted server: it answers the
// REST calls a V720 camera makes on boot after its naxclow hostnames are
// DNS-redirected to this server (findings doc §6; endpoints cross-referenced
// against a9-v720's v720_http.py). The pivotal response is getA9ConfCheck, which
// hands the camera the host:tcpPort of our coordinator so it opens its session
// back to us. Everything else the camera calls gets a permissive {"code":200}
// envelope so boot proceeds.
package cloudhttp

import (
	"encoding/json"
	"net/http"
	"time"
)

// Config is what the camera needs to be told to reach our coordinator.
type Config struct {
	// Host and TCPPort are where the camera should open its coordinator session —
	// i.e. this server's own LAN IP and the framed-TCP port (6123). Returned by
	// getA9ConfCheck.
	Host    string
	TCPPort int

	// DeviceCode is the uid reported to the camera (a9-v720 uses a "0800c00…"
	// style code). Pwd is the device password the camera expects (a9-v720 uses
	// the literal "deadbeef").
	DeviceCode string
	Pwd        string

	// now supplies the currTime timestamp; defaults to time.Now.
	now func() time.Time
}

// envelope is the standard naxclow REST wrapper (doc §5a): {code, message, data}.
type envelope struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data"`
}

// Handler returns an http.Handler serving the camera's boot REST. Unknown paths
// get a permissive {"code":200,"data":null} so an unmodelled call still lets the
// camera proceed.
func Handler(cfg Config) http.Handler {
	if cfg.now == nil {
		cfg.now = time.Now
	}
	if cfg.Pwd == "" {
		cfg.Pwd = "deadbeef"
	}

	mux := http.NewServeMux()

	// The pivotal bootstrap: tell the camera where to open its coordinator
	// session (our host:tcpPort) and the credentials it should use.
	mux.HandleFunc("/app/api/ApiServer/getA9ConfCheck", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, envelope{Code: 200, Message: "OK", Data: map[string]any{
			"host":     cfg.Host,
			"tcpPort":  cfg.TCPPort,
			"uid":      cfg.DeviceCode,
			"domain":   "v720.naxclow.com",
			"pwd":      cfg.Pwd,
			"isBind":   "8",
			"currTime": cfg.now().Unix(),
		}})
	})

	// registerDevices returns a device code; the rest just need a 200 envelope.
	mux.HandleFunc("/app/api/ApiSysDevicesBatch/registerDevices", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, envelope{Code: 200, Message: "OK", Data: cfg.DeviceCode})
	})
	mux.HandleFunc("/app/api/ApiSysDevices/getDevInfo", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, envelope{Code: 200, Message: "OK", Data: map[string]any{"userInfo_state": 1}})
	})

	// Catch-all: any other boot call (confirm, a9bindingAppDevice, …) succeeds.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, envelope{Code: 200, Message: "OK", Data: nil})
	})

	return mux
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
