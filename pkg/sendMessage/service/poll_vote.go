package send_service

import (
	"context"
	"errors"
	"fmt"

	"github.com/lucasgiovannibr/whatygo/pkg/apierror"
	instance_model "github.com/lucasgiovannibr/whatygo/pkg/instance/model"
	"github.com/lucasgiovannibr/whatygo/pkg/utils"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
)

// PollVoteStruct is the body of POST /send/pollVote.
type PollVoteStruct struct {
	// Chat where the poll lives (phone number, or group JID).
	Number string `json:"number" example:"5511999999999"`
	// Message ID of the poll (the `messageId` returned by /send/poll, or the ID of a received poll).
	PollMessageID string `json:"pollMessageId" example:"3EB0DBF1C91EA77B149327"`
	// Author of the poll when it was NOT sent by this instance. Required in groups;
	// in a 1:1 chat it defaults to `number`.
	Participant string `json:"participant,omitempty" example:"5511888888888@s.whatsapp.net"`
	// Set to true when the poll was sent by this instance.
	FromMe bool `json:"fromMe,omitempty"`
	// Option names to vote for (exactly as in the poll). Send an empty list to remove the vote.
	SelectedOptions []string `json:"selectedOptions"`
	FormatJid       *bool    `json:"formatJid,omitempty"`
	Delay           int32    `json:"delay,omitempty"`
}

// buildPollInfo assembles the identity of the poll being voted on. The vote key is
// derived from these fields, so they must match how the poll was originally
// received or sent.
func buildPollInfo(chat types.JID, pollID string, fromMe bool, participant string, own types.JID) (*types.MessageInfo, error) {
	if pollID == "" {
		return nil, apierror.Invalid("pollMessageId is required")
	}
	isGroup := chat.Server == types.GroupServer

	info := &types.MessageInfo{
		ID: pollID,
		MessageSource: types.MessageSource{
			Chat:     chat,
			IsFromMe: fromMe,
			IsGroup:  isGroup,
		},
	}

	switch {
	case fromMe:
		if own.IsEmpty() {
			return nil, errors.New("instance is not logged in")
		}
		info.Sender = own.ToNonAD()
	case participant != "":
		p, err := types.ParseJID(participant)
		if err != nil {
			return nil, apierror.Invalid(fmt.Sprintf("invalid participant: %v", err))
		}
		info.Sender = p.ToNonAD()
	case isGroup:
		return nil, apierror.Invalid("participant (the poll author) is required for polls in groups")
	default:
		info.Sender = chat.ToNonAD()
	}
	return info, nil
}

// SendPollVote votes on an existing poll using the poll's stored message secret.
func (s *sendService) SendPollVote(data *PollVoteStruct, instance *instance_model.Instance) (*MessageSendStruct, error) {
	client, err := s.ensureClientConnectedWithRetry(instance.Id, 2)
	if err != nil {
		return nil, err
	}

	recipient, err := validateMessageFields(data.Number, data.FormatJid, nil, nil)
	if err != nil {
		return nil, err
	}
	// Chat JIDs go into the vote key: canonical form only (no "+").
	recipient = utils.CanonicalJID(recipient)

	options := data.SelectedOptions
	if options == nil {
		options = []string{}
	}

	tryBuild := func(own types.JID) (*waE2E.Message, error) {
		info, err := buildPollInfo(recipient, data.PollMessageID, data.FromMe, data.Participant, own)
		if err != nil {
			return nil, err
		}
		return client.BuildPollVote(context.Background(), info, options)
	}

	ownPN := types.EmptyJID
	if client.Store.ID != nil {
		ownPN = *client.Store.ID
	}
	msg, err := tryBuild(ownPN)
	// A poll we sent may have its secret stored under our LID instead of our phone JID.
	if errors.Is(err, whatsmeow.ErrOriginalMessageSecretNotFound) && data.FromMe && !client.Store.LID.IsEmpty() {
		msg, err = tryBuild(client.Store.LID)
	}
	if err != nil {
		if errors.Is(err, whatsmeow.ErrOriginalMessageSecretNotFound) {
			return nil, apierror.NotFound("poll not found: this instance has no stored secret for that poll (it must have sent or received it)")
		}
		return nil, err
	}

	return s.SendMessage(instance, msg, "PollUpdateMessage", &SendDataStruct{
		Number:    data.Number,
		Delay:     data.Delay,
		FormatJid: data.FormatJid,
	})
}
