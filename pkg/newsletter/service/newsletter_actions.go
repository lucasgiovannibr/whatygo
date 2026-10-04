package newsletter_service

import (
	"context"
	"errors"
	"time"

	instance_model "github.com/lucasgiovannibr/whatygo/pkg/instance/model"
	"go.mau.fi/whatsmeow/types"
)

// newsletterActionTimeout bounds the calls whatsmeow makes without a deadline of
// their own (mark-viewed waits for a reply that may never come).
const newsletterActionTimeout = 20 * time.Second

// requestError marks a failure caused by what the caller sent, as opposed to one
// from WhatsApp.
type requestError struct{ msg string }

func (e *requestError) Error() string { return e.msg }

// IsNewsletterRequestError reports whether err is a problem with the request.
func IsNewsletterRequestError(err error) bool {
	var re *requestError
	return errors.As(err, &re)
}

// errNotANewsletter is what a JID that is not a channel gets.
var errNotANewsletter = &requestError{"jid must be a channel (…@newsletter)"}

type NewsletterMuteStruct struct {
	JID  types.JID `json:"jid"`
	Mute bool      `json:"mute"`
}

type NewsletterMarkViewedStruct struct {
	JID       types.JID `json:"jid"`
	ServerIDs []int     `json:"serverIds"`
}

type NewsletterReactStruct struct {
	JID      types.JID `json:"jid"`
	ServerID int       `json:"serverId"`
	// Reaction is the emoji; empty removes the reaction sent earlier.
	Reaction string `json:"reaction"`
	// MessageID is optional: the ID of the reaction itself.
	MessageID string `json:"messageId,omitempty"`
}

// validateChannel is shared by every action: the JID has to be a channel.
func validateChannel(jid types.JID) error {
	if jid.IsEmpty() || jid.Server != types.NewsletterServer {
		return errNotANewsletter
	}
	return nil
}

// Validate checks a mark-viewed request.
func (d *NewsletterMarkViewedStruct) Validate() error {
	if err := validateChannel(d.JID); err != nil {
		return err
	}
	if len(d.ServerIDs) == 0 {
		return &requestError{"serverIds is required"}
	}
	return nil
}

// Validate checks a reaction request.
func (d *NewsletterReactStruct) Validate() error {
	if err := validateChannel(d.JID); err != nil {
		return err
	}
	if d.ServerID <= 0 {
		return &requestError{"serverId is required (the message's server id in the channel)"}
	}
	return nil
}

// run executes fn under a deadline: whatsmeow's newsletter receipts wait on a reply
// with no timeout, and a handler must not hang on it.
func run(fn func(ctx context.Context) error) error {
	ctx, cancel := context.WithTimeout(context.Background(), newsletterActionTimeout)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- fn(ctx) }()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return errors.New("timed out waiting for WhatsApp")
	}
}

func (n *newsletterService) FollowNewsletter(data *GetNewsletterStruct, instance *instance_model.Instance) error {
	if err := validateChannel(data.JID); err != nil {
		return err
	}
	client, err := n.ensureClientConnected(instance.Id)
	if err != nil {
		return err
	}
	if err := run(func(ctx context.Context) error { return client.FollowNewsletter(ctx, data.JID) }); err != nil {
		n.loggerWrapper.GetLogger(instance.Id).LogError("[%s] error following channel: %v", instance.Id, err)
		return err
	}
	return nil
}

func (n *newsletterService) UnfollowNewsletter(data *GetNewsletterStruct, instance *instance_model.Instance) error {
	if err := validateChannel(data.JID); err != nil {
		return err
	}
	client, err := n.ensureClientConnected(instance.Id)
	if err != nil {
		return err
	}
	if err := run(func(ctx context.Context) error { return client.UnfollowNewsletter(ctx, data.JID) }); err != nil {
		n.loggerWrapper.GetLogger(instance.Id).LogError("[%s] error unfollowing channel: %v", instance.Id, err)
		return err
	}
	return nil
}

func (n *newsletterService) MuteNewsletter(data *NewsletterMuteStruct, instance *instance_model.Instance) error {
	if err := validateChannel(data.JID); err != nil {
		return err
	}
	client, err := n.ensureClientConnected(instance.Id)
	if err != nil {
		return err
	}
	if err := run(func(ctx context.Context) error { return client.NewsletterToggleMute(ctx, data.JID, data.Mute) }); err != nil {
		n.loggerWrapper.GetLogger(instance.Id).LogError("[%s] error toggling channel mute: %v", instance.Id, err)
		return err
	}
	return nil
}

func (n *newsletterService) MarkNewsletterViewed(data *NewsletterMarkViewedStruct, instance *instance_model.Instance) error {
	if err := data.Validate(); err != nil {
		return err
	}
	client, err := n.ensureClientConnected(instance.Id)
	if err != nil {
		return err
	}
	ids := make([]types.MessageServerID, len(data.ServerIDs))
	for i, id := range data.ServerIDs {
		ids[i] = types.MessageServerID(id)
	}
	if err := run(func(ctx context.Context) error { return client.NewsletterMarkViewed(ctx, data.JID, ids) }); err != nil {
		n.loggerWrapper.GetLogger(instance.Id).LogError("[%s] error marking channel messages viewed: %v", instance.Id, err)
		return err
	}
	return nil
}

func (n *newsletterService) ReactNewsletter(data *NewsletterReactStruct, instance *instance_model.Instance) error {
	if err := data.Validate(); err != nil {
		return err
	}
	client, err := n.ensureClientConnected(instance.Id)
	if err != nil {
		return err
	}
	if err := run(func(ctx context.Context) error {
		return client.NewsletterSendReaction(ctx, data.JID, types.MessageServerID(data.ServerID), data.Reaction, types.MessageID(data.MessageID))
	}); err != nil {
		n.loggerWrapper.GetLogger(instance.Id).LogError("[%s] error reacting in channel: %v", instance.Id, err)
		return err
	}
	return nil
}
