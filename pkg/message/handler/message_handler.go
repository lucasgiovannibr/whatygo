package message_handler

import (
	"net/http"

	"github.com/lucasgiovannibr/whatygo/pkg/apierror"
	instance_model "github.com/lucasgiovannibr/whatygo/pkg/instance/model"
	message_service "github.com/lucasgiovannibr/whatygo/pkg/message/service"
	"github.com/gin-gonic/gin"
)

type MessageHandler interface {
	React(ctx *gin.Context)
	ChatPresence(ctx *gin.Context)
	SubscribePresence(ctx *gin.Context)
	MarkRead(ctx *gin.Context)
	MarkPlayed(ctx *gin.Context)
	DownloadMedia(ctx *gin.Context)
	GetMessageStatus(ctx *gin.Context)
	DeleteMessageEveryone(ctx *gin.Context)
	RerequestMessage(ctx *gin.Context)
	EditMessage(ctx *gin.Context)
}

type messageHandler struct {
	messageService message_service.MessageService
}

// React a message
// @Summary React a message
// @Description React to a message with support for fromMe field and participant field for group messages
// @Tags Message
// @Accept json
// @Produce json
// @Param message body message_service.ReactStruct true "React to a message with fromMe and participant fields"
// @Success 200 {object} gin.H "success"
// @Failure 400 {object} gin.H "Error on validation"
// @Failure 500 {object} gin.H "Internal server error"
// @Router /message/react [post]
func (m *messageHandler) React(ctx *gin.Context) {
	getInstance := ctx.MustGet("instance")

	instance, ok := getInstance.(*instance_model.Instance)
	if !ok {
		apierror.Fail(ctx, http.StatusInternalServerError, "instance not found")
		return
	}

	var data *message_service.ReactStruct
	err := ctx.ShouldBindBodyWithJSON(&data)
	if err != nil {
		apierror.BadRequest(ctx, err)
		return
	}

	if data.Number == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "phone number is required")
		return
	}

	if data.Reaction == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "message reaction is required")
		return
	}

	message, err := m.messageService.React(data, instance)
	if err != nil {
		apierror.Respond(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "success", "data": message})
}

// ChatPresence set chat presence
// @Summary Set chat presence
// @Description Set chat presence
// @Tags Message
// @Accept json
// @Produce json
// @Param message body message_service.ChatPresenceStruct true "Set chat presence"
// @Success 200 {object} gin.H "success"
// @Failure 400 {object} gin.H "Error on validation"
// @Failure 500 {object} gin.H "Internal server error"
// @Router /message/presence [post]
func (m *messageHandler) ChatPresence(ctx *gin.Context) {
	getInstance := ctx.MustGet("instance")

	instance, ok := getInstance.(*instance_model.Instance)
	if !ok {
		apierror.Fail(ctx, http.StatusInternalServerError, "instance not found")
		return
	}

	var data *message_service.ChatPresenceStruct
	err := ctx.ShouldBindBodyWithJSON(&data)
	if err != nil {
		apierror.BadRequest(ctx, err)
		return
	}

	if data.Number == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "phone number is required")
		return
	}

	if data.State == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "state is required")
		return
	}

	ts, err := m.messageService.ChatPresence(data, instance)
	if err != nil {
		apierror.Respond(ctx, err)
		return
	}

	responseData := gin.H{
		"timestamp": ts,
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "success", "data": responseData})
}

// SubscribePresence subscribe to a contact's presence (online / last-seen)
// @Summary Subscribe to a contact's presence
// @Description Subscribe to a contact's presence so the instance starts receiving Presence (online/offline/last-seen) webhook events. number is one number or a list (up to 100); a list answers with the result of each number in data and the ones that failed in failed.
// @Tags Message
// @Accept json
// @Produce json
// @Param message body message_service.SubscribePresenceStruct true "Number to subscribe presence for"
// @Success 200 {object} gin.H "success"
// @Failure 400 {object} gin.H "Error on validation"
// @Failure 500 {object} gin.H "Internal server error"
// @Router /message/subscribe [post]
func (m *messageHandler) SubscribePresence(ctx *gin.Context) {
	getInstance := ctx.MustGet("instance")

	instance, ok := getInstance.(*instance_model.Instance)
	if !ok {
		apierror.Fail(ctx, http.StatusInternalServerError, "instance not found")
		return
	}

	var data *message_service.SubscribePresenceStruct
	err := ctx.ShouldBindBodyWithJSON(&data)
	if err != nil {
		apierror.BadRequest(ctx, err)
		return
	}

	results, err := m.messageService.SubscribePresence(data, instance)
	if err != nil {
		status := http.StatusInternalServerError
		if message_service.IsRequestError(err) {
			status = http.StatusBadRequest
		}
		apierror.RespondWith(ctx, err, status)
		return
	}

	// A single number keeps the answer it always had.
	if !data.Number.IsList {
		if r := results[0]; !r.Subscribed {
			status := http.StatusInternalServerError
			if r.Invalid {
				status = http.StatusBadRequest
			}
			apierror.Fail(ctx, status, r.Error)
			return
		}
		ctx.JSON(http.StatusOK, gin.H{"message": "success"})
		return
	}

	// A list reports each number; it only fails as a whole when none was subscribed.
	var failed []message_service.SubscribeResult
	for _, r := range results {
		if !r.Subscribed {
			failed = append(failed, r)
		}
	}
	status := http.StatusOK
	if len(failed) == len(results) {
		status = http.StatusInternalServerError
	}
	ctx.JSON(status, gin.H{"message": "success", "data": results, "failed": failed})
}

// MarkRead mark a message as read
// @Summary Mark a message as read
// @Description Mark a message as read
// @Tags Message
// @Accept json
// @Produce json
// @Param message body message_service.MarkReadStruct true "Mark a message as read"
// @Success 200 {object} gin.H "success"
// @Failure 400 {object} gin.H "Error on validation"
// @Failure 500 {object} gin.H "Internal server error"
// @Router /message/markread [post]
func (m *messageHandler) MarkRead(ctx *gin.Context) {
	getInstance := ctx.MustGet("instance")

	instance, ok := getInstance.(*instance_model.Instance)
	if !ok {
		apierror.Fail(ctx, http.StatusInternalServerError, "instance not found")
		return
	}

	var data *message_service.MarkReadStruct
	err := ctx.ShouldBindBodyWithJSON(&data)
	if err != nil {
		apierror.BadRequest(ctx, err)
		return
	}

	if data.Number == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "phone number is required")
		return
	}

	if len(data.Id) < 1 {
		apierror.Fail(ctx, http.StatusBadRequest, "id is required")
		return
	}

	ts, err := m.messageService.MarkRead(data, instance)
	if err != nil {
		apierror.Respond(ctx, err)
		return
	}

	responseData := gin.H{
		"timestamp": ts,
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "success", "data": responseData})
}

// MarkPlayed mark an audio message as played (blue mic icon)
// @Summary Mark an audio message as played
// @Description Mark an audio message as played
// @Tags Message
// @Accept json
// @Produce json
// @Param message body message_service.MarkPlayedStruct true "Mark an audio message as played"
// @Success 200 {object} gin.H "success"
// @Failure 400 {object} gin.H "Error on validation"
// @Failure 500 {object} gin.H "Internal server error"
// @Router /message/markplayed [post]
func (m *messageHandler) MarkPlayed(ctx *gin.Context) {
	getInstance := ctx.MustGet("instance")

	instance, ok := getInstance.(*instance_model.Instance)
	if !ok {
		apierror.Fail(ctx, http.StatusInternalServerError, "instance not found")
		return
	}

	var data *message_service.MarkPlayedStruct
	err := ctx.ShouldBindBodyWithJSON(&data)
	if err != nil {
		apierror.BadRequest(ctx, err)
		return
	}

	if data.Number == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "phone number is required")
		return
	}

	if len(data.Id) < 1 {
		apierror.Fail(ctx, http.StatusBadRequest, "id is required")
		return
	}

	ts, err := m.messageService.MarkPlayed(data, instance)
	if err != nil {
		apierror.Respond(ctx, err)
		return
	}

	responseData := gin.H{
		"timestamp": ts,
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "success", "data": responseData})
}

// DownloadMedia download a media message (image, video, audio, document)
// @Summary Download media
// @Description Download the media content of a message (image, video, audio or document)
// @Tags Message
// @Accept json
// @Produce json
// @Param message body message_service.DownloadMediaStruct true "Download media"
// @Success 200 {object} gin.H "success"
// @Failure 400 {object} gin.H "Error on validation"
// @Failure 500 {object} gin.H "Internal server error"
// @Router /message/downloadmedia [post]
func (m *messageHandler) DownloadMedia(ctx *gin.Context) {
	getInstance := ctx.MustGet("instance")

	instance, ok := getInstance.(*instance_model.Instance)
	if !ok {
		apierror.Fail(ctx, http.StatusInternalServerError, "instance not found")
		return
	}

	var data *message_service.DownloadMediaStruct
	err := ctx.ShouldBindBodyWithJSON(&data)
	if err != nil {
		apierror.BadRequest(ctx, err)
		return
	}

	dataUrl, ts, err := m.messageService.DownloadMedia(data, instance, ctx.Request)
	if err != nil {
		apierror.Respond(ctx, err)
		return
	}

	responseData := gin.H{
		"base64":    dataUrl.String(),
		"timestamp": ts,
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "success", "data": responseData})
}

// GetMessageStatus get message status
// @Summary Get message status
// @Description Get message status
// @Tags Message
// @Accept json
// @Produce json
// @Param message body message_service.MessageStatusStruct true "Get message status"
// @Success 200 {object} gin.H "success"
// @Failure 400 {object} gin.H "Error on validation"
// @Failure 500 {object} gin.H "Internal server error"
// @Router /message/status [post]
func (m *messageHandler) GetMessageStatus(ctx *gin.Context) {
	getInstance := ctx.MustGet("instance")

	instance, ok := getInstance.(*instance_model.Instance)
	if !ok {
		apierror.Fail(ctx, http.StatusInternalServerError, "instance not found")
		return
	}

	var data *message_service.MessageStatusStruct
	err := ctx.ShouldBindBodyWithJSON(&data)
	if err != nil {
		apierror.BadRequest(ctx, err)
		return
	}

	if data.Id == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "id is required")
		return
	}

	message, ts, err := m.messageService.GetMessageStatus(data, instance)
	if err != nil {
		apierror.Respond(ctx, err)
		return
	}

	responseData := gin.H{
		"result":    message,
		"timestamp": ts,
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "success", "data": responseData})
}

// DeleteMessageEveryone delete a message for everyone
// @Summary Delete a message for everyone
// @Description Delete a message for everyone
// @Tags Message
// @Accept json
// @Produce json
// @Param message body message_service.MessageStruct true "Delete a message for everyone"
// @Success 200 {object} gin.H "success"
// @Failure 400 {object} gin.H "Error on validation"
// @Failure 500 {object} gin.H "Internal server error"
// @Router /message/delete [post]
func (m *messageHandler) DeleteMessageEveryone(ctx *gin.Context) {
	getInstance := ctx.MustGet("instance")

	instance, ok := getInstance.(*instance_model.Instance)
	if !ok {
		apierror.Fail(ctx, http.StatusInternalServerError, "instance not found")
		return
	}

	var data *message_service.MessageStruct
	err := ctx.ShouldBindBodyWithJSON(&data)
	if err != nil {
		apierror.BadRequest(ctx, err)
		return
	}

	if data.Chat == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "chat is required")
		return
	}

	if data.MessageID == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "messageId is required")
		return
	}

	msgId, ts, err := m.messageService.DeleteMessageEveryone(data, instance)
	if err != nil {
		apierror.Respond(ctx, err)
		return
	}

	responseData := gin.H{
		"messageId": msgId,
		"timestamp": ts,
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "success", "data": responseData})
}

// EditMessage edit a message
// @Summary Edit a message
// @Description Edit a message
// @Tags Message
// @Accept json
// @Produce json
// @Param message body message_service.EditMessageStruct true "Edit a message"
// @Success 200 {object} gin.H "success"
// @Failure 400 {object} gin.H "Error on validation"
// @Failure 500 {object} gin.H "Internal server error"
// @Router /message/edit [post]
func (m *messageHandler) EditMessage(ctx *gin.Context) {
	getInstance := ctx.MustGet("instance")

	instance, ok := getInstance.(*instance_model.Instance)
	if !ok {
		apierror.Fail(ctx, http.StatusInternalServerError, "instance not found")
		return
	}

	var data *message_service.EditMessageStruct
	err := ctx.ShouldBindBodyWithJSON(&data)
	if err != nil {
		apierror.BadRequest(ctx, err)
		return
	}

	if data.Chat == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "chat is required")
		return
	}

	if data.Message == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "message is required")
		return
	}

	if data.MessageID == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "messageId is required")
		return
	}

	msgId, ts, err := m.messageService.EditMessage(data, instance)
	if err != nil {
		apierror.Respond(ctx, err)
		return
	}

	responseData := gin.H{
		"messageId": msgId,
		"timestamp": ts,
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "success", "data": responseData})
}

func NewMessageHandler(
	messageService message_service.MessageService,
) MessageHandler {
	return &messageHandler{
		messageService: messageService,
	}
}
