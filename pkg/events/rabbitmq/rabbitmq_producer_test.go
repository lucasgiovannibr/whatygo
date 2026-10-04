package rabbitmq_producer

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	logger_wrapper "github.com/lucasgiovannibr/whatygo/pkg/logger"
	amqp "github.com/rabbitmq/amqp091-go"
)

func newTestProducer(t *testing.T, url string) *rabbitMQProducer {
	t.Helper()
	p := NewRabbitMQProducer(nil, false, nil, nil, url, logger_wrapper.NewLoggerManagerInTempDir(t)).(*rabbitMQProducer)
	t.Cleanup(func() { _ = p.Close(context.Background()) })
	return p
}

func TestProduceIgnoresDisabledWithoutConnecting(t *testing.T) {
	p := newTestProducer(t, "amqp://guest:guest@127.0.0.1:1/")
	if err := p.Produce("q", []byte("x"), "", "u"); err != nil {
		t.Fatal(err)
	}
	if p.conn != nil || !p.lastDialErr.IsZero() {
		t.Fatal("a disabled instance must not touch the broker")
	}
}

// With the broker down, the first event pays one dial; the following ones fail at once
// (they used to sleep in the send path, 2 s between attempts).
func TestProduceFailsFastWhileBrokerIsDown(t *testing.T) {
	p := newTestProducer(t, "amqp://guest:guest@127.0.0.1:1/")
	if err := p.Produce("q", []byte("x"), "enabled", "u"); err == nil {
		t.Fatal("expected an error with the broker down")
	}
	start := time.Now()
	for i := 0; i < 20; i++ {
		if err := p.Produce("q", []byte("x"), "enabled", "u"); err == nil {
			t.Fatal("expected an error with the broker down")
		}
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("20 events took %v while the broker is down", d)
	}
}

func TestProduceAfterCloseFails(t *testing.T) {
	p := newTestProducer(t, "amqp://guest:guest@127.0.0.1:1/")
	_ = p.Close(context.Background())
	if err := p.Produce("q", []byte("x"), "enabled", "u"); err == nil {
		t.Fatal("expected an error after Close")
	}
}

// The tests below need a broker: AMQP_TEST_URL=amqp://guest:guest@localhost:5672/
func brokerURL(t *testing.T) string {
	t.Helper()
	url := os.Getenv("AMQP_TEST_URL")
	if url == "" {
		t.Skip("AMQP_TEST_URL not set")
	}
	return url
}

func consume(t *testing.T, url, queue string, want int) []string {
	t.Helper()
	conn, err := amqp.Dial(url)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ch, err := conn.Channel()
	if err != nil {
		t.Fatal(err)
	}
	deliveries, err := ch.Consume(queue, "", true, false, false, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	timeout := time.After(10 * time.Second)
	for len(got) < want {
		select {
		case d := <-deliveries:
			got = append(got, string(d.Body))
		case <-timeout:
			t.Fatalf("got %d of %d messages", len(got), want)
		}
	}
	return got
}

func uniqueQueue(t *testing.T) string {
	return fmt.Sprintf("evo-test-%s-%d", t.Name(), time.Now().UnixNano())
}

func TestProducePublishesConfirmedAndDeclaresOnce(t *testing.T) {
	url := brokerURL(t)
	p := newTestProducer(t, url)
	q := uniqueQueue(t)

	for i := 0; i < 20; i++ {
		if err := p.Produce(q, []byte(fmt.Sprint("m", i)), "enabled", "u"); err != nil {
			t.Fatalf("publish %d: %v", i, err)
		}
	}
	if len(p.declared) != 1 {
		t.Fatalf("queue declared %d times, want once per channel", len(p.declared))
	}
	got := consume(t, url, q, 20)
	for i, m := range got {
		if m != fmt.Sprint("m", i) {
			t.Fatalf("order lost at %d: %s", i, m)
		}
	}
}

func TestProduceRecoversWhenConnectionDies(t *testing.T) {
	url := brokerURL(t)
	p := newTestProducer(t, url)
	q := uniqueQueue(t)

	if err := p.Produce(q, []byte("before"), "enabled", "u"); err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	_ = p.conn.Close() // the broker went away / the network dropped
	p.mu.Unlock()

	if err := p.Produce(q, []byte("after"), "enabled", "u"); err != nil {
		t.Fatalf("did not recover: %v", err)
	}
	got := consume(t, url, q, 2)
	if got[0] != "before" || got[1] != "after" {
		t.Fatalf("got %v", got)
	}
}

func TestProduceIsSafeForConcurrentUse(t *testing.T) {
	url := brokerURL(t)
	p := newTestProducer(t, url)
	q := uniqueQueue(t)

	errs := make(chan error, 8)
	for g := 0; g < 8; g++ {
		go func(g int) {
			var err error
			for i := 0; i < 10 && err == nil; i++ {
				err = p.Produce(q, []byte(fmt.Sprintf("%d-%d", g, i)), "global", "u")
			}
			errs <- err
		}(g)
	}
	for g := 0; g < 8; g++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	consume(t, url, q, 80)
}
