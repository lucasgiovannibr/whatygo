package whatsmeow_service

import (
	"testing"

	"github.com/lucasgiovannibr/whatygo/pkg/internal/event_types"
)

func TestGlobalEventTypeFor(t *testing.T) {
	cases := map[string]string{
		"Message":             event_types.MESSAGE,
		"Picture":             event_types.PICTURE,
		"UserAbout":           event_types.USER_ABOUT,
		"ButtonClick":         event_types.BUTTON_CLICK,
		"PasskeyRequest":      event_types.QRCODE,
		"PasskeyConfirmation": event_types.QRCODE,
		"PasskeyError":        event_types.QRCODE,
		"QRTimeout":           event_types.QRCODE,
		"Archive":             event_types.CHAT_PRESENCE,
		"KeepAliveTimeout":    event_types.CONNECTION,
		"KeepAliveRestored":   event_types.CONNECTION,
		"SomethingUnknown":    "",
	}
	for in, want := range cases {
		if got := globalEventTypeFor(in); got != want {
			t.Errorf("globalEventTypeFor(%q) = %q, want %q", in, got, want)
		}
	}
}

// Every event group that can be configured must be reachable from at least one
// whatsmeow event name, otherwise the subscription is silently dead (#193).
func TestEveryConfigurableGroupIsReachable(t *testing.T) {
	names := []string{"Message", "SendMessage", "Receipt", "Presence", "HistorySync", "ChatPresence",
		"CallOffer", "Connected", "LabelEdit", "Contact", "Picture", "UserAbout", "GroupInfo",
		"NewsletterJoin", "QRCode", "ButtonClick"}
	reached := map[string]bool{}
	for _, n := range names {
		reached[globalEventTypeFor(n)] = true
	}
	for _, g := range event_types.AllEventTypes {
		if !reached[g] {
			t.Errorf("event group %s is not produced by any event name", g)
		}
	}
}
