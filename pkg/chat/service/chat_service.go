package chat_service

import (
	"context"
	"github.com/lucasgiovannibr/whatygo/pkg/safemap"
	"github.com/lucasgiovannibr/whatygo/pkg/utils"

	instance_model "github.com/lucasgiovannibr/whatygo/pkg/instance/model"
	logger_wrapper "github.com/lucasgiovannibr/whatygo/pkg/logger"
	whatsmeow_service "github.com/lucasgiovannibr/whatygo/pkg/whatsmeow/service"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

type ChatService interface {
	ChatPin(data *BodyStruct, instance *instance_model.Instance) (string, error)
	ChatUnpin(data *BodyStruct, instance *instance_model.Instance) (string, error)
	ChatArchive(data *BodyStruct, instance *instance_model.Instance) (string, error)
	ChatUnarchive(data *BodyStruct, instance *instance_model.Instance) (string, error)
	ChatMute(data *BodyStruct, instance *instance_model.Instance) (string, error)
	ChatUnmute(data *BodyStruct, instance *instance_model.Instance) (string, error)
	HistorySyncRequest(ctx context.Context, data *HistorySyncRequestStruct, instance *instance_model.Instance) (*whatsmeow.SendResponse, error)
	SetDisappearing(data *DisappearingStruct, instance *instance_model.Instance) error
	SetDefaultDisappearing(data *DefaultDisappearingStruct, instance *instance_model.Instance) error
}

type chatService struct {
	clientPointer    *safemap.Map[*whatsmeow.Client]
	whatsmeowService whatsmeow_service.WhatsmeowService
	loggerWrapper    *logger_wrapper.LoggerManager
}

type BodyStruct struct {
	Chat string `json:"chat"`
	// Duration is only read by /chat/mute: "8h", "1w" (or "7d"), "always", or any Go
	// duration such as "30m". Empty keeps the historical 1 hour.
	Duration string `json:"duration,omitempty"`
}

type HistorySyncRequestStruct struct {
	MessageInfo *types.MessageInfo `json:"messageInfo"`
	Count       int                `json:"count"`
}

func (c *chatService) ensureClientConnected(instanceId string) (*whatsmeow.Client, error) {
	return utils.ClientProvider{Clients: c.clientPointer, Starter: c.whatsmeowService, Gate: true}.Ensure(context.Background(), instanceId, c.loggerWrapper.GetLogger(instanceId))
}

func (c *chatService) HistorySyncRequest(ctx context.Context, data *HistorySyncRequestStruct, instance *instance_model.Instance) (*whatsmeow.SendResponse, error) {
	client, err := c.ensureClientConnected(instance.Id)
	if err != nil {
		return nil, err
	}

	messageInfo := types.MessageInfo{
		MessageSource: types.MessageSource{
			Chat:     data.MessageInfo.Chat,
			IsFromMe: data.MessageInfo.IsFromMe,
			IsGroup:  data.MessageInfo.IsGroup,
		},
		ID:        data.MessageInfo.ID,
		Timestamp: data.MessageInfo.Timestamp,
	}

	histRequest := client.BuildHistorySyncRequest(&messageInfo, data.Count)

	// On-demand history-sync requests must be sent to our OWN JID as a peer message, not to the
	// contact. SendPeerMessage does exactly that (getOwnID().ToNonAD() + Peer:true). Sending it to
	// messageInfo.Chat (the contact) fails with "no signal session established" and never returns
	// history. whatsmeow's BuildHistorySyncRequest doc: "The built message can be sent using
	// Client.SendPeerMessage." The target chat/cursor is already encoded inside histRequest.
	// Uses the caller's context so the request can be cancelled / time-bounded.
	res, err := client.SendPeerMessage(ctx, histRequest)
	if err != nil {
		c.loggerWrapper.GetLogger(instance.Id).LogError("[%s] error history sync request: %v", instance.Id, err)
		return nil, err
	}

	return &res, nil
}

func NewChatService(
	clientPointer *safemap.Map[*whatsmeow.Client],
	whatsmeowService whatsmeow_service.WhatsmeowService,
	loggerWrapper *logger_wrapper.LoggerManager,
) ChatService {
	return &chatService{
		clientPointer:    clientPointer,
		whatsmeowService: whatsmeowService,
		loggerWrapper:    loggerWrapper,
	}
}
