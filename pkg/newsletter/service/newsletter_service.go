package newsletter_service

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

type NewsletterService interface {
	CreateNewsletter(data *CreateNewsletterStruct, instance *instance_model.Instance) (*types.NewsletterMetadata, error)
	ListNewsletter(instance *instance_model.Instance) ([]*types.NewsletterMetadata, error)
	GetNewsletter(data *GetNewsletterStruct, instance *instance_model.Instance) (*types.NewsletterMetadata, error)
	GetNewsletterInvite(data *GetNewsletterInviteStruct, instance *instance_model.Instance) (*types.NewsletterMetadata, error)
	SubscribeNewsletter(data *GetNewsletterStruct, instance *instance_model.Instance) error
	GetNewsletterMessages(data *GetNewsletterMessagesStruct, instance *instance_model.Instance) ([]*types.NewsletterMessage, error)
	FollowNewsletter(data *GetNewsletterStruct, instance *instance_model.Instance) error
	UnfollowNewsletter(data *GetNewsletterStruct, instance *instance_model.Instance) error
	MuteNewsletter(data *NewsletterMuteStruct, instance *instance_model.Instance) error
	MarkNewsletterViewed(data *NewsletterMarkViewedStruct, instance *instance_model.Instance) error
	ReactNewsletter(data *NewsletterReactStruct, instance *instance_model.Instance) error
}

type newsletterService struct {
	clientPointer    *safemap.Map[*whatsmeow.Client]
	whatsmeowService whatsmeow_service.WhatsmeowService
	loggerWrapper    *logger_wrapper.LoggerManager
}

type CreateNewsletterStruct struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type GetNewsletterStruct struct {
	JID types.JID `json:"jid"`
}

type GetNewsletterInviteStruct struct {
	Key string `json:"key"`
}

type GetNewsletterMessagesStruct struct {
	JID      types.JID `json:"jid"`
	Count    int       `json:"count"`
	BeforeID int       `json:"before_id"`
}

func (n *newsletterService) ensureClientConnected(instanceId string) (*whatsmeow.Client, error) {
	return utils.ClientProvider{Clients: n.clientPointer, Starter: n.whatsmeowService, Gate: true}.Ensure(context.Background(), instanceId, n.loggerWrapper.GetLogger(instanceId))
}

func (n *newsletterService) CreateNewsletter(data *CreateNewsletterStruct, instance *instance_model.Instance) (*types.NewsletterMetadata, error) {
	client, err := n.ensureClientConnected(instance.Id)
	if err != nil {
		return nil, err
	}

	newsletter, err := client.CreateNewsletter(context.Background(), whatsmeow.CreateNewsletterParams{
		Name:        data.Name,
		Description: data.Description,
	})
	if err != nil {
		n.loggerWrapper.GetLogger(instance.Id).LogError("[%s] error create newsletter: %v", instance.Id, err)
		return nil, err
	}

	return newsletter, nil
}

func (n *newsletterService) ListNewsletter(instance *instance_model.Instance) ([]*types.NewsletterMetadata, error) {
	client, err := n.ensureClientConnected(instance.Id)
	if err != nil {
		return nil, err
	}

	newsletters, err := client.GetSubscribedNewsletters(context.Background())
	if err != nil {
		n.loggerWrapper.GetLogger(instance.Id).LogError("[%s] error list newsletters: %v", instance.Id, err)
		return nil, err
	}

	// For each newsletter, fetch full info to get subscribers_count
	fullNewsletters := make([]*types.NewsletterMetadata, 0, len(newsletters))
	for _, newsletter := range newsletters {
		fullInfo, err := client.GetNewsletterInfo(context.Background(), newsletter.ID)
		if err != nil {
			// If we can't get full info, use the basic one
			n.loggerWrapper.GetLogger(instance.Id).LogWarn("[%s] error getting full info for newsletter %s: %v", instance.Id, newsletter.ID.String(), err)
			fullNewsletters = append(fullNewsletters, newsletter)
			continue
		}
		fullNewsletters = append(fullNewsletters, fullInfo)
	}

	return fullNewsletters, nil
}

func (n *newsletterService) GetNewsletter(data *GetNewsletterStruct, instance *instance_model.Instance) (*types.NewsletterMetadata, error) {
	client, err := n.ensureClientConnected(instance.Id)
	if err != nil {
		return nil, err
	}

	newsletter, err := client.GetNewsletterInfo(context.Background(), data.JID)
	if err != nil {
		n.loggerWrapper.GetLogger(instance.Id).LogError("[%s] error list newsletter: %v", instance.Id, err)
		return nil, err
	}

	return newsletter, nil
}

func (n *newsletterService) GetNewsletterInvite(data *GetNewsletterInviteStruct, instance *instance_model.Instance) (*types.NewsletterMetadata, error) {
	client, err := n.ensureClientConnected(instance.Id)
	if err != nil {
		return nil, err
	}

	newsletter, err := client.GetNewsletterInfoWithInvite(context.Background(), data.Key)
	if err != nil {
		n.loggerWrapper.GetLogger(instance.Id).LogError("[%s] error list newsletter: %v", instance.Id, err)
		return nil, err
	}

	return newsletter, nil
}

func (n *newsletterService) SubscribeNewsletter(data *GetNewsletterStruct, instance *instance_model.Instance) error {
	client, err := n.ensureClientConnected(instance.Id)
	if err != nil {
		return err
	}

	_, err = client.NewsletterSubscribeLiveUpdates(context.TODO(), data.JID)
	if err != nil {
		n.loggerWrapper.GetLogger(instance.Id).LogError("[%s] error list newsletter: %v", instance.Id, err)
		return err
	}

	return nil
}

func (n *newsletterService) GetNewsletterMessages(data *GetNewsletterMessagesStruct, instance *instance_model.Instance) ([]*types.NewsletterMessage, error) {
	client, err := n.ensureClientConnected(instance.Id)
	if err != nil {
		return nil, err
	}

	messages, err := client.GetNewsletterMessages(context.Background(), data.JID,
		&whatsmeow.GetNewsletterMessagesParams{
			Count: data.Count, Before: data.BeforeID,
		})
	if err != nil {
		n.loggerWrapper.GetLogger(instance.Id).LogError("[%s] error list newsletter: %v", instance.Id, err)
		return nil, err
	}

	return messages, nil
}

func NewNewsletterService(
	clientPointer *safemap.Map[*whatsmeow.Client],
	whatsmeowService whatsmeow_service.WhatsmeowService,
	loggerWrapper *logger_wrapper.LoggerManager,
) NewsletterService {
	return &newsletterService{
		clientPointer:    clientPointer,
		whatsmeowService: whatsmeowService,
		loggerWrapper:    loggerWrapper,
	}
}
