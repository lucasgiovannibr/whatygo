package whatsmeow_service

import (
	"fmt"
	"strings"
	"time"

	message_model "github.com/lucasgiovannibr/whatygo/pkg/message/model"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

// handleReceipt processes delivery and read receipts.
// It reports whether the event goes out to the webhooks and queues, and the chat it concerns
// (for the subscription filter). A plain return means "nothing to dispatch".
func (mycli *MyClient) handleReceipt(evt *events.Receipt, postMap map[string]interface{}) (dispatch bool, eventChat string) {
	postMap["event"] = "Receipt"
	eventChat = evt.Chat.String()

	// se ignoreGroup for true e o chat for grupo retorna
	if mycli.inst().IgnoreGroups && strings.Contains(evt.Chat.String(), "@g.us") {
		return
	}

	if mycli.config.EventIgnoreGroup && strings.Contains(evt.Chat.String(), "@g.us") {
		return
	}

	mycli.loggerWrapper.GetLogger(mycli.userID).LogInfo("[%s] Receipt received with ID: %s from %s with type %s", mycli.userID, evt.MessageIDs[0], evt.SourceString(), evt.Type)

	if evt.Type == types.ReceiptTypeRead || evt.Type == types.ReceiptTypeReadSelf {

		mycli.loggerWrapper.GetLogger(mycli.userID).LogInfo("[%s] Message was read by %s", mycli.userID, evt.SourceString())
		if evt.Type == types.ReceiptTypeRead {
			postMap["state"] = "Read"
			for _, v := range evt.MessageIDs {
				messageKey := fmt.Sprintf("%s_%s_%s", mycli.userID, v, "Read")
				if _, found := mycli.processedMessages.Get(messageKey); found {
					mycli.loggerWrapper.GetLogger(mycli.userID).LogInfo("[%s] Message duplicated ignored: %s", mycli.userID, v)
					continue
				}

				mycli.processedMessages.Set(messageKey, true, 30*time.Minute)

				var message message_model.Message

				message.InstanceID = mycli.userID
				message.MessageID = v
				message.Timestamp = evt.Timestamp.Format("2006-01-02 15:04:05")
				message.Status = "Read"
				message.Source = evt.Chat.ToNonAD().User

				if mycli.config.DatabaseSaveMessages {
					mycli.persistMessageAsync(message)
				}
			}
		} else {
			postMap["state"] = "ReadSelf"
		}
	} else if evt.Type == types.ReceiptTypeDelivered {
		postMap["state"] = "Delivered"

		var message message_model.Message

		message.InstanceID = mycli.userID
		message.MessageID = evt.MessageIDs[0]
		message.Timestamp = evt.Timestamp.Format("2006-01-02 15:04:05")
		message.Status = "Delivered"
		message.Source = evt.Chat.ToNonAD().User

		messageKey := fmt.Sprintf("%s_%s_%s", mycli.userID, evt.MessageIDs[0], "Delivered")
		if _, found := mycli.processedMessages.Get(messageKey); found {
			mycli.loggerWrapper.GetLogger(mycli.userID).LogInfo("[%s] Message duplicated ignored: %s", mycli.userID, evt.MessageIDs[0])
			return
		}

		mycli.processedMessages.Set(messageKey, true, 30*time.Minute)

		if mycli.config.DatabaseSaveMessages {
			mycli.persistMessageAsync(message)
		}

		mycli.loggerWrapper.GetLogger(mycli.userID).LogInfo("[%s] Message delivered to %s", mycli.userID, evt.SourceString())
	} else {
		return
	}
	return true, eventChat
}
