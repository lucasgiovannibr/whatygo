// Package apierror answers failed requests the same way everywhere.
//
// Every handler used to answer any service error with 500 and the error text, so a client
// could not tell "the instance is not connected" (retry later) from "WhatsApp rejected the
// request" (do not retry) from "this is a bug". Now a failed request gets
//
//	{"error": "<message>", "code": "<machine readable>"}
//
// with a status that says what kind of failure it is. "error" is exactly what it always
// was; "code" is new and stable. Errors that are not recognised stay 500 / internal_error.
package apierror

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"go.mau.fi/whatsmeow"
	"gorm.io/gorm"

	"github.com/lucasgiovannibr/whatygo/pkg/utils"
)

// Body is the JSON of a failed request.
type Body struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

// Error is an error that already knows how to be answered. Services return it (wrapped or
// not) for failures a client caused or can fix; Respond uses its status and code.
type Error struct {
	Status  int
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Message }

// Invalid is a request the client has to fix (400).
func Invalid(message string) error {
	return &Error{Status: http.StatusBadRequest, Code: "invalid_request", Message: message}
}

// NotFound is something that does not exist (404).
func NotFound(message string) error {
	return &Error{Status: http.StatusNotFound, Code: "not_found", Message: message}
}

// Conflict is a request that clashes with the current state (409).
func Conflict(message string) error {
	return &Error{Status: http.StatusConflict, Code: "conflict", Message: message}
}

// retryAfterer is implemented by errors that tell the client when to come back
// (send_service.ErrSendThrottled).
type retryAfterer interface{ RetryAfterSeconds() int }

type mapping struct {
	target error
	status int
	code   string
}

// Order matters: the first match wins.
var mappings = []mapping{
	{utils.ErrNoActiveSession, http.StatusServiceUnavailable, "instance_not_connected"},
	{utils.ErrClientDisconnected, http.StatusServiceUnavailable, "instance_disconnected"},
	{utils.ErrNotLoggedIn, http.StatusConflict, "instance_not_logged_in"},
	{utils.ErrDisconnectedByUser, http.StatusConflict, "instance_disconnected_by_user"},
	{utils.ErrOwnedElsewhere, http.StatusConflict, "instance_on_another_replica"},
	{utils.ErrDownloadTooLarge, http.StatusRequestEntityTooLarge, "payload_too_large"},
	{utils.ErrDownloadFailed, http.StatusBadRequest, "invalid_media_url"},

	{whatsmeow.ErrNotConnected, http.StatusServiceUnavailable, "instance_not_connected"},
	{whatsmeow.ErrNotLoggedIn, http.StatusConflict, "instance_not_logged_in"},
	{whatsmeow.ErrIQRateOverLimit, http.StatusTooManyRequests, "whatsapp_rate_limited"},
	{whatsmeow.ErrIQForbidden, http.StatusForbidden, "whatsapp_forbidden"},
	{whatsmeow.ErrIQNotAuthorized, http.StatusForbidden, "whatsapp_forbidden"},
	{whatsmeow.ErrIQNotFound, http.StatusNotFound, "not_found"},
	{whatsmeow.ErrIQTimedOut, http.StatusGatewayTimeout, "whatsapp_timeout"},
	{whatsmeow.ErrMessageTimedOut, http.StatusGatewayTimeout, "whatsapp_timeout"},
	{whatsmeow.ErrGroupNotFound, http.StatusNotFound, "not_found"},
	{whatsmeow.ErrNotInGroup, http.StatusForbidden, "not_in_group"},
	{whatsmeow.ErrInviteLinkInvalid, http.StatusBadRequest, "invalid_request"},
	{whatsmeow.ErrInviteLinkRevoked, http.StatusBadRequest, "invalid_request"},
	{whatsmeow.ErrInvalidImageFormat, http.StatusBadRequest, "invalid_request"},
	{whatsmeow.ErrPhoneNumberTooShort, http.StatusBadRequest, "invalid_request"},
	{whatsmeow.ErrPhoneNumberIsNotInternational, http.StatusBadRequest, "invalid_request"},

	{context.DeadlineExceeded, http.StatusGatewayTimeout, "timeout"},
	{gorm.ErrRecordNotFound, http.StatusNotFound, "not_found"},
}

// Classify returns the status and code for err.
func Classify(err error) (status int, code string) {
	var apiErr *Error
	if errors.As(err, &apiErr) {
		return apiErr.Status, apiErr.Code
	}
	if _, ok := asRetryAfter(err); ok {
		return http.StatusTooManyRequests, "rate_limited"
	}
	for _, m := range mappings {
		if errors.Is(err, m.target) {
			return m.status, m.code
		}
	}
	// Any other rejection of an info query by WhatsApp: a failure of the other side, not ours.
	var iq *whatsmeow.IQError
	if errors.As(err, &iq) && iq.Code >= 400 && iq.Code < 500 {
		return http.StatusBadGateway, "whatsapp_rejected"
	}
	return http.StatusInternalServerError, "internal_error"
}

func asRetryAfter(err error) (retryAfterer, bool) {
	var r retryAfterer
	if errors.As(err, &r) {
		return r, true
	}
	return nil, false
}

// Respond answers a failed request with the status and code that fit err.
func Respond(c *gin.Context, err error) {
	status, code := Classify(err)
	if r, ok := asRetryAfter(err); ok {
		c.Header("Retry-After", strconv.Itoa(r.RetryAfterSeconds()))
	} else if status == http.StatusTooManyRequests {
		c.Header("Retry-After", "5")
	}
	c.JSON(status, Body{Error: err.Error(), Code: code})
}

// BadRequest answers a request that could not be read or is invalid (400), for the bind
// errors handlers used to report with a bare status.
func BadRequest(c *gin.Context, err error) {
	c.JSON(http.StatusBadRequest, Body{Error: err.Error(), Code: "invalid_request"})
}

// codeForStatus is the code of a failure that only has a status (authentication, limits,
// plain "not found" answers written by hand).
func codeForStatus(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "invalid_request"
	case http.StatusUnauthorized:
		return "unauthorized"
	case http.StatusForbidden:
		return "forbidden"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusConflict:
		return "conflict"
	case http.StatusRequestEntityTooLarge:
		return "payload_too_large"
	case http.StatusTooManyRequests:
		return "rate_limited"
	case http.StatusServiceUnavailable:
		return "unavailable"
	case http.StatusGatewayTimeout:
		return "timeout"
	}
	if status >= 500 {
		return "internal_error"
	}
	return "error"
}

// Fail answers with a status and a message the handler wrote itself; the code follows the status.
func Fail(c *gin.Context, status int, message string) {
	c.JSON(status, Body{Error: message, Code: codeForStatus(status)})
}

// Abort is Fail for middleware: it also stops the request.
func Abort(c *gin.Context, status int, message string) {
	c.AbortWithStatusJSON(status, Body{Error: message, Code: codeForStatus(status)})
}

// RespondWith is Respond for handlers that derive a status of their own (a validation error
// their service defines, a call state): a recognised error keeps its classification, any other
// gets fallback, and the code follows that status.
func RespondWith(c *gin.Context, err error, fallback int) {
	status, code := Classify(err)
	if status == http.StatusInternalServerError {
		status, code = fallback, codeForStatus(fallback)
	}
	if r, ok := asRetryAfter(err); ok {
		c.Header("Retry-After", strconv.Itoa(r.RetryAfterSeconds()))
	} else if status == http.StatusTooManyRequests {
		c.Header("Retry-After", "5")
	}
	c.JSON(status, Body{Error: err.Error(), Code: code})
}
