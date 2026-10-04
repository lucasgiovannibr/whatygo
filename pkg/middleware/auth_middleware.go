package auth_middleware

import (
	"crypto/subtle"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/lucasgiovannibr/whatygo/pkg/apierror"
	"github.com/lucasgiovannibr/whatygo/pkg/config"
	instance_service "github.com/lucasgiovannibr/whatygo/pkg/instance/service"
)

type Middleware interface {
	Auth(ctx *gin.Context)
	AuthAdmin(ctx *gin.Context)
	AuthInstanceScoped(ctx *gin.Context)
}

type middleware struct {
	config          *config.Config
	instanceService instance_service.InstanceService
	// failures limits the guessing of credentials (see failLimiter); nil = no limit.
	failures *failLimiter
}

// refuse answers a request that failed to authenticate and counts the failure against the
// client address.
func (m middleware) refuse(ctx *gin.Context, status int, message string, counts bool) {
	if counts {
		m.failures.fail(ctx.ClientIP())
	}
	apierror.Abort(ctx, status, message)
}

// throttled stops the request, with 429, when its address has made too many failed attempts.
func (m middleware) throttled(ctx *gin.Context) bool {
	retryAfter, blocked := m.failures.blocked(ctx.ClientIP())
	if !blocked {
		return false
	}
	ctx.Header("Retry-After", retryAfter)
	apierror.Abort(ctx, http.StatusTooManyRequests, "too many failed authentication attempts, try again later")
	return true
}

// AdminTokenValid is the check of the global API key for the routes that cannot use the
// apikey header (the websocket takes it in the query). It counts a failure against the client
// address and refuses the request, with 429, from an address that has made too many.
func (m middleware) AdminTokenValid(ctx *gin.Context, token string) bool {
	if m.throttled(ctx) {
		return false
	}
	if !isGlobalKey(token, m.config.GlobalApiKey) {
		m.failures.fail(ctx.ClientIP())
		return false
	}
	return true
}

func (m middleware) Auth(ctx *gin.Context) {
	if m.throttled(ctx) {
		return
	}
	token := ctx.GetHeader("apikey")
	if token == "" {
		m.refuse(ctx, http.StatusUnauthorized, "not authorized", true)
		return
	}

	instance, err := m.instanceService.GetInstanceByToken(token)
	if err != nil {
		m.refuse(ctx, http.StatusUnauthorized, "not authorized", true)
		return
	}

	ctx.Set("instance", instance)

	ctx.Next()
}

func (m middleware) AuthAdmin(ctx *gin.Context) {
	if m.throttled(ctx) {
		return
	}
	token := ctx.GetHeader("apikey")
	if token == "" {
		m.refuse(ctx, http.StatusUnauthorized, "not authorized", true)
		return
	}

	if !isGlobalKey(token, m.config.GlobalApiKey) {
		m.refuse(ctx, http.StatusUnauthorized, "not authorized", true)
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
	if m.throttled(ctx) {
		return
	}
	token := ctx.GetHeader("apikey")
	if token == "" {
		m.refuse(ctx, http.StatusUnauthorized, "not authorized", true)
		return
	}

	if isGlobalKey(token, m.config.GlobalApiKey) {
		ctx.Next()
		return
	}

	instance, err := m.instanceService.GetInstanceByToken(token)
	if err != nil {
		m.refuse(ctx, http.StatusUnauthorized, "not authorized", true)
		return
	}

	if id := ctx.Param("instanceId"); id != "" && id != instance.Id {
		// A valid token, only of another instance: not a guess, so it is not counted.
		m.refuse(ctx, http.StatusForbidden, "token does not belong to this instance", false)
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
	return &middleware{config: config, instanceService: instanceService, failures: newFailLimiter(config.AuthFailLimit)}
}
