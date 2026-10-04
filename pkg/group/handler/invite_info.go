package group_handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/lucasgiovannibr/whatygo/pkg/apierror"
	group_service "github.com/lucasgiovannibr/whatygo/pkg/group/service"
	instance_model "github.com/lucasgiovannibr/whatygo/pkg/instance/model"
)

// Group info from an invite
// @Summary Group info from an invite
// @Description Look a group up from an invite WITHOUT joining it. For an invite link send only `code` (the code or the whole link). For an invite message received in a chat send groupJid, inviter, code and expiration.
// @Tags Group
// @Accept json
// @Produce json
// @Param message body group_service.GroupInviteStruct true "Invite"
// @Success 200 {object} gin.H "success"
// @Failure 400 {object} gin.H "Error on validation"
// @Failure 500 {object} gin.H "Internal server error"
// @Router /group/inviteinfo [post]
func (g *groupHandler) GetInviteInfo(ctx *gin.Context) {
	instance, ok := ctx.MustGet("instance").(*instance_model.Instance)
	if !ok {
		apierror.Fail(ctx, http.StatusInternalServerError, "instance not found")
		return
	}

	var data *group_service.GroupInviteStruct
	if err := ctx.ShouldBindBodyWithJSON(&data); err != nil {
		apierror.BadRequest(ctx, err)
		return
	}
	if data.Code == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "code is required")
		return
	}

	info, err := g.groupService.GetInviteInfo(data, instance)
	if err != nil {
		apierror.Respond(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "success", "data": info})
}

// Join a group from an invite message
// @Summary Join a group from an invite message
// @Description Join a group from the invite message (the "join this group" card) received in a chat. For an invite link use /group/join.
// @Tags Group
// @Accept json
// @Produce json
// @Param message body group_service.GroupInviteStruct true "Invite message"
// @Success 200 {object} gin.H "success"
// @Failure 400 {object} gin.H "Error on validation"
// @Failure 500 {object} gin.H "Internal server error"
// @Router /group/joininvite [post]
func (g *groupHandler) JoinGroupInvite(ctx *gin.Context) {
	instance, ok := ctx.MustGet("instance").(*instance_model.Instance)
	if !ok {
		apierror.Fail(ctx, http.StatusInternalServerError, "instance not found")
		return
	}

	var data *group_service.GroupInviteStruct
	if err := ctx.ShouldBindBodyWithJSON(&data); err != nil {
		apierror.BadRequest(ctx, err)
		return
	}
	if data.Code == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "code is required")
		return
	}
	if data.GroupJID == "" || data.Inviter == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "groupJid and inviter are required (use /group/join for an invite link)")
		return
	}

	if err := g.groupService.JoinGroupInvite(data, instance); err != nil {
		apierror.Respond(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "success"})
}
