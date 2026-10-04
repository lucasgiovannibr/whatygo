package message_repository

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	message_model "github.com/lucasgiovannibr/whatygo/pkg/message/model"
)

func msg(inst, id, status string) message_model.Message {
	return message_model.Message{InstanceID: inst, MessageID: id, Status: status}
}

func TestCoalesceKeepsFurthestStatusPerMessage(t *testing.T) {
	ref := []byte(`{"a":1}`)
	in := []message_model.Message{
		{InstanceID: "i", MessageID: "m1", Status: "Received", Referral: ref},
		msg("i", "m2", "Delivered"),
		msg("i", "m1", "Read"),      // later and further: wins, inherits the referral
		msg("i", "m1", "Delivered"), // later but behind: ignored
		msg("j", "m1", "Received"),  // same id, other instance: its own row
	}
	out := coalesce(in)
	if len(out) != 3 {
		t.Fatalf("want 3 rows, got %d: %+v", len(out), out)
	}
	if out[0].MessageID != "m1" || out[0].Status != "Read" || string(out[0].Referral) != string(ref) {
		t.Fatalf("m1 of i: %+v", out[0])
	}
	if out[1].MessageID != "m2" || out[2].InstanceID != "j" {
		t.Fatalf("order/instance lost: %+v", out)
	}
}

type recorder struct {
	mu      sync.Mutex
	batches [][]message_model.Message
	err     error
}

func (r *recorder) write(b []message_model.Message) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.batches = append(r.batches, append([]message_model.Message(nil), b...))
	return r.err
}

func (r *recorder) total() (rows, batches int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, b := range r.batches {
		rows += len(b)
	}
	return rows, len(r.batches)
}

func TestBatchWriterGroupsBurstsAndDrainsOnClose(t *testing.T) {
	r := &recorder{}
	w := newBatchWriter(r.write)

	const n = 1000
	for i := 0; i < n; i++ {
		if !w.enqueue(msg("i", fmt.Sprint(i), "Received")) {
			t.Fatalf("message %d rejected", i)
		}
	}
	w.close() // must write everything still queued

	rows, batches := r.total()
	if rows != n {
		t.Fatalf("wrote %d of %d messages", rows, n)
	}
	if batches > n/writerBatchMax+3 {
		t.Fatalf("%d messages took %d writes: not batched", n, batches)
	}
	for _, b := range r.batches {
		if len(b) > writerBatchMax {
			t.Fatalf("batch of %d exceeds the limit", len(b))
		}
	}
}

func TestBatchWriterFlushesByTimeWithoutClose(t *testing.T) {
	r := &recorder{}
	w := newBatchWriter(r.write)
	defer w.close()

	w.enqueue(msg("i", "m", "Read"))
	deadline := time.After(3 * time.Second)
	for {
		if rows, _ := r.total(); rows == 1 {
			return
		}
		select {
		case <-deadline:
			t.Fatal("a lone message was never written")
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func TestBatchWriterDropsWhenFullInsteadOfBlocking(t *testing.T) {
	block := make(chan struct{})
	w := newBatchWriter(func([]message_model.Message) error { <-block; return nil })

	accepted := 0
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < writerQueueSize+writerBatchMax+100; i++ {
			if w.enqueue(msg("i", fmt.Sprint(i), "Received")) {
				accepted++
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("enqueue blocked on a stuck database")
	}
	if accepted >= writerQueueSize+writerBatchMax+100 {
		t.Fatal("nothing was dropped although the writer is stuck")
	}
	close(block)
	w.close()
}

func TestBatchWriterSurvivesFailedBatchAndRejectsAfterClose(t *testing.T) {
	r := &recorder{err: errors.New("db down")}
	w := newBatchWriter(r.write)
	w.enqueue(msg("i", "a", "Received"))
	w.close()
	if rows, _ := r.total(); rows != 1 {
		t.Fatalf("batch not attempted: %d", rows)
	}
	if w.enqueue(msg("i", "b", "Received")) {
		t.Fatal("accepted a message after close")
	}
	w.close() // second close is harmless
}

func TestBatchWriterNeverStartedCloses(t *testing.T) {
	newBatchWriter(func([]message_model.Message) error { return nil }).close()
}

func TestBatchWriterConcurrentEnqueueAndClose(t *testing.T) {
	r := &recorder{}
	w := newBatchWriter(r.write)
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				w.enqueue(msg("i", fmt.Sprintf("%d-%d", g, i), "Received"))
			}
		}(g)
	}
	time.Sleep(time.Millisecond)
	w.close() // must not panic with "send on closed channel"
	wg.Wait()
}
