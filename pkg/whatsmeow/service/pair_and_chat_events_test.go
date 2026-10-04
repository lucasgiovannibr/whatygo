package whatsmeow_service

import (
	"errors"
	"testing"
	"time"

	"github.com/lucasgiovannibr/whatygo/pkg/config"
	instance_model "github.com/lucasgiovannibr/whatygo/pkg/instance/model"
	"github.com/lucasgiovannibr/whatygo/pkg/internal/event_types"
	logger_wrapper "github.com/lucasgiovannibr/whatygo/pkg/logger"
	"go.mau.fi/whatsmeow/proto/waSyncAction"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

var (
	testChat = types.NewJID("5511999999999", types.DefaultUserServer)
	testTime = time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
)

func TestPairAndChatEventData(t *testing.T) {
	cases := []struct {
		name    string
		evt     interface{}
		want    string
		checkFn func(map[string]interface{}) bool
	}{
		{"pair error", &events.PairError{ID: testChat, Platform: "chrome", Error: errors.New("bad adv signature")}, "PairError",
			func(d map[string]interface{}) bool {
				return d["error"] == "bad adv signature" && d["platform"] == "chrome" && d["id"] == testChat.String()
			}},
		{"qr without multidevice", &events.QRScannedWithoutMultidevice{}, "QRScannedWithoutMultidevice",
			func(d map[string]interface{}) bool { return d["message"] != "" }},
		{"cat refresh error", &events.CATRefreshError{Error: errors.New("boom")}, "CATRefreshError",
			func(d map[string]interface{}) bool { return d["error"] == "boom" }},
		{"mute", &events.Mute{JID: testChat, Timestamp: testTime, Action: &waSyncAction.MuteAction{Muted: proto.Bool(true), MuteEndTimestamp: proto.Int64(1790000000)}}, "Mute",
			func(d map[string]interface{}) bool {
				return d["muted"] == true && d["muteEndTimestamp"] == int64(1790000000) && d["jid"] == testChat.String() && d["timestamp"] == testTime.Format(time.RFC3339)
			}},
		{"pin", &events.Pin{JID: testChat, Timestamp: testTime, Action: &waSyncAction.PinAction{Pinned: proto.Bool(true)}}, "Pin",
			func(d map[string]interface{}) bool { return d["pinned"] == true }},
		{"unpin", &events.Pin{JID: testChat, Timestamp: testTime, Action: &waSyncAction.PinAction{Pinned: proto.Bool(false)}}, "Pin",
			func(d map[string]interface{}) bool { return d["pinned"] == false }},
		{"star", &events.Star{ChatJID: testChat, MessageID: "M1", IsFromMe: true, Timestamp: testTime, Action: &waSyncAction.StarAction{Starred: proto.Bool(true)}}, "Star",
			func(d map[string]interface{}) bool {
				return d["starred"] == true && d["messageId"] == "M1" && d["isFromMe"] == true && d["chatJid"] == testChat.String()
			}},
		{"mark as read", &events.MarkChatAsRead{JID: testChat, Timestamp: testTime, Action: &waSyncAction.MarkChatAsReadAction{Read: proto.Bool(true)}}, "MarkChatAsRead",
			func(d map[string]interface{}) bool { return d["read"] == true }},
		{"clear chat", &events.ClearChat{JID: testChat, Timestamp: testTime, DeleteMedia: true}, "ClearChat",
			func(d map[string]interface{}) bool { return d["deleteMedia"] == true }},
		{"delete chat", &events.DeleteChat{JID: testChat, Timestamp: testTime}, "DeleteChat",
			func(d map[string]interface{}) bool { return d["jid"] == testChat.String() && d["deleteMedia"] == false }},
		{"delete for me", &events.DeleteForMe{ChatJID: testChat, MessageID: "M2", Timestamp: testTime, Action: &waSyncAction.DeleteMessageForMeAction{DeleteMedia: proto.Bool(true)}}, "DeleteForMe",
			func(d map[string]interface{}) bool { return d["messageId"] == "M2" && d["deleteMedia"] == true }},
		{"unarchive setting", &events.UnarchiveChatsSetting{Timestamp: testTime, Action: &waSyncAction.UnarchiveChatsSetting{UnarchiveChats: proto.Bool(true)}}, "UnarchiveChatsSetting",
			func(d map[string]interface{}) bool { return d["unarchiveChats"] == true }},
		{"status mute", &events.UserStatusMute{JID: testChat, Timestamp: testTime, Action: &waSyncAction.UserStatusMuteAction{Muted: proto.Bool(true)}}, "UserStatusMute",
			func(d map[string]interface{}) bool { return d["muted"] == true }},
	}

	for _, c := range cases {
		name, data, publish, handled := pairAndChatEventData(c.evt)
		if !handled || !publish || name != c.want {
			t.Errorf("%s: name=%q publish=%v handled=%v", c.name, name, publish, handled)
			continue
		}
		if !c.checkFn(data) {
			t.Errorf("%s: unexpected data %#v", c.name, data)
		}
	}
}

// A nil action must not panic (the getters are nil-safe): whatsmeow can emit an
// event without an action.
func TestPairAndChatEventDataToleratesMissingActions(t *testing.T) {
	for _, evt := range []interface{}{&events.Mute{}, &events.Pin{}, &events.Star{}, &events.MarkChatAsRead{}, &events.DeleteForMe{}, &events.UnarchiveChatsSetting{}, &events.UserStatusMute{}} {
		if _, _, _, handled := pairAndChatEventData(evt); !handled {
			t.Errorf("%T was not handled", evt)
		}
	}
}

// After pairing the phone replays its whole app state; those are not changes.
func TestFullSyncChatEventsAreNotPublished(t *testing.T) {
	for _, evt := range []interface{}{
		&events.Mute{FromFullSync: true},
		&events.Pin{FromFullSync: true},
		&events.Star{FromFullSync: true},
		&events.MarkChatAsRead{FromFullSync: true},
		&events.ClearChat{FromFullSync: true},
		&events.DeleteChat{FromFullSync: true},
		&events.DeleteForMe{FromFullSync: true},
		&events.UnarchiveChatsSetting{FromFullSync: true},
		&events.UserStatusMute{FromFullSync: true},
	} {
		if _, _, publish, handled := pairAndChatEventData(evt); !handled || publish {
			t.Errorf("%T from a full sync must be handled but not published (handled=%v publish=%v)", evt, handled, publish)
		}
	}
}

func TestUnrelatedEventsAreNotHandledHere(t *testing.T) {
	if _, _, _, handled := pairAndChatEventData(&events.Connected{}); handled {
		t.Fatal("other events must fall through")
	}
}

func TestHandlerPublishesPairErrorAndChatEvents(t *testing.T) {
	mycli, capture := newHandlerClient(t, &config.Config{})

	mycli.myEventHandler(&events.PairError{ID: testChat, Error: errors.New("failed")})
	p := waitPublished(t, capture)
	if p.queue != "inst-1.pairerror" || p.body["event"] != "PairError" || p.body["data"].(map[string]interface{})["error"] != "failed" {
		t.Fatalf("unexpected publication: %#v", p)
	}

	mycli.myEventHandler(&events.Pin{JID: testChat, Timestamp: testTime, Action: &waSyncAction.PinAction{Pinned: proto.Bool(true)}})
	p = waitPublished(t, capture)
	if p.queue != "inst-1.pin" || p.body["data"].(map[string]interface{})["pinned"] != true {
		t.Fatalf("unexpected publication: %#v", p)
	}

	// full sync: nothing must be published
	mycli.myEventHandler(&events.Pin{JID: testChat, FromFullSync: true})
	select {
	case p := <-capture.published:
		t.Fatalf("a full-sync event must not be published: %#v", p)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestPairAndChatEventGroups(t *testing.T) {
	want := map[string]string{
		"PairError":                   event_types.QRCODE,
		"QRScannedWithoutMultidevice": event_types.QRCODE,
		"CATRefreshError":             event_types.CONNECTION,
		"Mute":                        event_types.CHAT_PRESENCE,
		"Pin":                         event_types.CHAT_PRESENCE,
		"Star":                        event_types.CHAT_PRESENCE,
		"MarkChatAsRead":              event_types.CHAT_PRESENCE,
		"ClearChat":                   event_types.CHAT_PRESENCE,
		"DeleteChat":                  event_types.CHAT_PRESENCE,
		"DeleteForMe":                 event_types.CHAT_PRESENCE,
		"UnarchiveChatsSetting":       event_types.CHAT_PRESENCE,
		"UserStatusMute":              event_types.CHAT_PRESENCE,
	}
	for name, group := range want {
		if got := globalEventTypeFor(name); got != group {
			t.Errorf("globalEventTypeFor(%q) = %q, want %q", name, got, group)
		}
	}
}

// End of the chain: each group's subscribers receive their events through the real
// CallWebhook, and subscribers of another group do not.
func TestPairAndChatEventsReachTheRightSubscribers(t *testing.T) {
	cfg := &config.Config{LogDirectory: t.TempDir()}
	webhook := &recordingProducer{}
	w := &whatsmeowService{config: cfg, loggerWrapper: logger_wrapper.NewLoggerManagerForTest(t, cfg), webhookProducer: webhook}

	send := func(subscription, event string) bool {
		before := webhook.count()
		inst := &instance_model.Instance{Id: "a", Events: subscription, Webhook: "http://example.invalid/hook"}
		w.CallWebhook(inst, "a."+event, []byte(`{"event":"`+event+`","data":{}}`))
		return webhook.count() > before
	}

	for event, group := range map[string]string{"PairError": "QRCODE", "CATRefreshError": "CONNECTION", "Pin": "CHAT_PRESENCE", "DeleteForMe": "CHAT_PRESENCE"} {
		if !send(group, event) {
			t.Errorf("a %s subscriber must receive %s", group, event)
		}
		if send("MESSAGE", event) {
			t.Errorf("a MESSAGE-only subscriber must not receive %s", event)
		}
	}
}
