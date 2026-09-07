package naxclowapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"v720_go_client/internal/domain"
)

// newTestClient starts an httptest server with the given handler and returns a
// Client pointed at it.
func newTestClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return New(srv.URL, srv.Client())
}

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}

func TestLogin_ReturnsAccountAndStoresToken(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/app/api/ApiAppUser/login" {
			t.Errorf("login req = %s %s", r.Method, r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("phone") != "alice" || q.Get("pwd") != "pw" || q.Get("app") != "v720" {
			t.Errorf("login query = %v", q)
		}
		writeJSON(t, w, map[string]any{
			"code": 200, "message": "ok",
			"data": map[string]any{"token": "tok-123", "userId": "user-9"},
		})
	})

	acc, err := c.Login(context.Background(), "alice", "pw")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if acc.UID != "user-9" {
		t.Errorf("Account.UID = %q, want user-9", acc.UID)
	}
	c.mu.RLock()
	tok := c.token
	c.mu.RUnlock()
	if tok != "tok-123" {
		t.Errorf("stored token = %q, want tok-123", tok)
	}
}

func TestAuthedCallSendsToken(t *testing.T) {
	var gotAuth string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/app/api/ApiAppUser/login":
			writeJSON(t, w, map[string]any{"code": 200, "data": map[string]any{"token": "tok-xyz", "userId": "u1"}})
		case "/app/api/ApiServer/getA9VideoConf":
			gotAuth = r.Header.Get("Authorization")
			writeJSON(t, w, map[string]any{"code": 200, "data": map[string]any{
				"host": "1.2.3.4", "tcpPort": 6123, "pwd": "p", "tarPwd": "tp", "domain": "d",
			}})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})

	if _, err := c.Login(context.Background(), "a", "b"); err != nil {
		t.Fatalf("Login: %v", err)
	}
	conf, err := c.GetA9VideoConf(context.Background(), "dev123")
	if err != nil {
		t.Fatalf("GetA9VideoConf: %v", err)
	}
	if gotAuth != "tok-xyz" {
		t.Errorf("Authorization header = %q, want the raw login token tok-xyz", gotAuth)
	}
	want := domain.LiveConf{Host: "1.2.3.4", TCPPort: 6123, Pwd: "p", TarPwd: "tp", Domain: "d"}
	if conf != want {
		t.Errorf("LiveConf = %+v, want %+v", conf, want)
	}
}

func TestGetA9VideoConf_SendsDevicesCode(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/app/api/ApiAppUser/login" {
			writeJSON(t, w, map[string]any{"code": 200, "data": map[string]any{"token": "t", "userId": "u"}})
			return
		}
		if got := r.URL.Query().Get("devicesCode"); got != "dev123" {
			t.Errorf("devicesCode = %q, want dev123", got)
		}
		writeJSON(t, w, map[string]any{"code": 200, "data": map[string]any{"host": "h", "tcpPort": 1}})
	})
	if _, err := c.Login(context.Background(), "a", "b"); err != nil {
		t.Fatalf("Login: %v", err)
	}
	if _, err := c.GetA9VideoConf(context.Background(), "dev123"); err != nil {
		t.Fatalf("GetA9VideoConf: %v", err)
	}
}

func TestListDevices_MapsDevices(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/app/api/ApiAppUser/login" {
			writeJSON(t, w, map[string]any{"code": 200, "data": map[string]any{"token": "t", "userId": "u"}})
			return
		}
		writeJSON(t, w, map[string]any{"code": 200, "data": []map[string]any{
			{"devicesCode": "dev1", "devicesName": "Kitchen", "isOnline": "1"},
			{"devicesCode": "dev2", "devicesName": "Garage", "isOnline": "0"},
		}})
	})
	if _, err := c.Login(context.Background(), "a", "b"); err != nil {
		t.Fatalf("Login: %v", err)
	}
	devices, err := c.ListDevices(context.Background())
	if err != nil {
		t.Fatalf("ListDevices: %v", err)
	}
	want := []domain.Device{
		{ID: "dev1", Name: "Kitchen", Online: true},
		{ID: "dev2", Name: "Garage", Online: false},
	}
	if len(devices) != 2 || devices[0] != want[0] || devices[1] != want[1] {
		t.Errorf("devices = %+v, want %+v", devices, want)
	}
}

func TestWatching_SucceedsAndFails(t *testing.T) {
	code := 200
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/app/api/ApiAppUser/login" {
			writeJSON(t, w, map[string]any{"code": 200, "data": map[string]any{"token": "t", "userId": "u"}})
			return
		}
		q := r.URL.Query()
		if q.Get("pwd") != "secret" || q.Get("devicesCode") != "dev123" {
			t.Errorf("watching query = %v", q)
		}
		writeJSON(t, w, map[string]any{"code": code, "message": "denied"})
	})
	if _, err := c.Login(context.Background(), "a", "b"); err != nil {
		t.Fatalf("Login: %v", err)
	}
	if err := c.Watching(context.Background(), "dev123", "secret"); err != nil {
		t.Fatalf("Watching (code 200): %v", err)
	}
	code = 401
	if err := c.Watching(context.Background(), "dev123", "secret"); err == nil {
		t.Fatal("Watching (code 401) = nil, want backend error")
	}
}

// An authed call before Login fails fast (with ErrNotLoggedIn) without hitting
// the network.
func TestAuthedCallBeforeLoginFails(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("server should not be hit; got %s", r.URL.Path)
	})
	_, err := c.GetA9VideoConf(context.Background(), "dev123")
	if !errors.Is(err, ErrNotLoggedIn) {
		t.Fatalf("GetA9VideoConf before Login = %v, want ErrNotLoggedIn", err)
	}
}

// Login itself must not send an Authorization header (it is the unauthenticated
// bootstrap call).
func TestLoginSendsNoAuthHeader(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("login sent Authorization = %q, want none", got)
		}
		writeJSON(t, w, map[string]any{"code": 200, "data": map[string]any{"token": "t", "userId": "u"}})
	})
	if _, err := c.Login(context.Background(), "a", "b"); err != nil {
		t.Fatalf("Login: %v", err)
	}
}

// A non-2xx HTTP status is surfaced as an error.
func TestHTTPErrorStatusSurfaces(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	if _, err := c.Login(context.Background(), "a", "b"); err == nil {
		t.Fatal("Login on HTTP 500 = nil, want error")
	}
}

// A backend envelope error code (HTTP 200 but code != 200) is surfaced with its
// message.
func TestBackendErrorCodeSurfaces(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{"code": 1001, "message": "bad credentials"})
	})
	_, err := c.Login(context.Background(), "a", "b")
	if err == nil {
		t.Fatal("Login with backend error code = nil, want error")
	}
}
