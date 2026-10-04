package message_service

import (
	"bytes"
	"context"
	"encoding/json"
	"time"

	instance_model "github.com/lucasgiovannibr/whatygo/pkg/instance/model"
	"github.com/lucasgiovannibr/whatygo/pkg/utils"
	"go.mau.fi/whatsmeow/types"
)

// maxSubscribeNumbers bounds one /message/subscribe request.
const maxSubscribeNumbers = 100

// SubscribeNumbers is the "number" of /message/subscribe: one number (a string, as
// always) or a list of them.
type SubscribeNumbers struct {
	Numbers []string
	// IsList is true when the caller sent an array, even one with a single item.
	IsList bool
}

func (n *SubscribeNumbers) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if bytes.Equal(b, []byte("null")) {
		return nil
	}
	if len(b) > 0 && b[0] == '[' {
		n.IsList = true
		return json.Unmarshal(b, &n.Numbers)
	}
	var one string
	if err := json.Unmarshal(b, &one); err != nil {
		return err
	}
	if one != "" {
		n.Numbers = []string{one}
	}
	return nil
}

func (n SubscribeNumbers) MarshalJSON() ([]byte, error) {
	if n.IsList {
		return json.Marshal(n.Numbers)
	}
	if len(n.Numbers) == 1 {
		return json.Marshal(n.Numbers[0])
	}
	return json.Marshal("")
}

type SubscribePresenceStruct struct {
	// Number is one number or a list of numbers.
	Number SubscribeNumbers `json:"number" swaggertype:"string"`
}

// SubscribeResult is the outcome for one number.
type SubscribeResult struct {
	Number     string `json:"number"`
	Subscribed bool   `json:"subscribed"`
	Error      string `json:"error,omitempty"`
	// Invalid marks a number that could not be parsed, as opposed to a WhatsApp failure.
	Invalid bool `json:"-"`
}

// SubscribePresence subscribes to the presence (online / last-seen) of one or several
// contacts and reports what happened to each one: a list used to be all-or-nothing,
// so a single bad number hid the result of the others.
//
// WhatsApp only delivers events.Presence for JIDs we've explicitly subscribed to,
// and only while we're marked available — so we send available first (idempotent;
// ChatPresence and the background presence loop already do this). Subscriptions are
// ephemeral (reset on reconnect), so the caller re-subscribes when a chat is opened.
func (m *messageService) SubscribePresence(data *SubscribePresenceStruct, instance *instance_model.Instance) ([]SubscribeResult, error) {
	numbers := data.Number.Numbers
	if len(numbers) == 0 {
		return nil, &requestError{"phone number is required"}
	}
	if len(numbers) > maxSubscribeNumbers {
		return nil, &requestError{"too many numbers: at most 100 per request"}
	}

	client, err := m.ensureClientConnected(instance.Id)
	if err != nil {
		return nil, err
	}

	// Must be available to receive others' presence updates (non-fatal if it fails).
	if presErr := client.SendPresence(context.Background(), types.PresenceAvailable); presErr != nil {
		m.loggerWrapper.GetLogger(instance.Id).LogWarn("[%s] SendPresence(available) before subscribe failed (non-fatal): %v", instance.Id, presErr)
	}

	results := make([]SubscribeResult, 0, len(numbers))
	anySubscribed := false
	for _, number := range numbers {
		res := SubscribeResult{Number: number}
		recipient, ok := utils.ParseJID(number)
		if !ok {
			m.loggerWrapper.GetLogger(instance.Id).LogError("[%s] SubscribePresence: invalid number %s", instance.Id, number)
			res.Error, res.Invalid = "invalid phone number", true
		} else if err := client.SubscribePresence(context.Background(), utils.CanonicalJID(recipient)); err != nil {
			res.Error = err.Error()
		} else {
			res.Subscribed = true
			anySubscribed = true
			m.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] Subscribed to presence of %s", instance.Id, number)
		}
		results = append(results, res)
	}

	// Being "available" is what stops WhatsApp from pushing notifications to the
	// account's phone (#55). Unless the instance is configured alwaysOnline, hand the
	// presence back after a while so subscribing does not leave the phone silent.
	if anySubscribed && !instance.AlwaysOnline {
		instanceID := instance.Id
		time.AfterFunc(presenceLinger, func() {
			defer func() {
				if r := recover(); r != nil {
					m.loggerWrapper.GetLogger(instanceID).LogError("[%s] panic restoring unavailable presence: %v", instanceID, r)
				}
			}()
			if err := client.SendPresence(context.Background(), types.PresenceUnavailable); err != nil {
				m.loggerWrapper.GetLogger(instanceID).LogWarn("[%s] failed to restore unavailable presence: %v", instanceID, err)
			}
		})
	}

	return results, nil
}

// presenceLinger is how long the instance stays "available" after a presence
// subscription when alwaysOnline is off. Presence updates of the contact are only
// delivered while available, so this is the window in which they arrive.
const presenceLinger = 2 * time.Minute
