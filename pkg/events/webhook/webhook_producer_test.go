package webhook_producer

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lucasgiovannibr/whatygo/pkg/config"
	logger_wrapper "github.com/lucasgiovannibr/whatygo/pkg/logger"
)

func newTestProducer(t *testing.T, timeout time.Duration) *webhookProducer {
	t.Helper()
	cfg := &config.Config{LogDirectory: t.TempDir()}
	return &webhookProducer{
		loggerWrapper: logger_wrapper.NewLoggerManagerForTest(t, cfg),
		httpClient:    &http.Client{Timeout: timeout},
		maxEvents:     1000,
		maxBytes:      64 << 20,
		workers:       1,
		backoff:       []time.Duration{time.Millisecond},
		queues:        map[string]*destQueue{},
		draining:      make(chan struct{}),
		stop:          make(chan struct{}),
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// A receiver that never answers must not hold the delivery forever.
func TestSendWebhookGivesUpOnAHangingReceiver(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-release }))
	defer srv.Close()
	defer close(release)

	p := newTestProducer(t, 200*time.Millisecond)
	start := time.Now()
	err, _, _ := p.sendWebhook(srv.URL, []byte(`{}`), "u")
	if err == nil {
		t.Fatal("a hanging receiver must produce an error")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("took %v, the timeout was not applied", elapsed)
	}
}

// Only a bounded part of the answer is read.
func TestSendWebhookLimitsTheResponseRead(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", 10*maxLoggedResponse)))
	}))
	defer srv.Close()

	p := newTestProducer(t, 5*time.Second)
	err, body, status := p.sendWebhook(srv.URL, []byte(`{}`), "u")
	if err != nil || status != 200 {
		t.Fatalf("err=%v status=%d", err, status)
	}
	if len(body) != maxLoggedResponse {
		t.Fatalf("read %d bytes, want %d", len(body), maxLoggedResponse)
	}
}

// A failing receiver is retried, and there is no wait after the last attempt.
func TestSendWithRetryDoesNotSleepAfterTheLastAttempt(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	p := newTestProducer(t, 5*time.Second)
	p.backoff = []time.Duration{150 * time.Millisecond}
	start := time.Now()
	ok := p.sendWithRetry(srv.URL, []byte(`{}`), 3, "u")
	elapsed := time.Since(start)

	if ok || hits.Load() != 3 {
		t.Fatalf("ok=%v hits=%d, want failure after 3 attempts", ok, hits.Load())
	}
	// two waits between three attempts (2 x ~150-180ms), not three
	if elapsed < 280*time.Millisecond || elapsed > 520*time.Millisecond {
		t.Fatalf("elapsed %v, want about 300ms", elapsed)
	}
}

// With one worker the events reach the receiver in the order they were produced.
func TestQueueDeliversInOrderWithOneWorker(t *testing.T) {
	var mu sync.Mutex
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := make([]byte, 16)
		n, _ := r.Body.Read(b)
		mu.Lock()
		got = append(got, string(b[:n]))
		mu.Unlock()
	}))
	defer srv.Close()

	p := newTestProducer(t, 5*time.Second)
	p.url = srv.URL
	for i := 0; i < 50; i++ {
		_ = p.Produce("inst.event", []byte(fmt.Sprintf("%03d", i)), "", "u")
	}
	waitFor(t, "delivery", func() bool { mu.Lock(); defer mu.Unlock(); return len(got) == 50 })

	for i, v := range got {
		if v != fmt.Sprintf("%03d", i) {
			t.Fatalf("event %d arrived as %q", i, v)
		}
	}
	waitFor(t, "queue to be released", func() bool { return p.WebhookStats().Destinations == 0 })
	if st := p.WebhookStats(); st.Sent != 50 || st.Dropped != 0 || st.Failed != 0 {
		t.Fatalf("%+v", st)
	}
}

// A receiver that is down cannot make the queue, or the goroutines, grow without limit:
// the oldest events are dropped and counted.
func TestQueueIsBoundedAndDropsTheOldest(t *testing.T) {
	release := make(chan struct{})
	var mu sync.Mutex
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		b := make([]byte, 16)
		n, _ := r.Body.Read(b)
		mu.Lock()
		got = append(got, string(b[:n]))
		mu.Unlock()
	}))
	defer srv.Close()

	p := newTestProducer(t, 30*time.Second)
	p.url = srv.URL
	p.maxEvents = 3

	before := runtime.NumGoroutine()
	for i := 0; i < 500; i++ {
		_ = p.Produce("inst.event", []byte(fmt.Sprintf("%03d", i)), "", "u")
	}

	waitFor(t, "the first event to be in flight", func() bool { return p.WebhookStats().InFlight == 1 })
	st := p.WebhookStats()
	if st.Pending > 3 {
		t.Fatalf("pending = %d, the limit is 3", st.Pending)
	}
	if st.Dropped != 500-1-3 {
		t.Fatalf("dropped = %d, want %d", st.Dropped, 500-1-3)
	}
	if grown := runtime.NumGoroutine() - before; grown > 25 {
		t.Fatalf("%d goroutines more than before: the queue must not create one per event", grown)
	}

	close(release)
	waitFor(t, "drain", func() bool { return p.WebhookStats().Destinations == 0 })

	mu.Lock()
	defer mu.Unlock()
	// the event that was in flight (whichever the worker picked first) and the three
	// newest; everything in between was dropped
	if len(got) != 4 || got[1] != "497" || got[2] != "498" || got[3] != "499" {
		t.Fatalf("delivered %v: the in-flight event and the three newest were expected", got)
	}
}

func TestQueueIsBoundedInBytesToo(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-release }))
	defer srv.Close()
	defer close(release)

	p := newTestProducer(t, 30*time.Second)
	p.url = srv.URL
	p.maxBytes = 100

	for i := 0; i < 20; i++ {
		_ = p.Produce("inst.event", []byte(strings.Repeat("x", 40)), "", "u")
	}
	waitFor(t, "in flight", func() bool { return p.WebhookStats().InFlight == 1 })
	if st := p.WebhookStats(); st.PendingBytes > 100 || st.Dropped == 0 {
		t.Fatalf("%+v", st)
	}
}

// After an event exhausted its retries the destination is down: the events behind it
// get one attempt each, and a success brings the full retries back.
func TestQueueDegradesAndRecovers(t *testing.T) {
	var fail atomic.Bool
	fail.Store(true)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if fail.Load() {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	p := newTestProducer(t, 5*time.Second)
	p.url = srv.URL
	p.backoff = []time.Duration{time.Millisecond, time.Millisecond} // 3 attempts per event

	for i := 0; i < 3; i++ {
		_ = p.Produce("inst.event", []byte("e"), "", "u")
	}
	waitFor(t, "drain", func() bool { return p.WebhookStats().Destinations == 0 })

	// first event: 3 attempts; the other two: 1 attempt each
	if got := hits.Load(); got != 5 {
		t.Fatalf("hits = %d, want 5 (3 + 1 + 1)", got)
	}
	if st := p.WebhookStats(); st.Failed != 3 || st.Sent != 0 {
		t.Fatalf("%+v", st)
	}

	// once idle the queue is gone and a recovered receiver gets its events again
	fail.Store(false)
	_ = p.Produce("inst.event", []byte("e"), "", "u")
	waitFor(t, "delivery", func() bool { return p.WebhookStats().Sent == 1 })
}

// The same URL as the global one is not delivered twice, and a queue name without a
// dot is ignored as before.
func TestProduceDeduplicatesAndIgnoresBadQueueNames(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer srv.Close()
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(100) }))
	defer other.Close()

	p := newTestProducer(t, 5*time.Second)
	p.url = srv.URL

	_ = p.Produce("nodot", []byte("e"), srv.URL, "u")
	_ = p.Produce("inst.event", []byte("e"), srv.URL, "u")   // same as global: once
	_ = p.Produce("inst.event", []byte("e"), other.URL, "u") // global + the other one
	waitFor(t, "delivery", func() bool { return p.WebhookStats().Sent == 3 })
	time.Sleep(50 * time.Millisecond)

	// the global URL got two events, the other one got one (worth 100)
	if got := hits.Load(); got != 102 {
		t.Fatalf("hits = %d, want 102", got)
	}
}

// Several workers deliver a burst faster than one, within the limit.
func TestQueueUsesSeveralWorkers(t *testing.T) {
	var cur, peak atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := cur.Add(1)
		for {
			old := peak.Load()
			if n <= old || peak.CompareAndSwap(old, n) {
				break
			}
		}
		time.Sleep(30 * time.Millisecond)
		cur.Add(-1)
	}))
	defer srv.Close()

	p := newTestProducer(t, 5*time.Second)
	p.url = srv.URL
	p.workers = 4
	for i := 0; i < 20; i++ {
		_ = p.Produce("inst.event", []byte("e"), "", "u")
	}
	waitFor(t, "delivery", func() bool { return p.WebhookStats().Sent == 20 })

	if got := peak.Load(); got < 2 || got > 4 {
		t.Fatalf("peak concurrency %d, want between 2 and 4", got)
	}
}

// Close waits for what is queued: a deploy used to lose every pending event.
func TestCloseDrainsQueuedEvents(t *testing.T) {
	var got atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(20 * time.Millisecond)
		got.Add(1)
	}))
	defer srv.Close()

	p := newTestProducer(t, time.Second)
	for i := 0; i < 10; i++ {
		_ = p.Produce("inst.message", []byte(`{}`), srv.URL, "u")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.Close(ctx); err != nil {
		t.Fatalf("close: %v", err)
	}
	if got.Load() != 10 {
		t.Fatalf("delivered %d of 10 before Close returned", got.Load())
	}
}

// A receiver that is down must not keep the process from stopping: once Close starts, the
// retries stop waiting between attempts, and at the deadline they stop altogether.
func TestCloseDoesNotWaitOutTheBackoffOfADeadReceiver(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	p := newTestProducer(t, time.Second)
	p.backoff = []time.Duration{time.Minute, time.Minute} // would retry for minutes
	_ = p.Produce("inst.message", []byte(`{}`), srv.URL, "u")
	waitFor(t, "the first attempt", func() bool { return p.WebhookStats().InFlight == 1 })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	err := p.Close(ctx)
	if time.Since(start) > 2*time.Second {
		t.Fatalf("Close waited %v for a dead receiver", time.Since(start))
	}
	if err != nil {
		t.Fatalf("the event was given up on, nothing is left: %v", err)
	}
	if st := p.WebhookStats(); st.Failed != 1 {
		t.Fatalf("the undelivered event must be counted as failed: %+v", st)
	}
}

// A receiver that hangs is cut at the deadline.
func TestCloseGivesUpAtTheDeadline(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-release }))
	defer srv.Close()

	p := newTestProducer(t, 10*time.Second)
	_ = p.Produce("inst.message", []byte(`{}`), srv.URL, "u")
	waitFor(t, "the attempt", func() bool { return p.WebhookStats().InFlight == 1 })

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := p.Close(ctx); err == nil {
		t.Fatal("expected an error: the event could not be delivered")
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("Close took %v", time.Since(start))
	}
	// Let the stuck request finish before the test ends: the worker logs when it does, and
	// the log directory is removed with the test.
	close(release)
	waitFor(t, "the worker to finish", func() bool { return p.WebhookStats().InFlight == 0 })
}
