package auth_middleware

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lucasgiovannibr/whatygo/pkg/config"
	instance_model "github.com/lucasgiovannibr/whatygo/pkg/instance/model"
	instance_service "github.com/lucasgiovannibr/whatygo/pkg/instance/service"
	"github.com/gin-gonic/gin"
)

// fakeInstances only implements the lookup the middleware needs.
type fakeInstances struct {
	instance_service.InstanceService
	byToken map[string]*instance_model.Instance
}

func (f fakeInstances) GetInstanceByToken(token string) (*instance_model.Instance, error) {
	if i, ok := f.byToken[token]; ok {
		return i, nil
	}
	return nil, errors.New("not found")
}

func run(t *testing.T, apikey string) int {
	t.Helper()
	gin.SetMode(gin.TestMode)
	m := NewMiddleware(&config.Config{GlobalApiKey: "global"}, fakeInstances{byToken: map[string]*instance_model.Instance{
		"tokA": {Id: "A"},
		"tokB": {Id: "B"},
	}})
	r := gin.New()
	r.GET("/instance/:instanceId/advanced-settings", m.AuthInstanceScoped, func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/instance/A/advanced-settings", nil)
	if apikey != "" {
		req.Header.Set("apikey", apikey)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code
}

func TestAuthInstanceScoped(t *testing.T) {
	cases := []struct {
		name   string
		apikey string
		want   int
	}{
		{"no key", "", http.StatusUnauthorized},
		{"unknown key", "nope", http.StatusUnauthorized},
		{"global key", "global", http.StatusOK},
		{"own instance token", "tokA", http.StatusOK},
		{"other instance token", "tokB", http.StatusForbidden},
	}
	for _, c := range cases {
		if got := run(t, c.apikey); got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
}

// A refused request carries the same body as every other failure: the text and a stable code.
func TestAuthFailuresHaveACode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	m := NewMiddleware(&config.Config{GlobalApiKey: "global"}, fakeInstances{byToken: map[string]*instance_model.Instance{"tokA": {Id: "A"}}})
	r := gin.New()
	r.GET("/admin", m.AuthAdmin, func(c *gin.Context) { c.Status(http.StatusOK) })
	r.GET("/instance/:instanceId/x", m.AuthInstanceScoped, func(c *gin.Context) { c.Status(http.StatusOK) })

	cases := []struct {
		path, key string
		status    int
		code      string
	}{
		{"/admin", "", 401, "unauthorized"},
		{"/admin", "wrong", 401, "unauthorized"},
		{"/instance/A/x", "unknown", 401, "unauthorized"},
		{"/instance/B/x", "tokA", 403, "forbidden"}, // a valid token, another instance
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodGet, c.path, nil)
		if c.key != "" {
			req.Header.Set("apikey", c.key)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		var b struct{ Error, Code string }
		_ = json.Unmarshal(w.Body.Bytes(), &b)
		if w.Code != c.status || b.Code != c.code || b.Error == "" {
			t.Errorf("%s key=%q: %d %s", c.path, c.key, w.Code, w.Body.String())
		}
	}
}
