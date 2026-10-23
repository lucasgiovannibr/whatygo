package server_handler

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"

	"github.com/lucasgiovannibr/whatygo/pkg/config"
	auth_middleware "github.com/lucasgiovannibr/whatygo/pkg/middleware"
)

func serve(t *testing.T, h ServerHandler) (int, map[string]interface{}) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/health", h.Health)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health", nil))
	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("bad json: %v (%s)", err, w.Body.String())
	}
	return w.Code, body
}

func mockDB(t *testing.T, delay time.Duration, fail bool) *sql.DB {
	t.Helper()
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	e := mock.ExpectPing()
	if fail {
		e.WillReturnError(sql.ErrConnDone)
	} else if delay > 0 {
		e.WillDelayFor(delay)
	}
	return db
}

func TestHealthOk(t *testing.T) {
	code, body := serve(t, NewServerHandler(HealthCheck{Name: "usersDb", DB: mockDB(t, 0, false)}))
	if code != http.StatusOK || body["status"] != "ok" {
		t.Fatalf("got %d %v", code, body)
	}
	if body["checks"].(map[string]interface{})["usersDb"] != "ok" {
		t.Fatalf("checks: %v", body["checks"])
	}
}

func TestHealthUnavailableWhenAPingFails(t *testing.T) {
	code, body := serve(t, NewServerHandler(
		HealthCheck{Name: "usersDb", DB: mockDB(t, 0, false)},
		HealthCheck{Name: "authDb", DB: mockDB(t, 0, true)},
	))
	if code != http.StatusServiceUnavailable || body["status"] != "unavailable" {
		t.Fatalf("got %d %v", code, body)
	}
	checks := body["checks"].(map[string]interface{})
	if checks["usersDb"] != "ok" || checks["authDb"] != "error" {
		t.Fatalf("checks: %v", checks)
	}
}

func TestHealthDegradedWhenSlow(t *testing.T) {
	code, body := serve(t, NewServerHandler(HealthCheck{Name: "authDb", DB: mockDB(t, healthSlowThreshold+200*time.Millisecond, false)}))
	if code != http.StatusOK || body["status"] != "degraded" {
		t.Fatalf("got %d %v", code, body)
	}
}

func TestHealthSkipsMissingDatabases(t *testing.T) {
	// e.g. no Postgres auth DB in SQLite mode: must not turn into a permanent 503
	code, body := serve(t, NewServerHandler(
		HealthCheck{Name: "usersDb", DB: mockDB(t, 0, false)},
		HealthCheck{Name: "authDb", DB: nil},
	))
	if code != http.StatusOK || body["status"] != "ok" {
		t.Fatalf("got %d %v", code, body)
	}
	if _, present := body["checks"].(map[string]interface{})["authDb"]; present {
		t.Fatal("a missing database must not be reported")
	}
}

func TestHealthMetricsRequiresAdminKeyAndReturnsShape(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewServerHandlerWithMetrics(nil, nil)
	auth := auth_middleware.NewMiddleware(&config.Config{GlobalApiKey: "secret", AuthFailLimit: 30}, nil)
	r := gin.New()
	r.GET("/health/metrics", auth.AuthAdmin, h.HealthMetrics)

	withoutKey := httptest.NewRecorder()
	r.ServeHTTP(withoutKey, httptest.NewRequest(http.MethodGet, "/health/metrics", nil))
	if withoutKey.Code != http.StatusUnauthorized {
		t.Fatalf("without admin key: got %d, want 401", withoutKey.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/health/metrics", nil)
	req.Header.Set("apikey", "secret")
	withKey := httptest.NewRecorder()
	r.ServeHTTP(withKey, req)
	if withKey.Code != http.StatusOK {
		t.Fatalf("with admin key: got %d, body=%s", withKey.Code, withKey.Body.String())
	}
	var body map[string]interface{}
	if err := json.Unmarshal(withKey.Body.Bytes(), &body); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	if _, ok := body["instances"].(map[string]interface{}); !ok {
		t.Fatalf("missing instances object: %v", body)
	}
	webhooks, ok := body["webhooks"].(map[string]interface{})
	if !ok {
		t.Fatalf("missing webhooks object: %v", body)
	}
	for _, key := range []string{"pending", "pendingBytes", "inFlight", "destinations", "degraded", "sent", "failed"} {
		if _, ok := webhooks[key]; !ok {
			t.Fatalf("missing webhooks.%s: %v", key, webhooks)
		}
	}
}
