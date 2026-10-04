package label_service

import (
	"context"
	"github.com/lucasgiovannibr/whatygo/pkg/apierror"
	"github.com/lucasgiovannibr/whatygo/pkg/safemap"

	instance_model "github.com/lucasgiovannibr/whatygo/pkg/instance/model"
	label_model "github.com/lucasgiovannibr/whatygo/pkg/label/model"
	label_repository "github.com/lucasgiovannibr/whatygo/pkg/label/repository"
	logger_wrapper "github.com/lucasgiovannibr/whatygo/pkg/logger"
	"github.com/lucasgiovannibr/whatygo/pkg/utils"
	whatsmeow_service "github.com/lucasgiovannibr/whatygo/pkg/whatsmeow/service"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"
)

type LabelService interface {
	ChatLabel(data *ChatLabelStruct, instance *instance_model.Instance) error
	MessageLabel(data *MessageLabelStruct, instance *instance_model.Instance) error
	EditLabel(data *EditLabelStruct, instance *instance_model.Instance) error
	ChatUnlabel(data *ChatLabelStruct, instance *instance_model.Instance) error
	MessageUnlabel(data *MessageLabelStruct, instance *instance_model.Instance) error
	GetLabels(instance *instance_model.Instance) ([]label_model.Label, error)
}

type labelService struct {
	clientPointer    *safemap.Map[*whatsmeow.Client]
	whatsmeowService whatsmeow_service.WhatsmeowService
	labelRepository  label_repository.LabelRepository
	loggerWrapper    *logger_wrapper.LoggerManager
}

type ChatLabelStruct struct {
	JID     string `json:"jid"`
	LabelID string `json:"labelId"`
}

type MessageLabelStruct struct {
	JID       string `json:"jid"`
	LabelID   string `json:"labelId"`
	MessageID string `json:"messageId"`
}

type EditLabelStruct struct {
	LabelID string `json:"labelId"`
	Name    string `json:"name"`
	Color   int    `json:"color"`
	Deleted bool   `json:"deleted"`
}

func (l *labelService) ensureClientConnected(instanceId string) (*whatsmeow.Client, error) {
	return utils.ClientProvider{Clients: l.clientPointer, Starter: l.whatsmeowService, Gate: true}.Ensure(context.Background(), instanceId, l.loggerWrapper.GetLogger(instanceId))
}

func (l *labelService) ChatLabel(data *ChatLabelStruct, instance *instance_model.Instance) error {
	client, err := l.ensureClientConnected(instance.Id)
	if err != nil {
		return err
	}

	jid, ok := utils.ParseJID(data.JID)
	if !ok {
		l.loggerWrapper.GetLogger(instance.Id).LogError("[%s] error parse chat jid", instance.Id)
		return apierror.Invalid("invalid jid")
	}
	// The label patch is matched to the chat by its JID in the app-state index: no "+",
	// and the LID the phone knows the chat by (see utils.AppStateChatJID).
	jid = utils.AppStateChatJID(client, utils.CanonicalJID(jid))

	err = client.SendAppState(context.Background(), appstate.BuildLabelChat(
		jid,
		data.LabelID,
		true,
	))
	if err != nil {
		l.loggerWrapper.GetLogger(instance.Id).LogError("[%s] error label chat: %v", instance.Id, err)
		return err
	}

	return nil
}

func (l *labelService) MessageLabel(data *MessageLabelStruct, instance *instance_model.Instance) error {
	client, err := l.ensureClientConnected(instance.Id)
	if err != nil {
		return err
	}

	jid, ok := utils.ParseJID(data.JID)
	if !ok {
		l.loggerWrapper.GetLogger(instance.Id).LogError("[%s] error parse chat jid", instance.Id)
		return apierror.Invalid("invalid jid")
	}
	// The label patch is matched to the chat by its JID in the app-state index: no "+",
	// and the LID the phone knows the chat by (see utils.AppStateChatJID).
	jid = utils.AppStateChatJID(client, utils.CanonicalJID(jid))

	err = client.SendAppState(context.Background(), appstate.BuildLabelMessage(
		jid,
		data.LabelID,
		data.MessageID,
		true,
	))
	if err != nil {
		l.loggerWrapper.GetLogger(instance.Id).LogError("[%s] error label message: %v", instance.Id, err)
		return err
	}

	return nil
}

func (l *labelService) EditLabel(data *EditLabelStruct, instance *instance_model.Instance) error {
	client, err := l.ensureClientConnected(instance.Id)
	if err != nil {
		return err
	}

	err = client.SendAppState(context.Background(), appstate.BuildLabelEdit(
		data.LabelID,
		data.Name,
		int32(data.Color),
		data.Deleted,
	))
	if err != nil {
		l.loggerWrapper.GetLogger(instance.Id).LogError("[%s] error label message: %v", instance.Id, err)
		return err
	}

	return nil
}

func (l *labelService) ChatUnlabel(data *ChatLabelStruct, instance *instance_model.Instance) error {
	client, err := l.ensureClientConnected(instance.Id)
	if err != nil {
		return err
	}

	jid, ok := utils.ParseJID(data.JID)
	if !ok {
		l.loggerWrapper.GetLogger(instance.Id).LogError("[%s] error parse chat jid", instance.Id)
		return apierror.Invalid("invalid jid")
	}
	// The label patch is matched to the chat by its JID in the app-state index: no "+",
	// and the LID the phone knows the chat by (see utils.AppStateChatJID).
	jid = utils.AppStateChatJID(client, utils.CanonicalJID(jid))

	err = client.SendAppState(context.Background(), appstate.BuildLabelChat(
		jid,
		data.LabelID,
		false,
	))
	if err != nil {
		l.loggerWrapper.GetLogger(instance.Id).LogError("[%s] error label chat: %v", instance.Id, err)
		return err
	}

	return nil
}

func (l *labelService) MessageUnlabel(data *MessageLabelStruct, instance *instance_model.Instance) error {
	client, err := l.ensureClientConnected(instance.Id)
	if err != nil {
		return err
	}

	jid, ok := utils.ParseJID(data.JID)
	if !ok {
		l.loggerWrapper.GetLogger(instance.Id).LogError("[%s] error parse chat jid", instance.Id)
		return apierror.Invalid("invalid jid")
	}
	// The label patch is matched to the chat by its JID in the app-state index: no "+",
	// and the LID the phone knows the chat by (see utils.AppStateChatJID).
	jid = utils.AppStateChatJID(client, utils.CanonicalJID(jid))

	err = client.SendAppState(context.Background(), appstate.BuildLabelMessage(
		jid,
		data.LabelID,
		data.MessageID,
		false,
	))
	if err != nil {
		l.loggerWrapper.GetLogger(instance.Id).LogError("[%s] error label message: %v", instance.Id, err)
		return err
	}

	return nil
}

func (l *labelService) GetLabels(instance *instance_model.Instance) ([]label_model.Label, error) {
	_, err := l.ensureClientConnected(instance.Id)
	if err != nil {
		return nil, err
	}

	labels, err := l.labelRepository.GetAllLabelsByInstanceID(instance.Id)
	if err != nil {
		l.loggerWrapper.GetLogger(instance.Id).LogError("[%s] error fetching labels from database: %v", instance.Id, err)
		return nil, err
	}

	return labels, nil
}

func NewLabelService(
	clientPointer *safemap.Map[*whatsmeow.Client],
	whatsmeowService whatsmeow_service.WhatsmeowService,
	labelRepository label_repository.LabelRepository,
	loggerWrapper *logger_wrapper.LoggerManager,
) LabelService {
	return &labelService{
		clientPointer:    clientPointer,
		whatsmeowService: whatsmeowService,
		labelRepository:  labelRepository,
		loggerWrapper:    loggerWrapper,
	}
}
