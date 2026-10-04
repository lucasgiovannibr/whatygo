package message_service

import (
	"context"
	"errors"

	instance_model "github.com/lucasgiovannibr/whatygo/pkg/instance/model"
	"github.com/lucasgiovannibr/whatygo/pkg/utils"
	"go.mau.fi/whatsmeow/types"
)

// RerequestStruct is the body of POST /message/rerequest: the message this device
// could not decrypt, as published by the UndecryptableMessage event.
type RerequestStruct struct {
	Chat      string `json:"chat"`
	MessageID string `json:"messageId"`
	// Sender is who sent the message. Optional in a one-to-one chat (it is the chat
	// itself); required in a group.
	Sender string `json:"sender,omitempty"`
}

// requestError marks a failure caused by what the caller sent.
type requestError struct{ msg string }

func (e *requestError) Error() string { return e.msg }

// IsRequestError reports whether err is a problem with the request.
func IsRequestError(err error) bool {
	var re *requestError
	return errors.As(err, &re)
}

// resolve validates the request and returns the canonical chat and sender JIDs.
func (d *RerequestStruct) resolve() (chat, sender types.JID, err error) {
	if d.MessageID == "" {
		return chat, sender, &requestError{"messageId is required"}
	}
	if d.Chat == "" {
		return chat, sender, &requestError{"chat is required"}
	}
	parsedChat, ok := utils.ParseJID(d.Chat)
	if !ok {
		return chat, sender, &requestError{"invalid chat"}
	}
	chat = utils.CanonicalJID(parsedChat)
	if chat.Server == types.NewsletterServer || chat.Server == types.BroadcastServer {
		return chat, sender, &requestError{"channels and status have no copy to re-request"}
	}

	if d.Sender != "" {
		parsedSender, ok := utils.ParseJID(d.Sender)
		if !ok {
			return chat, sender, &requestError{"invalid sender"}
		}
		return chat, utils.CanonicalJID(parsedSender), nil
	}
	if chat.Server == types.GroupServer {
		return chat, sender, &requestError{"sender is required in a group"}
	}
	// One-to-one: the message came from the other side of the chat.
	return chat, chat, nil
}

// RerequestMessage asks the user's phone to send this device another copy of a
// message it could not decrypt. The phone's answer arrives later as a normal Message
// event carrying UnavailableRequestID equal to the returned request id.
func (m *messageService) RerequestMessage(data *RerequestStruct, instance *instance_model.Instance) (string, error) {
	chat, sender, err := data.resolve()
	if err != nil {
		return "", err
	}

	client, err := m.ensureClientConnected(instance.Id)
	if err != nil {
		return "", err
	}

	m.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] Asking the phone to resend message %s from %s in %s", instance.Id, data.MessageID, sender, chat)

	resp, err := client.SendPeerMessage(context.Background(), client.BuildUnavailableMessageRequest(chat, sender, data.MessageID))
	if err != nil {
		m.loggerWrapper.GetLogger(instance.Id).LogError("[%s] error requesting the message again: %v", instance.Id, err)
		return "", err
	}
	return resp.ID, nil
}
