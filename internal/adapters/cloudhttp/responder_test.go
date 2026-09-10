package cloudhttp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func testServer(t *testing.T) *httptest.Server {
	t.Helper()
	h := Handler(Config{
		Host:       "192.168.178.50",
		TCPPort:    6123,
		DeviceCode: "0800c0012345",
		now:        func() time.Time { return time.Unix(1700000000, 0) },
	})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

func get(t *testing.T, srv *httptest.Server, path string) map[string]any {
	t.Helper()
	resp, err := http.Get(srv.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		t.Fatalf("GET %s status = %d", path, resp.StatusCode)
	}
	var m map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return m
}

// getA9ConfCheck is the pivotal call: it must hand the camera our coordinator
// host:tcpPort plus the expected credentials.
func TestResponder_GetA9ConfCheck(t *testing.T) {
	srv := testServer(t)
	m := get(t, srv, "/app/api/ApiServer/getA9ConfCheck")
	if m["code"] != float64(200) {
		t.Fatalf("code = %v, want 200", m["code"])
	}
	data, _ := m["data"].(map[string]any)
	if data["host"] != "192.168.178.50" || data["tcpPort"] != float64(6123) ||
		data["uid"] != "0800c0012345" || data["pwd"] != "deadbeef" ||
		data["domain"] != "v720.naxclow.com" || data["currTime"] != float64(1700000000) {
		t.Fatalf("getA9ConfCheck data = %v", data)
	}
}

func TestResponder_RegisterDevices(t *testing.T) {
	srv := testServer(t)
	m := get(t, srv, "/app/api/ApiSysDevicesBatch/registerDevices")
	if m["code"] != float64(200) || m["data"] != "0800c0012345" {
		t.Fatalf("registerDevices = %v", m)
	}
}

func TestResponder_GetDevInfo(t *testing.T) {
	srv := testServer(t)
	m := get(t, srv, "/app/api/ApiSysDevices/getDevInfo")
	data, _ := m["data"].(map[string]any)
	if m["code"] != float64(200) || data["userInfo_state"] != float64(1) {
		t.Fatalf("getDevInfo = %v", m)
	}
}

// Any unmodelled boot call still gets a 200 envelope so the camera proceeds.
func TestResponder_CatchAllSucceeds(t *testing.T) {
	srv := testServer(t)
	for _, path := range []string{
		"/app/api/ApiSysDevicesBatch/confirm",
		"/app/api/ApiSysDevices/a9bindingAppDevice",
		"/app/api/something/we/did/not/model",
	} {
		m := get(t, srv, path)
		if m["code"] != float64(200) {
			t.Errorf("%s code = %v, want 200", path, m["code"])
		}
	}
}

// Pwd defaults to the a9-v720 literal when unset.
func TestResponder_PwdDefault(t *testing.T) {
	h := Handler(Config{Host: "h", TCPPort: 6123, DeviceCode: "d"})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	m := get(t, srv, "/app/api/ApiServer/getA9ConfCheck")
	data, _ := m["data"].(map[string]any)
	if data["pwd"] != "deadbeef" {
		t.Fatalf("default pwd = %v, want deadbeef", data["pwd"])
	}
}
