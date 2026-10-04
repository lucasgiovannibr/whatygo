package chat_service

import (
	"context"
	"strings"
	"time"

	instance_model "github.com/lucasgiovannibr/whatygo/pkg/instance/model"
	"github.com/lucasgiovannibr/whatygo/pkg/utils"
	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/types"
)

// Pin, archive and mute are app-state patches: the phone matches them to a chat by the
// JID written in the patch's index. Two things used to make that index wrong for a
// one-to-one chat, so the phone silently ignored the change (the routes were marked
// "not working"):
//
//   - the JID came out of utils.ParseJID, which prefixes a phone number with "+" (see
//     utils.CanonicalJID): the patch targeted "+5511...@s.whatsapp.net", a chat that
//     does not exist, and the store even grew a settings row for it;
//   - the phone keys one-to-one chats by LID, not by phone number (67 of the 68 chat
//     settings of the live test account were @lid), so a number has to be resolved to
//     its LID whenever the mapping is known.
//
// Groups were never affected, which is why the bug looked intermittent.

// defaultMuteDuration is what /chat/mute has always done when no duration is given.
const defaultMuteDuration = time.Hour

// muteAlways is the sentinel for "muted with no end" (WhatsApp's "Always").
const muteAlways = time.Duration(-1)

// parseMuteDuration reads the optional duration of /chat/mute.
func parseMuteDuration(val string) (time.Duration, error) {
	switch v := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(val), " ", "")); v {
	case "":
		return defaultMuteDuration, nil
	case "always", "forever":
		return muteAlways, nil
	case "8h", "8hours":
		return 8 * time.Hour, nil
	case "1w", "1week", "7d":
		return 7 * 24 * time.Hour, nil
	case "1d", "24h":
		return 24 * time.Hour, nil
	default:
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			return 0, &requestError{`invalid duration: use "8h", "1w", "always" or a duration such as "30m"`}
		}
		return d, nil
	}
}

// parseChat validates the chat of the request and returns its canonical JID.
func parseChat(chat string) (types.JID, error) {
	if strings.TrimSpace(chat) == "" {
		return types.JID{}, &requestError{"chat is required"}
	}
	parsed, ok := utils.ParseJID(chat)
	if !ok {
		return types.JID{}, &requestError{"invalid chat"}
	}
	return utils.CanonicalJID(parsed), nil
}

// applyChatState builds and sends one chat-state patch. It returns the moment it was
// applied (the routes used to return a zero time).
func (c *chatService) applyChatState(instance *instance_model.Instance, chat, what string, build func(target types.JID) appstate.PatchInfo) (string, error) {
	jid, err := parseChat(chat)
	if err != nil {
		return "", err
	}
	client, err := c.ensureClientConnected(instance.Id)
	if err != nil {
		return "", err
	}
	target := utils.AppStateChatJID(client, jid)

	if err := client.SendAppState(context.Background(), build(target)); err != nil {
		c.loggerWrapper.GetLogger(instance.Id).LogError("[%s] error %s chat %s: %v", instance.Id, what, target, err)
		return "", err
	}
	return time.Now().UTC().Format(time.RFC3339), nil
}

func (c *chatService) ChatPin(data *BodyStruct, instance *instance_model.Instance) (string, error) {
	return c.applyChatState(instance, data.Chat, "pin", func(t types.JID) appstate.PatchInfo { return appstate.BuildPin(t, true) })
}

func (c *chatService) ChatUnpin(data *BodyStruct, instance *instance_model.Instance) (string, error) {
	return c.applyChatState(instance, data.Chat, "unpin", func(t types.JID) appstate.PatchInfo { return appstate.BuildPin(t, false) })
}

func (c *chatService) ChatArchive(data *BodyStruct, instance *instance_model.Instance) (string, error) {
	return c.applyChatState(instance, data.Chat, "archive", func(t types.JID) appstate.PatchInfo {
		return appstate.BuildArchive(t, true, time.Time{}, nil)
	})
}

func (c *chatService) ChatUnarchive(data *BodyStruct, instance *instance_model.Instance) (string, error) {
	return c.applyChatState(instance, data.Chat, "unarchive", func(t types.JID) appstate.PatchInfo {
		return appstate.BuildArchive(t, false, time.Time{}, nil)
	})
}

func (c *chatService) ChatMute(data *BodyStruct, instance *instance_model.Instance) (string, error) {
	d, err := parseMuteDuration(data.Duration)
	if err != nil {
		return "", err
	}
	return c.applyChatState(instance, data.Chat, "mute", func(t types.JID) appstate.PatchInfo {
		if d == muteAlways {
			return appstate.BuildMuteAbs(t, true, nil) // no end timestamp = always
		}
		return appstate.BuildMute(t, true, d)
	})
}

func (c *chatService) ChatUnmute(data *BodyStruct, instance *instance_model.Instance) (string, error) {
	return c.applyChatState(instance, data.Chat, "unmute", func(t types.JID) appstate.PatchInfo { return appstate.BuildMute(t, false, 0) })
}
