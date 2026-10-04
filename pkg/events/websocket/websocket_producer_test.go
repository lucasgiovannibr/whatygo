package websocket_producer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/lucasgiovannibr/whatygo/pkg/config"
	logger_wrapper "github.com/lucasgiovannibr/whatygo/pkg/logger"
)

func newTestProducer(t *testing.T) *websocketProducer {
	t.Helper()
	lw := logger_wrapper.NewLoggerManagerForTest(t, &config.Config{LogDirectory: t.TempDir()})
	return NewWebsocketProducer(lw)
}

func dial(t *testing.T, srv *httptest.Server, path string) *websocket.Conn {
	t.Helper()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + path
	c, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial %s: %v", path, err)
	}
	return c
}

func newServer(p *websocketProducer) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ServeWs(w, r, r.URL.Query().Get("instance"), p)
	}))
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met in time")
}

// Regression for issue #99: concurrent Produce calls on the same connection used
// to panic inside gorilla/websocket ("concurrent write to websocket connection").
func TestProduceConcurrentWritesDoNotPanic(t *testing.T) {
	p := newTestProducer(t)
	srv := newServer(p)
	defer srv.Close()

	c := dial(t, srv, "/?instance=inst1")
	defer c.Close()
	waitFor(t, func() bool {
		p.clientsMux.RLock()
		defer p.clientsMux.RUnlock()
		return len(p.clients["inst1"]) == 1
	})

	const writers, perWriter = 16, 25
	var received int
	done := make(chan struct{})
	go func() {
		defer close(done)
		for received < writers*perWriter {
			if _, _, err := c.ReadMessage(); err != nil {
				return
			}
			received++
		}
	}()

	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perWriter; j++ {
				if err := p.Produce("inst1.message", []byte(`{"n":1}`), "inst1", ""); err != nil {
					t.Errorf("produce: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("received %d/%d messages", received, writers*perWriter)
	}
}

// Several subscribers of the same instance must all get the event, and one of
// them disconnecting must not silence the others.
func TestMultipleSubscribersPerInstance(t *testing.T) {
	p := newTestProducer(t)
	srv := newServer(p)
	defer srv.Close()

	a := dial(t, srv, "/?instance=inst1")
	b := dial(t, srv, "/?instance=inst1")
	defer b.Close()
	waitFor(t, func() bool {
		p.clientsMux.RLock()
		defer p.clientsMux.RUnlock()
		return len(p.clients["inst1"]) == 2
	})

	if err := p.Produce("inst1.message", []byte("x"), "inst1", ""); err != nil {
		t.Fatalf("produce: %v", err)
	}
	for name, c := range map[string]*websocket.Conn{"a": a, "b": b} {
		_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
		if _, _, err := c.ReadMessage(); err != nil {
			t.Fatalf("subscriber %s did not receive event: %v", name, err)
		}
	}

	// a disconnects; b keeps receiving.
	a.Close()
	waitFor(t, func() bool {
		p.clientsMux.RLock()
		defer p.clientsMux.RUnlock()
		return len(p.clients["inst1"]) == 1
	})
	if err := p.Produce("inst1.message", []byte("y"), "inst1", ""); err != nil {
		t.Fatalf("produce after disconnect: %v", err)
	}
	_ = b.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := b.ReadMessage(); err != nil {
		t.Fatalf("remaining subscriber stopped receiving: %v", err)
	}
}

// Close sends a proper close frame (going away) instead of cutting the connections.
func TestCloseTellsSubscribersTheServerIsGoing(t *testing.T) {
	p := newTestProducer(t)
	srv := newServer(p)
	defer srv.Close()

	a := dial(t, srv, "/?instance=inst1")
	b := dial(t, srv, "/")
	waitFor(t, func() bool {
		p.clientsMux.RLock()
		defer p.clientsMux.RUnlock()
		return len(p.clients["inst1"]) == 1 && len(p.broadcast) == 1
	})

	if err := p.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]*websocket.Conn{"instance": a, "broadcast": b} {
		_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, _, err := c.ReadMessage()
		if !websocket.IsCloseError(err, websocket.CloseGoingAway) {
			t.Fatalf("%s subscriber: want a going-away close frame, got %v", name, err)
		}
	}
	// Nothing is left to write to.
	if err := p.Produce("inst1.message", []byte("x"), "inst1", ""); err != nil {
		t.Fatalf("produce after close: %v", err)
	}
}
