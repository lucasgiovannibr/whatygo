package instance_service

import (
	"strings"
	"testing"

	instance_model "github.com/lucasgiovannibr/whatygo/pkg/instance/model"
	event_types "github.com/lucasgiovannibr/whatygo/pkg/internal/event_types"
)

func TestApplyConnectSettings(t *testing.T) {
	tests := []struct {
		name            string
		instance        instance_model.Instance
		data            ConnectStruct
		wantEvents      string
		wantRabbitmq    string
		wantWebhook     string
		wantUpdatesKeys []string
		wantNoUpdateKey string
	}{
		{
			name: "empty subscribe keeps existing events",
			instance: instance_model.Instance{
				Events:         "MESSAGE,SEND_MESSAGE,CONNECTION",
				RabbitmqEnable: "enabled",
			},
			data:            ConnectStruct{},
			wantEvents:      "MESSAGE,SEND_MESSAGE,CONNECTION",
			wantRabbitmq:    "enabled",
			wantUpdatesKeys: nil,
			wantNoUpdateKey: "events",
		},
		{
			name: "empty subscribe and empty events defaults to MESSAGE",
			instance: instance_model.Instance{
				Events: "",
			},
			data:            ConnectStruct{},
			wantEvents:      event_types.MESSAGE,
			wantUpdatesKeys: []string{"events"},
		},
		{
			name:     "subscribe ALL expands all event types",
			instance: instance_model.Instance{},
			data: ConnectStruct{
				Subscribe: []string{"ALL"},
			},
			wantEvents:      strings.Join(event_types.AllEventTypes, ","),
			wantUpdatesKeys: []string{"events"},
		},
		{
			name: "empty rabbitmqEnable preserves enabled",
			instance: instance_model.Instance{
				Events:         "MESSAGE",
				RabbitmqEnable: "enabled",
			},
			data: ConnectStruct{
				Subscribe: []string{"MESSAGE"},
			},
			wantEvents:      "MESSAGE",
			wantRabbitmq:    "enabled",
			wantNoUpdateKey: "rabbitmq_enable",
		},
		{
			name: "rabbitmqEnable disabled is written",
			instance: instance_model.Instance{
				Events:         "MESSAGE",
				RabbitmqEnable: "enabled",
			},
			data: ConnectStruct{
				RabbitmqEnable: "disabled",
			},
			wantEvents:      "MESSAGE",
			wantRabbitmq:    "disabled",
			wantUpdatesKeys: []string{"rabbitmq_enable"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inst := tt.instance
			updates := applyConnectSettings(&inst, &tt.data)

			if inst.Events != tt.wantEvents {
				t.Fatalf("Events = %q, want %q", inst.Events, tt.wantEvents)
			}
			if tt.wantRabbitmq != "" && inst.RabbitmqEnable != tt.wantRabbitmq {
				t.Fatalf("RabbitmqEnable = %q, want %q", inst.RabbitmqEnable, tt.wantRabbitmq)
			}
			if tt.wantWebhook != "" && inst.Webhook != tt.wantWebhook {
				t.Fatalf("Webhook = %q, want %q", inst.Webhook, tt.wantWebhook)
			}
			for _, key := range tt.wantUpdatesKeys {
				if _, ok := updates[key]; !ok {
					t.Fatalf("expected updates to contain %q, got %#v", key, updates)
				}
			}
			if tt.wantNoUpdateKey != "" {
				if _, ok := updates[tt.wantNoUpdateKey]; ok {
					t.Fatalf("expected updates to omit %q, got %#v", tt.wantNoUpdateKey, updates)
				}
			}
		})
	}
}

// The bundled manager sends webhookUrl:"" whenever its field is blank (also when it just
// reconnects), so an empty value must leave the webhook alone.
func TestConnectWithEmptyWebhookKeepsIt(t *testing.T) {
	inst := instance_model.Instance{Events: "MESSAGE", Webhook: "https://example.com/hook"}
	updates := applyConnectSettings(&inst, &ConnectStruct{WebhookUrl: "", Subscribe: []string{"MESSAGE"}})

	if inst.Webhook != "https://example.com/hook" {
		t.Fatalf("Webhook = %q, must be kept", inst.Webhook)
	}
	if _, ok := updates["webhook"]; ok {
		t.Fatalf("an empty webhookUrl must not be written: %#v", updates)
	}
}

// "disabled" / "false" is the explicit way to clear the webhook; it is stored empty
// (the manager validates the field as a URL, so a literal "disabled" made it complain).
func TestConnectClearsTheWebhookExplicitly(t *testing.T) {
	for _, v := range []string{"disabled", "false", "FALSE", " Disabled "} {
		inst := instance_model.Instance{Events: "MESSAGE", Webhook: "https://example.com/hook"}
		updates := applyConnectSettings(&inst, &ConnectStruct{WebhookUrl: v})

		if inst.Webhook != "" {
			t.Fatalf("%q: Webhook = %q, want it cleared", v, inst.Webhook)
		}
		if got, ok := updates["webhook"]; !ok || got != "" {
			t.Fatalf("%q: updates = %#v, want webhook written as empty", v, updates)
		}
	}
}

func TestConnectSetsANewWebhook(t *testing.T) {
	inst := instance_model.Instance{Events: "MESSAGE", Webhook: "https://old.example/hook"}
	applyConnectSettings(&inst, &ConnectStruct{WebhookUrl: "https://new.example/hook"})
	if inst.Webhook != "https://new.example/hook" {
		t.Fatalf("Webhook = %q", inst.Webhook)
	}
}
