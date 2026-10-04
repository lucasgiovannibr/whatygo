package instance_handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	config "github.com/lucasgiovannibr/whatygo/pkg/config"
	instance_model "github.com/lucasgiovannibr/whatygo/pkg/instance/model"
	instance_service "github.com/lucasgiovannibr/whatygo/pkg/instance/service"
)

type createFake struct {
	instance_service.InstanceService
	got *instance_service.CreateStruct
}

func (f *createFake) Create(d *instance_service.CreateStruct) (*instance_model.Instance, error) {
	f.got = d
	return &instance_model.Instance{Id: "id", Name: d.Name, Token: d.Token}, nil
}

func create(t *testing.T, f *createFake, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := NewInstanceHandler(f, &config.Config{MinTokenLength: 16})
	r.POST("/instance/create", h.Create)
	req := httptest.NewRequest(http.MethodPost, "/instance/create", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestCreateRefusesAShortToken(t *testing.T) {
	f := &createFake{}
	w := create(t, f, `{"name":"n","token":"a"}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "at least 16") {
		t.Fatalf("want 400 naming the minimum, got %d %s", w.Code, w.Body.String())
	}
	if f.got != nil {
		t.Fatal("nothing may be created")
	}
	if w := create(t, f, `{"name":"n","token":"123456789012345"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("15 characters are still short, got %d", w.Code)
	}
}

func TestCreateKeepsAnAcceptableToken(t *testing.T) {
	f := &createFake{}
	w := create(t, f, `{"name":"n","token":"1234567890123456"}`)
	if w.Code != http.StatusOK || f.got == nil || f.got.Token != "1234567890123456" {
		t.Fatalf("got %d %s %+v", w.Code, w.Body.String(), f.got)
	}
}

func TestCreateGeneratesATokenWhenNoneIsGiven(t *testing.T) {
	f := &createFake{}
	w := create(t, f, `{"name":"n"}`)
	if w.Code != http.StatusOK || f.got == nil {
		t.Fatalf("got %d %s", w.Code, w.Body.String())
	}
	if _, err := uuid.Parse(f.got.Token); err != nil {
		t.Fatalf("a generated token is a UUID, got %q", f.got.Token)
	}
	var res struct {
		Data struct{ Token string }
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil || res.Data.Token != f.got.Token {
		t.Fatalf("the response must carry the generated token: %s", w.Body.String())
	}
	f2 := &createFake{}
	create(t, f2, `{"name":"n"}`)
	if f2.got.Token == f.got.Token {
		t.Fatal("tokens must differ")
	}
}
