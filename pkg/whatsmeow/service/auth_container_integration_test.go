package whatsmeow_service

import (
	"database/sql"
	"os"
	"testing"

	_ "github.com/lib/pq"
	"github.com/lucasgiovannibr/whatygo/pkg/config"
)

// Integration test, skipped unless WHATYGO_TEST_POSTGRES_DSN points at an empty
// database. It proves the fix for the connection-pool leak (#106 #109 #112 #118
// #165 #175 #186): every StartClient used to open a brand new pool; now the
// container is created once on top of the existing authDB pool.
//
//	WHATYGO_TEST_POSTGRES_DSN='postgresql://postgres:root@localhost:5432/whatygo_auth?sslmode=disable' go test ./pkg/whatsmeow/service -run AuthContainer
func TestAuthContainerReusesPoolOnPostgres(t *testing.T) {
	dsn := os.Getenv("WHATYGO_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("WHATYGO_TEST_POSTGRES_DSN not set")
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)

	sharedAuthContainerMu.Lock()
	sharedAuthContainer = nil
	sharedAuthContainerMu.Unlock()

	w := whatsmeowService{config: &config.Config{PostgresAuthDB: dsn}, authDB: db}

	first, err := w.getAuthContainer()
	if err != nil {
		t.Fatalf("first call: %v", err)
	}

	var before int
	if err := db.QueryRow("SELECT count(*) FROM pg_stat_activity WHERE datname = current_database()").Scan(&before); err != nil {
		t.Fatal(err)
	}

	// 50 simulated (re)connects.
	for i := 0; i < 50; i++ {
		c, err := w.getAuthContainer()
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		if c != first {
			t.Fatalf("call %d returned a different container", i)
		}
	}

	var after int
	if err := db.QueryRow("SELECT count(*) FROM pg_stat_activity WHERE datname = current_database()").Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after > before {
		t.Fatalf("connections grew from %d to %d across 50 reconnects", before, after)
	}
	if db.Stats().OpenConnections > 5 {
		t.Fatalf("pool holds %d connections, expected the bounded pool", db.Stats().OpenConnections)
	}
}
