package group_handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	group_service "github.com/lucasgiovannibr/whatygo/pkg/group/service"
	instance_model "github.com/lucasgiovannibr/whatygo/pkg/instance/model"
	"go.mau.fi/whatsmeow/types"
)

type listFake struct {
	group_service.GroupService
	includeParticipants []bool
}

func (f *listFake) ListGroups(_ *instance_model.Instance, include bool) ([]*types.GroupInfo, error) {
	f.includeParticipants = append(f.includeParticipants, include)
	return nil, nil
}

func list(t *testing.T, query string) *listFake {
	t.Helper()
	gin.SetMode(gin.TestMode)
	f := &listFake{}
	h := NewGroupHandler(f)
	r := gin.New()
	r.GET("/group/list", func(c *gin.Context) { c.Set("instance", &instance_model.Instance{Id: "i"}) }, h.ListGroups)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/group/list"+query, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	return f
}

// ListGroups sends the members of every group; ?participants=false leaves them out, and the
// default stays as it was.
func TestListGroupsParticipantsAreOptional(t *testing.T) {
	for query, want := range map[string]bool{"": true, "?participants=true": true, "?participants=false": false, "?participants=0": true} {
		f := list(t, query)
		if len(f.includeParticipants) != 1 || f.includeParticipants[0] != want {
			t.Errorf("%q: includeParticipants = %v, want %v", query, f.includeParticipants, want)
		}
	}
}
