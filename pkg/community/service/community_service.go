package community_service

import (
	"context"
	"errors"
	"github.com/lucasgiovannibr/whatygo/pkg/safemap"

	instance_model "github.com/lucasgiovannibr/whatygo/pkg/instance/model"
	logger_wrapper "github.com/lucasgiovannibr/whatygo/pkg/logger"
	"github.com/lucasgiovannibr/whatygo/pkg/utils"
	whatsmeow_service "github.com/lucasgiovannibr/whatygo/pkg/whatsmeow/service"
	"github.com/gin-gonic/gin"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

type CommunityService interface {
	CreateCommunity(data *CreateCommunityStruct, instance *instance_model.Instance) (*types.GroupInfo, error)
	CommunityAdd(data *AddParticipantStruct, instance *instance_model.Instance) (gin.H, error)
	CommunityRemove(data *AddParticipantStruct, instance *instance_model.Instance) (gin.H, error)
}

type communityService struct {
	clientPointer    *safemap.Map[*whatsmeow.Client]
	whatsmeowService whatsmeow_service.WhatsmeowService
	loggerWrapper    *logger_wrapper.LoggerManager
}

type CreateCommunityStruct struct {
	CommunityName string `json:"communityName"`
}

type AddParticipantStruct struct {
	CommunityJID string   `json:"communityJid"`
	GroupJID     []string `json:"groupJid"`
}

func (c *communityService) ensureClientConnected(instanceId string) (*whatsmeow.Client, error) {
	return utils.ClientProvider{Clients: c.clientPointer, Starter: c.whatsmeowService, Gate: true}.Ensure(context.Background(), instanceId, c.loggerWrapper.GetLogger(instanceId))
}

func (c *communityService) CreateCommunity(data *CreateCommunityStruct, instance *instance_model.Instance) (*types.GroupInfo, error) {
	client, err := c.ensureClientConnected(instance.Id)
	if err != nil {
		return nil, err
	}

	resp, err := client.CreateGroup(context.Background(), whatsmeow.ReqCreateGroup{
		Name: data.CommunityName,
		GroupParent: types.GroupParent{
			IsParent: true,
		},
	})
	if err != nil {
		c.loggerWrapper.GetLogger(instance.Id).LogError("[%s] error create community: %v", instance.Id, err)
		return nil, err
	}

	return resp, nil
}

func (c *communityService) CommunityAdd(data *AddParticipantStruct, instance *instance_model.Instance) (gin.H, error) {
	client, err := c.ensureClientConnected(instance.Id)
	if err != nil {
		return nil, err
	}

	communityJID, ok := utils.ParseJID(data.CommunityJID)
	if !ok {
		c.loggerWrapper.GetLogger(instance.Id).LogError("[%s] error parse community jid", instance.Id)
		return nil, errors.New("error parse community jid")
	}

	successList, failedList := applyToGroups(data.GroupJID, func(groupJID types.JID) error {
		err := client.LinkGroup(context.Background(), communityJID, groupJID)
		if err != nil {
			c.loggerWrapper.GetLogger(instance.Id).LogError("[%s] error link group: %v", instance.Id, err)
		}
		return err
	})

	return gin.H{
		"success": successList,
		"failed":  failedList,
	}, nil
}

func (c *communityService) CommunityRemove(data *AddParticipantStruct, instance *instance_model.Instance) (gin.H, error) {
	client, err := c.ensureClientConnected(instance.Id)
	if err != nil {
		return nil, err
	}

	communityJID, ok := utils.ParseJID(data.CommunityJID)
	if !ok {
		c.loggerWrapper.GetLogger(instance.Id).LogError("[%s] error parse community jid", instance.Id)
		return nil, errors.New("error parse community jid")
	}

	successList, failedList := applyToGroups(data.GroupJID, func(groupJID types.JID) error {
		err := client.UnlinkGroup(context.Background(), communityJID, groupJID)
		if err != nil {
			c.loggerWrapper.GetLogger(instance.Id).LogError("[%s] error unlink group: %v", instance.Id, err)
		}
		return err
	})

	return gin.H{
		"success": successList,
		"failed":  failedList,
	}, nil
}

func NewCommunityService(
	clientPointer *safemap.Map[*whatsmeow.Client],
	whatsmeowService whatsmeow_service.WhatsmeowService,
	loggerWrapper *logger_wrapper.LoggerManager,
) CommunityService {
	return &communityService{
		clientPointer:    clientPointer,
		whatsmeowService: whatsmeowService,
		loggerWrapper:    loggerWrapper,
	}
}

// applyToGroups runs op on each group and reports which ones worked and which did not.
// A group that cannot be parsed is reported as failed instead of being sent as a
// zero JID. (The lists used to be built wrongly: every group, failed or not, ended up
// in "success", made of the failed list plus the current group.)
func applyToGroups(groups []string, op func(groupJID types.JID) error) (success, failed []string) {
	for _, g := range groups {
		groupJID, ok := utils.ParseJID(g)
		if !ok {
			failed = append(failed, g)
			continue
		}
		if err := op(groupJID); err != nil {
			failed = append(failed, groupJID.String())
			continue
		}
		success = append(success, groupJID.String())
	}
	return success, failed
}
