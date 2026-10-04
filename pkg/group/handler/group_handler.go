package group_handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/lucasgiovannibr/whatygo/pkg/apierror"
	group_service "github.com/lucasgiovannibr/whatygo/pkg/group/service"
	instance_model "github.com/lucasgiovannibr/whatygo/pkg/instance/model"
)

type GroupHandler interface {
	ListGroups(ctx *gin.Context)
	GetGroupInfo(ctx *gin.Context)
	GetGroupInviteLink(ctx *gin.Context)
	SetGroupPhoto(ctx *gin.Context)
	SetGroupName(ctx *gin.Context)
	SetGroupDescription(ctx *gin.Context)
	CreateGroup(ctx *gin.Context)
	UpdateParticipant(ctx *gin.Context)
	GetMyGroups(ctx *gin.Context)
	JoinGroupLink(ctx *gin.Context)
	GetInviteInfo(ctx *gin.Context)
	JoinGroupInvite(ctx *gin.Context)
	LeaveGroup(ctx *gin.Context)
	UpdateGroupSettings(ctx *gin.Context)
	GetGroupRequests(ctx *gin.Context)
	UpdateGroupRequests(ctx *gin.Context)
}

type groupHandler struct {
	groupService group_service.GroupService
}

// List groups
// @Summary List groups
// @Description List groups
// @Tags Group
// @Accept json
// @Produce json
// @Success 200 {object} gin.H "success"
// @Failure 500 {object} gin.H "Internal server error"
// @Router /group/list [get]
func (g *groupHandler) ListGroups(ctx *gin.Context) {
	getInstance := ctx.MustGet("instance")

	instance, ok := getInstance.(*instance_model.Instance)
	if !ok {
		apierror.Fail(ctx, http.StatusInternalServerError, "instance not found")
		return
	}

	resp, err := g.groupService.ListGroups(instance)
	if err != nil {
		apierror.Respond(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "success", "data": resp})
}

// Get group info
// @Summary Get group info
// @Description Get group info
// @Tags Group
// @Accept json
// @Produce json
// @Param message body group_service.GetGroupInfoStruct true "Group data"
// @Success 200 {object} gin.H "success"
// @Failure 400 {object} gin.H "Error on validation"
// @Failure 500 {object} gin.H "Internal server error"
// @Router /group/info [post]
func (g *groupHandler) GetGroupInfo(ctx *gin.Context) {
	getInstance := ctx.MustGet("instance")

	instance, ok := getInstance.(*instance_model.Instance)
	if !ok {
		apierror.Fail(ctx, http.StatusInternalServerError, "instance not found")
		return
	}

	var data *group_service.GetGroupInfoStruct
	err := ctx.ShouldBindBodyWithJSON(&data)
	if err != nil {
		apierror.BadRequest(ctx, err)
		return
	}

	if data.GroupJID == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "groupJID is required")
		return
	}

	resp, err := g.groupService.GetGroupInfo(data, instance)
	if err != nil {
		apierror.Respond(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "success", "data": resp})
}

// Get group invite link
// @Summary Get group invite link
// @Description Get group invite link
// @Tags Group
// @Accept json
// @Produce json
// @Param message body group_service.GetGroupInviteLinkStruct true "Group data"
// @Success 200 {object} gin.H "success"
// @Failure 400 {object} gin.H "Error on validation"
// @Failure 500 {object} gin.H "Internal server error"
// @Router /group/invitelink [post]
func (g *groupHandler) GetGroupInviteLink(ctx *gin.Context) {
	getInstance := ctx.MustGet("instance")

	instance, ok := getInstance.(*instance_model.Instance)
	if !ok {
		apierror.Fail(ctx, http.StatusInternalServerError, "instance not found")
		return
	}

	var data *group_service.GetGroupInviteLinkStruct
	err := ctx.ShouldBindBodyWithJSON(&data)
	if err != nil {
		apierror.BadRequest(ctx, err)
		return
	}

	if data.GroupJID == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "groupJID is required")
		return
	}

	resp, err := g.groupService.GetGroupInviteLink(data, instance)
	if err != nil {
		apierror.Respond(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "success", "data": resp})
}

// Set group photo
// @Summary Set group photo
// @Description Set group photo
// @Tags Group
// @Accept json
// @Produce json
// @Param message body group_service.SetGroupPhotoStruct true "Group data"
// @Success 200 {object} gin.H "success"
// @Failure 400 {object} gin.H "Error on validation"
// @Failure 500 {object} gin.H "Internal server error"
// @Router /group/photo [post]
func (g *groupHandler) SetGroupPhoto(ctx *gin.Context) {
	getInstance := ctx.MustGet("instance")

	instance, ok := getInstance.(*instance_model.Instance)
	if !ok {
		apierror.Fail(ctx, http.StatusInternalServerError, "instance not found")
		return
	}

	var data *group_service.SetGroupPhotoStruct
	err := ctx.ShouldBindBodyWithJSON(&data)
	if err != nil {
		apierror.BadRequest(ctx, err)
		return
	}

	if data.GroupJID == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "groupJID is required")
		return
	}

	if data.Image == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "image is required")
		return
	}

	resp, err := g.groupService.SetGroupPhoto(data, instance)
	if err != nil {
		apierror.Respond(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "success", "data": resp})
}

// Set group name
// @Summary Set group name
// @Description Set group name
// @Tags Group
// @Accept json
// @Produce json
// @Param message body group_service.SetGroupNameStruct true "Group data"
// @Success 200 {object} gin.H "success"
// @Failure 400 {object} gin.H "Error on validation"
// @Failure 500 {object} gin.H "Internal server error"
// @Router /group/name [post]
func (g *groupHandler) SetGroupName(ctx *gin.Context) {
	getInstance := ctx.MustGet("instance")

	instance, ok := getInstance.(*instance_model.Instance)
	if !ok {
		apierror.Fail(ctx, http.StatusInternalServerError, "instance not found")
		return
	}

	var data *group_service.SetGroupNameStruct
	err := ctx.ShouldBindBodyWithJSON(&data)
	if err != nil {
		apierror.BadRequest(ctx, err)
		return
	}

	if data.GroupJID == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "groupJID is required")
		return
	}

	if data.Name == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "name is required")
		return
	}

	err = g.groupService.SetGroupName(data, instance)
	if err != nil {
		apierror.Respond(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "success"})
}

// Set group description
// @Summary Set group description
// @Description Set group description
// @Tags Group
// @Accept json
// @Produce json
// @Param message body group_service.SetGroupDescriptionStruct true "Group data"
// @Success 200 {object} gin.H "success"
// @Failure 400 {object} gin.H "Error on validation"
// @Failure 500 {object} gin.H "Internal server error"
// @Router /group/description [post]
func (g *groupHandler) SetGroupDescription(ctx *gin.Context) {
	getInstance := ctx.MustGet("instance")

	instance, ok := getInstance.(*instance_model.Instance)
	if !ok {
		apierror.Fail(ctx, http.StatusInternalServerError, "instance not found")
		return
	}

	var data *group_service.SetGroupDescriptionStruct
	err := ctx.ShouldBindBodyWithJSON(&data)
	if err != nil {
		apierror.BadRequest(ctx, err)
		return
	}

	if data.GroupJID == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "groupJID is required")
		return
	}

	// Description can be empty to clear the group description
	// No validation needed for Description field

	err = g.groupService.SetGroupDescription(data, instance)
	if err != nil {
		apierror.Respond(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "success"})
}

// Create group
// @Summary Create group
// @Description Create group
// @Tags Group
// @Accept json
// @Produce json
// @Param message body group_service.CreateGroupStruct true "Group data"
// @Success 200 {object} gin.H "success"
// @Failure 400 {object} gin.H "Error on validation"
// @Failure 500 {object} gin.H "Internal server error"
// @Router /group/create [post]
func (g *groupHandler) CreateGroup(ctx *gin.Context) {
	getInstance := ctx.MustGet("instance")

	instance, ok := getInstance.(*instance_model.Instance)
	if !ok {
		apierror.Fail(ctx, http.StatusInternalServerError, "instance not found")
		return
	}

	var data *group_service.CreateGroupStruct
	err := ctx.ShouldBindBodyWithJSON(&data)
	if err != nil {
		apierror.BadRequest(ctx, err)
		return
	}

	if data.GroupName == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "groupName is required")
		return
	}

	if len(data.Participants) < 1 {
		apierror.Fail(ctx, http.StatusBadRequest, "participants are required")
		return
	}

	group, err := g.groupService.CreateGroup(data, instance)
	if err != nil {
		apierror.Respond(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "success", "data": group})
}

// Update participant
// @Summary Update participant
// @Description Update participant
// @Tags Group
// @Accept json
// @Produce json
// @Param message body group_service.AddParticipantStruct true "Group data"
// @Success 200 {object} gin.H "success"
// @Failure 400 {object} gin.H "Error on validation"
// @Failure 500 {object} gin.H "Internal server error"
// @Router /group/participant [post]
func (g *groupHandler) UpdateParticipant(ctx *gin.Context) {
	getInstance := ctx.MustGet("instance")

	instance, ok := getInstance.(*instance_model.Instance)
	if !ok {
		apierror.Fail(ctx, http.StatusInternalServerError, "instance not found")
		return
	}

	var data *group_service.AddParticipantStruct
	err := ctx.ShouldBindBodyWithJSON(&data)
	if err != nil {
		apierror.BadRequest(ctx, err)
		return
	}

	if data.GroupJID.String() == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "groupJid is required")
		return
	}

	if data.Action == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "action is required")
		return
	}

	if len(data.Participants) < 1 {
		apierror.Fail(ctx, http.StatusBadRequest, "participants are required")
		return
	}

	results, err := g.groupService.UpdateParticipant(data, instance)
	if err != nil {
		apierror.Respond(ctx, err)
		return
	}

	// Per-participant outcome: Error is 0 on success (e.g. 404 = number not on
	// WhatsApp, 403 = not allowed). The request itself still answers 200.
	failed := 0
	for _, r := range results {
		if r.Error != 0 {
			failed++
		}
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "success", "data": results, "failed": failed})
}

// Get my groups
// @Summary Get my groups
// @Description Get my groups
// @Tags Group
// @Accept json
// @Produce json
// @Success 200 {object} gin.H "success"
// @Failure 500 {object} gin.H "Internal server error"
// @Router /group/myall [get]
func (g *groupHandler) GetMyGroups(ctx *gin.Context) {
	getInstance := ctx.MustGet("instance")

	instance, ok := getInstance.(*instance_model.Instance)
	if !ok {
		apierror.Fail(ctx, http.StatusInternalServerError, "instance not found")
		return
	}

	groups, err := g.groupService.GetMyGroups(instance)
	if err != nil {
		apierror.Respond(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "success", "data": groups})
}

// Join group link
// @Summary Join group link
// @Description Join group link
// @Tags Group
// @Accept json
// @Produce json
// @Param message body group_service.JoinGroupStruct true "Group data"
// @Success 200 {object} gin.H "success"
// @Failure 400 {object} gin.H "Error on validation"
// @Failure 500 {object} gin.H "Internal server error"
// @Router /group/join [post]
func (g *groupHandler) JoinGroupLink(ctx *gin.Context) {
	getInstance := ctx.MustGet("instance")

	instance, ok := getInstance.(*instance_model.Instance)
	if !ok {
		apierror.Fail(ctx, http.StatusInternalServerError, "instance not found")
		return
	}

	var data *group_service.JoinGroupStruct
	err := ctx.ShouldBindBodyWithJSON(&data)
	if err != nil {
		apierror.BadRequest(ctx, err)
		return
	}

	if data.Code == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "code is required")
		return
	}

	err = g.groupService.JoinGroupLink(data, instance)
	if err != nil {
		apierror.Respond(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "success"})
}

// Leave group
// @Summary Leave group
// @Description Leave group
// @Tags Group
// @Accept json
// @Produce json
// @Param message body group_service.LeaveGroupStruct true "Group data"
// @Success 200 {object} gin.H "success"
// @Failure 400 {object} gin.H "Error on validation"
// @Failure 500 {object} gin.H "Internal server error"
// @Router /group/leave [post]
func (g *groupHandler) LeaveGroup(ctx *gin.Context) {
	getInstance := ctx.MustGet("instance")

	instance, ok := getInstance.(*instance_model.Instance)
	if !ok {
		apierror.Fail(ctx, http.StatusInternalServerError, "instance not found")
		return
	}

	var data *group_service.LeaveGroupStruct
	err := ctx.ShouldBindBodyWithJSON(&data)
	if err != nil {
		apierror.BadRequest(ctx, err)
		return
	}

	if data.GroupJID.String() == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "groupJid is required")
		return
	}

	err = g.groupService.LeaveGroup(data, instance)
	if err != nil {
		apierror.Respond(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "success"})
}

// Update group settings
// @Summary Update group settings
// @Description Update group settings (announcement, not_announcement, locked, unlocked, approval_on, approval_off, admin_add, all_member_add)
// @Tags Group
// @Accept json
// @Produce json
// @Param message body group_service.UpdateGroupSettingsStruct true "Group data"
// @Success 200 {object} gin.H "success"
// @Failure 400 {object} gin.H "Error on validation"
// @Failure 500 {object} gin.H "Internal server error"
// @Router /group/settings [post]
func (g *groupHandler) UpdateGroupSettings(ctx *gin.Context) {
	getInstance := ctx.MustGet("instance")

	instance, ok := getInstance.(*instance_model.Instance)
	if !ok {
		apierror.Fail(ctx, http.StatusInternalServerError, "instance not found")
		return
	}

	var data *group_service.UpdateGroupSettingsStruct
	err := ctx.ShouldBindBodyWithJSON(&data)
	if err != nil {
		apierror.BadRequest(ctx, err)
		return
	}

	if data.GroupJID == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "groupJid is required")
		return
	}

	if data.Action == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "action is required")
		return
	}

	err = g.groupService.UpdateGroupSettings(data, instance)
	if err != nil {
		apierror.Respond(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "success"})
}

// List pending join requests
// @Summary List pending join requests
// @Description List the people waiting for approval to join a group (groups with join approval enabled)
// @Tags Group
// @Accept json
// @Produce json
// @Param message body group_service.GetGroupRequestParticipantsStruct true "Group data"
// @Success 200 {object} gin.H "success"
// @Failure 400 {object} gin.H "Error on validation"
// @Failure 500 {object} gin.H "Internal server error"
// @Router /group/requests [post]
func (g *groupHandler) GetGroupRequests(ctx *gin.Context) {
	getInstance := ctx.MustGet("instance")

	instance, ok := getInstance.(*instance_model.Instance)
	if !ok {
		apierror.Fail(ctx, http.StatusInternalServerError, "instance not found")
		return
	}

	var data *group_service.GetGroupRequestParticipantsStruct
	if err := ctx.ShouldBindBodyWithJSON(&data); err != nil {
		apierror.BadRequest(ctx, err)
		return
	}

	if data.GroupJID == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "groupJid is required")
		return
	}

	requests, err := g.groupService.GetGroupRequestParticipants(data, instance)
	if err != nil {
		apierror.Respond(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "success", "data": requests})
}

// Approve or reject pending join requests
// @Summary Approve or reject pending join requests
// @Description Approve or reject people waiting to join a group. action: approve | reject
// @Tags Group
// @Accept json
// @Produce json
// @Param message body group_service.UpdateGroupRequestParticipantsStruct true "Request data"
// @Success 200 {object} gin.H "success"
// @Failure 400 {object} gin.H "Error on validation"
// @Failure 500 {object} gin.H "Internal server error"
// @Router /group/requests/update [post]
func (g *groupHandler) UpdateGroupRequests(ctx *gin.Context) {
	getInstance := ctx.MustGet("instance")

	instance, ok := getInstance.(*instance_model.Instance)
	if !ok {
		apierror.Fail(ctx, http.StatusInternalServerError, "instance not found")
		return
	}

	var data *group_service.UpdateGroupRequestParticipantsStruct
	if err := ctx.ShouldBindBodyWithJSON(&data); err != nil {
		apierror.BadRequest(ctx, err)
		return
	}

	if data.GroupJID == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "groupJid is required")
		return
	}
	if data.Action == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "action is required")
		return
	}
	if len(data.Participants) == 0 {
		apierror.Fail(ctx, http.StatusBadRequest, "participants is required and cannot be empty")
		return
	}

	results, err := g.groupService.UpdateGroupRequestParticipants(data, instance)
	if err != nil {
		apierror.Respond(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "success", "data": results})
}

func NewGroupHandler(
	groupService group_service.GroupService,
) GroupHandler {
	return &groupHandler{
		groupService: groupService,
	}
}
