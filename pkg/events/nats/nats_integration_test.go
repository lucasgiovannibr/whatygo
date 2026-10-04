package nats_producer

import (
	"context"
	"os"
	"testing"
	"time"

	logger_wrapper "github.com/lucasgiovannibr/whatygo/pkg/logger"
	"github.com/nats-io/nats.go"
)

// A NATS server that is down when the process starts must not disable the producer for
// good: it connects when the server shows up. Needs a server: NATS_TEST_URL=nats://localhost:4222
func TestProducerConnectsEvenIfStartedBeforeServerIsReachable(t *testing.T) {
	url := os.Getenv("NATS_TEST_URL")
	if url == "" {
		t.Skip("NATS_TEST_URL not set")
	}
	lm := logger_wrapper.NewLoggerManagerInTempDir(t)

	p := NewNatsProducer(url, true, nil, lm).(*natsProducer)
	defer p.Close(context.Background())
	if p.conn == nil {
		t.Fatal("producer has no connection object")
	}

	sub, err := nats.Connect(url)
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	got := make(chan string, 1)
	s, _ := sub.Subscribe("evo.test", func(m *nats.Msg) { got <- string(m.Data) })
	defer s.Unsubscribe()
	_ = sub.Flush()

	if err := p.Produce("evo.test", []byte("hello"), "enabled", "u"); err != nil {
		t.Fatal(err)
	}
	select {
	case m := <-got:
		if m != "hello" {
			t.Fatal(m)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("message not delivered")
	}
}

// The unreachable-at-start case needs no server at all.
func TestNewNatsProducerDoesNotFailWhenServerIsDown(t *testing.T) {
	lm := logger_wrapper.NewLoggerManagerInTempDir(t)
	p := NewNatsProducer("nats://127.0.0.1:1", true, nil, lm).(*natsProducer)
	defer p.Close(context.Background())
	if p.conn == nil {
		t.Fatal("producer must keep a connection that retries in the background")
	}
	if err := p.Produce("s", []byte("x"), "enabled", "u"); err != nil {
		t.Fatalf("publish while reconnecting is buffered, got %v", err)
	}
}
