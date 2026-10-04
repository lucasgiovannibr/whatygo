package send_service

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"go.mau.fi/whatsmeow"

	"github.com/lucasgiovannibr/whatygo/pkg/config"
	logger_wrapper "github.com/lucasgiovannibr/whatygo/pkg/logger"
)

func TestDisconnectionErrorsAreMatchedByIdentityNotText(t *testing.T) {
	for _, err := range []error{
		ErrNoActiveSession,
		ErrClientDisconnected,
		fmt.Errorf("failed to connect client: %w", ErrClientDisconnected), // how SendMessage wraps it
		fmt.Errorf("a: %w", fmt.Errorf("b: %w", ErrNoActiveSession)),
	} {
		if !isDisconnectionError(err) {
			t.Errorf("%v must be a disconnection error", err)
		}
	}
	for _, err := range []error{
		nil,
		errors.New("client disconnected"), // same text, different error: not matched
		errors.New("number is not registered on WhatsApp"),
		fmt.Errorf("lost: %v", ErrClientDisconnected), // %v drops the chain
	} {
		if isDisconnectionError(err) {
			t.Errorf("%v must not be a disconnection error", err)
		}
	}
	// the text the API returns did not change
	if ErrNoActiveSession.Error() != "no active session found" || ErrClientDisconnected.Error() != "client disconnected" {
		t.Fatal("the error texts are part of the API")
	}
}

func TestSendDelayIsCapped(t *testing.T) {
	if d, capped := sendDelay(1500); d != 1500*time.Millisecond || capped {
		t.Fatalf("a normal delay is kept, got %v %v", d, capped)
	}
	if d, capped := sendDelay(int32(maxSendDelay.Milliseconds())); d != maxSendDelay || capped {
		t.Fatalf("the limit itself is allowed, got %v %v", d, capped)
	}
	if d, capped := sendDelay(2147483647); d != maxSendDelay || !capped {
		t.Fatalf("a ~24 day delay must be capped, got %v %v", d, capped)
	}
	if d, capped := sendDelay(-5); d > 0 || capped {
		t.Fatalf("a negative delay is no delay, got %v %v", d, capped)
	}
}

func pngBytes(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func prepService(t *testing.T) *sendService {
	cfg := &config.Config{LogDirectory: t.TempDir()}
	return &sendService{config: cfg, loggerWrapper: logger_wrapper.NewLoggerManagerForTest(t, cfg)}
}

func TestPrepareMediaFile(t *testing.T) {
	s := prepService(t)
	file := pngBytes(t)

	p, err := s.prepareMediaFile(&MediaStruct{Type: "image"}, file)
	if err != nil || p.uploadType != whatsmeow.MediaImage || p.mimeType != "image/png" || !bytes.Equal(p.fileData, file) {
		t.Fatalf("image: %+v %v", p, err)
	}
	if p, err := s.prepareMediaFile(&MediaStruct{Type: "document"}, file); err != nil || p.uploadType != whatsmeow.MediaDocument {
		t.Fatalf("document: %+v %v", p, err)
	}
	if _, err := s.prepareMediaFile(&MediaStruct{Type: "video"}, file); err == nil {
		t.Fatal("a PNG is not a video/mp4")
	}
	if _, err := s.prepareMediaFile(&MediaStruct{Type: "banana"}, file); err == nil {
		t.Fatal("an unknown type is refused")
	}
}

// The download of a retried send happens once: prepareMediaURL is what runs outside the loop.
func TestPrepareMediaURLDownloadsOnce(t *testing.T) {
	var hits atomic.Int32
	file := pngBytes(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Write(file)
	}))
	defer srv.Close()

	s := prepService(t)
	p, err := s.prepareMediaURL(&MediaStruct{Type: "image", Url: srv.URL + "/a.png"}, "inst", time.Now())
	if err != nil || p.uploadType != whatsmeow.MediaImage || !bytes.Equal(p.fileData, file) {
		t.Fatalf("prepare: %+v %v", p, err)
	}
	if hits.Load() != 1 {
		t.Fatalf("downloaded %d times, want 1", hits.Load())
	}

	if _, err := s.prepareMediaURL(&MediaStruct{Type: "video", Url: srv.URL + "/a.png"}, "inst", time.Now()); err == nil {
		t.Fatal("a PNG url is not a video")
	}
}
