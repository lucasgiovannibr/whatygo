package server_handler

import (
	"context"
	"database/sql"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type ServerHandler interface {
	ServerOk(ctx *gin.Context)
	Health(ctx *gin.Context)
}

// HealthCheck is one database the service depends on.
type HealthCheck struct {
	Name string
	DB   *sql.DB
}

type serverHandler struct {
	checks []HealthCheck
}

// healthPingTimeout bounds each database ping so the probe itself never hangs
// when a database does; healthSlowThreshold is the ping latency above which a
// database is reported as "slow".
const (
	healthPingTimeout   = 2 * time.Second
	healthSlowThreshold = 500 * time.Millisecond
)

// ServerOk is the liveness probe: the process is up and serving HTTP. It does not
// look at anything else, on purpose (an orchestrator must not restart the service
// just because a database is slow).
func (s *serverHandler) ServerOk(ctx *gin.Context) {
	ctx.JSON(200, gin.H{
		"status": "ok",
	})
}

// Health is the readiness probe. It answers what /server/ok cannot: can the
// service actually reach the databases it needs?
//
//   - each check is "ok", "slow" (the ping worked but took longer than 500ms, which
//     is what an exhausted or overloaded pool looks like: the ping waits for a free
//     connection) or "error" (the ping failed or timed out after 2s);
//   - any "error" => HTTP 503 and status "unavailable";
//   - "slow" => HTTP 200 and status "degraded", so a monitor can alert before the
//     connections run out (issue #175: the health check was blind exactly when the
//     Postgres connections were exhausted).
//
// It only reports states, no counts or identifiers, because it is public, like /server/ok. Per-instance details are in
// GET /instance/runtimes (global key).
func (s *serverHandler) Health(ctx *gin.Context) {
	// Checks run in parallel so the whole probe stays within one ping timeout even
	// when several databases are down.
	states := make([]string, len(s.checks))
	var wg sync.WaitGroup
	for idx, c := range s.checks {
		wg.Add(1)
		go func(idx int, c HealthCheck) {
			defer wg.Done()
			states[idx] = checkDB(ctx.Request.Context(), c.DB)
		}(idx, c)
	}
	wg.Wait()

	results := gin.H{}
	status := "ok"

	for idx, c := range s.checks {
		state := states[idx]
		results[c.Name] = state
		switch state {
		case "error":
			status = "unavailable"
		case "slow":
			if status == "ok" {
				status = "degraded"
			}
		}
	}

	code := http.StatusOK
	if status == "unavailable" {
		code = http.StatusServiceUnavailable
	}
	ctx.JSON(code, gin.H{"status": status, "checks": results})
}

func checkDB(parent context.Context, db *sql.DB) string {
	if db == nil {
		return "error"
	}

	ctx, cancel := context.WithTimeout(parent, healthPingTimeout)
	defer cancel()

	start := time.Now()
	if err := db.PingContext(ctx); err != nil {
		return "error"
	}
	if time.Since(start) > healthSlowThreshold {
		return "slow"
	}
	return "ok"
}

func NewServerHandler(checks ...HealthCheck) ServerHandler {
	// Drop checks without a database (e.g. the Postgres auth DB in SQLite mode).
	valid := make([]HealthCheck, 0, len(checks))
	for _, c := range checks {
		if c.DB != nil {
			valid = append(valid, c)
		}
	}
	return &serverHandler{checks: valid}
}
