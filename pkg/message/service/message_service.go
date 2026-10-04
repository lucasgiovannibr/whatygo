package message_service

import (
	"context"
	"errors"
	"fmt"
	"github.com/lucasgiovannibr/whatygo/pkg/apierror"
	"github.com/lucasgiovannibr/whatygo/pkg/safemap"
	"net/http"
	"strings"
	"time"

	instance_model "github.com/lucasgiovannibr/whatygo/pkg/instance/model"
	logger_wrapper "github.com/lucasgiovannibr/whatygo/pkg/logger"
	message_model "github.com/lucasgiovannibr/whatygo/pkg/message/model"
	message_repository "github.com/lucasgiovannibr/whatygo/pkg/message/repository"
	"github.com/lucasgiovannibr/whatygo/pkg/utils"
	whatsmeow_service "github.com/lucasgiovannibr/whatygo/pkg/whatsmeow/service"
	"github.com/vincent-petithory/dataurl"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

type MessageService interface {
	React(data *ReactStruct, instance *instance_model.Instance) (*MessageSendStruct, error)
	ChatPresence(data *ChatPresenceStruct, instance *instance_model.Instance) (string, error)
	SubscribePresence(data *SubscribePresenceStruct, instance *instance_model.Instance) ([]SubscribeResult, error)
	MarkRead(data *MarkReadStruct, instance *instance_model.Instance) (string, error)
	MarkPlayed(data *MarkPlayedStruct, instance *instance_model.Instance) (string, error)
	DownloadMedia(data *DownloadMediaStruct, instance *instance_model.Instance, request *http.Request) (*dataurl.DataURL, string, error)
	GetMessageStatus(data *MessageStatusStruct, instance *instance_model.Instance) (*message_model.Message, string, error)
	DeleteMessageEveryone(data *MessageStruct, instance *instance_model.Instance) (string, string, error)
	EditMessage(data *EditMessageStruct, instance *instance_model.Instance) (string, string, error)
	RerequestMessage(data *RerequestStruct, instance *instance_model.Instance) (string, error)
}

type messageService struct {
	clientPointer     *safemap.Map[*whatsmeow.Client]
	messageRepository message_repository.MessageRepository
	whatsmeowService  whatsmeow_service.WhatsmeowService
	loggerWrapper     *logger_wrapper.LoggerManager
}

type ReactStruct struct {
	Number      string `json:"number"`
	Reaction    string `json:"reaction"`
	Id          string `json:"id"`
	FromMe      bool   `json:"fromMe"`
	Participant string `json:"participant,omitempty"`
}

type ChatPresenceStruct struct {
	Number  string `json:"number"`
	State   string `json:"state"`
	IsAudio bool   `json:"isAudio"`
	// Delay, in milliseconds, keeps the "composing"/"recording" indicator alive
	// for the given duration (re-sending it periodically) and then sends "paused".
	// Only applies when State is "composing". 0 = single fire (legacy behaviour).
	Delay int `json:"delay"`
}

type MarkReadStruct struct {
	Id     []string `json:"id"`
	Number string   `json:"number"`
}

type MarkPlayedStruct struct {
	Id     []string `json:"id"`
	Number string   `json:"number"`
}

type DownloadMediaStruct struct {
	Message *waE2E.Message `json:"message"`
}

type MessageStatusStruct struct {
	Id string `json:"id"`
}

type MessageStruct struct {
	Chat      string `json:"chat"`
	MessageID string `json:"messageId"`
}

type EditMessageStruct struct {
	Chat      string `json:"chat"`
	Message   string `json:"message"`
	MessageID string `json:"messageId"`
}

type MessageSendStruct struct {
	Info               types.MessageInfo
	Message            *waE2E.Message
	MessageContextInfo *waE2E.ContextInfo
}

func (m *messageService) ensureClientConnected(instanceId string) (*whatsmeow.Client, error) {
	return utils.ClientProvider{Clients: m.clientPointer, Starter: m.whatsmeowService, Gate: true}.Ensure(context.Background(), instanceId, m.loggerWrapper.GetLogger(instanceId))
}

func (m *messageService) React(data *ReactStruct, instance *instance_model.Instance) (*MessageSendStruct, error) {
	client, err := m.ensureClientConnected(instance.Id)
	if err != nil {
		return nil, err
	}

	msgId := ""

	recipient, ok := utils.ParseJID(data.Number)
	if !ok {
		m.loggerWrapper.GetLogger(instance.Id).LogError("[%s] Error validating message fields", instance.Id)
		return nil, apierror.Invalid("invalid phone number")
	}

	// Strip the "+" that ParseJID/CreateJID adds. The recipient is used both as
	// the SendMessage target (usync/device resolution) AND as the MessageKey
	// RemoteJID that references the reacted message's chat. A malformed "+JID"
	// breaks device resolution (usync) and prevents the reaction from matching
	// the original message's chat. See utils.CanonicalJID.
	recipient = utils.CanonicalJID(recipient)

	if data.Id == "" {
		m.loggerWrapper.GetLogger(instance.Id).LogError("[%s] Missing Id in Payload", instance.Id)
		return nil, apierror.Invalid("missing id in payload")
	} else {
		msgId = data.Id
	}

	fromMe := data.FromMe
	reaction := data.Reaction
	if reaction == "remove" {
		reaction = ""
	}

	// Create MessageKey — msgId is the ID of the message being reacted to,
	// NOT the ID of the reaction envelope itself.
	messageKey := &waCommon.MessageKey{
		RemoteJID: proto.String(recipient.String()),
		FromMe:    proto.Bool(fromMe),
		ID:        proto.String(msgId),
	}

	// Add participant if provided (for group messages)
	if data.Participant != "" {
		participantJID, ok := utils.ParseJID(data.Participant)
		if ok {
			messageKey.Participant = proto.String(utils.CanonicalJID(participantJID).String())
		}
	}

	msg := &waE2E.Message{
		ReactionMessage: &waE2E.ReactionMessage{
			Key:               messageKey,
			Text:              proto.String(reaction),
			SenderTimestampMS: proto.Int64(time.Now().UnixMilli()),
		},
	}

	// Do NOT pass ID: msgId in SendRequestExtra. Doing so would reuse the
	// original message ID as the reaction envelope ID; WhatsApp silently
	// deduplicates it and drops the reaction. Let whatsmeow generate a
	// fresh, unique ID for the envelope.
	response, err := client.SendMessage(context.Background(), recipient, msg)
	if err != nil {
		return nil, err
	}

	isGroup := strings.Contains(data.Number, "@g.us")
	messageType := "ReactionMessage"

	messageInfo := types.MessageInfo{
		MessageSource: types.MessageSource{
			Chat:     recipient,
			Sender:   *client.Store.ID,
			IsFromMe: true,
			IsGroup:  isGroup,
		},
		ID:        response.ID,
		Timestamp: time.Now(),
		ServerID:  response.ServerID,
		Type:      messageType,
	}

	messageSent := &MessageSendStruct{
		Info:    messageInfo,
		Message: msg,
	}

	return messageSent, nil
}

func (m *messageService) ChatPresence(data *ChatPresenceStruct, instance *instance_model.Instance) (string, error) {
	client, err := m.ensureClientConnected(instance.Id)
	if err != nil {
		return "", err
	}

	ts := time.Now()

	recipient, ok := utils.ParseJID(data.Number)
	if !ok {
		m.loggerWrapper.GetLogger(instance.Id).LogError("[%s] Error validating message fields", instance.Id)
		return "", apierror.Invalid("invalid phone number")
	}

	// chatstate (typing) is a RAW node sent without usync normalization, so it
	// needs a canonical digits-only JID or WhatsApp silently drops it. See
	// utils.CanonicalJID for the full rationale.
	recipient = utils.CanonicalJID(recipient)

	media := ""

	if data.IsAudio {
		media = "audio"
	}

	// WhatsApp only forwards chatstate (typing / recording) events to the
	// recipient while the sender is marked online. SendChatPresence merely
	// sends the chatstate node — it does NOT mark us available. Background
	// presence handling (events.AppStateSyncComplete) may have set us to
	// Unavailable, in which case the server silently drops the typing
	// indicator. Mark ourselves available first to guarantee delivery.
	if presErr := client.SendPresence(context.Background(), types.PresenceAvailable); presErr != nil {
		m.loggerWrapper.GetLogger(instance.Id).LogWarn("[%s] SendPresence(available) before chatstate failed (non-fatal): %v", instance.Id, presErr)
	}

	state := types.ChatPresence(data.State)
	mediaType := types.ChatPresenceMedia(media)

	err = client.SendChatPresence(context.Background(), recipient, state, mediaType)
	if err != nil {
		return "", err
	}

	// A single "composing" indicator is ephemeral: WhatsApp expires it after a
	// few seconds unless refreshed. When a Delay is provided (and we're typing),
	// keep the indicator alive for the requested duration by re-sending it, then
	// send "paused" so the indicator clears cleanly instead of timing out.
	if data.Delay > 0 && state == types.ChatPresenceComposing {
		const keepAliveInterval = 5 * time.Second
		const maxDelay = 60 * time.Second

		remaining := time.Duration(data.Delay) * time.Millisecond
		if remaining > maxDelay {
			remaining = maxDelay
		}

		for remaining > 0 {
			sleep := keepAliveInterval
			if remaining < sleep {
				sleep = remaining
			}
			time.Sleep(sleep)
			remaining -= sleep

			if remaining > 0 {
				// Refresh the indicator so it doesn't expire mid-delay.
				if refreshErr := client.SendChatPresence(context.Background(), recipient, state, mediaType); refreshErr != nil {
					m.loggerWrapper.GetLogger(instance.Id).LogWarn("[%s] Refresh chatstate failed (non-fatal): %v", instance.Id, refreshErr)
				}
			}
		}

		if pausedErr := client.SendChatPresence(context.Background(), recipient, types.ChatPresencePaused, mediaType); pausedErr != nil {
			m.loggerWrapper.GetLogger(instance.Id).LogWarn("[%s] SendChatPresence(paused) failed (non-fatal): %v", instance.Id, pausedErr)
		}
	}

	m.loggerWrapper.GetLogger(instance.Id).LogInfo("Presence (%s) sent to %s", data.State, data.Number)

	return ts.String(), nil
}

func (m *messageService) MarkRead(data *MarkReadStruct, instance *instance_model.Instance) (string, error) {
	client, err := m.ensureClientConnected(instance.Id)
	if err != nil {
		return "", err
	}

	ts := time.Now()

	jid, ok := utils.ParseJID(data.Number)
	if !ok {
		m.loggerWrapper.GetLogger(instance.Id).LogError("[%s] Error validating message fields", instance.Id)
		return "", apierror.Invalid("invalid phone number")
	}

	// Read receipts are RAW nodes (no usync) — strip the "+" so the receipt
	// reaches the recipient. Same root cause as the typing fix above.
	jid = utils.CanonicalJID(jid)

	err = client.MarkRead(context.Background(), data.Id, time.Now(), jid, jid)
	if err != nil {
		m.loggerWrapper.GetLogger(instance.Id).LogError("[%s] error marking message as read: %v", instance.Id, err)
		return "", errors.New("error marking message as read")
	}

	return ts.String(), nil
}

func (m *messageService) MarkPlayed(data *MarkPlayedStruct, instance *instance_model.Instance) (string, error) {
	client, err := m.ensureClientConnected(instance.Id)
	if err != nil {
		return "", err
	}

	ts := time.Now()

	jid, ok := utils.ParseJID(data.Number)
	if !ok {
		m.loggerWrapper.GetLogger(instance.Id).LogError("[%s] Error validating message fields", instance.Id)
		return "", apierror.Invalid("invalid phone number")
	}

	// Played receipts are RAW nodes (no usync) — strip the "+" so the receipt
	// reaches the recipient. Same root cause as the MarkRead fix.
	jid = utils.CanonicalJID(jid)

	err = client.MarkRead(context.Background(), data.Id, time.Now(), jid, jid, types.ReceiptTypePlayed)
	if err != nil {
		m.loggerWrapper.GetLogger(instance.Id).LogError("[%s] error marking message as played: %v", instance.Id, err)
		return "", errors.New("error marking message as played")
	}

	return ts.String(), nil
}

func (m *messageService) DownloadMedia(data *DownloadMediaStruct, instance *instance_model.Instance, request *http.Request) (*dataurl.DataURL, string, error) {
	client, err := m.ensureClientConnected(instance.Id)
	if err != nil {
		return nil, "", err
	}

	ts := time.Now()

	msg := data.Message

	mimetype := ""
	var mediaData []byte

	img := msg.GetImageMessage()
	audio := msg.GetAudioMessage()
	document := msg.GetDocumentMessage()
	video := msg.GetVideoMessage()
	sticker := msg.GetStickerMessage()

	if img == nil && audio == nil && document == nil && video == nil && sticker == nil {
		return nil, "", apierror.Invalid("invalid media type")
	}

	// The first media of the message, in the order the handler always looked at them. Only
	// the type of the file and how big the message says it is are needed from it.
	var target whatsmeow.DownloadableMessage
	var kind string
	var announced uint64
	switch {
	case img != nil:
		target, kind, mimetype, announced = img, "image", img.GetMimetype(), img.GetFileLength()
	case audio != nil:
		target, kind, mimetype, announced = audio, "audio", audio.GetMimetype(), audio.GetFileLength()
	case document != nil:
		target, kind, mimetype, announced = document, "document", document.GetMimetype(), document.GetFileLength()
	case video != nil:
		target, kind, mimetype, announced = video, "video", video.GetMimetype(), video.GetFileLength()
	default:
		target, kind, mimetype, announced = sticker, "sticker", sticker.GetMimetype(), sticker.GetFileLength()
	}

	// Through a temporary file, never above MAX_RECEIVED_MEDIA_MB: whatsmeow's Download keeps
	// the whole file in memory twice, and a document may be 2 GB.
	file, err := utils.DownloadMedia(context.Background(), int64(announced), utils.MaxReceivedMediaBytes(), func(ctx context.Context, f whatsmeow.File) error {
		return client.DownloadToFile(ctx, target, f)
	})
	if err != nil {
		m.loggerWrapper.GetLogger(instance.Id).LogError("[%s] Failed to download %s: %v", instance.Id, kind, err)
		if utils.IsMediaTooLarge(err) {
			return nil, "", err
		}
		return nil, "", fmt.Errorf("Failed to download %s %v", kind, err)
	}
	defer file.Close()

	mediaData, err = file.Bytes()
	if err != nil {
		return nil, "", err
	}

	dataURL := dataurl.New(mediaData, mimetype)

	return dataURL, ts.String(), nil
}

func (m *messageService) GetMessageStatus(data *MessageStatusStruct, instance *instance_model.Instance) (*message_model.Message, string, error) {
	_, err := m.ensureClientConnected(instance.Id)
	if err != nil {
		return nil, "", err
	}

	ts := time.Now()

	result, err := m.messageRepository.GetMessageByID(instance.Id, data.Id)
	if err != nil {
		return nil, "", err
	}

	return result, ts.String(), nil
}

func (m *messageService) DeleteMessageEveryone(data *MessageStruct, instance *instance_model.Instance) (string, string, error) {
	client, err := m.ensureClientConnected(instance.Id)
	if err != nil {
		return "", "", err
	}

	recipient, ok := utils.ParseJID(data.Chat)
	if !ok {
		m.loggerWrapper.GetLogger(instance.Id).LogError("[%s] Error validating message fields", instance.Id)
		return "", "", apierror.Invalid("invalid phone number")
	}
	// The chat JID lands inside the revoke's protocolMessage Key: with the "+"
	// prefix CreateJID adds, receiving devices look up a chat that doesn't
	// exist and silently ignore the revoke. See utils.CanonicalJID.
	recipient = utils.CanonicalJID(recipient)

	m.loggerWrapper.GetLogger(instance.Id).LogInfo("Revoking message %s from %s", data.MessageID, recipient)

	resp, err := client.SendMessage(
		context.Background(),
		recipient,
		client.BuildRevoke(recipient, types.EmptyJID, data.MessageID))
	if err != nil {
		m.loggerWrapper.GetLogger(instance.Id).LogError("[%s] error revoking message: %v", instance.Id, err)
		return "", "", err
	}

	response := resp.ID

	return response, resp.Timestamp.String(), nil
}

func (m *messageService) EditMessage(data *EditMessageStruct, instance *instance_model.Instance) (string, string, error) {
	client, err := m.ensureClientConnected(instance.Id)
	if err != nil {
		return "", "", err
	}

	recipient, ok := utils.ParseJID(data.Chat)
	if !ok {
		m.loggerWrapper.GetLogger(instance.Id).LogError("[%s] Error validating message fields", instance.Id)
		return "", "", apierror.Invalid("invalid phone number")
	}
	// Same as DeleteMessageEveryone: the JID lands inside the edit's
	// protocolMessage Key, so the "+" prefix makes recipients ignore it.
	recipient = utils.CanonicalJID(recipient)

	resp, err := client.SendMessage(
		context.Background(),
		recipient,
		client.BuildEdit(
			recipient,
			data.MessageID,
			&waE2E.Message{
				ExtendedTextMessage: &waE2E.ExtendedTextMessage{
					Text: &data.Message,
				},
			}))
	if err != nil {
		m.loggerWrapper.GetLogger(instance.Id).LogError("[%s] error revoking message: %v", instance.Id, err)
		return "", "", err
	}

	return resp.ID, resp.Timestamp.String(), nil
}

func NewMessageService(
	clientPointer *safemap.Map[*whatsmeow.Client],
	messageRepository message_repository.MessageRepository,
	whatsmeowService whatsmeow_service.WhatsmeowService,
	loggerWrapper *logger_wrapper.LoggerManager,
) MessageService {
	return &messageService{
		clientPointer:     clientPointer,
		messageRepository: messageRepository,
		whatsmeowService:  whatsmeowService,
		loggerWrapper:     loggerWrapper,
	}
}
