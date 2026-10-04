package label_handler

import (
	"net/http"

	"github.com/lucasgiovannibr/whatygo/pkg/apierror"
	instance_model "github.com/lucasgiovannibr/whatygo/pkg/instance/model"
	label_service "github.com/lucasgiovannibr/whatygo/pkg/label/service"
	"github.com/gin-gonic/gin"
)

type LabelHandler interface {
	ChatLabel(ctx *gin.Context)
	MessageLabel(ctx *gin.Context)
	EditLabel(ctx *gin.Context)
	ChatUnlabel(ctx *gin.Context)
	MessageUnlabel(ctx *gin.Context)
	GetLabels(ctx *gin.Context)
}

type labelHandler struct {
	labelService label_service.LabelService
}

// Add label to chat
// @Summary Add label to chat
// @Description Add label to chat
// @Tags Label
// @Accept json
// @Produce json
// @Param message body label_service.ChatLabelStruct true "Label data"
// @Success 200 {object} gin.H "success"
// @Failure 400 {object} gin.H "Error on validation"
// @Failure 500 {object} gin.H "Internal server error"
// @Router /label/chat [post]
func (l *labelHandler) ChatLabel(ctx *gin.Context) {
	getInstance := ctx.MustGet("instance")

	instance, ok := getInstance.(*instance_model.Instance)
	if !ok {
		apierror.Fail(ctx, http.StatusInternalServerError, "instance not found")
		return
	}

	var data *label_service.ChatLabelStruct
	err := ctx.ShouldBindBodyWithJSON(&data)
	if err != nil {
		apierror.BadRequest(ctx, err)
		return
	}

	if data.JID == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "jid is required")
		return
	}

	if data.LabelID == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "label id is required")
		return
	}

	err = l.labelService.ChatLabel(data, instance)
	if err != nil {
		apierror.Respond(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "success"})
}

// Add label to message
// @Summary Add label to message
// @Description Add label to message
// @Tags Label
// @Accept json
// @Produce json
// @Param message body label_service.MessageLabelStruct true "Label data"
// @Success 200 {object} gin.H "success"
// @Failure 400 {object} gin.H "Error on validation"
// @Failure 500 {object} gin.H "Internal server error"
// @Router /label/message [post]
func (l *labelHandler) MessageLabel(ctx *gin.Context) {
	getInstance := ctx.MustGet("instance")

	instance, ok := getInstance.(*instance_model.Instance)
	if !ok {
		apierror.Fail(ctx, http.StatusInternalServerError, "instance not found")
		return
	}

	var data *label_service.MessageLabelStruct
	err := ctx.ShouldBindBodyWithJSON(&data)
	if err != nil {
		apierror.BadRequest(ctx, err)
		return
	}

	if data.JID == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "jid is required")
		return
	}

	if data.LabelID == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "label id is required")
		return
	}

	if data.MessageID == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "message id is required")
		return
	}

	err = l.labelService.MessageLabel(data, instance)
	if err != nil {
		apierror.Respond(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "success"})
}

// Edit label
// @Summary Edit label
// @Description Edit label
// @Tags Label
// @Accept json
// @Produce json
// @Param message body label_service.EditLabelStruct true "Label data"
// @Success 200 {object} gin.H "success"
// @Failure 400 {object} gin.H "Error on validation"
// @Failure 500 {object} gin.H "Internal server error"
// @Router /label/edit [post]
func (l *labelHandler) EditLabel(ctx *gin.Context) {
	getInstance := ctx.MustGet("instance")

	instance, ok := getInstance.(*instance_model.Instance)
	if !ok {
		apierror.Fail(ctx, http.StatusInternalServerError, "instance not found")
		return
	}

	var data *label_service.EditLabelStruct
	err := ctx.ShouldBindBodyWithJSON(&data)
	if err != nil {
		apierror.BadRequest(ctx, err)
		return
	}

	if data.LabelID == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "label id is required")
		return
	}

	if data.Name == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "name is required")
		return
	}

	err = l.labelService.EditLabel(data, instance)
	if err != nil {
		apierror.Respond(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "success"})
}

// Remove label from chat
// @Summary Remove label from chat
// @Description Remove label from chat
// @Tags Label
// @Accept json
// @Produce json
// @Param message body label_service.ChatLabelStruct true "Label data"
// @Success 200 {object} gin.H "success"
// @Failure 400 {object} gin.H "Error on validation"
// @Failure 500 {object} gin.H "Internal server error"
// @Router /unlabel/chat [post]
func (l *labelHandler) ChatUnlabel(ctx *gin.Context) {
	getInstance := ctx.MustGet("instance")

	instance, ok := getInstance.(*instance_model.Instance)
	if !ok {
		apierror.Fail(ctx, http.StatusInternalServerError, "instance not found")
		return
	}

	var data *label_service.ChatLabelStruct
	err := ctx.ShouldBindBodyWithJSON(&data)
	if err != nil {
		apierror.BadRequest(ctx, err)
		return
	}

	if data.JID == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "jid is required")
		return
	}

	if data.LabelID == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "label id is required")
		return
	}

	err = l.labelService.ChatUnlabel(data, instance)
	if err != nil {
		apierror.Respond(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "success"})
}

// Remove label from message
// @Summary Remove label from message
// @Description Remove label from message
// @Tags Label
// @Accept json
// @Produce json
// @Param message body label_service.MessageLabelStruct true "Label data"
// @Success 200 {object} gin.H "success"
// @Failure 400 {object} gin.H "Error on validation"
// @Failure 500 {object} gin.H "Internal server error"
// @Router /unlabel/message [post]
func (l *labelHandler) MessageUnlabel(ctx *gin.Context) {
	getInstance := ctx.MustGet("instance")

	instance, ok := getInstance.(*instance_model.Instance)
	if !ok {
		apierror.Fail(ctx, http.StatusInternalServerError, "instance not found")
		return
	}

	var data *label_service.MessageLabelStruct
	err := ctx.ShouldBindBodyWithJSON(&data)
	if err != nil {
		apierror.BadRequest(ctx, err)
		return
	}

	if data.JID == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "jid is required")
		return
	}

	if data.LabelID == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "label id is required")
		return
	}

	if data.MessageID == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "message id is required")
		return
	}

	err = l.labelService.MessageUnlabel(data, instance)
	if err != nil {
		apierror.Respond(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "success"})
}

// Get all labels
// @Summary Get all labels
// @Description Get all labels
// @Tags Label
// @Accept json
// @Produce json
// @Success 200 {object} gin.H "success"
// @Failure 500 {object} gin.H "Internal server error"
// @Router /label/list [get]
func (l *labelHandler) GetLabels(ctx *gin.Context) {
	getInstance := ctx.MustGet("instance")

	instance, ok := getInstance.(*instance_model.Instance)
	if !ok {
		apierror.Fail(ctx, http.StatusInternalServerError, "instance not found")
		return
	}

	labels, err := l.labelService.GetLabels(instance)
	if err != nil {
		apierror.Respond(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, labels)
}

func NewLabelHandler(
	labelService label_service.LabelService,
) LabelHandler {
	return &labelHandler{
		labelService: labelService,
	}
}
