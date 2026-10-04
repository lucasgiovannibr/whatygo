package auth_middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/lucasgiovannibr/whatygo/pkg/apierror"
)

// ValidInstanceID refuses, with 400, a request whose :instanceId is not a UUID. Instance ids
// always are; anything else used to travel down to the services (an unknown id answered 500
// "invalid UUID format") and to the logger, where it named a directory.
func ValidInstanceID() gin.HandlerFunc {
	return func(c *gin.Context) {
		if id := c.Param("instanceId"); id != "" {
			if _, err := uuid.Parse(id); err != nil {
				apierror.Abort(c, http.StatusBadRequest, "invalid instance id: it must be a UUID")
				return
			}
		}
		c.Next()
	}
}
