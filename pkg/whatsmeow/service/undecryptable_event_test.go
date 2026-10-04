package whatsmeow_service

import (
	"testing"
	"time"

	"github.com/lucasgiovannibr/whatygo/pkg/config"
	instance_model "github.com/lucasgiovannibr/whatygo/pkg/instance/model"
	logger_wrapper "github.com/lucasgiovannibr/whatygo/pkg/logger"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

func TestUndecryptableEventData(t *testing.T) {
	ts := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	evt := &events.UndecryptableMessage{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:    types.NewJID("120363000000000001", types.GroupServer),
				Sender:  types.NewJID("5511999990001", types.DefaultUserServer),
				IsGroup: true,
			},
			ID:        "3EB0AAAA",
			Timestamp: ts,
			PushName:  "Fulano",
		},
		IsUnavailable: true,
	}

	d := undecryptableEventData(evt)
	want := map[string]interface{}{
		"id": "3EB0AAAA", "chat": "120363000000000001@g.us", "sender": "5511999990001@s.whatsapp.net",
		"isGroup": true, "isFromMe": false, "timestamp": "2026-09-29T12:00:00Z", "pushName": "Fulano",
		"isUnavailable": true, "unavailableType": "", "decryptFailMode": "",
	}
	for k, v := range want {
		if d[k] != v {
			t.Errorf("%s = %#v, want %#v", k, d[k], v)
		}
	}
}

// An undecryptable message is delivered to MESSAGE subscribers (and only them).
func TestUndecryptableMessageReachesMessageSubscribers(t *testing.T) {
	if got := globalEventTypeFor("UndecryptableMessage"); got != "MESSAGE" {
		t.Fatalf("global group = %q, want MESSAGE", got)
	}

	cfg := &config.Config{LogDirectory: t.TempDir()}
	webhook := &recordingProducer{}
	w := &whatsmeowService{config: cfg, loggerWrapper: logger_wrapper.NewLoggerManagerForTest(t, cfg), webhookProducer: webhook}

	send := func(subscription string) bool {
		before := webhook.count()
		inst := &instance_model.Instance{Id: "a", Events: subscription, Webhook: "http://example.invalid/hook"}
		w.CallWebhook(inst, "a.undecryptablemessage", []byte(`{"event":"UndecryptableMessage","data":{}}`))
		return webhook.count() > before
	}
	if !send("MESSAGE") {
		t.Error("a MESSAGE subscriber must receive it")
	}
	if send("CONNECTION") {
		t.Error("a CONNECTION-only subscriber must not receive it")
	}
}
