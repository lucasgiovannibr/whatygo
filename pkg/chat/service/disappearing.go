package chat_service

import (
	"context"
	"errors"
	"fmt"
	"time"

	instance_model "github.com/lucasgiovannibr/whatygo/pkg/instance/model"
	"github.com/lucasgiovannibr/whatygo/pkg/utils"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

// DisappearingStruct is the body of POST /chat/disappearing.
type DisappearingStruct struct {
	Chat string `json:"chat"`
	// Timer is "off", "24h", "7d" or "90d" (WhatsApp accepts no other duration).
	Timer string `json:"timer"`
}

// DefaultDisappearingStruct is the body of POST /user/defaultDisappearing.
type DefaultDisappearingStruct struct {
	Timer string `json:"timer"`
}

// requestError marks a failure caused by what the caller sent, as opposed to one
// from WhatsApp.
type requestError struct{ msg string }

func (e *requestError) Error() string { return e.msg }

// IsDisappearingRequestError reports whether err is a problem with the request.
func IsDisappearingRequestError(err error) bool {
	var re *requestError
	return errors.As(err, &re)
}

// parseDisappearingTimer turns the request value into a duration. Only the four
// durations WhatsApp itself offers are accepted.
func parseDisappearingTimer(val string) (time.Duration, error) {
	if val == "" {
		return 0, &requestError{`timer is required: "off", "24h", "7d" or "90d"`}
	}
	d, ok := whatsmeow.ParseDisappearingTimerString(val)
	if !ok {
		return 0, &requestError{fmt.Sprintf(`invalid timer %q: use "off", "24h", "7d" or "90d"`, val)}
	}
	return d, nil
}

// SetDisappearing sets the disappearing-messages timer of a chat (a contact or a
// group) and remembers it, so the messages sent to that chat afterwards carry it.
func (c *chatService) SetDisappearing(data *DisappearingStruct, instance *instance_model.Instance) error {
	timer, err := parseDisappearingTimer(data.Timer)
	if err != nil {
		return err
	}
	chat, ok := utils.ParseJID(data.Chat)
	if !ok {
		return &requestError{"invalid chat"}
	}
	chat = utils.CanonicalJID(chat)
	if chat.Server != types.DefaultUserServer && chat.Server != types.HiddenUserServer && chat.Server != types.GroupServer {
		return &requestError{"disappearing messages exist only in contact and group chats"}
	}

	client, err := c.ensureClientConnected(instance.Id)
	if err != nil {
		return err
	}

	if err := client.SetDisappearingTimer(context.Background(), chat, timer, time.Time{}); err != nil {
		c.loggerWrapper.GetLogger(instance.Id).LogError("[%s] error setting disappearing timer on %s: %v", instance.Id, chat, err)
		return err
	}

	c.whatsmeowService.RememberChatDisappearing(instance.Id, chat, uint32(timer.Seconds()))
	return nil
}

// SetDefaultDisappearing sets the timer that new one-to-one chats start with.
func (c *chatService) SetDefaultDisappearing(data *DefaultDisappearingStruct, instance *instance_model.Instance) error {
	timer, err := parseDisappearingTimer(data.Timer)
	if err != nil {
		return err
	}
	client, err := c.ensureClientConnected(instance.Id)
	if err != nil {
		return err
	}
	if err := client.SetDefaultDisappearingTimer(context.Background(), timer); err != nil {
		c.loggerWrapper.GetLogger(instance.Id).LogError("[%s] error setting default disappearing timer: %v", instance.Id, err)
		return err
	}
	return nil
}
