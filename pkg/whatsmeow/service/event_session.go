package whatsmeow_service

import (
	"encoding/json"

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

	// The event goes out once, from handleEvent, like every other (this handler used to dispatch
	// it itself as well, so a webhook received every LoggedOut twice).
	// The device is gone from the phone's list and from the store: the instance has none now.
	if err := mycli.instanceRepository.UpdateJid(mycli.userID, ""); err != nil {
		mycli.loggerWrapper.GetLogger(mycli.userID).LogError("[%s] Error clearing the jid: %v", mycli.userID, err)
	}

	// Stop the runtime for good, after the event is out. The kill channel used to be sent
	// `true` ("restart"), which started a new client for the account that had just been logged
	// out, with a new QR code, and kept the instance in QR cycles until someone noticed; it
	// also held this event handler for up to 10 s waiting for the supervisor. Connecting the
	// instance again (or opening its QR code) starts it.
	_ = mycli.service.ClearInstanceCache(mycli.userID, mycli.token)
	return true, eventChat
}
