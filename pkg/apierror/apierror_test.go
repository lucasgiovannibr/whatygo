package apierror

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"go.mau.fi/whatsmeow"
	"gorm.io/gorm"

	"github.com/lucasgiovannibr/whatygo/pkg/utils"
)

type throttled struct{}

func (throttled) Error() string          { return "slow down" }
func (throttled) RetryAfterSeconds() int { return 7 }

func do(err error) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	Respond(c, err)
	return w
}

func TestClassify(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"no session", utils.ErrNoActiveSession, 503, "instance_not_connected"},
		{"disconnected", utils.ErrClientDisconnected, 503, "instance_disconnected"},
		{"not logged in", utils.ErrNotLoggedIn, 409, "instance_not_logged_in"},
		{"disconnected by user", utils.ErrDisconnectedByUser, 409, "instance_disconnected_by_user"},
		{"other replica", fmt.Errorf("%w: abc", utils.ErrOwnedElsewhere), 409, "instance_on_another_replica"},
		{"media url unusable", fmt.Errorf("%w: failed to fetch x: HTTP status 403", utils.ErrDownloadFailed), 400, "invalid_media_url"},
		{"media too large", fmt.Errorf("%w: 9 bytes (limit 1)", utils.ErrDownloadTooLarge), 413, "payload_too_large"},
		{"whatsmeow not connected", whatsmeow.ErrNotConnected, 503, "instance_not_connected"},
		{"whatsmeow not logged in", whatsmeow.ErrNotLoggedIn, 409, "instance_not_logged_in"},
		{"wrapped rate limit", fmt.Errorf("sending: %w", whatsmeow.ErrIQRateOverLimit), 429, "whatsapp_rate_limited"},
		{"forbidden", whatsmeow.ErrIQForbidden, 403, "whatsapp_forbidden"},
		{"iq timeout", whatsmeow.ErrIQTimedOut, 504, "whatsapp_timeout"},
		{"group missing", whatsmeow.ErrGroupNotFound, 404, "not_found"},
		{"deadline", context.DeadlineExceeded, 504, "timeout"},
		{"db miss", gorm.ErrRecordNotFound, 404, "not_found"},
		{"other iq rejection", &whatsmeow.IQError{Code: 406, Text: "not-acceptable"}, 502, "whatsapp_rejected"},
		{"throttle", throttled{}, 429, "rate_limited"},
		{"invalid", Invalid("phone is required"), 400, "invalid_request"},
		{"wrapped not found", fmt.Errorf("x: %w", NotFound("no such label")), 404, "not_found"},
		{"unknown", errors.New("boom"), 500, "internal_error"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			status, code := Classify(c.err)
			if status != c.status || code != c.code {
				t.Fatalf("got %d %s, want %d %s", status, code, c.status, c.code)
			}
		})
	}
}

func TestRespondBodyKeepsTheMessageAndAddsACode(t *testing.T) {
	w := do(fmt.Errorf("sending: %w", utils.ErrNoActiveSession))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d", w.Code)
	}
	var b Body
	if err := json.Unmarshal(w.Body.Bytes(), &b); err != nil {
		t.Fatal(err)
	}
	if b.Error != "sending: no active session found" || b.Code != "instance_not_connected" {
		t.Fatalf("%+v", b)
	}
}

func TestRespondRetryAfter(t *testing.T) {
	if got := do(throttled{}).Header().Get("Retry-After"); got != "7" {
		t.Fatalf("Retry-After %q", got)
	}
	if got := do(whatsmeow.ErrIQRateOverLimit).Header().Get("Retry-After"); got == "" {
		t.Fatal("a rate limit answer needs a Retry-After")
	}
	if got := do(errors.New("boom")).Header().Get("Retry-After"); got != "" {
		t.Fatalf("Retry-After on a 500: %q", got)
	}
}

func TestBadRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	BadRequest(c, errors.New("bad json"))
	if w.Code != 400 || w.Body.String() != `{"code":"invalid_request","error":"bad json"}` && w.Body.String() != `{"error":"bad json","code":"invalid_request"}` {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}

func TestFailAndAbortDeriveTheCodeFromTheStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for status, code := range map[int]string{401: "unauthorized", 403: "forbidden", 404: "not_found", 409: "conflict", 413: "payload_too_large", 429: "rate_limited", 500: "internal_error", 418: "error"} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		Fail(c, status, "msg")
		var b Body
		_ = json.Unmarshal(w.Body.Bytes(), &b)
		if w.Code != status || b.Code != code || b.Error != "msg" {
			t.Errorf("%d: %d %+v", status, w.Code, b)
		}
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	Abort(c, 401, "nope")
	if !c.IsAborted() || w.Code != 401 {
		t.Fatal("Abort must stop the request")
	}
}

func TestConflict(t *testing.T) {
	if s, c := Classify(Conflict("already there")); s != 409 || c != "conflict" {
		t.Fatalf("%d %s", s, c)
	}
}
