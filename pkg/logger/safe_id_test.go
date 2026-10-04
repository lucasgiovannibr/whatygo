package logger

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lucasgiovannibr/whatygo/pkg/config"
)

func TestSafeLoggerID(t *testing.T) {
	for _, ok := range []string{
		"6f1c1b2e-3c1d-4a51-9a4b-0d3a7d9f2b11", // an instance
		"system", "poll-service", "whatygo-test", "client_1", "a.b",
	} {
		if !safeLoggerID(ok) {
			t.Errorf("%q must be a valid log id", ok)
		}
	}
	long := make([]byte, 129)
	for i := range long {
		long[i] = 'a'
	}
	for _, bad := range []string{
		"", ".", "..", "../x", "a/b", `a\b`, "a b", "x\x00y", "café", string(long),
	} {
		if safeLoggerID(bad) {
			t.Errorf("%q must not name a log directory", bad)
		}
	}
}

// GET /instance/%2e%2e/advanced-settings reached GetLogger("..") and wrote instance.log one
// directory above the log directory.
func TestAnUnsafeIdNeverWritesOutsideTheLogDirectory(t *testing.T) {
	parent := t.TempDir()
	logs := filepath.Join(parent, "logs")
	lm := NewLoggerManagerForTest(t, &config.Config{LogDirectory: logs})

	for _, id := range []string{"..", "../../escape", "a/b", "."} {
		l := lm.GetLogger(id)
		l.LogInfo("hello from %q", id)
		l.Flush()
	}
	lm.GetLogger("system").LogInfo("a legitimate one")
	lm.Close()

	var found []string
	_ = filepath.Walk(parent, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			rel, _ := filepath.Rel(parent, path)
			found = append(found, rel)
		}
		return nil
	})
	if len(found) != 1 || found[0] != filepath.Join("logs", "system", "instance.log") {
		t.Fatalf("only the legitimate log may exist, found %v", found)
	}
}

// With no CLIENT_NAME the process's own messages (ConnectOnStartup) use the "system" log.
func TestEmptyIdIsTheSystemLog(t *testing.T) {
	dir := t.TempDir()
	lm := NewLoggerManagerForTest(t, &config.Config{LogDirectory: dir})
	lm.GetLogger("").LogInfo("boot")
	lm.Close()
	if _, err := os.Stat(filepath.Join(dir, "system", "instance.log")); err != nil {
		t.Fatalf("the system log must exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "instance.log")); err == nil {
		t.Fatal("nothing may be written to the root of the log directory")
	}
}
