package instance_service

import (
	"strings"

	instance_model "github.com/lucasgiovannibr/whatygo/pkg/instance/model"
	event_types "github.com/lucasgiovannibr/whatygo/pkg/internal/event_types"
)

// applyConnectSettings mutates instance only for fields explicitly provided.
// Empty subscribe keeps existing Events; defaults to MESSAGE only when Events is empty.
// Empty producer strings keep existing values; send "disabled" or "false" to turn off.
//
// An empty webhookUrl must NOT clear the webhook: the bundled manager sends
// webhookUrl:"" whenever its field is blank, including when it merely reconnects an
// instance, so treating "" as "clear" would wipe the webhook on every reconnect (the
// same class of bug as #111). Clearing is explicit: "disabled" or "false" stores an
// empty webhook (delivery already ignores an empty one).
func applyConnectSettings(instance *instance_model.Instance, data *ConnectStruct) map[string]interface{} {
	updates := map[string]interface{}{}
	if instance == nil || data == nil {
		return updates
	}

	if len(data.Subscribe) > 0 {
		var subscribedEvents []string
		if data.Subscribe[0] == "ALL" {
			subscribedEvents = append(subscribedEvents, event_types.AllEventTypes...)
		} else {
			for _, arg := range data.Subscribe {
				if !event_types.IsEventType(arg) {
					continue
				}
				subscribedEvents = append(subscribedEvents, arg)
			}
		}
		eventString := strings.Join(subscribedEvents, ",")
		instance.Events = eventString
		updates["events"] = eventString
	} else if instance.Events == "" {
		instance.Events = event_types.MESSAGE
		updates["events"] = event_types.MESSAGE
	}

	switch {
	case isOffValue(data.WebhookUrl):
		instance.Webhook = ""
		updates["webhook"] = ""
	case data.WebhookUrl != "":
		instance.Webhook = data.WebhookUrl
		updates["webhook"] = data.WebhookUrl
	}
	if data.RabbitmqEnable != "" {
		instance.RabbitmqEnable = data.RabbitmqEnable
		updates["rabbitmq_enable"] = data.RabbitmqEnable
	}
	if data.NatsEnable != "" {
		instance.NatsEnable = data.NatsEnable
		updates["nats_enable"] = data.NatsEnable
	}
	if data.WebSocketEnable != "" {
		instance.WebSocketEnable = data.WebSocketEnable
		updates["web_socket_enable"] = data.WebSocketEnable
	}

	return updates
}

func splitSubscribedEvents(events string) []string {
	if events == "" {
		return []string{event_types.MESSAGE}
	}
	parts := strings.Split(events, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return []string{event_types.MESSAGE}
	}
	return out
}

// isOffValue reports whether a setting was given the explicit "turn off" value.
func isOffValue(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "disabled", "false":
		return true
	}
	return false
}
