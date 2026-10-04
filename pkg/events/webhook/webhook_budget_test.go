package webhook_producer

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func queued(p *webhookProducer, url string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	if q := p.queues[url]; q != nil {
		return len(q.events)
	}
	return 0
}

// The limit per destination alone allowed N x 64 MB for N destinations. All of them together
// are bounded too, and the room is made by the oldest events of the destination that holds
// the most.
func TestGlobalBudgetTakesTheOldestOfTheBiggestDestination(t *testing.T) {
	p := newTestProducer(t, time.Second)
	p.workers = 0 // nothing is delivered: the events stay queued
	p.globalMaxBytes = 1000

	for i := 0; i < 3; i++ {
		p.enqueue("http://a", []byte(strings.Repeat("a", 300)), "u")
	}
	p.enqueue("http://b", []byte(strings.Repeat("b", 100)), "u")
	if queued(p, "http://a") != 3 || queued(p, "http://b") != 1 || p.totalBytes != 1000 || p.dropped != 0 {
		t.Fatalf("everything fits: a=%d b=%d total=%d dropped=%d", queued(p, "http://a"), queued(p, "http://b"), p.totalBytes, p.dropped)
	}

	// 200 more bytes for b do not fit: a, the biggest, loses its oldest event
	p.enqueue("http://b", []byte(strings.Repeat("b", 200)), "u")
	if queued(p, "http://a") != 2 || queued(p, "http://b") != 2 {
		t.Fatalf("a must lose one: a=%d b=%d", queued(p, "http://a"), queued(p, "http://b"))
	}
	if p.dropped != 1 || p.totalBytes != 900 {
		t.Fatalf("dropped=%d total=%d, want 1 and 900", p.dropped, p.totalBytes)
	}
	if st := p.WebhookStats(); st.PendingBytes != 900 || st.Pending != 4 {
		t.Fatalf("stats must agree: %+v", st)
	}
}

func TestGlobalBudgetNeverDropsTheEventBeingAddedAndIsOffAtZero(t *testing.T) {
	p := newTestProducer(t, time.Second)
	p.workers = 0
	p.globalMaxBytes = 100

	// one event bigger than the whole budget is still queued (alone)
	p.enqueue("http://a", []byte(strings.Repeat("a", 500)), "u")
	if queued(p, "http://a") != 1 {
		t.Fatal("an oversized event is queued, not lost")
	}
	p.enqueue("http://a", []byte(strings.Repeat("a", 50)), "u")
	if queued(p, "http://a") != 1 || p.dropped != 1 {
		t.Fatalf("the old one makes room for the new one: queued=%d dropped=%d", queued(p, "http://a"), p.dropped)
	}

	p = newTestProducer(t, time.Second)
	p.workers = 0 // globalMaxBytes is 0: no limit
	for i := 0; i < 50; i++ {
		p.enqueue("http://x", []byte(strings.Repeat("x", 1000)), "u")
	}
	if queued(p, "http://x") != 50 || p.dropped != 0 {
		t.Fatal("without a global limit nothing is dropped by it")
	}
}

func TestTotalBytesFollowsDeliveries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	p := newTestProducer(t, 5*time.Second)
	p.globalMaxBytes = 1 << 20
	for i := 0; i < 20; i++ {
		p.enqueue(srv.URL, []byte(`{"n":1}`), "u")
	}
	waitFor(t, "the queue to drain", func() bool { return p.WebhookStats().Pending+p.WebhookStats().InFlight == 0 })
	p.mu.Lock()
	total := p.totalBytes
	p.mu.Unlock()
	if total != 0 {
		t.Fatalf("totalBytes must return to 0 once delivered, got %d", total)
	}
}

func TestRetryable(t *testing.T) {
	for status, want := range map[int]bool{
		0: true, 200: true, 500: true, 502: true, 503: true, 504: true,
		408: true, 425: true, 429: true,
		400: false, 401: false, 403: false, 404: false, 410: false, 413: false, 422: false,
	} {
		if got := retryable(status); got != want {
			t.Errorf("retryable(%d) = %v, want %v", status, got, want)
		}
	}
}

// A receiver that says "no, this is wrong" is asked once, not five times over two and a half
// minutes; one that is only busy or failing is still retried.
func TestAPermanentRefusalIsNotRetried(t *testing.T) {
	for status, wantHits := range map[int]int32{
		http.StatusNotFound:            1,
		http.StatusBadRequest:          1,
		http.StatusGone:                1,
		http.StatusTooManyRequests:     4,
		http.StatusInternalServerError: 4,
	} {
		var hits atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hits.Add(1)
			w.WriteHeader(status)
		}))
		p := newTestProducer(t, 5*time.Second)
		p.backoff = []time.Duration{time.Millisecond}
		if p.sendWithRetry(srv.URL, []byte(`{}`), 4, "u") {
			t.Fatalf("status %d must fail", status)
		}
		srv.Close()
		if hits.Load() != wantHits {
			t.Errorf("status %d: %d attempts, want %d", status, hits.Load(), wantHits)
		}
	}
}
