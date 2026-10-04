package whatsmeow_service

import (
	"sync"
	"testing"

	"github.com/lucasgiovannibr/whatygo/pkg/config"
	instance_model "github.com/lucasgiovannibr/whatygo/pkg/instance/model"
	logger_wrapper "github.com/lucasgiovannibr/whatygo/pkg/logger"
)

type recordingProducer struct {
	mu     sync.Mutex
	queues []string
}

func (r *recordingProducer) Produce(queueName string, _ []byte, _ string, _ string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.queues = append(r.queues, queueName)
	return nil
}
func (r *recordingProducer) CreateGlobalQueues() error { return nil }

func (r *recordingProducer) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.queues)
}

// End of the chain: a webhook subscriber of CONNECTION receives the new events, one
// that only subscribed to MESSAGE does not.
func TestOperationalEventsAreDeliveredToConnectionSubscribers(t *testing.T) {
	cfg := &config.Config{LogDirectory: t.TempDir()}
	webhook := &recordingProducer{}
	w := &whatsmeowService{config: cfg, loggerWrapper: logger_wrapper.NewLoggerManagerForTest(t, cfg), webhookProducer: webhook}

	payload := func(event string) []byte { return []byte(`{"event":"` + event + `","data":{}}`) }

	subscriber := &instance_model.Instance{Id: "a", Events: "CONNECTION", Webhook: "http://example.invalid/hook"}
	other := &instance_model.Instance{Id: "b", Events: "MESSAGE", Webhook: "http://example.invalid/hook"}

	for _, ev := range []string{"ReachoutTimelock", "StreamError", "ClientOutdated"} {
		w.CallWebhook(subscriber, "a."+ev, payload(ev))
	}
	if got := webhook.count(); got != 3 {
		t.Fatalf("a CONNECTION subscriber must receive all 3 events, got %d", got)
	}

	before := webhook.count()
	for _, ev := range []string{"ReachoutTimelock", "StreamError", "ClientOutdated"} {
		w.CallWebhook(other, "b."+ev, payload(ev))
	}
	if webhook.count() != before {
		t.Fatal("a subscriber that did not ask for CONNECTION must not receive them")
	}
}
