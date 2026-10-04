package group_service

import (
	"context"
	"strings"

	"github.com/lucasgiovannibr/whatygo/pkg/apierror"
	instance_model "github.com/lucasgiovannibr/whatygo/pkg/instance/model"
	"github.com/lucasgiovannibr/whatygo/pkg/utils"
	"go.mau.fi/whatsmeow/types"
)

// GroupInviteStruct is the body of POST /group/inviteinfo and POST /group/joininvite.
//
// WhatsApp has two different kinds of invite:
//   - an invite LINK (chat.whatsapp.com/CODE): send only `code` (the code or the
//     whole link);
//   - an invite MESSAGE, the "join this group" card received in a chat: send
//     `groupJid`, `inviter`, `code` and `expiration` exactly as they came in the
//     received GroupInviteMessage.
type GroupInviteStruct struct {
	Code       string `json:"code"`
	GroupJID   string `json:"groupJid,omitempty"`
	Inviter    string `json:"inviter,omitempty"`
	Expiration int64  `json:"expiration,omitempty"`
}

// IsInviteMessage tells which of the two kinds the request is.
func (s *GroupInviteStruct) IsInviteMessage() bool {
	return s.GroupJID != ""
}

// normalizeInviteCode accepts the bare code or a full invite link, with or without
// scheme, query string or surrounding spaces, and returns the bare code.
func normalizeInviteCode(code string) string {
	code = strings.TrimSpace(code)
	if i := strings.IndexAny(code, "?#"); i >= 0 {
		code = code[:i]
	}
	code = strings.TrimRight(code, "/")
	if i := strings.LastIndex(code, "/"); i >= 0 {
		code = code[i+1:]
	}
	return code
}

// validate checks the fields required by the kind of invite.
func (s *GroupInviteStruct) validate() error {
	if normalizeInviteCode(s.Code) == "" {
		return apierror.Invalid("code is required")
	}
	if s.IsInviteMessage() && s.Inviter == "" {
		return apierror.Invalid("inviter is required for an invite message")
	}
	return nil
}

func (g *groupService) parseInviteMessageJIDs(data *GroupInviteStruct) (group, inviter types.JID, err error) {
	group, err = types.ParseJID(data.GroupJID)
	if err != nil || group.Server != types.GroupServer {
		return group, inviter, apierror.Invalid("invalid groupJid")
	}
	parsed, ok := utils.ParseJID(data.Inviter)
	if !ok {
		return group, inviter, apierror.Invalid("invalid inviter")
	}
	return group, utils.CanonicalJID(parsed), nil
}

// GetInviteInfo looks a group up from an invite WITHOUT joining it.
func (g *groupService) GetInviteInfo(data *GroupInviteStruct, instance *instance_model.Instance) (*types.GroupInfo, error) {
	if err := data.validate(); err != nil {
		return nil, err
	}
	client, err := g.ensureClientConnected(instance.Id)
	if err != nil {
		return nil, err
	}

	code := normalizeInviteCode(data.Code)

	if data.IsInviteMessage() {
		group, inviter, err := g.parseInviteMessageJIDs(data)
		if err != nil {
			return nil, err
		}
		return client.GetGroupInfoFromInvite(context.Background(), group, inviter, code, data.Expiration)
	}

	return client.GetGroupInfoFromLink(context.Background(), code)
}

// JoinGroupInvite joins a group from an invite MESSAGE (for links use /group/join).
func (g *groupService) JoinGroupInvite(data *GroupInviteStruct, instance *instance_model.Instance) error {
	if err := data.validate(); err != nil {
		return err
	}
	if !data.IsInviteMessage() {
		return apierror.Invalid("groupJid is required: this route joins from an invite message; for an invite link use /group/join")
	}
	client, err := g.ensureClientConnected(instance.Id)
	if err != nil {
		return err
	}

	group, inviter, err := g.parseInviteMessageJIDs(data)
	if err != nil {
		return err
	}
	return client.JoinGroupWithInvite(context.Background(), group, inviter, normalizeInviteCode(data.Code), data.Expiration)
}
