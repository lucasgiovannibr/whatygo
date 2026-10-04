package whatsmeow_service

import (
	"testing"
	"time"

	"github.com/lucasgiovannibr/whatygo/pkg/config"
	instance_model "github.com/lucasgiovannibr/whatygo/pkg/instance/model"
	logger_wrapper "github.com/lucasgiovannibr/whatygo/pkg/logger"
	"github.com/lucasgiovannibr/whatygo/pkg/safemap"
)

func newCallEventService(t *testing.T, webhook *recordingProducer, instance *instance_model.Instance) *whatsmeowService {
	t.Helper()
	cfg := &config.Config{LogDirectory: t.TempDir()}
	w := &whatsmeowService{
		config:          cfg,
		loggerWrapper:   logger_wrapper.NewLoggerManagerForTest(t, cfg),
		webhookProducer: webhook,
		myClientPointer: safemap.New[*MyClient](),
	}
	w.myClientPointer.Set(instance.Id, clientFor(instance))
	return w
}

func waitForCount(t *testing.T, p *recordingProducer, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for p.count() < want {
		if time.Now().After(deadline) {
			t.Fatalf("got %d deliveries, want %d", p.count(), want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// The call engine reports through publishCallEvent: a CALL subscriber receives
// CallReady and CallEnded, one that did not subscribe to CALL does not.
func TestCallLifecycleEventsReachCallSubscribers(t *testing.T) {
	webhook := &recordingProducer{}
	subscriber := &instance_model.Instance{Id: "a", Events: "CALL", Webhook: "http://example.invalid/hook"}
	w := newCallEventService(t, webhook, subscriber)

	data := map[string]interface{}{"callId": "C1", "reason": "terminate"}
	w.publishCallEvent("a", "CallReady", data)
	w.publishCallEvent("a", "CallEnded", data)
	waitForCount(t, webhook, 2)

	other := &instance_model.Instance{Id: "b", Events: "MESSAGE", Webhook: "http://example.invalid/hook"}
	w.myClientPointer.Set("b", clientFor(other))
	w.publishCallEvent("b", "CallEnded", data)
	time.Sleep(100 * time.Millisecond)
	if webhook.count() != 2 {
		t.Fatalf("a subscriber that did not ask for CALL received the event (%d deliveries)", webhook.count())
	}
}

func TestCallLifecycleEventsAreCallEvents(t *testing.T) {
	for _, name := range []string{"CallReady", "CallEnded", "CallVideoState", "CallMediaStalled", "CallMediaResumed"} {
		if got := globalEventTypeFor(name); got != "CALL" {
			t.Errorf("globalEventTypeFor(%q) = %q, want CALL", name, got)
		}
	}
}

// A call that ends because its instance was stopped no longer has a running client,
// and there may be no repository either (as here): nothing to publish, and no panic.
func TestPublishCallEventOfAnInstanceThatIsGone(t *testing.T) {
	webhook := &recordingProducer{}
	w := newCallEventService(t, webhook, &instance_model.Instance{Id: "a"})
	w.publishCallEvent("gone", "CallEnded", map[string]interface{}{"callId": "C1"})
	time.Sleep(50 * time.Millisecond)
	if webhook.count() != 0 {
		t.Fatalf("published %d events for an unknown instance", webhook.count())
	}
}

// clientFor is a MyClient that works with the given instance record.
func clientFor(instance *instance_model.Instance) *MyClient {
	c := &MyClient{}
	c.setInst(instance)
	return c
}
