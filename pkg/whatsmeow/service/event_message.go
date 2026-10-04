package whatsmeow_service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	message_model "github.com/lucasgiovannibr/whatygo/pkg/message/model"
	poll_service "github.com/lucasgiovannibr/whatygo/pkg/poll/service"
	"github.com/lucasgiovannibr/whatygo/pkg/utils"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

// handleMessage processes a received message: filters, JID clean-up, poll votes, media, group data, persistence and button clicks.
// It reports whether the event goes out to the webhooks and queues, and the chat it concerns
// (for the subscription filter). A plain return means "nothing to dispatch".
func (mycli *MyClient) handleMessage(evt *events.Message, postMap map[string]interface{}) (dispatch bool, eventChat string) {
	userID := mycli.userID
	postMap["event"] = "Message"
	learnChatTimerFromMessage(mycli.userID, evt, time.Now())
	// Message received

	// Log message arrival with detailed info
	messageSize := "unknown"
	if evt.Message.GetDocumentMessage() != nil && evt.Message.GetDocumentMessage().FileLength != nil {
		messageSize = fmt.Sprintf("%d bytes", *evt.Message.GetDocumentMessage().FileLength)
	} else if evt.Message.GetVideoMessage() != nil && evt.Message.GetVideoMessage().FileLength != nil {
		messageSize = fmt.Sprintf("%d bytes", *evt.Message.GetVideoMessage().FileLength)
	} else if evt.Message.GetImageMessage() != nil && evt.Message.GetImageMessage().FileLength != nil {
		messageSize = fmt.Sprintf("%d bytes", *evt.Message.GetImageMessage().FileLength)
	} else if evt.Message.GetAudioMessage() != nil && evt.Message.GetAudioMessage().FileLength != nil {
		messageSize = fmt.Sprintf("%d bytes", *evt.Message.GetAudioMessage().FileLength)
	}

	mycli.loggerWrapper.GetLogger(mycli.userID).LogInfo("[%s] ===== MESSAGE RECEIVED ===== ID: %s, From: %s, Type: %s, Size: %s", mycli.userID, evt.Info.ID, evt.Info.Chat.String(), evt.Info.Type, messageSize)

	// readMessages: the message is marked as read once, further below, after the
	// ignore filters and the LID/PN swap (it used to be marked here too, before any
	// filter, and with the sender as the chat, which is wrong in groups).

	// se ignoreStatus for true e o chat for broadcast ou o id for broadcast retorna
	if mycli.inst().IgnoreStatus && (strings.Contains(evt.Info.Chat.String(), "@broadcast") || strings.Contains(evt.Info.ID, "@broadcast")) {
		return
	}

	// se ignoreGroup for true e o chat for grupo retorna
	if mycli.inst().IgnoreGroups && strings.Contains(evt.Info.Chat.String(), "@g.us") {
		return
	}

	// Verifica advanced settings para ignorar grupos
	if (mycli.config.EventIgnoreGroup || mycli.inst().IgnoreGroups) && strings.Contains(evt.Info.Chat.String(), "@g.us") {
		return
	}

	// Verifica advanced settings para ignorar status/broadcast
	if (mycli.config.EventIgnoreStatus || mycli.inst().IgnoreStatus) && (strings.Contains(evt.Info.Chat.String(), "@broadcast") || strings.Contains(evt.Info.ID, "@broadcast")) {
		return
	}

	// Edits arrive sealed in a secretEncryptedMessage envelope. Unwrap before typing the
	// message, so it is classified as "edit" and the webhook carries the new text.
	// This MUST run before the LID/PN swap below: the decryption key is derived from
	// the sender JID in the form it had on the wire, and swapping it first makes the
	// decrypt fail with "message authentication failed" for @lid contacts.
	mycli.unwrapSecretEncryptedEdit(evt)

	// Poll votes have the same constraint: the vote key is derived from the voter
	// and chat JIDs as received, so decrypting after the swap always failed for
	// @lid contacts, no vote was ever stored and /polls/{id}/results answered 404
	// "No votes found" (#60).
	var decryptedPollVote *waE2E.PollVoteMessage
	var pollVoteDecryptErr error
	if evt.Message.GetPollUpdateMessage() != nil {
		decryptedPollVote, pollVoteDecryptErr = mycli.decryptPollVote(evt)
	}

	normalizeMessageJIDs(mycli.loggerWrapper.GetLogger(mycli.userID), mycli.userID, &evt.Info)

	// Auto-marca mensagens como lidas se configurado
	if mycli.inst().ReadMessages && !evt.Info.IsFromMe {
		go func() {
			time.Sleep(1 * time.Second) // Pequeno delay para parecer mais natural
			err := mycli.WAClient.MarkRead(context.Background(), []types.MessageID{evt.Info.ID}, evt.Info.Timestamp, evt.Info.Chat, evt.Info.Sender)
			if err != nil {
				mycli.loggerWrapper.GetLogger(mycli.userID).LogError("[%s] Failed to auto-mark message as read: %v", mycli.userID, err)
			} else {
				mycli.loggerWrapper.GetLogger(mycli.userID).LogInfo("[%s] Auto-marked message as read from %s", mycli.userID, evt.Info.Chat.String())
			}
		}()
	}

	parsedMessageType := utils.GetMessageType(evt.Message)
	if parsedMessageType == "ignore" || strings.HasPrefix(parsedMessageType, "unknown_protocol_") {
		mycli.loggerWrapper.GetLogger(mycli.userID).LogInfo("[%s] Message ignored because it's a unknown protocol message", mycli.userID)
		return
	}

	// Nobody receives this event (no subscription, no output, no global queue): do not
	// download its media, ask WhatsApp for the group or serialize it. Those are the
	// costly parts of a message and used to run for every message regardless.
	eventChat = evt.Info.Chat.String()
	wantMessage := mycli.service.EventWanted(mycli.inst(), "Message", eventChat)

	if postMap["data"] != nil {
		jsonBytes, err := json.Marshal(postMap["data"])
		if err != nil {
			mycli.loggerWrapper.GetLogger(mycli.userID).LogError("[%s] Failed to marshal postMap['data']: %v", mycli.userID, err)
			return
		}

		var dataMap map[string]interface{}
		err = json.Unmarshal(jsonBytes, &dataMap)
		if err != nil {
			mycli.loggerWrapper.GetLogger(mycli.userID).LogError("[%s] Failed to unmarshal postMap['data'] to map[string]interface{}: %v", mycli.userID, err)
			return
		}

		postMap["data"] = dataMap
	} else {
		postMap["data"] = make(map[string]interface{})
	}

	dataMap, ok := postMap["data"].(map[string]interface{})
	if !ok {
		dataMap = make(map[string]interface{})
	}

	referral := extractReferralFromMessage(evt.Message)

	if evt.Message.GetPollUpdateMessage() != nil {
		decrypted, err := decryptedPollVote, pollVoteDecryptErr
		if err != nil {
			mycli.loggerWrapper.GetLogger(mycli.userID).LogError("[%s] Failed to decrypt vote: %v", mycli.userID, err)
		} else {
			mycli.loggerWrapper.GetLogger(mycli.userID).LogInfo("[%s] Selected options in decrypted vote:", mycli.userID)
			for _, option := range decrypted.SelectedOptions {
				mycli.loggerWrapper.GetLogger(mycli.userID).LogInfo("- %X", option)

			}

			// NOVO: Salvar voto no banco de dados de forma NÃO-INVASIVA
			if mycli.pollService != nil {
				go func() {
					defer func() {
						if r := recover(); r != nil {
							mycli.loggerWrapper.GetLogger(mycli.userID).LogError("[%s] Panic ao salvar voto: %v", mycli.userID, r)
						}
					}()

					pollKey := evt.Message.GetPollUpdateMessage().GetPollCreationMessageKey()
					if pollKey == nil {
						mycli.loggerWrapper.GetLogger(mycli.userID).LogWarn("[%s] PollCreationMessageKey not found", mycli.userID)
						return
					}

					pollInfo := &types.MessageInfo{
						ID: pollKey.GetID(),
						MessageSource: types.MessageSource{
							Chat: evt.Info.Chat, // Usar o chat do evento atual
						},
					}

					// Construir modelo de voto usando helper seguro
					// evt.Info já passou pelo JID swap, então Sender = número real
					pollVote := poll_service.BuildPollVoteFromEvent(
						pollInfo,
						&evt.Info,
						decrypted,
						"", // CompanyID não disponível no MyClient, será vazio
						mycli.inst().Id,
					)

					// Salvar no banco com timeout de segurança
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()

					// Voters that only have a @lid (e.g. votes from our own phone) got the LID
					// digits as "phone". Resolve the real number from the LID store.
					if evt.Info.Sender.Server == types.HiddenUserServer && mycli.WAClient != nil && mycli.WAClient.Store.LIDs != nil {
						if pn, err := mycli.WAClient.Store.LIDs.GetPNForLID(ctx, evt.Info.Sender.ToNonAD()); err == nil && !pn.IsEmpty() {
							pollVote.VoterPhone = pn.User
						}
					}

					if err := mycli.pollService.SavePollVote(ctx, pollVote); err != nil {
						mycli.loggerWrapper.GetLogger(mycli.userID).LogError("[%s] Failed to save poll vote to database: %v", mycli.userID, err)
					} else {
						mycli.loggerWrapper.GetLogger(mycli.userID).LogInfo("[%s] Poll vote saved to database successfully", mycli.userID)
					}
				}()
			}
		}
	}

	if quotedMessage, stanzaID := quotedContext(evt.Message); stanzaID != "" && quotedMessage != nil {
		dataMap["quoted"] = map[string]interface{}{"stanzaID": stanzaID, "quotedMessage": quotedMessage}
		dataMap["isQuoted"] = true
	}

	if len(referral) > 0 {
		dataMap["referral"] = referral
	}

	if mycli.config.WebhookFiles && wantMessage {
		mycli.attachMedia(evt, dataMap)
	}

	isGroup := strings.HasSuffix(evt.Info.Chat.String(), "@g.us")
	if isGroup && wantMessage {
		groupData, err := groupInfos.get(groupInfoKey(mycli.userID, evt.Info.Chat), func() (*types.GroupInfo, error) {
			return mycli.WAClient.GetGroupInfo(context.Background(), evt.Info.Chat)
		})
		if err == nil {
			dataMap["groupData"] = groupData
		}
	}

	delete(dataMap, "RawMessage")

	if message, ok := dataMap["Message"].(map[string]interface{}); ok {
		if imageMessage, ok := message["imageMessage"].(map[string]interface{}); ok {
			delete(imageMessage, "JPEGThumbnail")
			message["imageMessage"] = imageMessage
			dataMap["Message"] = message
		}

		if videoMessage, ok := message["videoMessage"].(map[string]interface{}); ok {
			delete(videoMessage, "JPEGThumbnail")
			message["videoMessage"] = videoMessage
			dataMap["Message"] = message
		}

		if documentMessage, ok := message["documentMessage"].(map[string]interface{}); ok {
			delete(documentMessage, "JPEGThumbnail")
			message["documentMessage"] = documentMessage
			dataMap["Message"] = message
		}
	}

	postMap["data"] = dataMap

	if mycli.config.DatabaseSaveMessages {
		message := message_model.Message{
			InstanceID: mycli.userID,
			MessageID:  evt.Info.ID,
			Timestamp:  evt.Info.Timestamp.Format("2006-01-02 15:04:05"),
			Status:     "Received",
			Source:     evt.Info.Chat.ToNonAD().User,
			Referral:   referral,
		}

		mycli.persistMessageAsync(message)
	}

	// ===== BUTTON CLICK EVENT DETECTION =====
	// A click is a message of its own (legacy buttons, native flow, template button, list): it
	// also goes out as a separate "ButtonClick" event.
	buttonClickData := buttonClickOf(evt.Message)
	if buttonClickData != nil {
		mycli.loggerWrapper.GetLogger(mycli.userID).LogInfo("[%s] Button click detected (%v): buttonId=%v, buttonText=%v", mycli.userID, buttonClickData["type"], buttonClickData["buttonId"], buttonClickData["buttonText"])
	}

	// Se detectou clique em botão, emite evento separado "ButtonClick"
	if buttonClickData != nil {
		buttonClickMap := map[string]interface{}{
			"event": "ButtonClick",
			"data": map[string]interface{}{
				"buttonId":   buttonClickData["buttonId"],
				"buttonText": buttonClickData["buttonText"],
				"type":       buttonClickData["type"],
				"phone":      dataMap["Sender"],
				"jid":        dataMap["Sender"],
				"pushName":   dataMap["PushName"],
				"messageId":  dataMap["ID"],
				"chat":       dataMap["Chat"],
				"fromMe":     dataMap["FromMe"],
				"timestamp":  evt.Info.Timestamp.Unix(),
				"extraData":  buttonClickData,
			},
			"instanceId":   mycli.userID,
			"instanceName": mycli.inst().Name,
		}
		mycli.config.AddInstanceToken(buttonClickMap, mycli.token)

		buttonClickJSON, err := json.Marshal(buttonClickMap)
		if err == nil {
			buttonClickQueue := strings.ToLower(fmt.Sprintf("%s.buttonclick", userID))
			go mycli.service.CallWebhook(mycli.inst(), buttonClickQueue, buttonClickJSON)
			if mycli.config.AmqpGlobalEnabled || mycli.config.NatsGlobalEnabled {
				go mycli.service.SendToGlobalQueues("ButtonClick", buttonClickJSON, mycli.userID)
			}
			mycli.loggerWrapper.GetLogger(mycli.userID).LogInfo("[%s] ===== BUTTON CLICK EVENT DISPATCHED ===== Type: %s, ButtonId: %s", mycli.userID, buttonClickData["type"], buttonClickData["buttonId"])
		}
	}

	mycli.loggerWrapper.GetLogger(mycli.userID).LogInfo("[%s] ===== MESSAGE PROCESSING COMPLETED ===== ID: %s, From: %s, Type: %s, Webhook: %v", mycli.userID, evt.Info.ID, evt.Info.Chat.String(), evt.Info.Type, true)
	return true, eventChat
}
