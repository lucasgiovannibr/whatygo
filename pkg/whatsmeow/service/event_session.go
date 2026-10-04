package whatsmeow_service

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/lucasgiovannibr/whatygo/pkg/utils"
	"github.com/patrickmn/go-cache"
	"go.mau.fi/whatsmeow/types/events"
)

// handlePairSuccess processes a completed pairing.
// It reports whether the event goes out to the webhooks and queues, and the chat it concerns
// (for the subscription filter). A plain return means "nothing to dispatch".
func (mycli *MyClient) handlePairSuccess(evt *events.PairSuccess, postMap map[string]interface{}) (dispatch bool, eventChat string) {
	postMap["event"] = "PairSuccess"
	mycli.loggerWrapper.GetLogger(mycli.userID).LogInfo("QR Pair Success for user '%s' with JID '%s' - '%s'", mycli.userID, evt.ID.String(), mycli.WAClient.Store.ID.String())

	// A failed lookup used to be logged and then dereferenced (instance is nil).
	instance, err := mycli.instanceRepository.GetInstanceByID(mycli.userID)
	if err != nil {
		mycli.loggerWrapper.GetLogger(mycli.userID).LogError("[%s] Error getting instance: %s", mycli.userID, err)
	} else {
		instance.Qrcode = ""
		instance.Connected = true
		instance.DisconnectReason = ""
		instance.Jid = mycli.WAClient.Store.ID.String()

		mycli.loggerWrapper.GetLogger(mycli.userID).LogInfo("[%s] Attempting to update instance in DB (jid %s)", mycli.userID, instance.Jid)
		err = mycli.instanceRepository.MarkPaired(instance.Id, instance.Jid)
		if err != nil {
			mycli.loggerWrapper.GetLogger(mycli.userID).LogError("[%s] Error updating instance: %s", mycli.userID, err)
		} else {
			mycli.loggerWrapper.GetLogger(mycli.userID).LogInfo("[%s] Instance successfully updated", mycli.userID)
		}
	}

	myUserInfo, found := mycli.userInfoCache.Get(mycli.token)

	if !found {
		mycli.loggerWrapper.GetLogger(mycli.userID).LogWarn("[%s] No user info cached on pairing?", mycli.userID)
	} else {
		txtid := myUserInfo.(Values).Get("Id")
		token := myUserInfo.(Values).Get("Token")

		updatedUserInfo := utils.UpdateUserInfo(myUserInfo, "Jid", evt.ID.String())

		mycli.userInfoCache.Set(token, updatedUserInfo, cache.NoExpiration)
		mycli.loggerWrapper.GetLogger(mycli.userID).LogInfo("[%s] User information set for user '%s'", mycli.userID, txtid)
	}

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

	dataMap := postMap["data"].(map[string]interface{})

	dataMap["status"] = "open"
	dataMap["jid"] = mycli.WAClient.Store.ID.String()

	if mycli.WAClient.Store.PushName != "" {
		dataMap["pushName"] = mycli.WAClient.Store.PushName
	}

	postMap["data"] = dataMap

	// Pairing succeeded — tear down any pending passkey ceremony for this instance.
	mycli.passkeyCeremony.Clear(mycli.userID)
	return true, eventChat
}

// handleLoggedOut processes the account being logged out from the phone.
// It reports whether the event goes out to the webhooks and queues, and the chat it concerns
// (for the subscription filter). A plain return means "nothing to dispatch".
func (mycli *MyClient) handleLoggedOut(evt *events.LoggedOut, postMap map[string]interface{}) (dispatch bool, eventChat string) {
	postMap["event"] = "LoggedOut"
	mycli.loggerWrapper.GetLogger(mycli.userID).LogInfo("[%s] Logged out for reason %s", mycli.userID, evt.Reason.String())

	// Limpar cache de userInfo para esta instância
	mycli.userInfoCache.Delete(mycli.inst().Token)
	mycli.loggerWrapper.GetLogger(mycli.userID).LogInfo("[%s] UserInfo cache cleared", mycli.userID)

	mycli.inst().DisconnectReason = evt.Reason.String()
	mycli.inst().Connected = false
	err := mycli.instanceRepository.UpdateConnected(mycli.inst().Id, mycli.inst().Connected, mycli.inst().DisconnectReason)
	if err != nil {
		mycli.loggerWrapper.GetLogger(mycli.userID).LogError("[%s] Error updating instance: %s", mycli.inst().Id, err)
	}

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

	dataMap := postMap["data"].(map[string]interface{})

	dataMap["reason"] = evt.Reason.String()

	// Enviar evento LoggedOut para webhook/RabbitMQ ANTES de matar o canal
	mycli.config.AddInstanceToken(postMap, mycli.inst().Token)
	postMap["instanceId"] = mycli.userID
	postMap["instanceName"] = mycli.inst().Name

	values, err := json.Marshal(postMap)
	if err != nil {
		mycli.loggerWrapper.GetLogger(mycli.userID).LogError("[%s] Failed to marshal JSON for LoggedOut event", mycli.userID)
	} else {
		var queueName string
		if _, ok := postMap["event"]; ok {
			queueName = strings.ToLower(fmt.Sprintf("%s.%s", mycli.userID, postMap["event"]))
		}

		mycli.loggerWrapper.GetLogger(mycli.userID).LogInfo("[%s] ===== DISPATCHING LOGGEDOUT EVENT ===== Queue: %s", mycli.userID, queueName)

		// Enviar para webhook/RabbitMQ
		go mycli.service.CallWebhook(mycli.inst(), queueName, values)

		if mycli.config.AmqpGlobalEnabled || mycli.config.NatsGlobalEnabled {
			mycli.loggerWrapper.GetLogger(mycli.userID).LogInfo("[%s] Sending LoggedOut to global queues - AMQP: %v, NATS: %v", mycli.userID, mycli.config.AmqpGlobalEnabled, mycli.config.NatsGlobalEnabled)
			go mycli.service.SendToGlobalQueues(postMap["event"].(string), values, mycli.userID)
		}
	}

	// Agora mata o canal DEPOIS de enviar o evento
	select {
	case mycli.killChannel.Get(mycli.userID) <- true:
	case <-time.After(10 * time.Second):
		mycli.loggerWrapper.GetLogger(mycli.userID).LogWarn("[%s] Kill signal after LoggedOut not received (runtime already ended?)", mycli.userID)
	}
	return true, eventChat
}
