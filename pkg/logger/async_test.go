package logger

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lucasgiovannibr/whatygo/pkg/config"
)

func newTestManager(t *testing.T, level string) (*LoggerManager, string) {
	t.Helper()
	dir := t.TempDir()
	lm := NewLoggerManager(&config.Config{LogDirectory: dir, LogMaxSize: 10, LogMaxBackups: 1, LogMaxAge: 1, LogLevel: level})
	t.Cleanup(lm.Close)
	return lm, dir
}

func readEntries(t *testing.T, dir, id string) []LogEntry {
	t.Helper()
	f, err := os.Open(filepath.Join(dir, id, "instance.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []LogEntry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		var e LogEntry
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			t.Fatalf("a log line is not valid JSON: %q", sc.Text())
		}
		out = append(out, e)
	}
	return out
}

func TestParseLevel(t *testing.T) {
	for in, want := range map[string]Level{
		"": LevelInfo, "info": LevelInfo, "INFO": LevelInfo, "garbage": LevelInfo,
		"debug": LevelDebug, " Debug ": LevelDebug,
		"warn": LevelWarn, "WARNING": LevelWarn, "error": LevelError,
	} {
		if got := ParseLevel(in); got != want {
			t.Errorf("ParseLevel(%q) = %v, want %v", in, got, want)
		}
	}
}

// DEBUG lines used to be written to the instance log on every run.
func TestLinesBelowTheLevelAreNotWritten(t *testing.T) {
	lm, dir := newTestManager(t, "info")
	l := lm.GetLogger("inst")
	l.LogDebug("debug line")
	l.LogInfo("info line")
	l.LogWarn("warn line")
	l.LogError("error line")
	l.Flush()

	var got []string
	for _, e := range readEntries(t, dir, "inst") {
		got = append(got, e.Level+":"+e.Message)
	}
	want := "INFO:info line,WARN:warn line,ERROR:error line"
	if strings.Join(got, ",") != want {
		t.Fatalf("got %v, want %s", got, want)
	}

	lm2, dir2 := newTestManager(t, "error")
	lm2.GetLogger("inst").LogWarn("not at error level")
	lm2.GetLogger("inst").LogError("only this")
	lm2.GetLogger("inst").Flush()
	if es := readEntries(t, dir2, "inst"); len(es) != 1 || es[0].Message != "only this" {
		t.Fatalf("LOG_LEVEL=error must keep only errors, got %+v", es)
	}
}

func TestDebugLevelWritesEverything(t *testing.T) {
	lm, dir := newTestManager(t, "debug")
	lm.GetLogger("inst").LogDebug("now visible")
	lm.GetLogger("inst").Flush()
	if es := readEntries(t, dir, "inst"); len(es) != 1 || es[0].Level != "DEBUG" {
		t.Fatalf("got %+v", es)
	}
}

func TestOrderIsKeptAndTimestampsAreFromTheCall(t *testing.T) {
	lm, dir := newTestManager(t, "info")
	l := lm.GetLogger("inst")
	before := time.Now()
	for i := 0; i < 200; i++ {
		l.LogInfo("line %d", i)
	}
	l.Flush()

	es := readEntries(t, dir, "inst")
	if len(es) != 200 {
		t.Fatalf("got %d lines, want 200", len(es))
	}
	for i, e := range es {
		if e.Message != fmt.Sprintf("line %d", i) {
			t.Fatalf("line %d out of order: %q", i, e.Message)
		}
		if e.Timestamp.Before(before.Add(-time.Second)) || e.Timestamp.After(time.Now().Add(time.Second)) {
			t.Fatalf("timestamp %v is not from the time of the call", e.Timestamp)
		}
	}
}

// Logging must not wait for the disk.
func TestLoggingDoesNotWaitForTheDisk(t *testing.T) {
	lm, _ := newTestManager(t, "info")
	l := lm.GetLogger("inst")

	start := time.Now()
	for i := 0; i < 20000; i++ {
		l.LogDebug("filtered out, costs nothing %d", i)
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("20000 filtered lines took %v", d)
	}
}

func TestAFullQueueDropsAndReportsInsteadOfBlocking(t *testing.T) {
	lm, dir := newTestManager(t, "info")
	l := lm.GetLogger("inst")

	// Stall the writer: it is blocked on a flush marker nobody completes... use the queue itself.
	l.mu.RLock()
	for i := 0; i < writeQueueSize; i++ { // fill it without the writer being able to keep up
		select {
		case l.queue <- &LogEntry{Timestamp: time.Now(), Level: "INFO", InstanceId: "inst", Message: "filler"}:
		default:
		}
	}
	l.mu.RUnlock()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 5000; i++ {
			l.LogInfo("burst %d", i)
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("logging blocked on a full queue")
	}

	l.Flush()
	// Either everything fitted (a fast disk) or the loss was reported, never silent.
	es := readEntries(t, dir, "inst")
	burst, reported := 0, 0
	for _, e := range es {
		switch {
		case strings.HasPrefix(e.Message, "burst "):
			burst++
		case strings.Contains(e.Message, "dropped because the disk could not keep up"):
			reported++
		}
	}
	if burst < 5000 && reported == 0 {
		t.Fatalf("%d of 5000 lines are missing and no drop was reported", 5000-burst)
	}
}

func TestCloseWritesWhatIsQueuedAndIsSafeToRepeat(t *testing.T) {
	lm, dir := newTestManager(t, "info")
	l := lm.GetLogger("inst")
	for i := 0; i < 100; i++ {
		l.LogInfo("queued %d", i)
	}

	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	if err := l.Close(); err != nil { // a second close is a no-op, not a panic
		t.Fatal(err)
	}
	if es := readEntries(t, dir, "inst"); len(es) != 100 {
		t.Fatalf("Close must write the queued lines, got %d of 100", len(es))
	}

	// Logging, and flushing, after the close are harmless.
	l.LogInfo("after close")
	l.Flush()
}

func TestCloseAllFlushesEveryManager(t *testing.T) {
	lm, dir := newTestManager(t, "info")
	lm.GetLogger("a").LogInfo("from a")
	lm.GetLogger("b").LogInfo("from b")

	CloseAll()

	for _, id := range []string{"a", "b"} {
		if es := readEntries(t, dir, id); len(es) != 1 {
			t.Fatalf("instance %s: %d lines after CloseAll, want 1", id, len(es))
		}
	}
}

func TestConcurrentLoggingLosesNothingAndRaces(t *testing.T) {
	lm, dir := newTestManager(t, "info")
	l := lm.GetLogger("inst")

	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				l.LogInfo("g%d line %d", g, i)
				if i%10 == 0 {
					l.Flush()
				}
			}
		}(g)
	}
	wg.Wait()
	l.Flush()

	if es := readEntries(t, dir, "inst"); len(es) != 400 {
		t.Fatalf("got %d lines, want 400", len(es))
	}
}

func TestReleasedInstanceKeepsLoggingToTheConsoleOnly(t *testing.T) {
	lm, dir := newTestManager(t, "info")
	lm.GetLogger("inst").LogInfo("before")
	lm.Release("inst")

	l := lm.GetLogger("inst")
	l.LogInfo("after release") // must not panic, queue is nil
	l.Flush()
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	if es := readEntries(t, dir, "inst"); len(es) != 1 || es[0].Message != "before" {
		t.Fatalf("a released instance must not write any more, got %+v", es)
	}
}

func BenchmarkLogInfoOnTheCallingGoroutine(b *testing.B) {
	dir := b.TempDir()
	lm := NewLoggerManager(&config.Config{LogDirectory: dir, LogMaxSize: 100, LogMaxBackups: 1, LogMaxAge: 1, LogLevel: "info"})
	defer lm.Close()
	l := lm.GetLogger("inst")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.LogInfo("[%s] message %d from %s of type %s", "instance-id", i, "chat@s.whatsapp.net", "text")
	}
}
