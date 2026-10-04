package user_handler

import (
	"errors"
	"net/http"

	"github.com/lucasgiovannibr/whatygo/pkg/apierror"
	instance_model "github.com/lucasgiovannibr/whatygo/pkg/instance/model"
	user_service "github.com/lucasgiovannibr/whatygo/pkg/user/service"
	"github.com/gin-gonic/gin"
)

// instanceOf reads the instance the auth middleware put in the context.
func instanceOf(ctx *gin.Context) (*instance_model.Instance, bool) {
	instance, ok := ctx.MustGet("instance").(*instance_model.Instance)
	if !ok {
		apierror.Fail(ctx, http.StatusInternalServerError, "instance not found")
	}
	return instance, ok
}

// writeQueryError maps the errors of the read-only lookups: a missing result is a 404,
// anything else follows writeUserWAError (400 / 429 / 504 / 500).
func writeQueryError(ctx *gin.Context, err error) {
	var notFound *user_service.NotFoundError
	if errors.As(err, &notFound) {
		apierror.Fail(ctx, http.StatusNotFound, err.Error())
		return
	}
	writeUserWAError(ctx, err)
}

// List the linked devices of users
// @Summary List the linked devices of users
// @Description Number is one number or a list (up to 50). Device 0 is the primary phone; this account's own device is not included.
// @Tags User
// @Accept json
// @Produce json
// @Param message body user_service.DevicesStruct true "Numbers"
// @Success 200 {object} gin.H "success"
// @Failure 400 {object} gin.H "Error on validation"
// @Failure 500 {object} gin.H "Internal server error"
// @Router /user/devices [post]
func (u *userHandler) GetUserDevices(ctx *gin.Context) {
	instance, ok := instanceOf(ctx)
	if !ok {
		return
	}

	// "number" may be a string or a list.
	var raw struct {
		Number interface{} `json:"number"`
	}
	if err := ctx.ShouldBindBodyWithJSON(&raw); err != nil {
		apierror.BadRequest(ctx, err)
		return
	}
	data := &user_service.DevicesStruct{}
	switch v := raw.Number.(type) {
	case string:
		if v != "" {
			data.Number = []string{v}
		}
	case []interface{}:
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				data.Number = append(data.Number, s)
			}
		}
	}
	if len(data.Number) == 0 {
		apierror.Fail(ctx, http.StatusBadRequest, "phone number is required")
		return
	}

	devices, err := u.userService.GetUserDevices(ctx.Request.Context(), data, instance)
	if err != nil {
		writeQueryError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"message": "success", "data": gin.H{"devices": devices, "count": len(devices)}})
}

// Who sees my status
// @Summary Status privacy settings
// @Description The stored "who sees my status" settings; the first one is the default.
// @Tags User
// @Produce json
// @Success 200 {object} gin.H "success"
// @Failure 500 {object} gin.H "Internal server error"
// @Router /user/statusprivacy [get]
func (u *userHandler) GetStatusPrivacy(ctx *gin.Context) {
	instance, ok := instanceOf(ctx)
	if !ok {
		return
	}

	settings, err := u.userService.GetStatusPrivacy(ctx.Request.Context(), instance)
	if err != nil {
		writeQueryError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"message": "success", "data": settings})
}

// Business profile
// @Summary Public profile of a WhatsApp Business account
// @Description 404 when the number is not a business account.
// @Tags User
// @Accept json
// @Produce json
// @Param message body user_service.BusinessProfileStruct true "Number"
// @Success 200 {object} gin.H "success"
// @Failure 400 {object} gin.H "Error on validation"
// @Failure 404 {object} gin.H "Not a business account"
// @Failure 500 {object} gin.H "Internal server error"
// @Router /user/business [post]
func (u *userHandler) GetBusinessProfile(ctx *gin.Context) {
	instance, ok := instanceOf(ctx)
	if !ok {
		return
	}

	var data *user_service.BusinessProfileStruct
	if err := ctx.ShouldBindBodyWithJSON(&data); err != nil || data == nil {
		msg := "invalid body"
		if err != nil {
			msg = err.Error()
		}
		apierror.Fail(ctx, http.StatusBadRequest, msg)
		return
	}

	profile, err := u.userService.GetBusinessProfile(ctx.Request.Context(), data, instance)
	if err != nil {
		writeQueryError(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"message": "success", "data": profile})
}
