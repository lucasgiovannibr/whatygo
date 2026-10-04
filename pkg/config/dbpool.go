package config

import (
	"database/sql"
	"os"
	"strconv"
	"strings"
	"time"

	config_env "github.com/lucasgiovannibr/whatygo/pkg/config/env"
)

// DBPool is the size of a database connection pool.
type DBPool struct {
	MaxOpen     int
	MaxIdle     int
	MaxLifetime time.Duration
	MaxIdleTime time.Duration
}

// DBPoolFromEnv reads DB_MAX_OPEN_CONNS (default 25), DB_MAX_IDLE_CONNS (default 10, never
// above the open limit), DB_CONN_MAX_LIFETIME_MIN (5) and DB_CONN_MAX_IDLE_MIN (1). The
// numbers used to be fixed in three places; an installation with many instances needs more
// connections, one on a small Postgres needs fewer.
func DBPoolFromEnv() DBPool {
	p := DBPool{
		MaxOpen:     envInt(config_env.DB_MAX_OPEN_CONNS, 25, 1),
		MaxIdle:     envInt(config_env.DB_MAX_IDLE_CONNS, 10, 0),
		MaxLifetime: time.Duration(envInt(config_env.DB_CONN_MAX_LIFETIME_MIN, 5, 1)) * time.Minute,
		MaxIdleTime: time.Duration(envInt(config_env.DB_CONN_MAX_IDLE_MIN, 1, 1)) * time.Minute,
	}
	if p.MaxIdle > p.MaxOpen {
		p.MaxIdle = p.MaxOpen
	}
	return p
}

// Apply configures db with the pool.
func (p DBPool) Apply(db *sql.DB) {
	db.SetMaxOpenConns(p.MaxOpen)
	db.SetMaxIdleConns(p.MaxIdle)
	db.SetConnMaxLifetime(p.MaxLifetime)
	db.SetConnMaxIdleTime(p.MaxIdleTime)
}

// ApplyDBPool is DBPoolFromEnv().Apply for callers that are not in this package.
func (c *Config) ApplyDBPool(db *sql.DB) { DBPoolFromEnv().Apply(db) }

func envInt(name string, def, min int) int {
	v, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name)))
	if err != nil || v < min {
		return def
	}
	return v
}
