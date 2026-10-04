package newsletter_handler

import (
	"net/http"

	"github.com/lucasgiovannibr/whatygo/pkg/apierror"
	instance_model "github.com/lucasgiovannibr/whatygo/pkg/instance/model"
	newsletter_service "github.com/lucasgiovannibr/whatygo/pkg/newsletter/service"
	"github.com/gin-gonic/gin"
)

type NewsletterHandler interface {
	CreateNewsletter(ctx *gin.Context)
	ListNewsletter(ctx *gin.Context)
	GetNewsletter(ctx *gin.Context)
	GetNewsletterInvite(ctx *gin.Context)
	SubscribeNewsletter(ctx *gin.Context)
	GetNewsletterMessages(ctx *gin.Context)
	FollowNewsletter(ctx *gin.Context)
	UnfollowNewsletter(ctx *gin.Context)
	MuteNewsletter(ctx *gin.Context)
	MarkNewsletterViewed(ctx *gin.Context)
	ReactNewsletter(ctx *gin.Context)
}

type newsletterHandler struct {
	newsletterService newsletter_service.NewsletterService
}

// Create newsletter
// @Summary Create newsletter
// @Description Create newsletter
// @Tags Newsletter
// @Accept json
// @Produce json
// @Param message body newsletter_service.CreateNewsletterStruct true "Newsletter data"
// @Success 200 {object} gin.H "success"
// @Failure 400 {object} gin.H "Error on validation"
// @Failure 500 {object} gin.H "Internal server error"
// @Router /newsletter/create [post]
func (n *newsletterHandler) CreateNewsletter(ctx *gin.Context) {
	getInstance := ctx.MustGet("instance")

	instance, ok := getInstance.(*instance_model.Instance)
	if !ok {
		apierror.Fail(ctx, http.StatusInternalServerError, "instance not found")
		return
	}

	var data *newsletter_service.CreateNewsletterStruct
	err := ctx.ShouldBindBodyWithJSON(&data)
	if err != nil {
		apierror.BadRequest(ctx, err)
		return
	}

	if data.Name == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "name is required")
		return
	}

	newsletter, err := n.newsletterService.CreateNewsletter(data, instance)
	if err != nil {
		apierror.Respond(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "success", "data": newsletter})
}

// List newsletters
// @Summary List newsletters
// @Description List newsletters
// @Tags Newsletter
// @Accept json
// @Produce json
// @Success 200 {object} gin.H "success"
// @Failure 500 {object} gin.H "Internal server error"
// @Router /newsletter/list [get]
func (n *newsletterHandler) ListNewsletter(ctx *gin.Context) {
	getInstance := ctx.MustGet("instance")

	instance, ok := getInstance.(*instance_model.Instance)
	if !ok {
		apierror.Fail(ctx, http.StatusInternalServerError, "instance not found")
		return
	}

	newsletters, err := n.newsletterService.ListNewsletter(instance)
	if err != nil {
		apierror.Respond(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "success", "data": newsletters})
}

// Get newsletter
// @Summary Get newsletter
// @Description Get newsletter
// @Tags Newsletter
// @Accept json
// @Produce json
// @Param message body newsletter_service.GetNewsletterStruct true "Newsletter data"
// @Success 200 {object} gin.H "success"
// @Failure 400 {object} gin.H "Error on validation"
// @Failure 500 {object} gin.H "Internal server error"
// @Router /newsletter/info [post]
func (n *newsletterHandler) GetNewsletter(ctx *gin.Context) {
	getInstance := ctx.MustGet("instance")

	instance, ok := getInstance.(*instance_model.Instance)
	if !ok {
		apierror.Fail(ctx, http.StatusInternalServerError, "instance not found")
		return
	}

	var data *newsletter_service.GetNewsletterStruct
	err := ctx.ShouldBindBodyWithJSON(&data)
	if err != nil {
		apierror.BadRequest(ctx, err)
		return
	}

	if data.JID.String() == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "jid is required")
		return
	}

	newsletter, err := n.newsletterService.GetNewsletter(data, instance)
	if err != nil {
		apierror.Respond(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "success", "data": newsletter})
}

// Get newsletter invite
// @Summary Get newsletter invite
// @Description Get newsletter invite
// @Tags Newsletter
// @Accept json
// @Produce json
// @Param message body newsletter_service.GetNewsletterInviteStruct true "Newsletter data"
// @Success 200 {object} gin.H "success"
// @Failure 400 {object} gin.H "Error on validation"
// @Failure 500 {object} gin.H "Internal server error"
// @Router /newsletter/link [post]
func (n *newsletterHandler) GetNewsletterInvite(ctx *gin.Context) {
	getInstance := ctx.MustGet("instance")

	instance, ok := getInstance.(*instance_model.Instance)
	if !ok {
		apierror.Fail(ctx, http.StatusInternalServerError, "instance not found")
		return
	}

	var data *newsletter_service.GetNewsletterInviteStruct
	err := ctx.ShouldBindBodyWithJSON(&data)
	if err != nil {
		apierror.BadRequest(ctx, err)
		return
	}

	if data.Key == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "key is required")
		return
	}

	newsletter, err := n.newsletterService.GetNewsletterInvite(data, instance)
	if err != nil {
		apierror.Respond(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "success", "data": newsletter})
}

// Subscribe newsletter
// @Summary Subscribe newsletter
// @Description Subscribe newsletter
// @Tags Newsletter
// @Accept json
// @Produce json
// @Param message body newsletter_service.GetNewsletterStruct true "Newsletter data"
// @Success 200 {object} gin.H "success"
// @Failure 400 {object} gin.H "Error on validation"
// @Failure 500 {object} gin.H "Internal server error"
// @Router /newsletter/subscribe [post]
func (n *newsletterHandler) SubscribeNewsletter(ctx *gin.Context) {
	getInstance := ctx.MustGet("instance")

	instance, ok := getInstance.(*instance_model.Instance)
	if !ok {
		apierror.Fail(ctx, http.StatusInternalServerError, "instance not found")
		return
	}

	var data *newsletter_service.GetNewsletterStruct
	err := ctx.ShouldBindBodyWithJSON(&data)
	if err != nil {
		apierror.BadRequest(ctx, err)
		return
	}

	if data.JID.String() == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "jid is required")
		return
	}

	err = n.newsletterService.SubscribeNewsletter(data, instance)
	if err != nil {
		apierror.Respond(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "success"})
}

// Get newsletter messages
// @Summary Get newsletter messages
// @Description Get newsletter messages
// @Tags Newsletter
// @Accept json
// @Produce json
// @Param message body newsletter_service.GetNewsletterMessagesStruct true "Newsletter data"
// @Success 200 {object} gin.H "success"
// @Failure 400 {object} gin.H "Error on validation"
// @Failure 500 {object} gin.H "Internal server error"
// @Router /newsletter/messages [post]
func (n *newsletterHandler) GetNewsletterMessages(ctx *gin.Context) {
	getInstance := ctx.MustGet("instance")

	instance, ok := getInstance.(*instance_model.Instance)
	if !ok {
		apierror.Fail(ctx, http.StatusInternalServerError, "instance not found")
		return
	}

	var data *newsletter_service.GetNewsletterMessagesStruct
	err := ctx.ShouldBindBodyWithJSON(&data)
	if err != nil {
		apierror.BadRequest(ctx, err)
		return
	}

	if data.JID.String() == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "jid is required")
		return
	}

	messages, err := n.newsletterService.GetNewsletterMessages(data, instance)
	if err != nil {
		apierror.Respond(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "success", "data": messages})
}

func NewNewsletterHandler(
	newsletterService newsletter_service.NewsletterService,
) NewsletterHandler {
	return &newsletterHandler{
		newsletterService: newsletterService,
	}
}
