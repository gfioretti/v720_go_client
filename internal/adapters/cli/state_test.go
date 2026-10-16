package cli

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gfioretti/v720_go_client/internal/domain"
)

func TestMJPEGServer_StateEmptyByDefault(t *testing.T) {
	ts := httptest.NewServer(newMJPEGServer().routes())
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/state")
	if err != nil {
		t.Fatalf("GET /state: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if strings.TrimSpace(string(body)) != "{}" {
		t.Errorf("empty state = %q, want {}", body)
	}
}

func TestMJPEGServer_MergeStateAccumulates(t *testing.T) {
	srv := newMJPEGServer()
	two := 2
	one := 1
	srv.mergeState(domain.DeviceState{SpeedGrade: &two, WifiName: "Cam"})
	srv.mergeState(domain.DeviceState{IrLed: &one}) // partial update

	st := srv.currentState()
	if st.SpeedGrade == nil || *st.SpeedGrade != 2 {
		t.Errorf("SpeedGrade = %v, want 2", st.SpeedGrade)
	}
	if st.IrLed == nil || *st.IrLed != 1 {
		t.Errorf("IrLed = %v, want 1 (merged)", st.IrLed)
	}
	if st.WifiName != "Cam" {
		t.Errorf("WifiName = %q, want preserved", st.WifiName)
	}
}

// End-to-end: a device-info message streamed by the camera is reflected at /state.
func TestRun_ServeStateReflectsDeviceInfo(t *testing.T) {
	const devID = "dev123"
	media := []domain.Frame{
		{Type: domain.FrameTypeJSON, Payload: []byte(`{"code":502,"content":{"code":4,"speedGrade":2,"IrLed":1,"devPower":88,"wifiName":"Cam","version":"v9"}}`)},
		{Type: domain.FrameTypeMJPEG, Payload: []byte("\xff\xd8x\xff\xd9")},
	}
	camAddr := startMockCameraStreaming(t, devID, media)
	httpAddr := freePort(t)

	app := NewApp(&syncBuffer{}, &syncBuffer{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan int, 1)
	go func() {
		done <- app.Run(ctx, []string{
			"ap", "serve", "--addr", camAddr, "--dev", devID,
			"--connect-timeout", "2s", "--http", httpAddr,
		})
	}()

	base := "http://" + httpAddr
	waitForHTTP(t, base+"/")

	// Poll /state until the device-info has propagated.
	var st domain.DeviceState
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(base + "/state")
		if err == nil {
			_ = json.NewDecoder(resp.Body).Decode(&st)
			_ = resp.Body.Close()
			if st.SpeedGrade != nil {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}

	if st.SpeedGrade == nil || *st.SpeedGrade != 2 {
		t.Errorf("state SpeedGrade = %v, want 2", st.SpeedGrade)
	}
	if st.DevPower == nil || *st.DevPower != 88 {
		t.Errorf("state DevPower = %v, want 88", st.DevPower)
	}
	if st.WifiName != "Cam" || st.Version != "v9" {
		t.Errorf("state wifi/version = %q/%q", st.WifiName, st.Version)
	}

	cancel()
	<-done
}
