package whatsmeow_service

import (
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	instance_model "github.com/lucasgiovannibr/whatygo/pkg/instance/model"
)

func handleMessageOnce(t *testing.T, nobodyWants bool, evtFn func() *waE2E.Message) (data map[string]interface{}, dispatched bool) {
	t.Helper()
	mycli, capture := receiptClient(t)
	capture.nobodyWants = nobodyWants
	evt := privateMessage("M1", "hello")
	if evtFn != nil {
		evt.Message = evtFn()
	}
	postMap := map[string]interface{}{"data": evt}
	dispatched, _ = mycli.handleMessage(evt, postMap)
	data, _ = postMap["data"].(map[string]interface{})
	return data, dispatched
}

// A message that nobody receives (no subscription, no output, no global queue) used to be
// marshalled and unmarshalled into a map, whole, only to be thrown away at the end.
func TestMessageNobodyReceivesIsNotConvertedToAMap(t *testing.T) {
	data, _ := handleMessageOnce(t, true, nil)
	if data == nil || len(data) != 0 {
		t.Fatalf("an unwanted message must not be converted: %v", data)
	}

	wanted, dispatched := handleMessageOnce(t, false, nil)
	if !dispatched || len(wanted) == 0 {
		t.Fatalf("a wanted message is converted (dispatched=%v): %v", dispatched, wanted)
	}
	if _, ok := wanted["Info"]; !ok {
		t.Fatalf("the converted message carries its Info: %v", wanted)
	}
}

// The button click event is built from the converted message, so a click is still converted
// when nobody wants the plain Message event.
func TestAButtonClickIsStillConvertedWhenMessageIsNotWanted(t *testing.T) {
	data, _ := handleMessageOnce(t, true, func() *waE2E.Message {
		return &waE2E.Message{ButtonsResponseMessage: &waE2E.ButtonsResponseMessage{
			SelectedButtonID: proto.String("b1"),
			Response:         &waE2E.ButtonsResponseMessage_SelectedDisplayText{SelectedDisplayText: "Yes"},
		}}
	})
	if _, ok := data["Info"]; !ok {
		t.Fatalf("a button click needs the converted message: %v", data)
	}
}

func TestUnwantedMessageAllocatesFarLessThanAWantedOne(t *testing.T) {
	run := func(nobodyWants bool) float64 {
		mycli, capture := receiptClient(t)
		capture.nobodyWants = nobodyWants
		return testing.AllocsPerRun(50, func() {
			evt := privateMessage("M1", "hello")
			mycli.handleMessage(evt, map[string]interface{}{"data": evt})
		})
	}
	unwanted, wanted := run(true), run(false)
	t.Logf("allocations per message: %.0f unwanted, %.0f wanted", unwanted, wanted)
	if unwanted > wanted/2 {
		t.Fatalf("allocations per message: %.0f unwanted vs %.0f wanted: the unwanted one must cost far less", unwanted, wanted)
	}
}

// The subscription list of an instance is parsed once per distinct string, not on every event.
func TestSubscriptionsAreCached(t *testing.T) {
	a := cachedSubscriptions("MESSAGE,CONNECTION,NOPE,MESSAGE")
	if len(a) != 2 || a[0] != "MESSAGE" || a[1] != "CONNECTION" {
		t.Fatalf("parsed: %v", a)
	}
	b := cachedSubscriptions("MESSAGE,CONNECTION,NOPE,MESSAGE")
	if &a[0] != &b[0] {
		t.Fatal("the second call must return the cached list")
	}
	if got := testing.AllocsPerRun(100, func() { cachedSubscriptions("MESSAGE,CONNECTION,NOPE,MESSAGE") }); got != 0 {
		t.Fatalf("a cached lookup must not allocate, got %.0f", got)
	}

	w := &whatsmeowService{}
	inst := &instance_model.Instance{Events: "MESSAGE", Webhook: "http://x"}
	if !w.EventWanted(inst, "Message", "5511999999999@s.whatsapp.net") || w.EventWanted(inst, "Presence", "") {
		t.Fatal("EventWanted must still follow the subscriptions")
	}
}

func TestSubscriptionCacheIsBounded(t *testing.T) {
	for i := 0; i < maxCachedSubscriptions+10; i++ {
		cachedSubscriptions("MESSAGE," + string(rune('A'+i%26)) + string(rune('a'+i/26%26)) + string(rune('0'+i/676)))
	}
	subscriptionsMu.RLock()
	n := len(subscriptionsCache)
	subscriptionsMu.RUnlock()
	if n > maxCachedSubscriptions {
		t.Fatalf("the cache must stay bounded, has %d", n)
	}
}
