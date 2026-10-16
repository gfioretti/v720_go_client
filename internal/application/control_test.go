package application

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/gfioretti/v720_go_client/internal/domain"
)

func TestSendCommand_WrapsInAPEnvelopeWithDevTarget(t *testing.T) {
	ft := newFakeTransport()
	s := NewAPSession(ft, testDevID, testPassword)

	if err := s.SendCommand(context.Background(), domain.NewVideoQuality(2)); err != nil {
		t.Fatalf("SendCommand error: %v", err)
	}

	sent := ft.sentFrames()
	if len(sent) != 1 {
		t.Fatalf("sent %d frames, want 1", len(sent))
	}
	if sent[0].Type != domain.FrameTypeJSON {
		t.Fatalf("frame type = %s, want JSON", sent[0].Type)
	}

	// Exactly the AP-mode wire form, with the session's devId injected as devTarget.
	want, err := domain.NewAPDeviceCommand(testDevID, domain.NewVideoQuality(2)).JSON()
	if err != nil {
		t.Fatalf("build expected: %v", err)
	}
	if !bytes.Equal(sent[0].Payload, want) {
		t.Fatalf("payload = %s, want %s", sent[0].Payload, want)
	}
}

func TestSendCommand_TransportClosed(t *testing.T) {
	ft := newFakeTransport()
	s := NewAPSession(ft, testDevID, testPassword)
	_ = ft.Close()

	err := s.SendCommand(context.Background(), domain.NewIRLed(1))
	if !errors.Is(err, ErrTransportClosed) {
		t.Fatalf("SendCommand = %v, want ErrTransportClosed", err)
	}
}
