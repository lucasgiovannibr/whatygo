package whatsmeow_service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lucasgiovannibr/whatygo/pkg/config"
	instance_model "github.com/lucasgiovannibr/whatygo/pkg/instance/model"
)

func TestEventSubscribed(t *testing.T) {
	type tc struct {
		subs  string
		event string
		chat  string
		want  bool
	}
	cases := []tc{
		{"ALL", "AnythingAtAll", "", true},
		{"MESSAGE", "Message", "5511@s.whatsapp.net", true},
		{"MESSAGE", "Message", "120363@g.us", true},
		{"GROUP", "Message", "120363@g.us", true},          // group fallback
		{"GROUP", "Message", "5511@s.whatsapp.net", false}, // ... not for private chats
		{"NEWSLETTER", "Message", "123@newsletter", true},
		{"NEWSLETTER", "Message", "120363@g.us", false},
		{"SEND_MESSAGE", "SendMessage", "5511@s.whatsapp.net", true},
		{"GROUP", "SendMessage", "120363@g.us", true},
		{"MESSAGE", "SendMessage", "5511@s.whatsapp.net", false},
		{"READ_RECEIPT", "Receipt", "", true},
		{"GROUP", "Receipt", "120363@g.us", true},
		{"MESSAGE", "Receipt", "5511@s.whatsapp.net", false},
		{"PRESENCE", "Presence", "", true},
		{"MESSAGE", "Presence", "", false},
		{"MESSAGE", "UndecryptableMessage", "", true},
		{"MESSAGE", "MediaRetry", "", true},
		{"HISTORY_SYNC", "HistorySync", "", true},
		{"CHAT_PRESENCE", "Archive", "", true},
		{"CALL", "CallOffer", "", true},
		{"CALL", "CallEnded", "", true},
		{"CONNECTION", "Connected", "", true},
		{"CONNECTION", "ReachoutTimelock", "", true},
		{"LABEL", "LabelEdit", "", true},
		{"CONTACT", "PushName", "", true},
		{"PICTURE", "Picture", "", true},
		{"USER_ABOUT", "UserAbout", "", true},
		{"GROUP", "JoinedGroup", "", true},
		{"NEWSLETTER", "NewsletterJoin", "", true},
		{"QRCODE", "QRCode", "", true},
		{"QRCODE", "PasskeyRequest", "", true},
		{"BUTTON_CLICK", "ButtonClick", "", true},
		{"MESSAGE", "ButtonClick", "", true}, // a MESSAGE subscriber also gets button clicks
		{"CONNECTION", "ButtonClick", "", false},
		{"MESSAGE", "SomethingUnknown", "", false},
		{"", "Message", "", false}, // an empty list delivers nothing
		{"NOT_AN_EVENT", "Message", "", false},
	}
	for _, c := range cases {
		got := eventSubscribed(parseSubscriptions(c.subs), c.event, c.chat)
		if got != c.want {
			t.Errorf("subscriptions %q, event %q, chat %q: got %v, want %v", c.subs, c.event, c.chat, got, c.want)
		}
	}
}

func TestParseSubscriptionsDropsUnknownAndDuplicates(t *testing.T) {
	got := parseSubscriptions("MESSAGE,NOPE,MESSAGE,GROUP,")
	if strings.Join(got, ",") != "MESSAGE,GROUP" {
		t.Fatalf("got %v", got)
	}
}

func TestReadEnvelope(t *testing.T) {
	env, ok := readEnvelope([]byte(`{"event":"Message","data":{"Info":{"Chat":"120363@g.us","ID":"x"},"Message":{"base64":"AAAA"}},"instanceId":"i"}`))
	if !ok || env.Event != "Message" || env.chat() != "120363@g.us" {
		t.Fatalf("message envelope: %+v ok=%v", env, ok)
	}

	env, ok = readEnvelope([]byte(`{"event":"Receipt","data":{"Chat":"5511@s.whatsapp.net","MessageIDs":["a"]}}`))
	if !ok || env.chat() != "5511@s.whatsapp.net" {
		t.Fatalf("receipt envelope: %+v ok=%v", env, ok)
	}

	// "data" that is not an object must not hide the event name.
	env, ok = readEnvelope([]byte(`{"event":"Weird","data":["a","b"]}`))
	if !ok || env.Event != "Weird" {
		t.Fatalf("a non-object data must still give the event, got %+v ok=%v", env, ok)
	}

	for _, bad := range []string{``, `not json`, `{"data":{}}`, `{"event":""}`} {
		if _, ok := readEnvelope([]byte(bad)); ok {
			t.Errorf("%q must not yield an envelope", bad)
		}
	}
}

func TestEventWanted(t *testing.T) {
	cfg := &config.Config{LogDirectory: t.TempDir()}
	w := &whatsmeowService{config: cfg}

	withWebhook := &instance_model.Instance{Id: "a", Events: "MESSAGE", Webhook: "http://example.invalid/h"}
	if !w.EventWanted(withWebhook, "Message", "x@s.whatsapp.net") {
		t.Fatal("a subscribed instance with a webhook wants Message")
	}
	if w.EventWanted(withWebhook, "Presence", "") {
		t.Fatal("not subscribed to PRESENCE")
	}

	noOutput := &instance_model.Instance{Id: "b", Events: "ALL"}
	if w.EventWanted(noOutput, "Message", "") {
		t.Fatal("an instance with nowhere to send events wants nothing")
	}
	disabled := &instance_model.Instance{Id: "c", Events: "ALL", Webhook: "disabled"}
	if w.EventWanted(disabled, "Message", "") {
		t.Fatal("a disabled webhook is not an output")
	}
	ws := &instance_model.Instance{Id: "d", Events: "MESSAGE", WebSocketEnable: "enabled"}
	if !w.EventWanted(ws, "Message", "") {
		t.Fatal("the websocket is an output")
	}
	if w.EventWanted(nil, "Message", "") {
		t.Fatal("nil instance")
	}

	// Global queues take events the instance itself did not subscribe to.
	w.config = &config.Config{AmqpGlobalEnabled: true, AmqpGlobalEvents: []string{"MESSAGE"}}
	if !w.EventWanted(noOutput, "Message", "") {
		t.Fatal("the global AMQP queue wants MESSAGE events")
	}
	if w.EventWanted(noOutput, "Presence", "") {
		t.Fatal("the global AMQP queue does not list PRESENCE")
	}
	w.config = &config.Config{AmqpGlobalEnabled: true, AmqpSpecificEvents: []string{"Message"}}
	if !w.EventWanted(noOutput, "Message", "") || w.EventWanted(noOutput, "Receipt", "") {
		t.Fatal("AMQP_SPECIFIC_EVENTS names exact event types")
	}
	w.config = &config.Config{NatsGlobalEnabled: true, NatsGlobalEvents: []string{"GROUP"}}
	if !w.EventWanted(noOutput, "GroupInfo", "") {
		t.Fatal("the global NATS subject wants GROUP events")
	}
}

// A message with 20 MB of media as base64: what CallWebhook has to look at to route it.
func mediaPayload() []byte {
	return []byte(`{"event":"Message","data":{"Info":{"Chat":"120363@g.us"},"Message":{"base64":"` +
		strings.Repeat("QUJD", 5*1024*1024) + `"}},"instanceId":"i"}`)
}

func BenchmarkReadEnvelope(b *testing.B) {
	payload := mediaPayload()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, ok := readEnvelope(payload); !ok {
			b.Fatal("no envelope")
		}
	}
}

// BenchmarkRoutingByFullParse is what CallWebhook used to do: parse the whole payload
// into a map to read two fields.
func BenchmarkRoutingByFullParse(b *testing.B) {
	payload := mediaPayload()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var data map[string]interface{}
		if err := json.Unmarshal(payload, &data); err != nil {
			b.Fatal(err)
		}
		_ = data["event"]
	}
}
