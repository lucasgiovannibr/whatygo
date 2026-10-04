package user_handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	user_service "github.com/lucasgiovannibr/whatygo/pkg/user/service"
	"go.mau.fi/whatsmeow"
)

func TestWriteUserWAError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{
			name:       "rate overlimit",
			err:        fmt.Errorf("usync: %w", whatsmeow.ErrIQRateOverLimit),
			wantStatus: http.StatusTooManyRequests,
		},
		{
			name:       "iq timeout",
			err:        fmt.Errorf("avatar: %w", whatsmeow.ErrIQTimedOut),
			wantStatus: http.StatusGatewayTimeout,
		},
		{
			name:       "context deadline",
			err:        fmt.Errorf("avatar: %w", context.DeadlineExceeded),
			wantStatus: http.StatusGatewayTimeout,
		},
		{
			name:       "context canceled",
			err:        fmt.Errorf("avatar: %w", context.Canceled),
			wantStatus: http.StatusGatewayTimeout,
		},
		{
			name:       "invalid number",
			err:        fmt.Errorf("check: %w", &user_service.InvalidNumberError{Err: errors.New("invalid number format")}),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "other error",
			err:        errors.New("no profile picture found"),
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)

			writeUserWAError(c, tt.err)

			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", w.Code, tt.wantStatus)
			}
			var body map[string]string
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("unmarshal body: %v", err)
			}
			if body["error"] == "" {
				t.Fatal("expected non-empty error field")
			}
		})
	}
}

func TestWriteQueryErrorMapsNotFoundTo404(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		err  error
		want int
	}{
		{&user_service.NotFoundError{Msg: "this number has no business profile"}, http.StatusNotFound},
		{fmt.Errorf("wrapped: %w", &user_service.NotFoundError{Msg: "x"}), http.StatusNotFound},
		{&user_service.InvalidNumberError{Err: errors.New("bad")}, http.StatusBadRequest},
		{fmt.Errorf("usync: %w", whatsmeow.ErrIQRateOverLimit), http.StatusTooManyRequests},
		{errors.New("boom"), http.StatusInternalServerError},
	}
	for _, c := range cases {
		w := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(w)
		writeQueryError(ctx, c.err)
		if w.Code != c.want {
			t.Errorf("%v: status %d, want %d", c.err, w.Code, c.want)
		}
	}
}
