package group_service

import (
	"testing"

	"go.mau.fi/whatsmeow/types"
)

func TestWithoutParticipantsDropsTheMemberLists(t *testing.T) {
	groups := []*types.GroupInfo{
		{GroupName: types.GroupName{Name: "a"}, Participants: []types.GroupParticipant{{}, {}, {}}},
		nil,
		{GroupName: types.GroupName{Name: "b"}},
	}
	withoutParticipants(groups)
	if groups[0].Participants != nil || groups[2].Participants != nil {
		t.Fatal("the members must be dropped")
	}
	if groups[0].GroupName.Name != "a" {
		t.Fatal("everything else is kept")
	}
}
