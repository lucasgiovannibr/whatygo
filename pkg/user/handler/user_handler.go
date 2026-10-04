package user_handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/lucasgiovannibr/whatygo/pkg/apierror"
	instance_model "github.com/lucasgiovannibr/whatygo/pkg/instance/model"
	user_service "github.com/lucasgiovannibr/whatygo/pkg/user/service"
	"go.mau.fi/whatsmeow"
)

// MaxNumbersPerQuery is the most numbers one /user/check or /user/info request may carry:
// the whole list goes to WhatsApp as one query, and an unbounded one is slow and the kind
// of bulk lookup WhatsApp restricts accounts for.
const MaxNumbersPerQuery = 100

// writeUserWAError maps WhatsApp IQ / context errors to honest HTTP statuses.
// rate-overlimit → 429; IQ/context timeout or cancel → 504; everything else → 500.
func writeUserWAError(ctx *gin.Context, err error) {
	var invalidNumber *user_service.InvalidNumberError
	switch {
	case errors.As(err, &invalidNumber):
		apierror.BadRequest(ctx, err)
	case errors.Is(err, whatsmeow.ErrIQRateOverLimit):
		apierror.Fail(ctx, http.StatusTooManyRequests, err.Error())
	case errors.Is(err, whatsmeow.ErrIQTimedOut),
		errors.Is(err, context.DeadlineExceeded),
		errors.Is(err, context.Canceled):
		apierror.Fail(ctx, http.StatusGatewayTimeout, err.Error())
	default:
		apierror.Respond(ctx, err)
	}
}

type UserHandler interface {
	GetUser(ctx *gin.Context)
	CheckUser(ctx *gin.Context)
	GetAvatar(ctx *gin.Context)
	GetContacts(ctx *gin.Context)
	SaveContact(ctx *gin.Context)
	GetPrivacy(ctx *gin.Context)
	SetPrivacy(ctx *gin.Context)
	BlockContact(ctx *gin.Context)
	UnblockContact(ctx *gin.Context)
	GetUserDevices(ctx *gin.Context)
	GetStatusPrivacy(ctx *gin.Context)
	GetBusinessProfile(ctx *gin.Context)
	GetBlockList(ctx *gin.Context)
	SetProfilePicture(ctx *gin.Context)
	SetProfileName(ctx *gin.Context)
	SetProfileStatus(ctx *gin.Context)
	ResolveLid(ctx *gin.Context)
}

type userHandler struct {
	userService user_service.UserService
}

// Get a user
// @Summary Get a user
// @Description Get a user
// @Tags User
// @Accept json
// @Produce json
// @Param message body user_service.CheckUserStruct true "User data"
// @Success 200 {object} gin.H "success"
// @Failure 400 {object} gin.H "Error on validation"
// @Failure 500 {object} gin.H "Internal server error"
// @Router /user/info [post]
func (u *userHandler) GetUser(ctx *gin.Context) {
	getInstance := ctx.MustGet("instance")

	instance, ok := getInstance.(*instance_model.Instance)
	if !ok {
		apierror.Fail(ctx, http.StatusInternalServerError, "instance not found")
		return
	}

	var data *user_service.CheckUserStruct
	err := ctx.ShouldBindBodyWithJSON(&data)
	if err != nil {
		apierror.BadRequest(ctx, err)
		return
	}

	if len(data.Number) < 1 {
		apierror.Fail(ctx, http.StatusBadRequest, "phone number is required")
		return
	}

	if len(data.Number) > MaxNumbersPerQuery {
		apierror.Fail(ctx, http.StatusBadRequest, fmt.Sprintf("at most %d numbers per request (got %d): split the list", MaxNumbersPerQuery, len(data.Number)))
		return
	}

	uc, err := u.userService.GetUser(ctx.Request.Context(), data, instance)
	if err != nil {
		writeUserWAError(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "success", "data": uc})
}

// Check a user
// @Summary Check a user
// @Description Check a user
// @Tags User
// @Accept json
// @Produce json
// @Param message body user_service.CheckUserStruct true "User data"
// @Success 200 {object} gin.H "success"
// @Failure 400 {object} gin.H "Error on validation"
// @Failure 500 {object} gin.H "Internal server error"
// @Router /user/check [post]
func (u *userHandler) CheckUser(ctx *gin.Context) {
	getInstance := ctx.MustGet("instance")

	instance, ok := getInstance.(*instance_model.Instance)
	if !ok {
		apierror.Fail(ctx, http.StatusInternalServerError, "instance not found")
		return
	}

	var data *user_service.CheckUserStruct
	err := ctx.ShouldBindBodyWithJSON(&data)
	if err != nil {
		apierror.BadRequest(ctx, err)
		return
	}

	if len(data.Number) < 1 {
		apierror.Fail(ctx, http.StatusBadRequest, "phone number is required")
		return
	}

	if len(data.Number) > MaxNumbersPerQuery {
		apierror.Fail(ctx, http.StatusBadRequest, fmt.Sprintf("at most %d numbers per request (got %d): split the list", MaxNumbersPerQuery, len(data.Number)))
		return
	}

	uc, err := u.userService.CheckUser(data, instance)
	if err != nil {
		writeUserWAError(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "success", "data": uc})
}

// Get a user's avatar
// @Summary Get a user's avatar
// @Description Get a user's avatar
// @Tags User
// @Accept json
// @Produce json
// @Param message body user_service.GetAvatarStruct true "Avatar data"
// @Success 200 {object} gin.H "success"
// @Failure 400 {object} gin.H "Error on validation"
// @Failure 429 {object} gin.H "WhatsApp rate limit"
// @Failure 500 {object} gin.H "Internal server error"
// @Failure 504 {object} gin.H "WhatsApp query timeout"
// @Router /user/avatar [post]
func (u *userHandler) GetAvatar(ctx *gin.Context) {
	getInstance := ctx.MustGet("instance")

	instance, ok := getInstance.(*instance_model.Instance)
	if !ok {
		apierror.Fail(ctx, http.StatusInternalServerError, "instance not found")
		return
	}

	var data *user_service.GetAvatarStruct
	err := ctx.ShouldBindBodyWithJSON(&data)
	if err != nil {
		apierror.BadRequest(ctx, err)
		return
	}

	if len(data.Number) < 1 {
		apierror.Fail(ctx, http.StatusBadRequest, "phone number is required")
		return
	}

	if data.Number == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "phone number is required")
		return
	}

	pic, err := u.userService.GetAvatar(ctx.Request.Context(), data, instance)
	if err != nil {
		writeUserWAError(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "success", "data": pic})
}

// Get a user's contacts
// @Summary Get a user's contacts
// @Description Get a user's contacts
// @Tags User
// @Accept json
// @Produce json
// @Success 200 {object} gin.H "success"
// @Failure 500 {object} gin.H "Internal server error"
// @Router /user/contacts [get]
func (u *userHandler) GetContacts(ctx *gin.Context) {
	getInstance := ctx.MustGet("instance")

	instance, ok := getInstance.(*instance_model.Instance)
	if !ok {
		apierror.Fail(ctx, http.StatusInternalServerError, "instance not found")
		return
	}

	contacts, err := u.userService.GetContacts(instance)
	if err != nil {
		apierror.Respond(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "success", "data": contacts})
}

// Get a user's privacy settings
// @Summary Get a user's privacy settings
// @Description Get a user's privacy settings
// @Tags User
// @Accept json
// @Produce json
// @Success 200 {object} gin.H "success"
// @Failure 500 {object} gin.H "Internal server error"
// @Router /user/privacy [get]
func (u *userHandler) GetPrivacy(ctx *gin.Context) {
	getInstance := ctx.MustGet("instance")

	instance, ok := getInstance.(*instance_model.Instance)
	if !ok {
		apierror.Fail(ctx, http.StatusInternalServerError, "instance not found")
		return
	}

	privacy, err := u.userService.GetPrivacy(instance)
	if err != nil {
		apierror.Respond(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "success", "data": privacy})
}

// Set a user's privacy settings
// @Summary Set a user's privacy settings
// @Description Set a user's privacy settings
// @Tags User
// @Accept json
// @Produce json
// @Param message body user_service.PrivacyStruct true "Privacy data"
// @Success 200 {object} gin.H "success"
// @Failure 500 {object} gin.H "Internal server error"
// @Router /user/privacy [post]
func (u *userHandler) SetPrivacy(ctx *gin.Context) {
	getInstance := ctx.MustGet("instance")

	instance, ok := getInstance.(*instance_model.Instance)
	if !ok {
		apierror.Fail(ctx, http.StatusInternalServerError, "instance not found")
		return
	}

	var data *user_service.PrivacyStruct
	err := ctx.ShouldBindBodyWithJSON(&data)
	if err != nil {
		apierror.BadRequest(ctx, err)
		return
	}

	if data.CallAdd == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "call add is required")
		return
	}

	if data.GroupAdd == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "group add is required")
		return
	}

	if data.LastSeen == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "last seen is required")
		return
	}

	if data.Online == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "online is required")
		return
	}

	if data.Profile == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "profile is required")
		return
	}

	if data.ReadReceipts == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "read receipts is required")
		return
	}

	if data.Status == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "status is required")
		return
	}

	privacy, err := u.userService.SetPrivacy(data, instance)
	if err != nil {
		apierror.Respond(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "success", "data": privacy})
}

// Block a contact
// @Summary Block a contact
// @Description Block a contact
// @Tags User
// @Accept json
// @Produce json
// @Param message body user_service.BlockStruct true "Block data"
// @Success 200 {object} gin.H "success"
// @Failure 400 {object} gin.H "Error on validation"
// @Failure 500 {object} gin.H "Internal server error"
// @Router /user/block [post]
func (u *userHandler) BlockContact(ctx *gin.Context) {
	getInstance := ctx.MustGet("instance")

	instance, ok := getInstance.(*instance_model.Instance)
	if !ok {
		apierror.Fail(ctx, http.StatusInternalServerError, "instance not found")
		return
	}

	var data *user_service.BlockStruct
	err := ctx.ShouldBindBodyWithJSON(&data)
	if err != nil {
		apierror.BadRequest(ctx, err)
		return
	}

	if len(data.Number) < 1 {
		apierror.Fail(ctx, http.StatusBadRequest, "phone number is required")
		return
	}

	if data.Number == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "phone number is required")
		return
	}

	resp, err := u.userService.BlockContact(data, instance)
	if err != nil {
		apierror.Respond(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "success", "data": resp})
}

// Unblock a contact
// @Summary Unblock a contact
// @Description Unblock a contact
// @Tags User
// @Accept json
// @Produce json
// @Param message body user_service.BlockStruct true "Block data"
// @Success 200 {object} gin.H "success"
// @Failure 400 {object} gin.H "Error on validation"
// @Failure 500 {object} gin.H "Internal server error"
// @Router /user/unblock [post]
func (u *userHandler) UnblockContact(ctx *gin.Context) {
	getInstance := ctx.MustGet("instance")

	instance, ok := getInstance.(*instance_model.Instance)
	if !ok {
		apierror.Fail(ctx, http.StatusInternalServerError, "instance not found")
		return
	}

	var data *user_service.BlockStruct
	err := ctx.ShouldBindBodyWithJSON(&data)
	if err != nil {
		apierror.BadRequest(ctx, err)
		return
	}

	if len(data.Number) < 1 {
		apierror.Fail(ctx, http.StatusBadRequest, "phone number is required")
		return
	}

	if data.Number == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "phone number is required")
		return
	}

	resp, err := u.userService.UnlockContact(data, instance)
	if err != nil {
		apierror.Respond(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "success", "data": resp})
}

// Get a user's block list
// @Summary Get a user's block list
// @Description Get a user's block list
// @Tags User
// @Accept json
// @Produce json
// @Success 200 {object} gin.H "success"
// @Failure 500 {object} gin.H "Internal server error"
// @Router /user/blocklist [get]
func (u *userHandler) GetBlockList(ctx *gin.Context) {
	getInstance := ctx.MustGet("instance")

	instance, ok := getInstance.(*instance_model.Instance)
	if !ok {
		apierror.Fail(ctx, http.StatusInternalServerError, "instance not found")
		return
	}

	resp, err := u.userService.GetBlockList(instance)
	if err != nil {
		apierror.Respond(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "success", "data": resp})
}

// Set a user's profile picture
// @Summary Set a user's profile picture
// @Description Set a user's profile picture
// @Tags User
// @Accept json
// @Produce json
// @Param message body user_service.SetProfilePictureStruct true "Profile picture data"
// @Success 200 {object} gin.H "success"
// @Failure 400 {object} gin.H "Error on validation"
// @Failure 500 {object} gin.H "Internal server error"
// @Router /user/profilePicture [post]
func (u *userHandler) SetProfilePicture(ctx *gin.Context) {
	getInstance := ctx.MustGet("instance")

	instance, ok := getInstance.(*instance_model.Instance)
	if !ok {
		apierror.Fail(ctx, http.StatusInternalServerError, "instance not found")
		return
	}

	var data *user_service.SetProfilePictureStruct
	err := ctx.ShouldBindBodyWithJSON(&data)
	if err != nil {
		apierror.BadRequest(ctx, err)
		return
	}

	if data.Image == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "image is required")
		return
	}

	resp, err := u.userService.SetProfilePicture(data, instance)
	if err != nil {
		apierror.Respond(ctx, err)
		return
	}

	if !resp {
		apierror.Fail(ctx, http.StatusInternalServerError, "failed to set profile picture")
		return
	}

	responseData := gin.H{"image": data.Image}

	ctx.JSON(http.StatusOK, gin.H{"message": "success", "data": responseData})
}

// Set a user's profile name
// @Summary Set a user's profile name
// @Description Set a user's profile name
// @Tags User
// @Accept json
// @Produce json
// @Param message body user_service.SetProfileNameStruct true "Profile name data"
// @Success 200 {object} gin.H "success"
// @Failure 400 {object} gin.H "Error on validation"
// @Failure 500 {object} gin.H "Internal server error"
// @Router /user/profileName [post]
func (u *userHandler) SetProfileName(ctx *gin.Context) {
	getInstance := ctx.MustGet("instance")

	instance, ok := getInstance.(*instance_model.Instance)
	if !ok {
		apierror.Fail(ctx, http.StatusInternalServerError, "instance not found")
		return
	}

	var data *user_service.SetProfileNameStruct
	err := ctx.ShouldBindBodyWithJSON(&data)
	if err != nil {
		apierror.BadRequest(ctx, err)
		return
	}

	if data.Name == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "name is required")
		return
	}

	resp, err := u.userService.SetProfileName(data, instance)
	if err != nil {
		apierror.Respond(ctx, err)
		return
	}

	if !resp {
		apierror.Fail(ctx, http.StatusInternalServerError, "failed to set profile name")
		return
	}

	responseData := gin.H{"name": data.Name}

	ctx.JSON(http.StatusOK, gin.H{"message": "success", "data": responseData})
}

// Set a user's profile status
// @Summary Set a user's profile status
// @Description Set a user's profile status
// @Tags User
// @Accept json
// @Produce json
// @Param message body user_service.SetProfilePictureStruct true "Profile status data"
// @Success 200 {object} gin.H "success"
// @Failure 400 {object} gin.H "Error on validation"
// @Failure 500 {object} gin.H "Internal server error"
// @Router /user/profileStatus [post]
func (u *userHandler) SetProfileStatus(ctx *gin.Context) {
	getInstance := ctx.MustGet("instance")

	instance, ok := getInstance.(*instance_model.Instance)
	if !ok {
		apierror.Fail(ctx, http.StatusInternalServerError, "instance not found")
		return
	}

	var data *user_service.SetProfileStatusStruct
	err := ctx.ShouldBindBodyWithJSON(&data)
	if err != nil {
		apierror.BadRequest(ctx, err)
		return
	}

	if data.Status == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "name is required")
		return
	}

	resp, err := u.userService.SetProfileStatus(data, instance)
	if err != nil {
		apierror.Respond(ctx, err)
		return
	}

	if !resp {
		apierror.Fail(ctx, http.StatusInternalServerError, "failed to set profile picture")
		return
	}

	responseData := gin.H{"status": data.Status}

	ctx.JSON(http.StatusOK, gin.H{"message": "success", "data": responseData})
}

// Resolve the phone number behind a LID
// @Summary Resolve the phone number behind a LID
// @Description Resolve the phone number behind a LID
// @Tags User
// @Accept json
// @Produce json
// @Param message body user_service.ResolveLidStruct true "Lid data"
// @Success 200 {object} gin.H "success"
// @Failure 400 {object} gin.H "Error on validation"
// @Failure 500 {object} gin.H "Internal server error"
// @Router /user/lid [post]
func (u *userHandler) ResolveLid(ctx *gin.Context) {
	getInstance := ctx.MustGet("instance")

	instance, ok := getInstance.(*instance_model.Instance)
	if !ok {
		apierror.Fail(ctx, http.StatusInternalServerError, "instance not found")
		return
	}

	var data *user_service.ResolveLidStruct
	err := ctx.ShouldBindBodyWithJSON(&data)
	if err != nil {
		apierror.BadRequest(ctx, err)
		return
	}

	if data.Lid == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "lid is required")
		return
	}

	resp, err := u.userService.ResolveLid(data, instance)
	if err != nil {
		apierror.Respond(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "success", "data": resp})
}

func NewUserHandler(
	userService user_service.UserService,
) UserHandler {
	return &userHandler{
		userService: userService,
	}
}
