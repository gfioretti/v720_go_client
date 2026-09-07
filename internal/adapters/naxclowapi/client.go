// Package naxclowapi is the secondary/driven adapter implementing the
// application.CloudAPI port against naxclow's real REST backend
// (v720.naxclow.com, findings doc §4/§5). The request/response shapes here were
// pinned by reading the decompiled app (retrofit interface com.naxclow.base
// .IHttpService and the LoginBean/DeviceLiveBean/DeviceListBean response beans) —
// see docs/v720-camera-findings-and-client-spec.md §5.
//
// Because the developer has no naxclow account and the camera was never
// cloud-bound, this adapter is BUILD-TO-SPEC and has NOT been exercised against
// the real cloud. It exists so the CloudAPI port has a concrete naxclow
// implementation; a self-hosted server is free to provide its own (see
// docs/mode-a-client-design.md).
package naxclowapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"v720_go_client/internal/application"
	"v720_go_client/internal/domain"
)

// ErrNotLoggedIn is returned by an authenticated call made before a successful
// Login, so callers can distinguish it with errors.Is.
var ErrNotLoggedIn = errors.New("naxclowapi: not logged in")

// DefaultBaseURL is naxclow's main REST API host (doc §4).
const DefaultBaseURL = "https://v720.naxclow.com/"

// loginAppParam is the value of the "app" query parameter on login — the app
// passes the literal "v720" (observed in SplashActivity).
const loginAppParam = "v720"

// successCode is the envelope "code" value that marks a successful response; the
// app's BaseProtocol.execute treats anything other than 200 as an error.
const successCode = 200

// Client talks to naxclow's REST backend. After Login it retains the bearer
// token internally and attaches it (raw, no "Bearer " prefix — as the app does)
// as the Authorization header on subsequent calls. Safe for concurrent use.
type Client struct {
	baseURL string
	http    *http.Client

	mu    sync.RWMutex
	token string
}

// Compile-time proof the adapter satisfies the port.
var _ application.CloudAPI = (*Client)(nil)

// New returns a Client for baseURL (use DefaultBaseURL for the real backend, or a
// self-hosted URL). A nil httpClient gets a default with a sane timeout.
func New(baseURL string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/") + "/", http: httpClient}
}

// Login authenticates via POST app/api/ApiAppUser/login (query: phone, pwd,
// app=v720) and retains the returned bearer token. It returns the account whose
// UID (the response's data.userId) the coordinator register message needs.
func (c *Client) Login(ctx context.Context, username, password string) (domain.Account, error) {
	q := url.Values{"phone": {username}, "pwd": {password}, "app": {loginAppParam}}
	var resp loginResponse
	if err := c.do(ctx, http.MethodPost, "app/api/ApiAppUser/login", q, false, &resp); err != nil {
		return domain.Account{}, err
	}
	c.mu.Lock()
	c.token = resp.Data.Token
	c.mu.Unlock()
	return domain.Account{UID: resp.Data.UserID}, nil
}

// ListDevices fetches the account's cameras via GET
// app/api/ApiSysDevices/getDevicesListByType (devicesType empty = all).
func (c *Client) ListDevices(ctx context.Context) ([]domain.Device, error) {
	q := url.Values{"devicesType": {""}}
	var resp deviceListResponse
	if err := c.do(ctx, http.MethodGet, "app/api/ApiSysDevices/getDevicesListByType", q, true, &resp); err != nil {
		return nil, err
	}
	devices := make([]domain.Device, 0, len(resp.Data))
	for _, d := range resp.Data {
		devices = append(devices, domain.Device{
			ID:   d.DevicesCode,
			Name: d.DevicesName,
			// isOnline is a string flag; "1" = online. Value convention is
			// UNCONFIRMED (jadx shows it as a String, not the exact truthy value).
			Online: d.IsOnline == "1",
		})
	}
	return devices, nil
}

// Watching validates the device password before a session via POST
// app/api/ApiServer/watching (query: pwd, devicesCode).
func (c *Client) Watching(ctx context.Context, devicesCode, pwd string) error {
	q := url.Values{"pwd": {pwd}, "devicesCode": {devicesCode}}
	var resp envelope
	return c.do(ctx, http.MethodPost, "app/api/ApiServer/watching", q, true, &resp)
}

// GetA9VideoConf fetches the rendezvous addressing + credentials via GET
// app/api/ApiServer/getA9VideoConf (query: devicesCode), mapping the response's
// data (DeviceLiveBean.DeviceDataBean) to a domain.LiveConf.
func (c *Client) GetA9VideoConf(ctx context.Context, devID string) (domain.LiveConf, error) {
	q := url.Values{"devicesCode": {devID}}
	var resp videoConfResponse
	if err := c.do(ctx, http.MethodGet, "app/api/ApiServer/getA9VideoConf", q, true, &resp); err != nil {
		return domain.LiveConf{}, err
	}
	return domain.LiveConf{
		Host:    resp.Data.Host,
		TCPPort: resp.Data.TCPPort,
		Pwd:     resp.Data.Pwd,
		TarPwd:  resp.Data.TarPwd,
		Domain:  resp.Data.Domain,
	}, nil
}

// do performs one request: it builds method+path with the query string, attaches
// the Authorization header when withAuth is set, decodes the JSON body into out,
// and checks the response envelope's code. out must embed envelope so its code()
// can be checked.
func (c *Client) do(ctx context.Context, method, path string, query url.Values, withAuth bool, out coded) error {
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, nil)
	if err != nil {
		return fmt.Errorf("naxclowapi: build request %s: %w", path, err)
	}
	if withAuth {
		c.mu.RLock()
		token := c.token
		c.mu.RUnlock()
		if token == "" {
			return fmt.Errorf("naxclowapi: %s: %w", path, ErrNotLoggedIn)
		}
		req.Header.Set("Authorization", token)
		// Note: the app additionally sends an explicit "Host: v720.naxclow.com"
		// header on the device-list call, but that equals the base URL's host, so
		// the default Host (set by net/http from the request URL) already matches.
		// We deliberately do NOT override it — doing so would send naxclow's host
		// to a self-hosted server the Client was pointed at.
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("naxclowapi: %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("naxclowapi: read %s response: %w", path, err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("naxclowapi: %s returned HTTP %d: %s", path, resp.StatusCode, snippet(body))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("naxclowapi: decode %s response: %w", path, err)
	}
	return out.code()
}

// envelope is the common wrapper on every response (doc §5): a numeric code plus
// a human message (the backend uses "message"; some endpoints use "msg").
type envelope struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Msg     string `json:"msg"`
}

// code reports the envelope as an error unless it carries the success code.
func (e envelope) code() error {
	if e.Code == successCode {
		return nil
	}
	msg := e.Message
	if msg == "" {
		msg = e.Msg
	}
	return fmt.Errorf("naxclowapi: backend error code %d: %s", e.Code, msg)
}

// coded is what do needs from a response: its embedded envelope's code check.
type coded interface{ code() error }

// snippet returns a short, single-line excerpt of a response body for error
// messages, so an HTTP-level failure surfaces whatever the server said.
func snippet(body []byte) string {
	const max = 200
	s := strings.TrimSpace(string(body))
	if len(s) > max {
		s = s[:max] + "…"
	}
	return strings.ReplaceAll(s, "\n", " ")
}

type loginResponse struct {
	envelope
	Data struct {
		Token  string `json:"token"`
		UserID string `json:"userId"`
	} `json:"data"`
}

type deviceListResponse struct {
	envelope
	Data []struct {
		DevicesCode string `json:"devicesCode"`
		DevicesName string `json:"devicesName"`
		IsOnline    string `json:"isOnline"`
	} `json:"data"`
}

type videoConfResponse struct {
	envelope
	Data struct {
		Host    string `json:"host"`
		TCPPort int    `json:"tcpPort"`
		Pwd     string `json:"pwd"`
		TarPwd  string `json:"tarPwd"`
		Domain  string `json:"domain"`
		// The bean also carries managePort/token/conId/target/uid; unused here.
	} `json:"data"`
}
