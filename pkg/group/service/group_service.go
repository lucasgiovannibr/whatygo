package group_service

import (
	"context"
	"errors"
	"fmt"
	"github.com/lucasgiovannibr/whatygo/pkg/apierror"
	"github.com/lucasgiovannibr/whatygo/pkg/safemap"
	"strings"
	"time"

	instance_model "github.com/lucasgiovannibr/whatygo/pkg/instance/model"
	logger_wrapper "github.com/lucasgiovannibr/whatygo/pkg/logger"
	"github.com/lucasgiovannibr/whatygo/pkg/utils"
	whatsmeow_service "github.com/lucasgiovannibr/whatygo/pkg/whatsmeow/service"
	"github.com/gin-gonic/gin"
	"github.com/vincent-petithory/dataurl"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

type GroupService interface {
	ListGroups(instance *instance_model.Instance) ([]*types.GroupInfo, error)
	GetGroupInfo(data *GetGroupInfoStruct, instance *instance_model.Instance) (*types.GroupInfo, error)
	GetGroupInviteLink(data *GetGroupInviteLinkStruct, instance *instance_model.Instance) (string, error)
	SetGroupPhoto(data *SetGroupPhotoStruct, instance *instance_model.Instance) (string, error)
	SetGroupName(data *SetGroupNameStruct, instance *instance_model.Instance) error
	SetGroupDescription(data *SetGroupDescriptionStruct, instance *instance_model.Instance) error
	CreateGroup(data *CreateGroupStruct, instance *instance_model.Instance) (gin.H, error)
	UpdateParticipant(data *AddParticipantStruct, instance *instance_model.Instance) ([]types.GroupParticipant, error)
	UpdateGroupSettings(data *UpdateGroupSettingsStruct, instance *instance_model.Instance) error
	GetGroupRequestParticipants(data *GetGroupRequestParticipantsStruct, instance *instance_model.Instance) ([]EnrichedGroupParticipantRequest, error)
	UpdateGroupRequestParticipants(data *UpdateGroupRequestParticipantsStruct, instance *instance_model.Instance) ([]types.GroupParticipant, error)
	GetMyGroups(instance *instance_model.Instance) ([]types.GroupInfo, error)
	JoinGroupLink(data *JoinGroupStruct, instance *instance_model.Instance) error
	GetInviteInfo(data *GroupInviteStruct, instance *instance_model.Instance) (*types.GroupInfo, error)
	JoinGroupInvite(data *GroupInviteStruct, instance *instance_model.Instance) error
	LeaveGroup(data *LeaveGroupStruct, instance *instance_model.Instance) error
}

type groupService struct {
	clientPointer    *safemap.Map[*whatsmeow.Client]
	whatsmeowService whatsmeow_service.WhatsmeowService
	loggerWrapper    *logger_wrapper.LoggerManager
}

type SimpleGroupInfo struct {
	JID       types.JID `json:"jid"`
	GroupName string    `json:"groupName"`
}

type GroupCollection struct {
	Groups []SimpleGroupInfo
}

type GetGroupInfoStruct struct {
	GroupJID string `json:"groupJid"`
}

type GetGroupInviteLinkStruct struct {
	GroupJID string `json:"groupJid"`
	Reset    bool   `json:"reset"`
}

type SetGroupPhotoStruct struct {
	GroupJID string `json:"groupJid"`
	Image    string `json:"image"`
}

type SetGroupNameStruct struct {
	GroupJID string `json:"groupJid"`
	Name     string `json:"name"`
}

type SetGroupDescriptionStruct struct {
	GroupJID    string `json:"groupJid"`
	Description string `json:"description"`
}

type CreateGroupStruct struct {
	GroupName    string   `json:"groupName"`
	Participants []string `json:"participants"`
}

type AddParticipantStruct struct {
	GroupJID     types.JID                   `json:"groupJid"`
	Participants []string                    `json:"participants"`
	Action       whatsmeow.ParticipantChange `json:"action"`
}

type JoinGroupStruct struct {
	Code string `json:"code"`
}

type LeaveGroupStruct struct {
	GroupJID types.JID `json:"groupJid"`
}

type UpdateGroupSettingsStruct struct {
	GroupJID string `json:"groupJid"`
	Action   string `json:"action"` // announcement, not_announcement, locked, unlocked
}

type GetGroupRequestParticipantsStruct struct {
	GroupJID string `json:"groupJid"`
}

// Estrutura enriquecida com PushName
type EnrichedGroupParticipantRequest struct {
	JID         types.JID `json:"JID"`
	RequestedAt time.Time `json:"RequestedAt"`
	PushName    string    `json:"PushName"`
}

type UpdateGroupRequestParticipantsStruct struct {
	GroupJID     string   `json:"groupJid"`
	Action       string   `json:"action"` // approve, reject
	Participants []string `json:"participants"`
}

func (g *groupService) ensureClientConnected(instanceId string) (*whatsmeow.Client, error) {
	return utils.ClientProvider{Clients: g.clientPointer, Starter: g.whatsmeowService, Gate: true}.Ensure(context.Background(), instanceId, g.loggerWrapper.GetLogger(instanceId))
}

func (g *groupService) ListGroups(instance *instance_model.Instance) ([]*types.GroupInfo, error) {
	client, err := g.ensureClientConnected(instance.Id)
	if err != nil {
		return nil, err
	}

	resp, err := client.GetJoinedGroups(context.Background())
	if err != nil {
		g.loggerWrapper.GetLogger(instance.Id).LogError("[%s] error getting groups: %v", instance.Id, err)
		return nil, err
	}

	gc := new(GroupCollection)
	for _, info := range resp {
		simpleGroup := SimpleGroupInfo{
			JID:       info.JID,
			GroupName: info.GroupName.Name,
		}
		gc.Groups = append(gc.Groups, simpleGroup)
	}

	return resp, nil
}

func (g *groupService) GetGroupInfo(data *GetGroupInfoStruct, instance *instance_model.Instance) (*types.GroupInfo, error) {
	client, err := g.ensureClientConnected(instance.Id)
	if err != nil {
		return nil, err
	}

	recipient, ok := utils.ParseJID(data.GroupJID)
	if !ok {
		g.loggerWrapper.GetLogger(instance.Id).LogError("[%s] Error validating message fields", instance.Id)
		return nil, apierror.Invalid("invalid group jid")
	}

	resp, err := client.GetGroupInfo(context.Background(), recipient)
	if err != nil {
		g.loggerWrapper.GetLogger(instance.Id).LogError("[%s] error mute chat: %v", instance.Id, err)
		return nil, err
	}

	return resp, nil
}

func (g *groupService) GetGroupInviteLink(data *GetGroupInviteLinkStruct, instance *instance_model.Instance) (string, error) {
	client, err := g.ensureClientConnected(instance.Id)
	if err != nil {
		return "", err
	}

	recipient, ok := utils.ParseJID(data.GroupJID)
	if !ok {
		g.loggerWrapper.GetLogger(instance.Id).LogError("[%s] Error validating message fields", instance.Id)
		return "", apierror.Invalid("invalid group jid")
	}

	resp, err := client.GetGroupInviteLink(context.Background(), recipient, data.Reset)
	if err != nil {
		g.loggerWrapper.GetLogger(instance.Id).LogError("[%s] error mute chat: %v", instance.Id, err)
		return "", err
	}

	return resp, nil
}

func (g *groupService) SetGroupPhoto(data *SetGroupPhotoStruct, instance *instance_model.Instance) (string, error) {
	client, err := g.ensureClientConnected(instance.Id)
	if err != nil {
		return "", err
	}

	recipient, ok := utils.ParseJID(data.GroupJID)
	if !ok {
		g.loggerWrapper.GetLogger(instance.Id).LogError("[%s] Error validating message fields", instance.Id)
		return "", apierror.Invalid("invalid group jid")
	}

	var fileData []byte

	if strings.HasPrefix(data.Image, "http://") || strings.HasPrefix(data.Image, "https://") {
		var err error
		fileData, err = utils.DownloadBytes(data.Image, utils.MaxImageDownload)
		if err != nil {
			g.loggerWrapper.GetLogger(instance.Id).LogError("[%s] Could not download image from URL: %v", instance.Id, err)
			return "", fmt.Errorf("failed to fetch image from URL: %v", err)
		}

	} else if strings.HasPrefix(data.Image, "data:image/jpeg;base64,") || strings.HasPrefix(data.Image, "data:image/png;base64,") {
		dataURL, err := dataurl.DecodeString(data.Image)
		if err != nil {
			g.loggerWrapper.GetLogger(instance.Id).LogError("[%s] Could not decode base64 encoded data from payload", instance.Id)
			return "", err
		}
		fileData = dataURL.Data
	} else {
		g.loggerWrapper.GetLogger(instance.Id).LogError("[%s] Image data should start with \"data:image/jpeg;base64,\" or be a valid URL", instance.Id)
		return "", errors.New("image data should be a valid URL or start with \"data:image/jpeg;base64,\"")
	}

	pictureID, err := client.SetGroupPhoto(context.Background(), recipient, fileData)
	if err != nil {
		g.loggerWrapper.GetLogger(instance.Id).LogError("[%s] Error setting group photo: %v", instance.Id, err)
		return "", err
	}

	return pictureID, nil
}

func (g *groupService) SetGroupName(data *SetGroupNameStruct, instance *instance_model.Instance) error {
	client, err := g.ensureClientConnected(instance.Id)
	if err != nil {
		return err
	}

	recipient, ok := utils.ParseJID(data.GroupJID)
	if !ok {
		g.loggerWrapper.GetLogger(instance.Id).LogError("[%s] Error validating message fields", instance.Id)
		return apierror.Invalid("invalid group jid")
	}

	g.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] Attempting to set group name for %s", instance.Id, recipient.String())

	err = client.SetGroupName(context.Background(), recipient, data.Name)
	if err != nil {
		// Log mais detalhado para erro 409
		if strings.Contains(err.Error(), "409") || strings.Contains(err.Error(), "conflict") {
			g.loggerWrapper.GetLogger(instance.Id).LogWarn("[%s] WhatsApp returned 409 conflict when setting name. This usually means: rate limit, duplicate content, or insufficient permissions", instance.Id)
		}
		g.loggerWrapper.GetLogger(instance.Id).LogError("[%s] error setting group name: %v", instance.Id, err)
		return err
	}

	g.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] Group name set successfully", instance.Id)
	return nil
}

func (g *groupService) SetGroupDescription(data *SetGroupDescriptionStruct, instance *instance_model.Instance) error {
	client, err := g.ensureClientConnected(instance.Id)
	if err != nil {
		return err
	}

	recipient, ok := utils.ParseJID(data.GroupJID)
	if !ok {
		g.loggerWrapper.GetLogger(instance.Id).LogError("[%s] Error validating message fields", instance.Id)
		return apierror.Invalid("invalid group jid")
	}

	g.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] Attempting to set group description for %s", instance.Id, recipient.String())

	// Use SetGroupTopic instead of SetGroupDescription (proper WhatsApp method)
	// Empty strings for previousID and newID will be auto-filled by the library
	err = client.SetGroupTopic(context.Background(), recipient, "", "", data.Description)
	if err != nil {
		// Log mais detalhado para erro 409
		if strings.Contains(err.Error(), "409") || strings.Contains(err.Error(), "conflict") {
			g.loggerWrapper.GetLogger(instance.Id).LogWarn("[%s] WhatsApp returned 409 conflict when setting description. This usually means: rate limit, duplicate content, or insufficient permissions", instance.Id)
		}
		g.loggerWrapper.GetLogger(instance.Id).LogError("[%s] error setting group description: %v", instance.Id, err)
		return err
	}

	g.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] Group description set successfully", instance.Id)
	return nil
}

func (g *groupService) CreateGroup(data *CreateGroupStruct, instance *instance_model.Instance) (gin.H, error) {
	client, err := g.ensureClientConnected(instance.Id)
	if err != nil {
		return nil, err
	}

	var participants []types.JID
	for _, participant := range data.Participants {
		recipient, ok := utils.ParseJID(participant)
		if !ok {
			g.loggerWrapper.GetLogger(instance.Id).LogError("[%s] Error validating message fields", instance.Id)
			return nil, apierror.Invalid("invalid phone number")
		}
		participants = append(participants, utils.CanonicalJID(recipient))
	}

	resp, err := client.CreateGroup(context.Background(), whatsmeow.ReqCreateGroup{
		Name:         data.GroupName,
		Participants: participants,
	})
	if err != nil {
		g.loggerWrapper.GetLogger(instance.Id).LogError("[%s] error create group: %v", instance.Id, err)
		return nil, err
	}

	var failed []types.JID
	for _, participant := range resp.Participants {
		if participant.Error != 0 {
			failed = append(failed, participant.JID)
		}
	}

	var added []types.JID
	infoResp, err := client.GetGroupInfo(context.Background(), resp.JID)
	if err != nil {
		g.loggerWrapper.GetLogger(instance.Id).LogError("[%s] error get group info: %v", instance.Id, err)
		return nil, err
	}
	for _, add := range infoResp.Participants {
		added = append(added, add.JID)
	}

	response := gin.H{
		"jid":    resp.JID,
		"name":   resp.Name,
		"owner":  resp.OwnerJID,
		"added":  added,
		"failed": failed,
	}

	return response, nil
}

func (g *groupService) UpdateParticipant(data *AddParticipantStruct, instance *instance_model.Instance) ([]types.GroupParticipant, error) {
	client, err := g.ensureClientConnected(instance.Id)
	if err != nil {
		return nil, err
	}

	var participants []types.JID
	for _, participant := range data.Participants {
		recipient, ok := utils.ParseJID(participant)
		if !ok {
			g.loggerWrapper.GetLogger(instance.Id).LogError("[%s] Error validating message fields", instance.Id)
			return nil, apierror.Invalid("invalid phone number")
		}
		// Participant JIDs go into raw group IQ attributes: no "+" prefix.
		participants = append(participants, utils.CanonicalJID(recipient))
	}

	// The per-participant results carry the outcome (e.g. 404 not on WhatsApp, 403
	// not allowed): they used to be discarded, so adding a non-existent number
	// answered "success".
	results, err := client.UpdateGroupParticipants(context.Background(), data.GroupJID, participants, data.Action)
	if err != nil {
		g.loggerWrapper.GetLogger(instance.Id).LogError("[%s] error updating group participants: %v", instance.Id, err)
		return nil, err
	}

	return results, nil
}

func (g *groupService) GetMyGroups(instance *instance_model.Instance) ([]types.GroupInfo, error) {
	client, err := g.ensureClientConnected(instance.Id)
	if err != nil {
		return nil, err
	}

	resp, err := client.GetJoinedGroups(context.Background())
	if err != nil {
		g.loggerWrapper.GetLogger(instance.Id).LogError("[%s] error create group: %v", instance.Id, err)
		return nil, err
	}

	var adminGroups []types.GroupInfo
	for _, group := range resp {
		if isGroupOwner(client, group) {
			adminGroups = append(adminGroups, *group)
		}
	}

	return adminGroups, nil
}

func (g *groupService) JoinGroupLink(data *JoinGroupStruct, instance *instance_model.Instance) error {
	client, err := g.ensureClientConnected(instance.Id)
	if err != nil {
		return err
	}

	_, err = client.JoinGroupWithLink(context.Background(), data.Code)
	if err != nil {
		g.loggerWrapper.GetLogger(instance.Id).LogError("[%s] error create group: %v", instance.Id, err)
		return err
	}

	return nil
}

func (g *groupService) LeaveGroup(data *LeaveGroupStruct, instance *instance_model.Instance) error {
	client, err := g.ensureClientConnected(instance.Id)
	if err != nil {
		return err
	}

	err = client.LeaveGroup(context.Background(), data.GroupJID)
	if err != nil {
		g.loggerWrapper.GetLogger(instance.Id).LogError("[%s] error leave group: %v", instance.Id, err)
		return err
	}

	return nil
}

func (g *groupService) UpdateGroupSettings(data *UpdateGroupSettingsStruct, instance *instance_model.Instance) error {
	client, err := g.ensureClientConnected(instance.Id)
	if err != nil {
		return err
	}

	recipient, ok := utils.ParseJID(data.GroupJID)
	if !ok {
		g.loggerWrapper.GetLogger(instance.Id).LogError("[%s] Error validating group jid", instance.Id)
		return apierror.Invalid("invalid group jid")
	}

	// Validate action
	validActions := map[string]bool{
		"announcement":     true,
		"not_announcement": true,
		"locked":           true,
		"unlocked":         true,
		"approval_on":      true,
		"approval_off":     true,
		"admin_add":        true,
		"all_member_add":   true,
	}

	if !validActions[data.Action] {
		g.loggerWrapper.GetLogger(instance.Id).LogError("[%s] Invalid action: %s", instance.Id, data.Action)
		return apierror.Invalid("invalid action. Valid actions: announcement, not_announcement, locked, unlocked, approval_on, approval_off, admin_add, all_member_add")
	}

	// Apply settings based on action
	switch data.Action {
	case "announcement":
		err = client.SetGroupAnnounce(context.Background(), recipient, true)
	case "not_announcement":
		err = client.SetGroupAnnounce(context.Background(), recipient, false)
	case "locked":
		err = client.SetGroupLocked(context.Background(), recipient, true)
	case "unlocked":
		err = client.SetGroupLocked(context.Background(), recipient, false)
	case "approval_on":
		err = client.SetGroupJoinApprovalMode(context.Background(), recipient, true)
	case "approval_off":
		err = client.SetGroupJoinApprovalMode(context.Background(), recipient, false)
	case "admin_add":
		err = client.SetGroupMemberAddMode(context.Background(), recipient, "admin_add")
	case "all_member_add":
		err = client.SetGroupMemberAddMode(context.Background(), recipient, "all_member_add")
	}

	if err != nil {
		g.loggerWrapper.GetLogger(instance.Id).LogError("[%s] error updating group settings: %v", instance.Id, err)
		return err
	}

	g.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] Group settings updated successfully: %s", instance.Id, data.Action)
	return nil
}

func (g *groupService) GetGroupRequestParticipants(data *GetGroupRequestParticipantsStruct, instance *instance_model.Instance) ([]EnrichedGroupParticipantRequest, error) {
	client, err := g.ensureClientConnected(instance.Id)
	if err != nil {
		return nil, err
	}

	recipient, ok := utils.ParseJID(data.GroupJID)
	if !ok {
		g.loggerWrapper.GetLogger(instance.Id).LogError("[%s] Error validating group jid", instance.Id)
		return nil, apierror.Invalid("invalid group jid")
	}

	requests, err := client.GetGroupRequestParticipants(context.Background(), recipient)
	if err != nil {
		g.loggerWrapper.GetLogger(instance.Id).LogError("[%s] error getting group request participants: %v", instance.Id, err)
		return nil, err
	}

	g.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] Retrieved %d pending group requests", instance.Id, len(requests))

	// Enriquecer com informações de usuário (PushName)
	enrichedRequests := make([]EnrichedGroupParticipantRequest, len(requests))
	jidsToFetch := make([]types.JID, 0, len(requests))

	for _, req := range requests {
		if req.JID.User != "" {
			jidsToFetch = append(jidsToFetch, req.JID)
		}
	}

	// Buscar informações de usuário em lote
	userInfoMap := make(map[types.JID]types.UserInfo)
	if len(jidsToFetch) > 0 {
		userInfoMap, err = client.GetUserInfo(context.Background(), jidsToFetch)
		if err != nil {
			g.loggerWrapper.GetLogger(instance.Id).LogWarn("[%s] Could not fetch user info: %v", instance.Id, err)
			// Continuar sem pushName se falhar
		}
	}

	// Montar resposta enriquecida
	for i, req := range requests {
		enrichedRequests[i] = EnrichedGroupParticipantRequest{
			JID:         req.JID,
			RequestedAt: req.RequestedAt,
			PushName:    "",
		}

		// Tentar obter PushName
		lookupJID := req.JID

		if userInfo, found := userInfoMap[lookupJID]; found {
			// VerifiedName é ponteiro, verificar se não é nil
			if userInfo.VerifiedName != nil && userInfo.VerifiedName.Details.GetVerifiedName() != "" {
				enrichedRequests[i].PushName = userInfo.VerifiedName.Details.GetVerifiedName()
			}
		}

		// Tentar obter do store de contatos se não tiver VerifiedName
		if enrichedRequests[i].PushName == "" && client.Store.Contacts != nil {
			if contactInfo, err := client.Store.Contacts.GetContact(context.Background(), lookupJID); err == nil && contactInfo.PushName != "" {
				enrichedRequests[i].PushName = contactInfo.PushName
			} else if contactInfo.FullName != "" {
				enrichedRequests[i].PushName = contactInfo.FullName
			}
		}
	}

	return enrichedRequests, nil
}

func (g *groupService) UpdateGroupRequestParticipants(data *UpdateGroupRequestParticipantsStruct, instance *instance_model.Instance) ([]types.GroupParticipant, error) {
	client, err := g.ensureClientConnected(instance.Id)
	if err != nil {
		return nil, err
	}

	recipient, ok := utils.ParseJID(data.GroupJID)
	if !ok {
		g.loggerWrapper.GetLogger(instance.Id).LogError("[%s] Error validating group jid", instance.Id)
		return nil, apierror.Invalid("invalid group jid")
	}

	// Validate action
	var action whatsmeow.ParticipantRequestChange
	switch data.Action {
	case "approve":
		action = whatsmeow.ParticipantChangeApprove
	case "reject":
		action = whatsmeow.ParticipantChangeReject
	default:
		g.loggerWrapper.GetLogger(instance.Id).LogError("[%s] Invalid action: %s", instance.Id, data.Action)
		return nil, apierror.Invalid("invalid action. Valid actions: approve, reject")
	}

	// Parse participants JIDs
	var participants []types.JID
	for _, participant := range data.Participants {
		participantJID, ok := utils.ParseJID(participant)
		if !ok {
			g.loggerWrapper.GetLogger(instance.Id).LogError("[%s] Error validating participant jid: %s", instance.Id, participant)
			return nil, apierror.Invalid("invalid participant jid: " + participant)
		}
		participants = append(participants, utils.CanonicalJID(participantJID))
	}

	results, err := client.UpdateGroupRequestParticipants(context.Background(), recipient, participants, action)
	if err != nil {
		g.loggerWrapper.GetLogger(instance.Id).LogError("[%s] error updating group request participants: %v", instance.Id, err)
		return nil, err
	}

	g.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] Successfully %sd %d participants", instance.Id, data.Action, len(participants))
	return results, nil
}

func NewGroupService(
	clientPointer *safemap.Map[*whatsmeow.Client],
	whatsmeowService whatsmeow_service.WhatsmeowService,
	loggerWrapper *logger_wrapper.LoggerManager,
) GroupService {
	return &groupService{
		clientPointer:    clientPointer,
		whatsmeowService: whatsmeowService,
		loggerWrapper:    loggerWrapper,
	}
}

// isGroupOwner tells whether the logged-in account owns the group. The owner comes as
// a LID (and, when known, a phone number), while the account's own JID carries a device
// suffix ("5511...:82@s.whatsapp.net"), so the users have to be compared, not the JIDs.
func isGroupOwner(client *whatsmeow.Client, group *types.GroupInfo) bool {
	if group == nil || client == nil || client.Store == nil {
		return false
	}
	ownsUser := func(owner types.JID) bool {
		if owner.IsEmpty() {
			return false
		}
		if id := client.Store.ID; id != nil && owner.Server == types.DefaultUserServer && owner.User == id.User {
			return true
		}
		return !client.Store.LID.IsEmpty() && owner.Server == types.HiddenUserServer && owner.User == client.Store.LID.User
	}
	return ownsUser(group.OwnerJID) || ownsUser(group.OwnerPN)
}
