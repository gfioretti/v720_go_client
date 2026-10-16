package application

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/gfioretti/v720_go_client/internal/domain"
)

const catalogueXML = `<video>` +
	`<catalogue date="20260805" hNumb="2"><option hours="14" minBin="6"/><option hours="9" minBin="1"/></catalogue>` +
	`<catalogue date="20260804" hNumb="1"><option hours="0" minBin="0"/></catalogue>` +
	`</video>`

func fileSizeFrame(t *testing.T, status, fileSize int) domain.Frame {
	t.Helper()
	b, err := json.Marshal(map[string]any{"code": 403, "status": status, "date": 20260805, "fileSize": fileSize})
	if err != nil {
		t.Fatalf("marshal 403: %v", err)
	}
	return domain.Frame{Type: domain.FrameTypeJSON, Payload: b}
}

func xmlChunk(b []byte) domain.Frame {
	return domain.Frame{Type: domain.FrameTypeSDCardXMLChunk, Payload: b}
}

func TestListRecordings_ReassemblesChunksAndParses(t *testing.T) {
	ft := newFakeTransport()
	s := NewAPSession(ft, testDevID, testPassword)

	xml := []byte(catalogueXML)
	// 403 announces the total size; the XML then arrives split across two type-8
	// chunks that must be reassembled before parsing.
	ft.push(fileSizeFrame(t, 200, len(xml)))
	mid := len(xml) / 2
	ft.push(xmlChunk(xml[:mid]))
	ft.push(xmlChunk(xml[mid:]))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got, err := s.ListRecordings(ctx)
	if err != nil {
		t.Fatalf("ListRecordings error: %v", err)
	}

	want, err := domain.ParseCatalogue(xml)
	if err != nil {
		t.Fatalf("parse expected: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("catalogue = %#v, want %#v", got, want)
	}

	// It sent exactly the code-400 date-list request.
	sent := ft.sentFrames()
	if len(sent) != 1 {
		t.Fatalf("sent %d frames, want 1", len(sent))
	}
	wantReq, _ := domain.NewSDCardDateList(testDevID).JSON()
	if string(sent[0].Payload) != string(wantReq) {
		t.Fatalf("request = %s, want %s", sent[0].Payload, wantReq)
	}
}

func TestListRecordings_SkipsUnrelatedFramesBeforeResponse(t *testing.T) {
	ft := newFakeTransport()
	s := NewAPSession(ft, testDevID, testPassword)

	xml := []byte(catalogueXML)
	// Noise the loop must ignore: a video frame and an unrelated control message.
	ft.push(domain.Frame{Type: domain.FrameTypeMJPEG, Payload: []byte("jpeg")})
	ft.push(domain.Frame{Type: domain.FrameTypeJSON, Payload: []byte(`{"code":53,"status":1}`)})
	ft.push(fileSizeFrame(t, 200, len(xml)))
	ft.push(xmlChunk(xml))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got, err := s.ListRecordings(ctx)
	if err != nil {
		t.Fatalf("ListRecordings error: %v", err)
	}
	if len(got.Days) != 2 {
		t.Fatalf("Days = %d, want 2", len(got.Days))
	}
}

func TestListRecordings_Rejected(t *testing.T) {
	ft := newFakeTransport()
	s := NewAPSession(ft, testDevID, testPassword)
	ft.push(fileSizeFrame(t, 500, 0))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := s.ListRecordings(ctx)
	if !errors.Is(err, ErrSDCardListRejected) {
		t.Fatalf("ListRecordings = %v, want ErrSDCardListRejected", err)
	}
}

func TestListRecordings_EmptyCard(t *testing.T) {
	ft := newFakeTransport()
	s := NewAPSession(ft, testDevID, testPassword)
	// fileSize 0 → no XML, no recordings.
	ft.push(fileSizeFrame(t, 200, 0))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got, err := s.ListRecordings(ctx)
	if err != nil {
		t.Fatalf("ListRecordings error: %v", err)
	}
	if len(got.Days) != 0 {
		t.Fatalf("Days = %#v, want empty", got.Days)
	}
}

func TestListRecordings_OversizeAnnouncementRejected(t *testing.T) {
	ft := newFakeTransport()
	s := NewAPSession(ft, testDevID, testPassword)
	ft.push(fileSizeFrame(t, 200, maxCatalogueSize+1))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := s.ListRecordings(ctx); err == nil {
		t.Fatal("ListRecordings accepted an oversize announcement")
	}
}

func TestListRecordings_ChunkOvershootRejected(t *testing.T) {
	ft := newFakeTransport()
	s := NewAPSession(ft, testDevID, testPassword)
	// Announce 4 bytes but deliver 6.
	ft.push(fileSizeFrame(t, 200, 4))
	ft.push(xmlChunk([]byte("abcdef")))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := s.ListRecordings(ctx); err == nil {
		t.Fatal("ListRecordings accepted a chunk overshoot")
	}
}

func TestListRecordings_MalformedXMLAfterReassembly(t *testing.T) {
	ft := newFakeTransport()
	s := NewAPSession(ft, testDevID, testPassword)
	garbage := []byte("<video><not-closed")
	ft.push(fileSizeFrame(t, 200, len(garbage)))
	ft.push(xmlChunk(garbage))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := s.ListRecordings(ctx); err == nil {
		t.Fatal("ListRecordings accepted malformed catalogue XML")
	}
}

// A type-8 chunk that arrives before the 403 announcement is dropped (not
// buffered), so the subsequent full delivery still parses cleanly.
func TestListRecordings_IgnoresChunkBeforeAnnouncement(t *testing.T) {
	ft := newFakeTransport()
	s := NewAPSession(ft, testDevID, testPassword)
	xml := []byte(catalogueXML)

	ft.push(xmlChunk([]byte("<stray/>"))) // out-of-spec early chunk, must be dropped
	ft.push(fileSizeFrame(t, 200, len(xml)))
	ft.push(xmlChunk(xml))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got, err := s.ListRecordings(ctx)
	if err != nil {
		t.Fatalf("ListRecordings error: %v", err)
	}
	if len(got.Days) != 2 {
		t.Fatalf("Days = %d, want 2", len(got.Days))
	}
}

func TestListRecordings_TransportClosedBeforeRequest(t *testing.T) {
	ft := newFakeTransport()
	s := NewAPSession(ft, testDevID, testPassword)
	_ = ft.Close()

	_, err := s.ListRecordings(context.Background())
	if !errors.Is(err, ErrTransportClosed) {
		t.Fatalf("ListRecordings = %v, want ErrTransportClosed", err)
	}
}

// The receive-loop error path: connection drops mid-stream (after the 403 and a
// partial chunk).
func TestListRecordings_TransportClosedMidStream(t *testing.T) {
	ft := newFakeTransport()
	s := NewAPSession(ft, testDevID, testPassword)
	ft.push(fileSizeFrame(t, 200, 100)) // announce more than we'll deliver
	ft.push(xmlChunk([]byte("<video>")))

	errCh := make(chan error, 1)
	go func() {
		_, err := s.ListRecordings(context.Background())
		errCh <- err
	}()
	time.Sleep(10 * time.Millisecond)
	_ = ft.Close()

	select {
	case err := <-errCh:
		if !errors.Is(err, ErrTransportClosed) {
			t.Fatalf("ListRecordings = %v, want ErrTransportClosed", err)
		}
	case <-time.After(time.Second):
		t.Fatal("ListRecordings did not return after transport close")
	}
}

// An incomplete stream must surface the context deadline rather than hang.
func TestListRecordings_ContextTimeoutOnIncompleteStream(t *testing.T) {
	ft := newFakeTransport()
	s := NewAPSession(ft, testDevID, testPassword)
	ft.push(fileSizeFrame(t, 200, 100))  // announced 100 bytes...
	ft.push(xmlChunk([]byte("<video>"))) // ...but only a few ever arrive

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := s.ListRecordings(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("ListRecordings = %v, want context.DeadlineExceeded", err)
	}
}
