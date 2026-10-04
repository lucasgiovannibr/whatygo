package whatsmeow_service

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/lucasgiovannibr/whatygo/pkg/config"
	instance_model "github.com/lucasgiovannibr/whatygo/pkg/instance/model"
	"github.com/lucasgiovannibr/whatygo/pkg/internal/event_types"
	logger_wrapper "github.com/lucasgiovannibr/whatygo/pkg/logger"
	"go.mau.fi/util/jsontime"
	"go.mau.fi/whatsmeow/types/events"
)

// webhookCapture stands in for the service: it only records what the event
// handler publishes.
type webhookCapture struct {
	WhatsmeowService
	published chan published
	// nobodyWants makes EventWanted answer false, like an instance without subscribers.
	nobodyWants bool
}

func (w *webhookCapture) EventWanted(*instance_model.Instance, string, string) bool {
	return !w.nobodyWants
}

type published struct {
	queue string
	body  map[string]interface{}
}

func (w *webhookCapture) CallWebhook(_ *instance_model.Instance, queueName string, jsonData []byte) {
	var body map[string]interface{}
	_ = json.Unmarshal(jsonData, &body)
	w.published <- published{queue: queueName, body: body}
}

func newHandlerClient(t *testing.T, cfg *config.Config) (*MyClient, *webhookCapture) {
	t.Helper()
	cfg.LogDirectory = t.TempDir()
	capture := &webhookCapture{published: make(chan published, 4)}
	mycli := &MyClient{
		service:       capture,
		userID:        "inst-1",
		token:         "tok",
		config:        cfg,
		loggerWrapper: logger_wrapper.NewLoggerManagerForTest(t, cfg),
	}
	mycli.setInst(&instance_model.Instance{Id: "inst-1", Name: "test", Token: "tok"})
	return mycli, capture
}

func waitPublished(t *testing.T, c *webhookCapture) published {
	t.Helper()
	select {
	case p := <-c.published:
		return p
	case <-time.After(2 * time.Second):
		t.Fatal("no webhook was published")
		return published{}
	}
}

func TestHandlerPublishesReachoutTimelock(t *testing.T) {
	mycli, capture := newHandlerClient(t, &config.Config{})
	end := time.Now().Add(48 * time.Hour).Truncate(time.Second)

	mycli.myEventHandler(&events.NotifyAccountReachoutTimelock{
		EnforcementType:     "TEST",
		IsActive:            true,
		TimeEnforcementEnds: jsontime.UnixString{Time: end},
	})

	st := mycli.reachoutTimelock.Load()
	if st == nil || !st.Active || st.EndsAt == nil || !st.EndsAt.Equal(end) {
		t.Fatalf("state was not stored: %#v", st)
	}

	p := waitPublished(t, capture)
	if p.queue != "inst-1.reachouttimelock" || p.body["event"] != "ReachoutTimelock" {
		t.Fatalf("unexpected publication: %#v", p)
	}
	data := p.body["data"].(map[string]interface{})
	if data["active"] != true || data["endsAt"] != end.Format(time.RFC3339) {
		t.Fatalf("unexpected data: %#v", data)
	}
	if p.body["instanceId"] != "inst-1" {
		t.Fatalf("the instance must be identified: %#v", p.body)
	}
}

func TestHandlerPublishesStreamError(t *testing.T) {
	mycli, capture := newHandlerClient(t, &config.Config{})

	mycli.myEventHandler(&events.StreamError{Code: "unknown-code"})

	if info := mycli.lastStreamError.Load(); info == nil || info.Code != "unknown-code" {
		t.Fatalf("state was not stored: %#v", info)
	}
	p := waitPublished(t, capture)
	if p.queue != "inst-1.streamerror" || p.body["event"] != "StreamError" || p.body["data"].(map[string]interface{})["code"] != "unknown-code" {
		t.Fatalf("unexpected publication: %#v", p)
	}
}

func TestHandlerClientOutdatedDropsTheVersionCache(t *testing.T) {
	cachedWebVersionMu.Lock()
	cachedWebVersion = &clientVersion{Major: 2, Minor: 3000, Patch: 1}
	cachedWebVersionAt = time.Now()
	cachedWebVersionMu.Unlock()

	mycli, capture := newHandlerClient(t, &config.Config{})
	mycli.myEventHandler(&events.ClientOutdated{})

	cachedWebVersionMu.Lock()
	stale := cachedWebVersion
	cachedWebVersionMu.Unlock()
	if stale != nil {
		t.Fatal("the refused version must not stay cached")
	}
	if mycli.clientOutdatedAt.Load() == 0 {
		t.Fatal("the refusal must be recorded")
	}

	p := waitPublished(t, capture)
	if p.body["event"] != "ClientOutdated" || p.body["data"].(map[string]interface{})["versionPinned"] != false {
		t.Fatalf("unexpected publication: %#v", p)
	}
}

func TestHandlerClientOutdatedReportsAPinnedVersion(t *testing.T) {
	mycli, capture := newHandlerClient(t, &config.Config{WhatsappVersionMajor: 2, WhatsappVersionMinor: 3000, WhatsappVersionPatch: 1})
	mycli.myEventHandler(&events.ClientOutdated{})

	p := waitPublished(t, capture)
	if p.body["data"].(map[string]interface{})["versionPinned"] != true {
		t.Fatalf("a version pinned by WHATSAPP_VERSION_* must be reported: %#v", p)
	}
}

// The new events must be deliverable through the same subscriptions as the other
// connection events.
func TestOperationalEventsBelongToTheConnectionGroup(t *testing.T) {
	for _, name := range []string{"ReachoutTimelock", "StreamError", "ClientOutdated"} {
		if got := globalEventTypeFor(name); got != event_types.CONNECTION {
			t.Errorf("globalEventTypeFor(%q) = %q, want CONNECTION", name, got)
		}
	}
}

// An event nobody would receive is not even serialized: CallWebhook is never reached.
func TestHandlerSkipsEventsNobodyWants(t *testing.T) {
	mycli, capture := newHandlerClient(t, &config.Config{})
	capture.nobodyWants = true

	mycli.myEventHandler(&events.StreamError{Code: "unknown-code"})

	select {
	case p := <-capture.published:
		t.Fatalf("an unwanted event must not be published, got %+v", p)
	case <-time.After(200 * time.Millisecond):
	}
}
