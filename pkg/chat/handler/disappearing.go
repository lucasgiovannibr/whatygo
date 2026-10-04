package chat_handler

import (
	"net/http"

	"github.com/lucasgiovannibr/whatygo/pkg/apierror"
	chat_service "github.com/lucasgiovannibr/whatygo/pkg/chat/service"
	instance_model "github.com/lucasgiovannibr/whatygo/pkg/instance/model"
	"github.com/gin-gonic/gin"
)

// Set the disappearing-messages timer of a chat
// @Summary Set the disappearing-messages timer of a chat
// @Description Turns disappearing messages on/off in a contact or group chat. timer: "off", "24h", "7d" or "90d". Afterwards the messages sent to that chat carry the timer automatically.
// @Tags Chat
// @Accept json
// @Produce json
// @Param message body chat_service.DisappearingStruct true "Chat and timer"
// @Success 200 {object} gin.H "success"
// @Failure 400 {object} gin.H "Error on validation"
// @Failure 500 {object} gin.H "Internal server error"
// @Router /chat/disappearing [post]
func (c *chatHandler) SetDisappearing(ctx *gin.Context) {
	instance, ok := ctx.MustGet("instance").(*instance_model.Instance)
	if !ok {
		apierror.Fail(ctx, http.StatusInternalServerError, "instance not found")
		return
	}

	var data *chat_service.DisappearingStruct
	if err := ctx.ShouldBindBodyWithJSON(&data); err != nil {
		apierror.BadRequest(ctx, err)
		return
	}
	if data.Chat == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "chat is required")
		return
	}

	if err := c.chatService.SetDisappearing(data, instance); err != nil {
		apierror.RespondWith(ctx, err, statusForChatError(err))
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "success"})
}

// Set the default disappearing-messages timer
// @Summary Set the default disappearing-messages timer
// @Description Timer that new one-to-one chats start with. timer: "off", "24h", "7d" or "90d".
// @Tags User
// @Accept json
// @Produce json
// @Param message body chat_service.DefaultDisappearingStruct true "Timer"
// @Success 200 {object} gin.H "success"
// @Failure 400 {object} gin.H "Error on validation"
// @Failure 500 {object} gin.H "Internal server error"
// @Router /user/defaultDisappearing [post]
func (c *chatHandler) SetDefaultDisappearing(ctx *gin.Context) {
	instance, ok := ctx.MustGet("instance").(*instance_model.Instance)
	if !ok {
		apierror.Fail(ctx, http.StatusInternalServerError, "instance not found")
		return
	}

	var data *chat_service.DefaultDisappearingStruct
	if err := ctx.ShouldBindBodyWithJSON(&data); err != nil {
		apierror.BadRequest(ctx, err)
		return
	}

	if err := c.chatService.SetDefaultDisappearing(data, instance); err != nil {
		apierror.RespondWith(ctx, err, statusForChatError(err))
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "success"})
}

// statusForChatError tells a request the caller got wrong (400) from a
// failure on the WhatsApp side (500).
func statusForChatError(err error) int {
	if chat_service.IsDisappearingRequestError(err) {
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}
