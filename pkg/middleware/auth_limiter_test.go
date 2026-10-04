package auth_middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/lucasgiovannibr/whatygo/pkg/config"
	instance_model "github.com/lucasgiovannibr/whatygo/pkg/instance/model"
)

func TestFailLimiterBlocksAfterTheLimitAndForgetsAfterTheWindow(t *testing.T) {
	now := time.Unix(1000, 0)
	l := newFailLimiter(3)
	l.now = func() time.Time { return now }

	for i := 0; i < 3; i++ {
		if _, blocked := l.blocked("1.1.1.1"); blocked {
			t.Fatalf("blocked after only %d failures", i)
		}
		l.fail("1.1.1.1")
	}
	retry, blocked := l.blocked("1.1.1.1")
	if !blocked || retry != "60" {
		t.Fatalf("want blocked for 60 s, got %q %v", retry, blocked)
	}
	if _, blocked := l.blocked("2.2.2.2"); blocked {
		t.Fatal("another address must not be affected")
	}

	now = now.Add(45 * time.Second)
	if retry, blocked := l.blocked("1.1.1.1"); !blocked || retry != "15" {
		t.Fatalf("15 s left, got %q %v", retry, blocked)
	}
	now = now.Add(15 * time.Second)
	if _, blocked := l.blocked("1.1.1.1"); blocked {
		t.Fatal("the window is over")
	}
	l.fail("1.1.1.1")
	if _, blocked := l.blocked("1.1.1.1"); blocked {
		t.Fatal("a new window starts at zero")
	}
}

func TestFailLimiterDisabledAndBounded(t *testing.T) {
	if newFailLimiter(0) != nil || newFailLimiter(-1) != nil {
		t.Fatal("a non-positive limit means no limiter")
	}
	var l *failLimiter // nil is safe to use
	l.fail("x")
	if _, blocked := l.blocked("x"); blocked {
		t.Fatal("no limiter blocks nobody")
	}

	b := newFailLimiter(1)
	for i := 0; i < maxTrackedAddresses+10; i++ {
		b.fail("addr-" + strconv.Itoa(i))
	}
	if len(b.entries) > maxTrackedAddresses {
		t.Fatalf("the table must stay bounded, has %d", len(b.entries))
	}
}

func limited(max int) *gin.Engine {
	gin.SetMode(gin.TestMode)
	m := NewMiddleware(&config.Config{GlobalApiKey: "global", AuthFailLimit: max}, fakeInstances{byToken: map[string]*instance_model.Instance{
		"tokA": {Id: "A"},
		"tokB": {Id: "B"},
	}})
	r := gin.New()
	r.GET("/admin", m.AuthAdmin, func(c *gin.Context) { c.Status(http.StatusOK) })
	r.GET("/send", m.Auth, func(c *gin.Context) { c.Status(http.StatusOK) })
	r.GET("/instance/:instanceId/x", m.AuthInstanceScoped, func(c *gin.Context) { c.Status(http.StatusOK) })
	r.GET("/open", func(c *gin.Context) { c.Status(http.StatusOK) })
	r.GET("/ws", func(c *gin.Context) {
		if !m.AdminTokenValid(c, c.Query("token")) {
			if !c.IsAborted() {
				c.Status(http.StatusUnauthorized)
			}
			return
		}
		c.Status(http.StatusOK)
	})
	return r
}

func call(r *gin.Engine, path, key, addr string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if key != "" {
		req.Header.Set("apikey", key)
	}
	req.RemoteAddr = addr + ":5555"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestGuessingCredentialsIsRefusedWith429(t *testing.T) {
	r := limited(3)
	for i := 0; i < 3; i++ {
		if w := call(r, "/send", "guess", "9.9.9.9"); w.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: %d", i, w.Code)
		}
	}
	w := call(r, "/send", "guess", "9.9.9.9")
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") == "" {
		t.Fatalf("want 429 with Retry-After, got %d %v", w.Code, w.Header())
	}
	var body struct{ Code string }
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.Code != "rate_limited" {
		t.Fatalf("the refusal carries the usual code: %s", w.Body.String())
	}

	// even the right credential gets no answer from an address that used up its failures, on
	// every kind of route
	for path, key := range map[string]string{"/send": "tokA", "/admin": "global", "/instance/A/x": "tokA"} {
		if w := call(r, path, key, "9.9.9.9"); w.Code != http.StatusTooManyRequests {
			t.Fatalf("%s from a blocked address: %d", path, w.Code)
		}
	}
	// other addresses and routes without authentication are not touched
	if w := call(r, "/send", "tokA", "8.8.8.8"); w.Code != http.StatusOK {
		t.Fatalf("another address: %d", w.Code)
	}
	if w := call(r, "/open", "", "9.9.9.9"); w.Code != http.StatusOK {
		t.Fatalf("a public route: %d", w.Code)
	}
}

func TestOnlyFailuresAreCounted(t *testing.T) {
	r := limited(2)
	for i := 0; i < 20; i++ {
		if w := call(r, "/send", "tokA", "7.7.7.7"); w.Code != http.StatusOK {
			t.Fatalf("a valid credential must never be limited, got %d at %d", w.Code, i)
		}
	}
	// a valid token of another instance is a refusal, not a guess
	for i := 0; i < 10; i++ {
		if w := call(r, "/instance/A/x", "tokB", "7.7.7.7"); w.Code != http.StatusForbidden {
			t.Fatalf("want 403, got %d at %d", w.Code, i)
		}
	}
	if w := call(r, "/send", "tokA", "7.7.7.7"); w.Code != http.StatusOK {
		t.Fatalf("403s must not count, got %d", w.Code)
	}
}

func TestWebsocketTokenUsesTheSameLimit(t *testing.T) {
	r := limited(2)
	for i := 0; i < 2; i++ {
		if w := call(r, "/ws?token=nope", "", "6.6.6.6"); w.Code != http.StatusUnauthorized {
			t.Fatalf("got %d", w.Code)
		}
	}
	if w := call(r, "/ws?token=global", "", "6.6.6.6"); w.Code != http.StatusTooManyRequests {
		t.Fatalf("the websocket must be limited as well, got %d", w.Code)
	}
	if w := call(r, "/ws?token=global", "", "5.5.5.5"); w.Code != http.StatusOK {
		t.Fatalf("got %d", w.Code)
	}
}

func TestLimitOffMeansNoLimit(t *testing.T) {
	r := limited(0)
	for i := 0; i < 200; i++ {
		if w := call(r, "/send", "guess", "4.4.4.4"); w.Code != http.StatusUnauthorized {
			t.Fatalf("got %d at %d", w.Code, i)
		}
	}
}

// gin used to believe X-Forwarded-For from anyone; with no trusted proxy the client is the
// connection's own address, and with one listed only that proxy is believed.
func TestClientAddressIgnoresForwardedHeadersUnlessTheProxyIsTrusted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ip := func(trusted []string, remote string) string {
		r := gin.New()
		if err := r.SetTrustedProxies(trusted); err != nil {
			t.Fatal(err)
		}
		var got string
		r.GET("/", func(c *gin.Context) { got = c.ClientIP() })
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = remote + ":1000"
		req.Header.Set("X-Forwarded-For", "203.0.113.77")
		r.ServeHTTP(httptest.NewRecorder(), req)
		return got
	}
	if got := ip(nil, "198.51.100.1"); got != "198.51.100.1" {
		t.Fatalf("no trusted proxy: a forged header must be ignored, got %s", got)
	}
	if got := ip([]string{"198.51.100.0/24"}, "198.51.100.1"); got != "203.0.113.77" {
		t.Fatalf("a listed proxy is believed, got %s", got)
	}
	if got := ip([]string{"198.51.100.0/24"}, "192.0.2.9"); got != "192.0.2.9" {
		t.Fatalf("a sender that is not listed is not, got %s", got)
	}
}
