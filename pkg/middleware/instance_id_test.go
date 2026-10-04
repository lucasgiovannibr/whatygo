package auth_middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestValidInstanceID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	reached := 0
	r.GET("/instance/:instanceId/x", ValidInstanceID(), func(c *gin.Context) { reached++; c.Status(http.StatusOK) })
	r.GET("/instance/all", ValidInstanceID(), func(c *gin.Context) { reached++; c.Status(http.StatusOK) })

	for path, want := range map[string]int{
		"/instance/6f1c1b2e-3c1d-4a51-9a4b-0d3a7d9f2b11/x": http.StatusOK,
		"/instance/..%2f..%2fetc/x":                        http.StatusNotFound, // the router does not match it at all
		"/instance/not-a-uuid/x":                           http.StatusBadRequest,
		"/instance/%2e%2e/x":                               http.StatusBadRequest,
		"/instance/all":                                    http.StatusOK, // a route without :instanceId is left alone
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != want {
			t.Errorf("%s: got %d, want %d (%s)", path, w.Code, want, w.Body.String())
		}
	}
	if reached != 2 {
		t.Fatalf("the handler ran %d times, want 2", reached)
	}
}
