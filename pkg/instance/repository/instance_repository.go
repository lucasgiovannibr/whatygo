package instance_repository

import (
	"fmt"

	"github.com/gomessguii/logger"
	"github.com/google/uuid"
	instance_model "github.com/lucasgiovannibr/whatygo/pkg/instance/model"
	"gorm.io/gorm"

	label_model "github.com/lucasgiovannibr/whatygo/pkg/label/model"

	message_model "github.com/lucasgiovannibr/whatygo/pkg/message/model"
)

type InstanceRepository interface {
	Create(instance instance_model.Instance) (*instance_model.Instance, error)
	GetInstanceByID(instanceId string) (*instance_model.Instance, error)
	GetConnectedInstanceByID(instanceId string) (*instance_model.Instance, error)
	GetInstanceByToken(token string) (*instance_model.Instance, error)
	GetInstanceByName(name string) (*instance_model.Instance, error)
	MarkPaired(instanceId string, jid string) error
	UpdateConnected(userId string, status bool, disconnectReason string) error
	UpdateQrcode(userId string, qr string) error
	UpdateProxy(userId string, proxy string) error
	UpdateJid(userId string, jid string) error
	UpdateConnectSettings(instanceId string, updates map[string]interface{}) error
	GetAllConnectedInstances() ([]*instance_model.Instance, error)
	GetAllConnectedInstancesByClientName(clientName string) ([]*instance_model.Instance, error)
	GetAll(clientName string) ([]*instance_model.Instance, error)
	Delete(instanceId string) error
	GetAdvancedSettings(instanceId string) (*instance_model.AdvancedSettings, error)
	UpdateAdvancedSettings(instanceId string, settings *instance_model.AdvancedSettings) error
}

type instanceRepository struct {
	db *gorm.DB
}

func (i *instanceRepository) Create(instance instance_model.Instance) (*instance_model.Instance, error) {
	if err := i.db.Create(&instance).Error; err != nil {
		return nil, err
	}
	return &instance, nil
}

func (i *instanceRepository) GetInstanceByToken(token string) (*instance_model.Instance, error) {
	var instance instance_model.Instance
	err := i.db.Where("token = ?", token).First(&instance).Error
	if err != nil {
		return nil, err
	}

	return &instance, nil
}

func (i *instanceRepository) GetInstanceByName(name string) (*instance_model.Instance, error) {
	var instance instance_model.Instance
	err := i.db.Where("name = ?", name).First(&instance).Error
	if err != nil {
		return nil, err
	}

	return &instance, nil
}

func (i *instanceRepository) GetInstanceByID(instanceId string) (*instance_model.Instance, error) {
	// Valida o formato do UUID
	if _, err := uuid.Parse(instanceId); err != nil {
		return nil, fmt.Errorf("invalid UUID format: %v", err)
	}

	var instance instance_model.Instance
	err := i.db.Where("id = ?", instanceId).First(&instance).Error
	if err != nil {
		return nil, err
	}

	return &instance, nil
}

func (i *instanceRepository) GetConnectedInstanceByID(instanceId string) (*instance_model.Instance, error) {
	var instance instance_model.Instance
	err := i.db.Where("id = ? AND connected = ?", instanceId, true).First(&instance).Error
	if err != nil {
		return nil, err
	}

	return &instance, nil
}

// MarkPaired records a completed pairing: one statement that touches only the columns it
// owns. It replaces Update, which saved the whole row it had read earlier and so wrote back
// stale values of everything else (a QR code or connection state changed in between).
func (i *instanceRepository) MarkPaired(instanceId string, jid string) error {
	err := i.db.Model(&instance_model.Instance{}).Where("id = ?", instanceId).Updates(map[string]interface{}{
		"qrcode":            "",
		"connected":         true,
		"disconnect_reason": "",
		"jid":               jid,
	}).Error
	if err != nil {
		logger.LogError("Error marking instance as paired in DB: %v", err)
	}
	return err
}

// UpdateConnected is a single UPDATE (it used to be two chained ones: two round trips, and
// a window where connected and disconnect_reason disagreed).
func (i *instanceRepository) UpdateConnected(userId string, status bool, disconnectReason string) error {
	return i.db.Model(&instance_model.Instance{}).Where("id = ?", userId).Updates(map[string]interface{}{
		"connected":         status,
		"disconnect_reason": disconnectReason,
	}).Error
}

func (i *instanceRepository) UpdateQrcode(userId string, qr string) error {
	return i.db.Model(&instance_model.Instance{}).Where("id = ?", userId).Update("qrcode", qr).Error
}

func (i *instanceRepository) UpdateProxy(userId string, proxy string) error {
	return i.db.Model(&instance_model.Instance{}).Where("id = ?", userId).Update("proxy", proxy).Error
}

func (i *instanceRepository) UpdateJid(userId string, jid string) error {
	return i.db.Model(&instance_model.Instance{}).Where("id = ?", userId).Update("jid", jid).Error
}

func (i *instanceRepository) UpdateConnectSettings(instanceId string, updates map[string]interface{}) error {
	if len(updates) == 0 {
		return nil
	}
	err := i.db.Model(&instance_model.Instance{}).Where("id = ?", instanceId).Updates(updates).Error
	if err != nil {
		logger.LogError("Error updating connect settings in DB: %v", err)
	}
	return err
}

// DisconnectedByAPIReason is the disconnect_reason written by POST /instance/disconnect.
// An instance that carries it is not started again by the next request that needs a client,
// only by an explicit connect.
const DisconnectedByAPIReason = "Disconnected by API"

// ReconnectingReason is the disconnect_reason written while an instance is being
// restarted by ReconnectClient.
const ReconnectingReason = "Reconnecting"

// startupRestoreCondition selects the instances CONNECT_ON_STARTUP must bring back:
// the ones marked connected, plus the ones caught mid-reconnect. ReconnectClient
// writes connected=false before restarting, so a process restart inside that
// window (or a crash, e.g. from a fatal error) used to lose the instance for good,
// leaving it offline until someone called /instance/connect. Instances that were
// deliberately disconnected or logged out carry a different reason and stay off.
const startupRestoreCondition = "connected = ? OR disconnect_reason = ?"

func (i *instanceRepository) GetAllConnectedInstances() ([]*instance_model.Instance, error) {
	var instances []*instance_model.Instance
	err := i.db.Where(startupRestoreCondition, true, ReconnectingReason).Find(&instances).Error
	if err != nil {
		return nil, err
	}

	return instances, nil
}

func (i *instanceRepository) GetAllConnectedInstancesByClientName(clientName string) ([]*instance_model.Instance, error) {
	var instances []*instance_model.Instance
	err := i.db.Where("("+startupRestoreCondition+") AND client_name = ?", true, ReconnectingReason, clientName).Find(&instances).Error
	if err != nil {
		return nil, err
	}

	return instances, nil
}

func (i *instanceRepository) GetAll(clientName string) ([]*instance_model.Instance, error) {
	var instances []*instance_model.Instance
	err := i.db.Where("client_name = ?", clientName).Find(&instances).Error
	if err != nil {
		return nil, err
	}

	return instances, nil
}

func (i *instanceRepository) Delete(instanceId string) error {
	return i.db.Transaction(func(tx *gorm.DB) error {
		// Deleta todas as labels associadas à instância
		if err := tx.Where("instance_id = ?", instanceId).Delete(&label_model.Label{}).Error; err != nil {
			return fmt.Errorf("erro ao deletar labels: %v", err)
		}

		// Deleta todas as mensagens associadas à instância
		if err := tx.Where("instance_id = ?", instanceId).Delete(&message_model.Message{}).Error; err != nil {
			return fmt.Errorf("erro ao deletar mensagens: %v", err)
		}

		// Deleta a instância
		if err := tx.Where("id = ?", instanceId).Delete(&instance_model.Instance{}).Error; err != nil {
			return fmt.Errorf("erro ao deletar instância: %v", err)
		}

		return nil
	})
}

func (i *instanceRepository) GetAdvancedSettings(instanceId string) (*instance_model.AdvancedSettings, error) {
	// Valida o formato do UUID
	if _, err := uuid.Parse(instanceId); err != nil {
		return nil, fmt.Errorf("invalid UUID format: %v", err)
	}

	var instance instance_model.Instance
	err := i.db.Select("always_online, reject_call, msg_reject_call, read_messages, ignore_groups, ignore_status, calls_enabled").
		Where("id = ?", instanceId).First(&instance).Error
	if err != nil {
		return nil, err
	}

	settings := &instance_model.AdvancedSettings{
		AlwaysOnline:  instance_model.BoolPtr(instance.AlwaysOnline),
		RejectCall:    instance_model.BoolPtr(instance.RejectCall),
		MsgRejectCall: &instance.MsgRejectCall,
		ReadMessages:  instance_model.BoolPtr(instance.ReadMessages),
		IgnoreGroups:  instance_model.BoolPtr(instance.IgnoreGroups),
		IgnoreStatus:  instance_model.BoolPtr(instance.IgnoreStatus),
		CallsEnabled:  instance_model.BoolPtr(instance.CallsEnabled),
	}

	return settings, nil
}

func (i *instanceRepository) UpdateAdvancedSettings(instanceId string, settings *instance_model.AdvancedSettings) error {
	// Valida o formato do UUID
	if _, err := uuid.Parse(instanceId); err != nil {
		return fmt.Errorf("invalid UUID format: %v", err)
	}

	updates := buildAdvancedSettingsUpdates(settings)
	if len(updates) == 0 {
		return nil
	}

	err := i.db.Model(&instance_model.Instance{}).Where("id = ?", instanceId).Updates(updates).Error
	if err != nil {
		logger.LogError("Error updating advanced settings in DB: %v", err)
		return err
	}

	return nil
}

// buildAdvancedSettingsUpdates only includes fields explicitly provided (*bool != nil).
// MsgRejectCall is written when it is present, even empty: that is how the reject message is cleared.
func buildAdvancedSettingsUpdates(settings *instance_model.AdvancedSettings) map[string]interface{} {
	updates := map[string]interface{}{}
	if settings == nil {
		return updates
	}
	if settings.AlwaysOnline != nil {
		updates["always_online"] = *settings.AlwaysOnline
	}
	if settings.RejectCall != nil {
		updates["reject_call"] = *settings.RejectCall
	}
	if settings.ReadMessages != nil {
		updates["read_messages"] = *settings.ReadMessages
	}
	if settings.IgnoreGroups != nil {
		updates["ignore_groups"] = *settings.IgnoreGroups
	}
	if settings.IgnoreStatus != nil {
		updates["ignore_status"] = *settings.IgnoreStatus
	}
	if settings.CallsEnabled != nil {
		updates["calls_enabled"] = *settings.CallsEnabled
	}
	if settings.MsgRejectCall != nil {
		updates["msg_reject_call"] = *settings.MsgRejectCall
	}
	return updates
}

func NewInstanceRepository(db *gorm.DB) InstanceRepository {
	return &instanceRepository{
		db: db,
	}
}
