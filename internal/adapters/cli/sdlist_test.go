package cli

import (
	"context"
	"net"
	"strings"
	"testing"

	"v720_go_client/internal/domain"
)

func TestRun_SDListMissingRequiredFlags(t *testing.T) {
	app, _, errb := newTestApp()
	code := app.Run(context.Background(), []string{"ap", "sd-list", "--addr", "127.0.0.1:9"})
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(errb.String(), "required") {
		t.Fatalf("stderr = %q", errb.String())
	}
}

func TestRun_SDListRejectsTrailingArgs(t *testing.T) {
	app, _, errb := newTestApp()
	code := app.Run(context.Background(), []string{"ap", "sd-list", "--addr", "127.0.0.1:9", "--dev", "d", "extra"})
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(errb.String(), "unexpected arguments") {
		t.Fatalf("stderr = %q", errb.String())
	}
}

// A device that rejects the catalogue request (403 non-200) is a runtime failure
// (exit 1).
func TestRun_SDListRejectedExits1(t *testing.T) {
	const devID = "dev123"
	addr := serveMockCamera(t, func(conn net.Conn) {
		if !mockHandshake(conn, devID, 200) {
			return
		}
		if _, err := readStreamFrame(conn); err != nil {
			return
		}
		_ = writeStreamFrame(conn, jsonControl(map[string]any{"code": 403, "status": 500}))
		drain(conn)
	})

	app, _, errb := newTestApp()
	code := app.Run(context.Background(), []string{
		"ap", "sd-list", "--addr", addr, "--dev", devID, "--connect-timeout", "2s", "--timeout", "2s",
	})
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errb.String(), "list recordings") {
		t.Fatalf("stderr = %q", errb.String())
	}
}

// End-to-end: connect, fetch the catalogue (403 + type-8 XML), and print it.
func TestRun_SDListPrintsCatalogue(t *testing.T) {
	const devID = "dev123"
	xml := `<video>` +
		`<catalogue date="20260805" hNumb="1"><option hours="14" minBin="6"/></catalogue>` +
		`<catalogue date="20260804" hNumb="1"><option hours="9" minBin="0"/></catalogue>` +
		`</video>`
	addr := startMockCameraServingCatalogue(t, devID, xml)

	app, out, errb := newTestApp()
	code := app.Run(context.Background(), []string{
		"ap", "sd-list", "--addr", addr, "--dev", devID, "--connect-timeout", "2s", "--timeout", "2s",
	})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %q)", code, errb.String())
	}
	s := out.String()
	// minBin 6 = bits 1,2 → minutes 01 02 under hour 14 on the first date.
	for _, want := range []string{"20260805", "14: 01 02", "20260804", "09: (none)"} {
		if !strings.Contains(s, want) {
			t.Fatalf("stdout missing %q:\n%s", want, s)
		}
	}
}

func TestRun_SDListEmptyCard(t *testing.T) {
	const devID = "dev123"
	addr := startMockCameraServingEmptyCatalogue(t, devID)

	app, out, errb := newTestApp()
	code := app.Run(context.Background(), []string{
		"ap", "sd-list", "--addr", addr, "--dev", devID, "--connect-timeout", "2s", "--timeout", "2s",
	})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %q)", code, errb.String())
	}
	if !strings.Contains(out.String(), "no recordings") {
		t.Fatalf("stdout = %q", out.String())
	}
}

// startMockCameraServingCatalogue does the handshake, reads the code-400 request,
// then replies with the 403 fileSize announcement followed by the catalogue XML
// as a type-8 chunk.
func startMockCameraServingCatalogue(t *testing.T, devID, xml string) string {
	t.Helper()
	return serveMockCamera(t, func(conn net.Conn) {
		if !mockHandshake(conn, devID, 200) {
			return
		}
		if _, err := readStreamFrame(conn); err != nil { // the code-400 request
			return
		}
		_ = writeStreamFrame(conn, jsonControl(map[string]any{
			"code": 403, "status": 200, "date": 20260805, "fileSize": len(xml),
		}))
		_ = writeStreamFrame(conn, domain.Frame{Type: domain.FrameTypeSDCardXMLChunk, Payload: []byte(xml)})
		drain(conn)
	})
}

func startMockCameraServingEmptyCatalogue(t *testing.T, devID string) string {
	t.Helper()
	return serveMockCamera(t, func(conn net.Conn) {
		if !mockHandshake(conn, devID, 200) {
			return
		}
		if _, err := readStreamFrame(conn); err != nil {
			return
		}
		_ = writeStreamFrame(conn, jsonControl(map[string]any{
			"code": 403, "status": 200, "date": 20260805, "fileSize": 0,
		}))
		drain(conn)
	})
}
