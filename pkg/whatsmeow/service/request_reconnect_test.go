package whatsmeow_service

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/lucasgiovannibr/whatygo/pkg/config"
	"github.com/lucasgiovannibr/whatygo/pkg/safemap"
)

type reconnectRecorder struct {
	WhatsmeowService
	calls atomic.Int32
}

func (r *reconnectRecorder) ReconnectClient(string) error { r.calls.Add(1); return nil }

func pacingState(id string) (fails int, pending bool) {
	autoReconnect.mu.Lock()
	defer autoReconnect.mu.Unlock()
	st := autoReconnect.states[id]
	if st == nil {
		return 0, false
	}
	return st.fails, st.pending
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestRequestReconnectWithoutARuntimeDoesNothing(t *testing.T) {
	w := whatsmeowService{myClientPointer: safemap.New[*MyClient]()}
	if w.RequestReconnect("no-runtime") {
		t.Fatal("an instance without a runtime has nothing to reconnect")
	}
}

// The first request reconnects at once; the next ones go through the backoff like the
// automatic reconnections, instead of restarting the instance every time they arrive.
func TestRequestReconnectIsPacedLikeAnAutomaticOne(t *testing.T) {
	const id = "request-reconnect-pacing"
	t.Cleanup(func() { autoReconnect.forget(id) })

	mycli, _ := newHandlerClient(t, &config.Config{})
	mycli.userID = id
	rec := &reconnectRecorder{}
	mycli.service = rec
	clients := safemap.New[*MyClient]()
	clients.Set(id, mycli)
	mycli.myClientPointer = clients
	w := whatsmeowService{myClientPointer: clients}
	t.Cleanup(func() { clients.Delete(id) }) // a goroutine still waiting finds the client replaced and ends

	if !w.RequestReconnect(id) {
		t.Fatal("the instance has a runtime")
	}
	waitFor(t, "the first reconnection", func() bool { return rec.calls.Load() == 1 })
	waitFor(t, "the first reconnection to be done", func() bool { _, pending := pacingState(id); return !pending })

	// a second request right away: accepted (counted) but waits for the backoff
	if !w.RequestReconnect(id) {
		t.Fatal("the instance has a runtime")
	}
	if fails, pending := pacingState(id); fails != 2 || !pending {
		t.Fatalf("the second must be scheduled behind the backoff, got fails=%d pending=%v", fails, pending)
	}
	time.Sleep(400 * time.Millisecond)
	if got := rec.calls.Load(); got != 1 {
		t.Fatalf("the second reconnection must wait for the backoff, ReconnectClient ran %d times", got)
	}

	// and a request while that one is pending joins it
	w.RequestReconnect(id)
	if fails, _ := pacingState(id); fails != 2 {
		t.Fatalf("a request must join the pending reconnection, fails=%d", fails)
	}
}
