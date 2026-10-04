package instance_handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	instance_model "github.com/lucasgiovannibr/whatygo/pkg/instance/model"
	instance_service "github.com/lucasgiovannibr/whatygo/pkg/instance/service"
)

const validID = "6f1c1b2e-3c1d-4a51-9a4b-0d3a7d9f2b11"

// fakeService overrides only UpdateIntegrations; any other call panics on the nil embedded interface.
type fakeService struct {
	instance_service.InstanceService
	got *instance_service.IntegrationsStruct
	ret *instance_model.Instance
	err error
}

func (f *fakeService) UpdateIntegrations(_ string, d *instance_service.IntegrationsStruct) (*instance_model.Instance, error) {
	f.got = d
	return f.ret, f.err
}

func put(t *testing.T, svc *fakeService, id, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := NewInstanceHandler(svc, nil)
	r.PUT("/instance/:instanceId/integrations", h.UpdateIntegrations)
	req := httptest.NewRequest(http.MethodPut, "/instance/"+id+"/integrations", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestUpdateIntegrationsStoresAndReturnsSettings(t *testing.T) {
	svc := &fakeService{ret: &instance_model.Instance{Webhook: "https://x.dev/h", Events: "MESSAGE,CALL", RabbitmqEnable: "enabled"}}
	w := put(t, svc, validID, `{"webhookUrl":"https://x.dev/h","subscribe":["MESSAGE","CALL"],"rabbitmqEnable":"enabled"}`)

	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if svc.got == nil || svc.got.WebhookUrl != "https://x.dev/h" || len(svc.got.Subscribe) != 2 {
		t.Fatalf("service received %+v", svc.got)
	}
	var res struct {
		Settings map[string]string `json:"settings"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.Settings["webhookUrl"] != "https://x.dev/h" || res.Settings["events"] != "MESSAGE,CALL" || res.Settings["rabbitmqEnable"] != "enabled" {
		t.Fatalf("settings %+v", res.Settings)
	}
}

func TestUpdateIntegrationsRejectsBadInputWithoutTouchingTheService(t *testing.T) {
	for name, tc := range map[string]struct{ id, body string }{
		"bad uuid":      {"not-a-uuid", `{}`},
		"bad json":      {validID, `{`},
		"unknown event": {validID, `{"subscribe":["NOPE"]}`},
		"bad webhook":   {validID, `{"webhookUrl":"example.com"}`},
		"bad producer":  {validID, `{"natsEnable":"maybe"}`},
	} {
		t.Run(name, func(t *testing.T) {
			svc := &fakeService{}
			if w := put(t, svc, tc.id, tc.body); w.Code != http.StatusBadRequest {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
			if svc.got != nil {
				t.Fatal("service must not be called for invalid input")
			}
		})
	}
}

func TestUpdateIntegrationsMapsNotFoundAndFailures(t *testing.T) {
	if w := put(t, &fakeService{err: gorm.ErrRecordNotFound}, validID, `{}`); w.Code != http.StatusNotFound {
		t.Fatalf("not found: status %d", w.Code)
	}
	if w := put(t, &fakeService{err: http.ErrAbortHandler}, validID, `{}`); w.Code != http.StatusInternalServerError {
		t.Fatalf("failure: status %d", w.Code)
	}
}
