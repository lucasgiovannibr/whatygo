package logger

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lucasgiovannibr/whatygo/pkg/config"
)

func TestReleaseFreesTheInstanceLogger(t *testing.T) {
	dir := t.TempDir()
	lm := NewLoggerManager(&config.Config{LogDirectory: dir, LogMaxSize: 1, LogMaxBackups: 1, LogMaxAge: 1})
	t.Cleanup(lm.Close)

	lm.GetLogger("inst-1").LogInfo("first line")
	lm.GetLogger("inst-1").Flush()
	logFile := filepath.Join(dir, "inst-1", "instance.log")
	before, err := os.ReadFile(logFile)
	if err != nil || len(before) == 0 {
		t.Fatalf("the instance log file should exist and have content: %v", err)
	}

	lm.Release("inst-1")

	lm.mu.RLock()
	_, stillCached := lm.loggers["inst-1"]
	lm.mu.RUnlock()
	if stillCached {
		t.Fatal("a released instance must not stay in the logger cache")
	}

	// Logging after the release must not resurrect a file logger nor touch the file.
	lm.GetLogger("inst-1").LogInfo("after release")
	lm.GetLogger("inst-1").Flush()
	after, _ := os.ReadFile(logFile)
	if string(after) != string(before) {
		t.Fatal("a released instance must not write to its log file any more")
	}
	lm.mu.RLock()
	_, recreated := lm.loggers["inst-1"]
	lm.mu.RUnlock()
	if recreated {
		t.Fatal("GetLogger must not recreate the logger of a released instance")
	}

	// Other instances are unaffected.
	lm.GetLogger("inst-2").LogInfo("still works")
	lm.GetLogger("inst-2").Flush()
	if _, err := os.Stat(filepath.Join(dir, "inst-2", "instance.log")); err != nil {
		t.Fatalf("other instances must keep their file logger: %v", err)
	}
}

func TestRemoveFilesDeletesTheLogDirectory(t *testing.T) {
	dir := t.TempDir()
	lm := NewLoggerManager(&config.Config{LogDirectory: dir, LogMaxSize: 1, LogMaxBackups: 1, LogMaxAge: 1})
	t.Cleanup(lm.Close)

	lm.GetLogger("inst-1").LogInfo("token and jids live here")
	lm.GetLogger("inst-1").Flush()
	lm.GetLogger("inst-2").LogInfo("another instance")
	lm.GetLogger("inst-2").Flush()

	lm.Release("inst-1")
	if err := lm.RemoveFiles("inst-1"); err != nil {
		t.Fatalf("RemoveFiles: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "inst-1")); !os.IsNotExist(err) {
		t.Fatalf("the log directory of a deleted instance must be gone, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "inst-2", "instance.log")); err != nil {
		t.Fatalf("other instances' logs must stay: %v", err)
	}
}

func TestRemoveFilesKeepsLogsWhenAsked(t *testing.T) {
	dir := t.TempDir()
	lm := NewLoggerManager(&config.Config{LogDirectory: dir, LogMaxSize: 1, LogMaxBackups: 1, LogMaxAge: 1, LogKeepDeleted: true})
	t.Cleanup(lm.Close)

	lm.GetLogger("inst-1").LogInfo("kept")
	lm.GetLogger("inst-1").Flush()
	lm.Release("inst-1")
	if err := lm.RemoveFiles("inst-1"); err != nil {
		t.Fatalf("RemoveFiles: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "inst-1", "instance.log")); err != nil {
		t.Fatalf("LOG_KEEP_DELETED=true must keep the logs: %v", err)
	}
}

func TestRemoveFilesRefusesPathsOutsideTheLogDirectory(t *testing.T) {
	dir := t.TempDir()
	lm := NewLoggerManager(&config.Config{LogDirectory: dir})
	t.Cleanup(lm.Close)
	for _, id := range []string{"", ".", "..", "../x", "a/b", `a\b`} {
		if id == `a\b` && string(filepath.Separator) != `\` {
			continue
		}
		if err := lm.RemoveFiles(id); err == nil {
			t.Errorf("RemoveFiles(%q) must be refused", id)
		}
	}
}
