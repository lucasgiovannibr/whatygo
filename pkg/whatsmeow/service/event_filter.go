package whatsmeow_service

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"

	instance_model "github.com/lucasgiovannibr/whatygo/pkg/instance/model"
	"github.com/lucasgiovannibr/whatygo/pkg/internal/event_types"
	"github.com/lucasgiovannibr/whatygo/pkg/utils"
)

// Which events go where is decided in three small, pure steps so that the expensive part
// of an event (downloading its media, asking WhatsApp for the group, serializing it) can
// be skipped when nobody would receive it:
//
//   - eventSubscribed: does the instance's subscription list include this event?
//   - instanceHasOutput: does the instance have anywhere to send events to?
//   - globalQueueWants: do the global RabbitMQ/NATS settings take this event?
//
// EventWanted combines them; CallWebhook uses the first.

// parseSubscriptions turns the instance's "A,B,C" event list into the valid event types
// it names, without duplicates. Unknown names are dropped (they used to be logged as a
// warning on every single event).
func parseSubscriptions(events string) []string {
	var subscriptions []string
	for _, arg := range strings.Split(events, ",") {
		if !event_types.IsEventType(arg) {
			continue
		}
		if !utils.Find(subscriptions, arg) {
			subscriptions = append(subscriptions, arg)
		}
	}
	return subscriptions
}

// subscriptionsCache remembers the parsed subscription list of each distinct "A,B,C" string:
// EventWanted and CallWebhook asked for it on every event (a split and a validation of each
// name, twice per event). The lists are shared and read-only. It is bounded: a table that
// outgrows maxCachedSubscriptions (a server with that many different lists) is emptied.
const maxCachedSubscriptions = 4096

var (
	subscriptionsMu    sync.RWMutex
	subscriptionsCache = map[string][]string{}
)

func cachedSubscriptions(events string) []string {
	subscriptionsMu.RLock()
	subs, ok := subscriptionsCache[events]
	subscriptionsMu.RUnlock()
	if ok {
		return subs
	}
	subs = parseSubscriptions(events)
	subscriptionsMu.Lock()
	if len(subscriptionsCache) >= maxCachedSubscriptions {
		subscriptionsCache = map[string][]string{}
	}
	subscriptionsCache[events] = subs
	subscriptionsMu.Unlock()
	return subs
}

// chatFallback is the subscription that also delivers message-like events of groups and
// newsletters to someone who did not subscribe to MESSAGE (or SEND_MESSAGE, READ_RECEIPT).
func chatFallback(subscriptions []string, chat string) bool {
	switch {
	case strings.HasSuffix(chat, "@g.us"):
		return contains(subscriptions, "GROUP")
	case strings.HasSuffix(chat, "@newsletter"):
		return contains(subscriptions, "NEWSLETTER")
	}
	return false
}

// eventSubscribed reports whether a subscription list delivers an event. chat is the
// chat JID the event belongs to ("" when the event has none or it is not known).
func eventSubscribed(subscriptions []string, eventType, chat string) bool {
	if contains(subscriptions, "ALL") {
		return true
	}
	switch eventType {
	case "Message":
		return contains(subscriptions, "MESSAGE") || chatFallback(subscriptions, chat)
	case "SendMessage":
		return contains(subscriptions, "SEND_MESSAGE") || chatFallback(subscriptions, chat)
	case "Receipt":
		return contains(subscriptions, "READ_RECEIPT") || chatFallback(subscriptions, chat)
	case "ButtonClick":
		return contains(subscriptions, "BUTTON_CLICK") || contains(subscriptions, "MESSAGE")
	}
	group := globalEventTypeFor(eventType)
	return group != "" && contains(subscriptions, group)
}

// instanceHasOutput reports whether sendToQueueOrWebhook would hand an event to at least
// one output of the instance.
func instanceHasOutput(instance *instance_model.Instance) bool {
	on := func(v string) bool { return v == "enabled" || v == "true" }
	return on(instance.RabbitmqEnable) || on(instance.NatsEnable) || on(instance.WebSocketEnable) ||
		(instance.Webhook != "" && instance.Webhook != "disabled")
}

// globalQueueWants reports whether the global RabbitMQ or NATS settings take an event
// (the same rules SendToGlobalQueues applies).
func (w *whatsmeowService) globalQueueWants(eventType string) bool {
	if w.config == nil {
		return false
	}
	group := globalEventTypeFor(eventType)

	if w.config.AmqpGlobalEnabled {
		if len(w.config.AmqpSpecificEvents) > 0 {
			if utils.Find(w.config.AmqpSpecificEvents, eventType) {
				return true
			}
		} else if group != "" && utils.Find(w.config.AmqpGlobalEvents, group) {
			return true
		}
	}
	if w.config.NatsGlobalEnabled && group != "" && utils.Find(w.config.NatsGlobalEvents, group) {
		return true
	}
	return false
}

// EventWanted reports whether anyone would receive an event of this type for the
// instance: through its own outputs and subscriptions, or through the global queues. When
// it is false, building the payload (downloading media, asking WhatsApp for the group,
// serializing) is wasted work. chat may be "" when the event has no chat.
func (w *whatsmeowService) EventWanted(instance *instance_model.Instance, eventType, chat string) bool {
	if instance == nil {
		return false
	}
	if instanceHasOutput(instance) && eventSubscribed(cachedSubscriptions(instance.Events), eventType, chat) {
		return true
	}
	return w.globalQueueWants(eventType)
}

// eventEnvelope is the little CallWebhook needs from a payload that can be tens of
// megabytes (media as base64): the event name and the chat. Decoding into this skips the
// rest without building it (the whole payload used to be parsed into a map, string by
// string, only to read two fields).
type eventEnvelope struct {
	Event string `json:"event"`
	Data  struct {
		Chat string `json:"Chat"` // Receipt
		Info struct {
			Chat string `json:"Chat"` // Message, SendMessage
		} `json:"Info"`
	} `json:"data"`
}

// chat is where the event happened, from whichever field the event type uses.
func (e *eventEnvelope) chat() string {
	if e.Data.Info.Chat != "" {
		return e.Data.Info.Chat
	}
	return e.Data.Chat
}

// readEnvelope extracts the envelope. A payload whose "data" is not an object (some
// events carry a list or a string) still yields its event name.
func readEnvelope(jsonData []byte) (*eventEnvelope, bool) {
	var env eventEnvelope
	err := json.Unmarshal(jsonData, &env)
	var typeErr *json.UnmarshalTypeError
	if err != nil && !errors.As(err, &typeErr) {
		return nil, false
	}
	if env.Event == "" {
		return nil, false
	}
	return &env, true
}
