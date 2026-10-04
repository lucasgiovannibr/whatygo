package user_handler

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	instance_model "github.com/lucasgiovannibr/whatygo/pkg/instance/model"
	user_service "github.com/lucasgiovannibr/whatygo/pkg/user/service"
)

// The service must never be reached with a list above the cap.
type neverCalledService struct {
	user_service.UserService
	called bool
}

func (n *neverCalledService) CheckUser(*user_service.CheckUserStruct, *instance_model.Instance) (*user_service.CheckUserCollection, error) {
	n.called = true
	return &user_service.CheckUserCollection{}, nil
}

func TestCheckUserRefusesAHugeList(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &neverCalledService{}
	h := &userHandler{userService: svc}

	r := gin.New()
	r.POST("/user/check", func(c *gin.Context) { c.Set("instance", &instance_model.Instance{Id: "i"}) }, h.CheckUser)

	numbers := make([]string, MaxNumbersPerQuery+1)
	for i := range numbers {
		numbers[i] = fmt.Sprintf(`"55119999%05d"`, i)
	}
	body := `{"number":[` + strings.Join(numbers, ",") + `]}`
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/user/check", strings.NewReader(body)))

	if w.Code != http.StatusBadRequest || svc.called {
		t.Fatalf("a list above %d must be refused with 400 before reaching WhatsApp (code %d, service called %v)", MaxNumbersPerQuery, w.Code, svc.called)
	}

	svc2 := &neverCalledService{}
	h2 := &userHandler{userService: svc2}
	r2 := gin.New()
	r2.POST("/user/check", func(c *gin.Context) { c.Set("instance", &instance_model.Instance{Id: "i"}) }, h2.CheckUser)
	w2 := httptest.NewRecorder()
	r2.ServeHTTP(w2, httptest.NewRequest(http.MethodPost, "/user/check", strings.NewReader(`{"number":["5511999990001"]}`)))
	if w2.Code != http.StatusOK || !svc2.called {
		t.Fatalf("a normal list must pass (code %d, called %v)", w2.Code, svc2.called)
	}
}
