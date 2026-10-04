package whatsmeow_service

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/lucasgiovannibr/whatygo/pkg/config"
	instance_model "github.com/lucasgiovannibr/whatygo/pkg/instance/model"
	logger_wrapper "github.com/lucasgiovannibr/whatygo/pkg/logger"
)

type failingProducer struct {
	err   error
	delay time.Duration
	calls int
	mu    sync.Mutex
}

func (f *failingProducer) Produce(string, []byte, string, string) error {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	time.Sleep(f.delay)
	return f.err
}
func (f *failingProducer) CreateGlobalQueues() error { return nil }

func (f *failingProducer) called() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func newOutputsService(t *testing.T) *whatsmeowService {
	t.Helper()
	cfg := &config.Config{LogDirectory: t.TempDir()}
	return &whatsmeowService{config: cfg, loggerWrapper: logger_wrapper.NewLoggerManagerForTest(t, cfg)}
}

// Reproduced on the test stack: rabbitmqEnable set and no broker => the webhook of the
// same event was never tried, because the first error ended the function.
func TestAFailingProducerDoesNotStopTheWebhook(t *testing.T) {
	w := newOutputsService(t)
	webhook := &recordingProducer{}
	w.rabbitmqProducer = &failingProducer{err: errors.New("broker is down")}
	w.natsProducer = &failingProducer{err: errors.New("nats is down")}
	w.websocketProducer = &failingProducer{err: errors.New("subscriber cannot take the frame")}
	w.webhookProducer = webhook

	instance := &instance_model.Instance{
		Id: "a", Webhook: "http://example.invalid/hook",
		RabbitmqEnable: "enabled", NatsEnable: "enabled", WebSocketEnable: "enabled",
	}
	w.sendToQueueOrWebhook(instance, "a.message", []byte(`{}`))

	if webhook.count() != 1 {
		t.Fatalf("the webhook must receive the event even when every other output fails, got %d", webhook.count())
	}
}

func TestEveryEnabledOutputIsTried(t *testing.T) {
	w := newOutputsService(t)
	rabbit, nats, ws := &failingProducer{}, &failingProducer{}, &failingProducer{}
	webhook := &recordingProducer{}
	w.rabbitmqProducer, w.natsProducer, w.websocketProducer, w.webhookProducer = rabbit, nats, ws, webhook

	// Only RabbitMQ and the webhook are enabled for this instance.
	instance := &instance_model.Instance{Id: "a", Webhook: "http://example.invalid/hook", RabbitmqEnable: "true"}
	w.sendToQueueOrWebhook(instance, "a.message", []byte(`{}`))

	if rabbit.called() != 1 || webhook.count() != 1 {
		t.Fatalf("rabbit=%d webhook=%d, want 1 and 1", rabbit.called(), webhook.count())
	}
	if nats.called() != 0 || ws.called() != 0 {
		t.Fatalf("outputs that are not enabled must not be called (nats=%d ws=%d)", nats.called(), ws.called())
	}
}

// A slow output (RabbitMQ reconnecting) must not delay the others.
func TestASlowProducerDoesNotDelayTheWebhook(t *testing.T) {
	w := newOutputsService(t)
	webhookAt := make(chan time.Time, 1)
	w.rabbitmqProducer = &failingProducer{delay: 300 * time.Millisecond}
	w.webhookProducer = producerFunc(func() { webhookAt <- time.Now() })

	instance := &instance_model.Instance{Id: "a", Webhook: "http://example.invalid/hook", RabbitmqEnable: "enabled"}
	start := time.Now()
	w.sendToQueueOrWebhook(instance, "a.message", []byte(`{}`))

	select {
	case at := <-webhookAt:
		if at.Sub(start) > 150*time.Millisecond {
			t.Fatalf("the webhook waited %v for a slow producer", at.Sub(start))
		}
	default:
		t.Fatal("the webhook was not called")
	}
}

type producerFunc func()

func (f producerFunc) Produce(string, []byte, string, string) error { f(); return nil }
func (f producerFunc) CreateGlobalQueues() error                    { return nil }
