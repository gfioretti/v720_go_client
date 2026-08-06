package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"v720_go_client/internal/domain"
)

// ErrSDCardListRejected means the device returned a non-200 status for the
// SD-card catalogue request (doc §12.6, the code-403 response).
var ErrSDCardListRejected = errors.New("application: SD-card catalogue request rejected by device")

// maxCatalogueSize bounds the announced XML size we will buffer. It matches the
// official app's fixed 1 MiB receive buffer (NaxclowApChannel), so a larger
// announcement is a protocol error rather than an unbounded allocation.
const maxCatalogueSize = 1 << 20

// ListRecordings requests the SD-card recording catalogue (code 400,
// reqSdcardDateList), reassembles the type-8 XML chunks the device streams back
// — whose total length the {code:403,status:200,fileSize} message announces
// first — and parses them into a domain.Catalogue. It must be called after a
// successful Connect.
//
// A fileSize of 0 (empty card) yields an empty Catalogue. Frames unrelated to
// this exchange (video, keepalive, other control messages) are ignored while it
// waits. ctx bounds the whole operation.
func (s *APSession) ListRecordings(ctx context.Context) (domain.Catalogue, error) {
	if err := s.sendControl(ctx, domain.NewSDCardDateList(s.devID)); err != nil {
		return domain.Catalogue{}, fmt.Errorf("sd-card list: request: %w", err)
	}

	var (
		fileSize     int
		haveFileSize bool
		buf          []byte
	)
	for {
		// Observe cancellation independently of Receive: a transport may deliver
		// a buffered frame in preference to reporting ctx (see Transport.Receive),
		// so a misbehaving peer flooding frames must not drown out ctx.
		if err := ctx.Err(); err != nil {
			return domain.Catalogue{}, fmt.Errorf("sd-card list: %w", err)
		}

		f, err := s.transport.Receive(ctx)
		if err != nil {
			return domain.Catalogue{}, fmt.Errorf("sd-card list: receive: %w", err)
		}

		switch f.Type {
		case domain.FrameTypeJSON:
			msg, ok := parseFileSizeMsg(f.Payload)
			if !ok || msg.Code != domain.CodeSDCardFileSize {
				continue // unrelated control message
			}
			if haveFileSize {
				continue // already announced; ignore a duplicate/late 403
			}
			if msg.Status != statusOK {
				return domain.Catalogue{}, fmt.Errorf("%w: status %d", ErrSDCardListRejected, msg.Status)
			}
			if msg.FileSize <= 0 {
				return domain.Catalogue{}, nil // empty card / no recordings
			}
			if msg.FileSize > maxCatalogueSize {
				return domain.Catalogue{}, fmt.Errorf("sd-card list: announced size %d exceeds limit %d", msg.FileSize, maxCatalogueSize)
			}
			fileSize, haveFileSize = msg.FileSize, true
			buf = make([]byte, 0, fileSize)

		case domain.FrameTypeSDCardXMLChunk:
			if !haveFileSize {
				// The spec guarantees the 403 announcement precedes the chunks
				// (doc §12.6); a chunk before it is out-of-spec noise. Dropping it
				// bounds buffering to fileSize rather than growing unbounded.
				continue
			}
			buf = append(buf, f.Payload...)
			if len(buf) >= fileSize {
				if len(buf) > fileSize {
					return domain.Catalogue{}, fmt.Errorf("sd-card list: received %d bytes, announced %d", len(buf), fileSize)
				}
				cat, err := domain.ParseCatalogue(buf)
				if err != nil {
					return domain.Catalogue{}, fmt.Errorf("sd-card list: %w", err)
				}
				return cat, nil
			}

		default:
			// Video/audio/keepalive/other frames during the exchange — ignore.
		}
	}
}

// sdCardFileSizeMsg is the subset of the code-403 response the catalogue reader
// inspects (doc §12.6).
type sdCardFileSizeMsg struct {
	Code     domain.Code `json:"code"`
	Status   int         `json:"status"`
	FileSize int         `json:"fileSize"`
}

func parseFileSizeMsg(payload []byte) (sdCardFileSizeMsg, bool) {
	var m sdCardFileSizeMsg
	if err := json.Unmarshal(payload, &m); err != nil {
		return sdCardFileSizeMsg{}, false
	}
	return m, true
}
