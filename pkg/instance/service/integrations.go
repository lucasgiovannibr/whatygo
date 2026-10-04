package instance_service

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/lucasgiovannibr/whatygo/pkg/apierror"
	event_types "github.com/lucasgiovannibr/whatygo/pkg/internal/event_types"
)

// IntegrationsStruct is the body of PUT /instance/{instanceId}/integrations.
//
// Field semantics match /instance/connect (see applyConnectSettings), except that this
// operation never starts the instance: it only stores the settings and, when the instance
// is already running, applies them to it.
//
//   - webhookUrl: empty keeps the current webhook; "disabled" or "false" removes it.
//   - subscribe: empty keeps the current events; ["ALL"] selects every event type.
//   - rabbitmqEnable, websocketEnable, natsEnable: empty keeps the current value.
type IntegrationsStruct struct {
	WebhookUrl      string   `json:"webhookUrl"`
	Subscribe       []string `json:"subscribe"`
	RabbitmqEnable  string   `json:"rabbitmqEnable"`
	WebSocketEnable string   `json:"websocketEnable"`
	NatsEnable      string   `json:"natsEnable"`
}

// producerValues are the states the runtime understands for a producer switch
// ("global" is only honoured by the RabbitMQ and NATS producers).
var producerValues = map[string]bool{
	"enabled":  true,
	"disabled": true,
	"true":     true,
	"false":    true,
	"global":   true,
}

// Validate rejects input that applyConnectSettings would otherwise drop silently
// (unknown events, a webhook that is not an http(s) URL, unknown producer states).
func (d *IntegrationsStruct) Validate() error {
	if hook := strings.TrimSpace(d.WebhookUrl); hook != "" && !isOffValue(hook) {
		if err := validateWebhookURL(hook); err != nil {
			return err
		}
	}

	if len(d.Subscribe) > 0 {
		var unknown []string
		hasAll := false
		for _, ev := range d.Subscribe {
			switch {
			case ev == event_types.ALL:
				hasAll = true
			case !event_types.IsEventType(ev):
				unknown = append(unknown, ev)
			}
		}
		if len(unknown) > 0 {
			return apierror.Invalid(fmt.Sprintf("unknown event types: %s", strings.Join(unknown, ", ")))
		}
		if hasAll && len(d.Subscribe) > 1 {
			return apierror.Invalid("subscribe: ALL cannot be combined with other events")
		}
	}

	for name, v := range map[string]string{
		"rabbitmqEnable":  d.RabbitmqEnable,
		"websocketEnable": d.WebSocketEnable,
		"natsEnable":      d.NatsEnable,
	} {
		if v != "" && !producerValues[v] {
			return apierror.Invalid(fmt.Sprintf("%s: invalid value %q (use enabled or disabled)", name, v))
		}
	}
	return nil
}

func validateWebhookURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return apierror.Invalid("webhookUrl must be an http(s) URL, or \"disabled\" to remove it")
	}
	return nil
}
