package auth_middleware

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gomessguii/logger"
	"github.com/lucasgiovannibr/whatygo/pkg/apierror"
	"github.com/lucasgiovannibr/whatygo/pkg/utils"
)

// JIDValidationMiddleware validates JID parameters in request bodies
type JIDValidationMiddleware struct{}

// NewJIDValidationMiddleware creates a new JID validation middleware
func NewJIDValidationMiddleware() *JIDValidationMiddleware {
	return &JIDValidationMiddleware{}
}

// isJSONRequest tells whether the middleware has a JSON body to look at.
func isJSONRequest(c *gin.Context) bool {
	return strings.Contains(c.ContentType(), "application/json")
}

// openBody reads the body and prepares it for in-place editing of the given top-level
// fields. On failure it answers 400 itself and returns false.
func openBody(c *gin.Context, keys ...string) (*bodyEditor, bool) {
	body, err := readBody(c.Request)
	if err != nil {
		apierror.Fail(c, http.StatusBadRequest, "Failed to read request body")
		c.Abort()
		return nil, false
	}

	editor, err := newBodyEditor(body, keys...)
	if err != nil {
		apierror.Fail(c, http.StatusBadRequest, "Invalid JSON format")
		c.Abort()
		return nil, false
	}
	return editor, true
}

// readBody reads the whole body. When its length is announced the buffer is allocated once,
// with a little room for the edits (io.ReadAll grows by doubling: reading 100 MB that way
// allocates about twice that).
func readBody(r *http.Request) ([]byte, error) {
	n := r.ContentLength
	if n <= 0 {
		return io.ReadAll(r.Body)
	}
	slack := 8<<10 + int(n/512)
	buf := make([]byte, n, int(n)+slack)
	if _, err := io.ReadFull(r.Body, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

// finish hands the (possibly rewritten) body to the next handler. It is also stored where
// gin keeps the body for ShouldBindBodyWithJSON, so the handler does not read it a second
// time (and grow another buffer) to bind it.
func finish(c *gin.Context, editor *bodyEditor) {
	body := editor.apply()
	c.Set(gin.BodyBytesKey, body)
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	c.Request.ContentLength = int64(len(body))
	c.Next()
}

func badRequest(c *gin.Context, message string) {
	apierror.Fail(c, http.StatusBadRequest, message)
	c.Abort()
}

func failEdit(c *gin.Context, err error) {
	logger.LogError("JID validation: failed to rewrite the request: %v", err)
	apierror.Fail(c, http.StatusInternalServerError, "Failed to process request")
	c.Abort()
}

// ValidateJIDFields validates and normalizes JID fields in request body
func (m *JIDValidationMiddleware) ValidateJIDFields(fieldNames ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Only process JSON requests
		if !isJSONRequest(c) {
			// For multipart/form-data, validate form fields
			if strings.Contains(c.ContentType(), "multipart/form-data") {
				m.validateFormFields(c, fieldNames...)
				return
			}
			c.Next()
			return
		}

		editor, ok := openBody(c, fieldNames...)
		if !ok {
			return
		}

		// Validate and normalize JID fields
		for _, fieldName := range fieldNames {
			raw, exists := editor.raw(fieldName)
			if !exists {
				continue
			}

			var value interface{}
			_ = json.Unmarshal(raw, &value)
			str, isString := value.(string)
			if !isString || str == "" {
				badRequest(c, fmt.Sprintf("%s is required and cannot be empty", fieldName))
				return
			}

			// Validate and normalize the JID
			normalizedJID, err := utils.CreateJID(str)
			if err != nil {
				badRequest(c, fmt.Sprintf("Invalid %s format: %s", fieldName, err.Error()))
				return
			}

			// Update the value if it was normalized
			if normalizedJID != str {
				if err := editor.set(fieldName, normalizedJID); err != nil {
					failEdit(c, err)
					return
				}
				logger.LogDebug("Normalized %s from %s to %s", fieldName, str, normalizedJID)
			}
		}

		finish(c, editor)
	}
}

// validateFormFields validates JID fields in multipart form data
func (m *JIDValidationMiddleware) validateFormFields(c *gin.Context, fieldNames ...string) {
	for _, fieldName := range fieldNames {
		value := c.PostForm(fieldName)
		if value != "" {
			// Validate the JID format
			_, err := utils.CreateJID(value)
			if err != nil {
				apierror.Fail(c, http.StatusBadRequest, fmt.Sprintf("Invalid %s format: %s", fieldName, err.Error()))
				c.Abort()
				return
			}
		} else if fieldName == "number" { // number is typically required
			apierror.Fail(c, http.StatusBadRequest, fmt.Sprintf("%s is required and cannot be empty", fieldName))
			c.Abort()
			return
		}
	}
	c.Next()
}

// normalizeNumber validates the "number" field, a string or a list of strings, and
// returns its normalized value (changed tells whether it differs from what was sent).
// With format=false the numbers are only checked for being present, and kept as received.
func normalizeNumber(raw json.RawMessage, format bool) (value interface{}, changed bool, errMessage string) {
	var parsed interface{}
	_ = json.Unmarshal(raw, &parsed)

	switch v := parsed.(type) {
	case []interface{}:
		if len(v) == 0 {
			return nil, false, "number array cannot be empty"
		}
		for i, item := range v {
			str, isString := item.(string)
			if !isString || str == "" {
				return nil, false, fmt.Sprintf("number[%d] cannot be empty", i)
			}
			if !format {
				continue
			}
			normalized, err := utils.CreateJID(str)
			if err != nil {
				return nil, false, fmt.Sprintf("Invalid number[%d] format: %s", i, err.Error())
			}
			if normalized != str {
				v[i] = normalized
				changed = true
				logger.LogDebug("Normalized number[%d] from %s to %s", i, str, normalized)
			}
		}
		return v, changed, ""

	case string:
		if v == "" {
			return nil, false, "number is required and cannot be empty"
		}
		if !format {
			return v, false, ""
		}
		normalized, err := utils.CreateJID(v)
		if err != nil {
			return nil, false, fmt.Sprintf("Invalid number format: %s", err.Error())
		}
		if normalized != v {
			logger.LogDebug("Normalized number from %s to %s", v, normalized)
			return normalized, true, ""
		}
		return v, false, ""
	}
	return nil, false, "number must be a string or array of strings"
}

// validateNumberBody is the body of ValidateNumberField and ValidateNumberFieldWithFormatJid.
func validateNumberBody(c *gin.Context, honourFormatJid bool) {
	keys := []string{"number"}
	if honourFormatJid {
		keys = append(keys, "formatJid")
	}
	editor, ok := openBody(c, keys...)
	if !ok {
		return
	}

	// Check FormatJid parameter (default is true)
	format := true
	if honourFormatJid {
		if raw, exists := editor.raw("formatJid"); exists {
			var flag bool
			if json.Unmarshal(raw, &flag) == nil {
				format = flag
			}
		}
	}

	if raw, exists := editor.raw("number"); exists {
		value, changed, message := normalizeNumber(raw, format)
		if message != "" {
			badRequest(c, message)
			return
		}
		if changed {
			if err := editor.set("number", value); err != nil {
				failEdit(c, err)
				return
			}
		}
	}

	finish(c, editor)
}

// ValidateNumberField is a convenience method for the common "number" field
// It handles both single strings and arrays of strings
func (m *JIDValidationMiddleware) ValidateNumberField() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Only process JSON requests
		if !isJSONRequest(c) {
			// For multipart/form-data, validate form fields
			if strings.Contains(c.ContentType(), "multipart/form-data") {
				m.validateFormFields(c, "number")
				return
			}
			c.Next()
			return
		}

		validateNumberBody(c, false)
	}
}

// ValidateMultipleNumbers validates multiple number fields (for arrays or multiple contacts)
func (m *JIDValidationMiddleware) ValidateMultipleNumbers(fieldName string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Only process JSON requests
		if !isJSONRequest(c) {
			c.Next()
			return
		}

		editor, ok := openBody(c, fieldName)
		if !ok {
			return
		}

		// Validate array of numbers
		if raw, exists := editor.raw(fieldName); exists {
			var value interface{}
			_ = json.Unmarshal(raw, &value)

			if items, isArray := value.([]interface{}); isArray {
				modified := false
				for i, item := range items {
					if str, isString := item.(string); isString && str != "" {
						normalized, err := utils.CreateJID(str)
						if err != nil {
							badRequest(c, fmt.Sprintf("Invalid %s[%d] format: %s", fieldName, i, err.Error()))
							return
						}
						if normalized != str {
							items[i] = normalized
							modified = true
						}
					}
				}

				if modified {
					if err := editor.set(fieldName, items); err != nil {
						failEdit(c, err)
						return
					}
				}
			}
		}

		finish(c, editor)
	}
}

// ValidateNumberFieldWithFormatJid validates number field but respects FormatJid parameter
// When FormatJid is true (default), numbers are normalized to full JID format
// When FormatJid is false, numbers are kept as received (raw format)
func (m *JIDValidationMiddleware) ValidateNumberFieldWithFormatJid() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Only process JSON requests
		if !isJSONRequest(c) {
			c.Next()
			return
		}

		validateNumberBody(c, true)
	}
}

// ValidateContactFields validates contact-specific fields that may contain phone numbers
func (m *JIDValidationMiddleware) ValidateContactFields() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Only process JSON requests
		if !isJSONRequest(c) {
			c.Next()
			return
		}

		editor, ok := openBody(c, "number", "vcard")
		if !ok {
			return
		}

		// Validate main number field
		if raw, exists := editor.raw("number"); exists {
			var value interface{}
			_ = json.Unmarshal(raw, &value)
			if str, isString := value.(string); isString && str != "" {
				normalized, err := utils.CreateJID(str)
				if err != nil {
					badRequest(c, fmt.Sprintf("Invalid number format: %s", err.Error()))
					return
				}
				if normalized != str {
					if err := editor.set("number", normalized); err != nil {
						failEdit(c, err)
						return
					}
				}
			}
		}

		// Validate vcard phone field if present. For vcard phone, we just validate format
		// but don't convert to JID
		if raw, exists := editor.raw("vcard"); exists {
			var vcard map[string]interface{}
			if json.Unmarshal(raw, &vcard) == nil {
				if phone, isString := vcard["phone"].(string); isString && phone != "" {
					if _, err := utils.CreateJID(phone); err != nil {
						badRequest(c, fmt.Sprintf("Invalid vcard phone format: %s", err.Error()))
						return
					}
				}
			}
		}

		finish(c, editor)
	}
}
