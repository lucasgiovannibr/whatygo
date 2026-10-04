package whatsmeow_service

import (
	"encoding/json"
	"fmt"
	"strings"

	instance_model "github.com/lucasgiovannibr/whatygo/pkg/instance/model"
)

// publishCallEvent sends a call lifecycle event of the call engine (CallReady,
// CallEnded) the way every other event goes out: to the instance's webhook, queues and
// websocket when it subscribed to CALL, and to the global queues.
//
// It is what the call engine calls, possibly from the library's own goroutine, so it
// only does what is needed to hand the event over.
func (w *whatsmeowService) publishCallEvent(instanceID, event string, data map[string]interface{}) {
	log := w.loggerWrapper.GetLogger(instanceID)

	// The running client knows the current subscriptions. A call that ends because the
	// instance was stopped no longer has one, and the database is the next best thing.
	var instance *instance_model.Instance
	if mycli := w.myClientPointer.Get(instanceID); mycli != nil && mycli.inst() != nil {
		instance = mycli.inst()
	} else if w.instanceRepository != nil {
		found, err := w.instanceRepository.GetInstanceByID(instanceID)
		if err != nil {
			log.LogWarn("[%s] Not publishing %s: instance not found: %v", instanceID, event, err)
			return
		}
		instance = found
	} else {
		return
	}

	payload := map[string]interface{}{
		"event":        event,
		"data":         data,
		"instanceId":   instance.Id,
		"instanceName": instance.Name,
	}
	w.config.AddInstanceToken(payload, instance.Token)
	values, err := json.Marshal(payload)
	if err != nil {
		log.LogError("[%s] Failed to marshal %s: %v", instanceID, event, err)
		return
	}

	queueName := strings.ToLower(fmt.Sprintf("%s.%s", instanceID, event))
	log.LogInfo("[%s] Publishing call event %s (call %v)", instanceID, event, data["callId"])
	go w.CallWebhook(instance, queueName, values)

	if w.config.AmqpGlobalEnabled || w.config.NatsGlobalEnabled {
		go w.SendToGlobalQueues(event, values, instanceID)
	}
}
