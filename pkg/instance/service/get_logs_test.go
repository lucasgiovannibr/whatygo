package instance_service

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lucasgiovannibr/whatygo/pkg/config"
)

func writeInstanceLog(t *testing.T, dir, instanceID string, n int, day time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, instanceID), 0o755); err != nil {
		t.Fatal(err)
	}
	var out []byte
	for i := 0; i < n; i++ {
		ts := day.Add(time.Duration(i) * time.Second).UTC().Format(time.RFC3339)
		out = append(out, []byte(fmt.Sprintf(`{"timestamp":%q,"level":"INFO","instance_id":%q,"message":"line %d"}`+"\n", ts, instanceID, i))...)
	}
	if err := os.WriteFile(filepath.Join(dir, instanceID, "instance.log"), out, 0o644); err != nil {
		t.Fatal(err)
	}
}

// Reproduced on the test stack: limit=2 returned the two OLDEST entries of the range.
func TestGetLogsReturnsTheMostRecentEntries(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC().Truncate(time.Second)
	writeInstanceLog(t, dir, "inst", 10, now)

	svc := &instances{config: &config.Config{LogDirectory: dir}}
	logs, err := svc.GetLogs("inst", time.Time{}, time.Time{}, "", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 3 {
		t.Fatalf("got %d entries, want 3", len(logs))
	}
	// newest first
	for i, want := range []string{"line 9", "line 8", "line 7"} {
		if logs[i].Message != want {
			t.Fatalf("entry %d = %q, want %q (all: %+v)", i, logs[i].Message, want, logs)
		}
	}
}

func TestGetLogsWithFewerEntriesThanTheLimit(t *testing.T) {
	dir := t.TempDir()
	writeInstanceLog(t, dir, "inst", 4, time.Now().UTC())

	svc := &instances{config: &config.Config{LogDirectory: dir}}
	logs, err := svc.GetLogs("inst", time.Time{}, time.Time{}, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 4 || logs[0].Message != "line 3" {
		t.Fatalf("got %+v", logs)
	}
}

func TestGetLogsWindowKeepsTheLastEntriesOfALargeFile(t *testing.T) {
	dir := t.TempDir()
	writeInstanceLog(t, dir, "inst", 1000, time.Now().UTC().Add(-20*time.Minute))

	svc := &instances{config: &config.Config{LogDirectory: dir}}
	logs, err := svc.GetLogs("inst", time.Time{}, time.Time{}, "", 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 7 || logs[0].Message != "line 999" || logs[6].Message != "line 993" {
		t.Fatalf("got %d entries, first %q last %q", len(logs), logs[0].Message, logs[len(logs)-1].Message)
	}
}
