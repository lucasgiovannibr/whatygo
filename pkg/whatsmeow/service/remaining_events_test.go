package whatsmeow_service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lucasgiovannibr/whatygo/pkg/config"
	instance_model "github.com/lucasgiovannibr/whatygo/pkg/instance/model"
	"github.com/lucasgiovannibr/whatygo/pkg/internal/event_types"
	logger_wrapper "github.com/lucasgiovannibr/whatygo/pkg/logger"
	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

func TestRemainingEventMapping(t *testing.T) {
	jid := types.NewJID("5531999990001", types.DefaultUserServer)
	ts := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	call := types.BasicCallMeta{From: jid, Timestamp: ts, CallCreator: jid, CallID: "CALL1"}

	cases := []struct {
		name string
		evt  interface{}
		want string
		key  string
		val  interface{}
	}{
		{"blocklist", &events.Blocklist{Action: "modify", DHash: "d2", PrevDHash: "d1"}, "Blocklist", "action", "modify"},
		{"privacy", &events.PrivacySettings{LastSeenChanged: true, NewSettings: types.PrivacySettings{LastSeen: types.PrivacySettingContacts}}, "PrivacySettings", "settings", nil},
		{"business name", &events.BusinessName{JID: jid, OldBusinessName: "A", NewBusinessName: "B"}, "BusinessName", "newBusinessName", "B"},
		{"call preaccept", &events.CallPreAccept{BasicCallMeta: call, CallRemoteMeta: types.CallRemoteMeta{RemotePlatform: "android"}}, "CallPreAccept", "remotePlatform", "android"},
		{"call transport", &events.CallTransport{BasicCallMeta: call}, "CallTransport", "callId", "CALL1"},
		{"call reject", &events.CallReject{BasicCallMeta: call}, "CallReject", "callId", "CALL1"},
		{"unknown call", &events.UnknownCallEvent{Node: &waBinary.Node{Tag: "weird", Attrs: waBinary.Attrs{"id": "x", "from": jid}}}, "UnknownCallEvent", "tag", "weird"},
		{"media retry", &events.MediaRetry{MessageID: "M1", ChatID: jid, Timestamp: ts, Ciphertext: []byte{1}}, "MediaRetry", "messageId", "M1"},
		{"newsletter update", &events.NewsletterLiveUpdate{JID: types.NewJID("1203", types.NewsletterServer), Time: ts}, "NewsletterLiveUpdate", "jid", "1203@newsletter"},
		{"newsletter mute", &events.NewsletterMuteChange{ID: types.NewJID("1203", types.NewsletterServer), Mute: "on"}, "NewsletterMuteChange", "mute", "on"},
		{"offline preview", &events.OfflineSyncPreview{Total: 10, Messages: 7}, "OfflineSyncPreview", "total", 10},
	}
	for _, tc := range cases {
		name, data, publish, handled := remainingEventData(tc.evt)
		if !handled || !publish || name != tc.want {
			t.Errorf("%s: name=%q publish=%v handled=%v, want %q published", tc.name, name, publish, handled, tc.want)
			continue
		}
		if tc.val != nil && data[tc.key] != tc.val {
			t.Errorf("%s: data[%q] = %#v, want %#v", tc.name, tc.key, data[tc.key], tc.val)
		}
	}
}

func TestRemainingEventDetails(t *testing.T) {
	jid := types.NewJID("5531999990001", types.DefaultUserServer)

	_, data, _, _ := remainingEventData(&events.Blocklist{Changes: []events.BlocklistChange{{JID: jid, Action: events.BlocklistChangeActionBlock}}})
	changes := data["changes"].([]map[string]interface{})
	if len(changes) != 1 || changes[0]["jid"] != jid.String() || changes[0]["action"] != "block" {
		t.Fatalf("changes = %#v", changes)
	}

	_, data, _, _ = remainingEventData(&events.MediaRetry{MessageID: "M1", Ciphertext: []byte{1, 2}, Error: &events.MediaRetryError{Code: 2}})
	if data["errorCode"] != 2 || data["hasCiphertext"] != true {
		t.Fatalf("media retry = %#v", data)
	}
	if _, leaked := data["ciphertext"]; leaked {
		t.Fatal("the ciphertext must not be published")
	}

	_, data, _, _ = remainingEventData(&events.PrivacySettings{OnlineChanged: true})
	if data["changed"].(map[string]interface{})["online"] != true || data["changed"].(map[string]interface{})["lastSeen"] != false {
		t.Fatalf("changed = %#v", data["changed"])
	}

	// an unknown call node without content must not panic
	if _, _, _, handled := remainingEventData(&events.UnknownCallEvent{}); !handled {
		t.Fatal("handled")
	}

	if _, _, _, handled := remainingEventData(&events.Connected{}); handled {
		t.Fatal("other events must fall through")
	}
}

func TestInternalEventsAreHandledButNeverPublished(t *testing.T) {
	for _, evt := range []interface{}{&events.RotateADVSecret{OldSecret: "old", NewSecret: "new"}, &events.ManualLoginReconnect{}} {
		if _, _, publish, handled := remainingEventData(evt); !handled || publish {
			t.Errorf("%T: handled=%v publish=%v, want handled and not published", evt, handled, publish)
		}
	}
}

// RotateADVSecret used to fall into the generic "Unhandled event ... %+v" log line, which wrote
// the session's old and new ADV secret into the instance log.
func TestRotateADVSecretIsNotLoggedWithItsContent(t *testing.T) {
	cfg := &config.Config{}
	mycli, capture := newHandlerClient(t, cfg)

	mycli.myEventHandler(&events.RotateADVSecret{OldSecret: "OLD-SECRET-VALUE", NewSecret: "NEW-SECRET-VALUE"})

	select {
	case p := <-capture.published:
		t.Fatalf("must not be published: %#v", p)
	case <-time.After(150 * time.Millisecond):
	}

	mycli.loggerWrapper.GetLogger("inst-1").Flush() // the log file is written by a goroutine
	logs, _ := os.ReadFile(filepath.Join(cfg.LogDirectory, "inst-1", "instance.log"))
	if strings.Contains(string(logs), "SECRET-VALUE") {
		t.Fatalf("the ADV secrets were written to the log:\n%s", logs)
	}
	if !strings.Contains(string(logs), "ADV secret was rotated") {
		t.Fatalf("expected a log line about the rotation:\n%s", logs)
	}
}

func TestHandlerPublishesTheRemainingEvents(t *testing.T) {
	mycli, capture := newHandlerClient(t, &config.Config{})

	mycli.myEventHandler(&events.OfflineSyncPreview{Total: 5, Messages: 5})
	p := waitPublished(t, capture)
	if p.queue != "inst-1.offlinesyncpreview" || p.body["event"] != "OfflineSyncPreview" || p.body["data"].(map[string]interface{})["total"] != float64(5) {
		t.Fatalf("unexpected publication: %#v", p)
	}

	mycli.myEventHandler(&events.Blocklist{Action: "modify"})
	p = waitPublished(t, capture)
	if p.queue != "inst-1.blocklist" || p.body["event"] != "Blocklist" {
		t.Fatalf("unexpected publication: %#v", p)
	}
}

// Both routing switches must agree on where each new event goes, and a subscriber of
// another group must not receive it.
func TestRemainingEventGroupsAndDelivery(t *testing.T) {
	want := map[string]string{
		"Blocklist":            event_types.CONTACT,
		"PrivacySettings":      event_types.CONTACT,
		"BusinessName":         event_types.CONTACT,
		"CallPreAccept":        event_types.CALL,
		"CallTransport":        event_types.CALL,
		"CallReject":           event_types.CALL,
		"UnknownCallEvent":     event_types.CALL,
		"MediaRetry":           event_types.MESSAGE,
		"NewsletterLiveUpdate": event_types.NEWSLETTER,
		"NewsletterMuteChange": event_types.NEWSLETTER,
		"OfflineSyncPreview":   event_types.CONNECTION,
	}

	cfg := &config.Config{LogDirectory: t.TempDir()}
	webhook := &recordingProducer{}
	w := &whatsmeowService{config: cfg, loggerWrapper: logger_wrapper.NewLoggerManagerForTest(t, cfg), webhookProducer: webhook}
	send := func(subscription, event string) bool {
		before := webhook.count()
		inst := &instance_model.Instance{Id: "a", Events: subscription, Webhook: "http://example.invalid/hook"}
		w.CallWebhook(inst, "a."+event, []byte(`{"event":"`+event+`","data":{}}`))
		return webhook.count() > before
	}

	for name, group := range want {
		if got := globalEventTypeFor(name); got != group {
			t.Errorf("globalEventTypeFor(%q) = %q, want %q", name, got, group)
		}
		if !send(group, name) {
			t.Errorf("a %s subscriber must receive %s", group, name)
		}
		other := event_types.LABEL
		if group == event_types.LABEL {
			other = event_types.PICTURE
		}
		if send(other, name) {
			t.Errorf("a %s-only subscriber must not receive %s", other, name)
		}
	}
}
