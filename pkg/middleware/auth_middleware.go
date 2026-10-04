package auth_middleware

import (
	"crypto/subtle"
	"net/http"

	"github.com/lucasgiovannibr/whatygo/pkg/apierror"
	"github.com/lucasgiovannibr/whatygo/pkg/config"
	instance_service "github.com/lucasgiovannibr/whatygo/pkg/instance/service"
	"github.com/gin-gonic/gin"
)

type Middleware interface {
	Auth(ctx *gin.Context)
	AuthAdmin(ctx *gin.Context)
	AuthInstanceScoped(ctx *gin.Context)
}

type middleware struct {
	config          *config.Config
	instanceService instance_service.InstanceService
}

func (m middleware) Auth(ctx *gin.Context) {
	token := ctx.GetHeader("apikey")
	if token == "" {
		apierror.Abort(ctx, http.StatusUnauthorized, "not authorized")
		return
	}

	instance, err := m.instanceService.GetInstanceByToken(token)
	if err != nil {
		apierror.Abort(ctx, http.StatusUnauthorized, "not authorized")
		return
	}

	ctx.Set("instance", instance)

	ctx.Next()
}

func (m middleware) AuthAdmin(ctx *gin.Context) {
	token := ctx.GetHeader("apikey")
	if token == "" {
		apierror.Abort(ctx, http.StatusUnauthorized, "not authorized")
		return
	}

	if !isGlobalKey(token, m.config.GlobalApiKey) {
		apierror.Abort(ctx, http.StatusUnauthorized, "not authorized")
		return
	}

	ctx.Next()
}

// AuthInstanceScoped protects routes that carry an :instanceId path parameter.
// It accepts either the global API key (administrative access to any instance)
// or the token of the instance named in the path. A valid token of a DIFFERENT
// instance is rejected: Auth alone only proved "some instance", so any instance
// token could read and change the settings of every other instance.
func (m middleware) AuthInstanceScoped(ctx *gin.Context) {
	token := ctx.GetHeader("apikey")
	if token == "" {
		apierror.Abort(ctx, http.StatusUnauthorized, "not authorized")
		return
	}

	if isGlobalKey(token, m.config.GlobalApiKey) {
		ctx.Next()
		return
	}

	instance, err := m.instanceService.GetInstanceByToken(token)
	if err != nil {
		apierror.Abort(ctx, http.StatusUnauthorized, "not authorized")
		return
	}

	if id := ctx.Param("instanceId"); id != "" && id != instance.Id {
		apierror.Abort(ctx, http.StatusForbidden, "token does not belong to this instance")
		return
	}

	ctx.Set("instance", instance)

	ctx.Next()
}

// isGlobalKey compares in constant time so the global API key cannot be probed
// byte by byte through response timing. An empty configured key never matches.
func isGlobalKey(token, globalKey string) bool {
	if globalKey == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(globalKey)) == 1
}

func NewMiddleware(config *config.Config, instanceService instance_service.InstanceService) *middleware {
	return &middleware{config: config, instanceService: instanceService}
}
