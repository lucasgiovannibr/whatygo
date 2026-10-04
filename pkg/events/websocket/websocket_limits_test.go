package websocket_producer

import (
	"bytes"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func subscribers(p *websocketProducer, instance string) int {
	p.clientsMux.RLock()
	defer p.clientsMux.RUnlock()
	return len(p.clients[instance])
}

// A subscriber has nothing to send but control frames: one 300 MB frame took the process from
// 81 MB to 873 MB. A frame over the limit is refused (close code 1009) and the subscriber is
// dropped.
func TestAFrameOverTheReadLimitDropsTheSubscriber(t *testing.T) {
	p := newTestProducer(t)
	srv := newServer(p)
	defer srv.Close()

	c := dial(t, srv, "/?instance=inst1")
	defer c.Close()
	waitFor(t, func() bool { return subscribers(p, "inst1") == 1 })

	// small messages are fine
	if err := c.WriteMessage(websocket.TextMessage, []byte("hi")); err != nil {
		t.Fatal(err)
	}
	if err := c.WriteMessage(websocket.BinaryMessage, bytes.Repeat([]byte{1}, 2*readLimit)); err != nil {
		t.Fatal(err)
	}

	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, err := c.ReadMessage()
	if !websocket.IsCloseError(err, websocket.CloseMessageTooBig) {
		t.Fatalf("want a close frame 1009 (message too big), got %v", err)
	}
	waitFor(t, func() bool { return subscribers(p, "inst1") == 0 })
}

// A subscriber that stops reading must not hold the goroutines that produce events: Produce
// never waits for a socket, and the subscriber is disconnected when what is waiting for it
// passes the limit, instead of piling up in memory.
func TestASlowSubscriberIsDisconnectedWithoutBlockingProduce(t *testing.T) {
	p := newTestProducer(t)
	srv := newServer(p)
	defer srv.Close()

	slow := dial(t, srv, "/?instance=inst1") // never reads
	defer slow.Close()
	waitFor(t, func() bool { return subscribers(p, "inst1") == 1 })

	payload := bytes.Repeat([]byte("x"), 1<<20)
	start := time.Now()
	for i := 0; i < 80; i++ { // 80 MB against a 32 MB queue and a socket nobody reads
		if err := p.Produce("inst1.message", payload, "inst1", ""); err != nil {
			t.Fatalf("produce: %v", err)
		}
	}
	if took := time.Since(start); took > 3*time.Second {
		t.Fatalf("Produce waited for the slow subscriber: %v", took)
	}
	waitFor(t, func() bool { return subscribers(p, "inst1") == 0 })
}

// One slow subscriber does not take the events from the others.
func TestAFastSubscriberKeepsReceivingWhileAnotherIsSlow(t *testing.T) {
	p := newTestProducer(t)
	srv := newServer(p)
	defer srv.Close()

	slow := dial(t, srv, "/?instance=inst1")
	defer slow.Close()
	fast := dial(t, srv, "/?instance=inst1")
	defer fast.Close()
	waitFor(t, func() bool { return subscribers(p, "inst1") == 2 })

	got := make(chan int, 1)
	go func() {
		n := 0
		for {
			if _, _, err := fast.ReadMessage(); err != nil {
				got <- n
				return
			}
			n++
			if n == 60 {
				got <- n
				return
			}
		}
	}()

	payload := bytes.Repeat([]byte("y"), 1<<20)
	for i := 0; i < 60; i++ {
		_ = p.Produce("inst1.message", payload, "inst1", "")
		time.Sleep(5 * time.Millisecond)
	}
	select {
	case n := <-got:
		if n != 60 {
			t.Fatalf("the fast subscriber got %d of 60", n)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the fast subscriber stalled")
	}
}

// A subscriber that does not answer the pings is dropped; one that does, is not.
func TestSilentSubscribersAreDroppedAndLiveOnesKept(t *testing.T) {
	oldPing, oldPong := pingPeriod, pongWait
	pingPeriod, pongWait = 40*time.Millisecond, 300*time.Millisecond
	defer func() { pingPeriod, pongWait = oldPing, oldPong }()

	p := newTestProducer(t)
	srv := newServer(p)
	defer srv.Close()

	silent := dial(t, srv, "/?instance=silent") // never reads: the ping handler never runs
	defer silent.Close()
	live := dial(t, srv, "/?instance=live")
	defer live.Close()
	waitFor(t, func() bool { return subscribers(p, "silent") == 1 && subscribers(p, "live") == 1 })

	go func() { // reading answers the pings
		for {
			if _, _, err := live.ReadMessage(); err != nil {
				return
			}
		}
	}()

	waitFor(t, func() bool { return subscribers(p, "silent") == 0 })
	time.Sleep(2 * pongWait) // longer than the silent one needed to be dropped
	if subscribers(p, "live") != 1 {
		t.Fatal("a subscriber that answers the pings must be kept")
	}
}
