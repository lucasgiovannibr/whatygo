package message_handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/lucasgiovannibr/whatygo/pkg/apierror"
	instance_model "github.com/lucasgiovannibr/whatygo/pkg/instance/model"
	message_service "github.com/lucasgiovannibr/whatygo/pkg/message/service"
)

// Ask the phone to resend a message that did not arrive
// @Summary Ask the phone to resend a message that could not be decrypted
// @Description Use the id, chat and sender of an UndecryptableMessage event. The phone's answer arrives as a normal Message event whose UnavailableRequestID is the returned requestId. sender is optional in a one-to-one chat and required in a group.
// @Tags Message
// @Accept json
// @Produce json
// @Param message body message_service.RerequestStruct true "Message to request again"
// @Success 200 {object} gin.H "success"
// @Failure 400 {object} gin.H "Error on validation"
// @Failure 500 {object} gin.H "Internal server error"
// @Router /message/rerequest [post]
func (m *messageHandler) RerequestMessage(ctx *gin.Context) {
	instance, ok := ctx.MustGet("instance").(*instance_model.Instance)
	if !ok {
		apierror.Fail(ctx, http.StatusInternalServerError, "instance not found")
		return
	}

	var data *message_service.RerequestStruct
	if err := ctx.ShouldBindBodyWithJSON(&data); err != nil || data == nil {
		msg := "invalid body"
		if err != nil {
			msg = err.Error()
		}
		apierror.Fail(ctx, http.StatusBadRequest, msg)
		return
	}

	requestID, err := m.messageService.RerequestMessage(data, instance)
	if err != nil {
		status := http.StatusInternalServerError
		if message_service.IsRequestError(err) {
			status = http.StatusBadRequest
		}
		apierror.RespondWith(ctx, err, status)
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "success", "data": gin.H{"requestId": requestID}})
}
