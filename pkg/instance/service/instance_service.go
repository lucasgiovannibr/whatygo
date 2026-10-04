package instance_service

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/lucasgiovannibr/whatygo/pkg/apierror"
	"github.com/lucasgiovannibr/whatygo/pkg/safemap"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/lucasgiovannibr/whatygo/pkg/config"
	instance_model "github.com/lucasgiovannibr/whatygo/pkg/instance/model"
	instance_repository "github.com/lucasgiovannibr/whatygo/pkg/instance/repository"
	logger_wrapper "github.com/lucasgiovannibr/whatygo/pkg/logger"
	"github.com/lucasgiovannibr/whatygo/pkg/utils"
	whatsmeow_service "github.com/lucasgiovannibr/whatygo/pkg/whatsmeow/service"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

type InstanceService interface {
	Create(data *CreateStruct) (*instance_model.Instance, error)
	Connect(data *ConnectStruct, instance *instance_model.Instance) (*instance_model.Instance, string, string, error)
	Reconnect(instance *instance_model.Instance) error
	Disconnect(instance *instance_model.Instance) (*instance_model.Instance, error)
	Logout(instance *instance_model.Instance) (*instance_model.Instance, error)
	Status(instance *instance_model.Instance) (*StatusStruct, error)
	GetQr(instance *instance_model.Instance) (*QrcodeStruct, error)
	Pair(data *PairStruct, instance *instance_model.Instance) (*PairReturnStruct, error)
	GetAll() ([]*instance_model.Instance, error)
	Info(instanceId string) (*instance_model.Instance, error)
	Delete(id string) error
	SetProxy(id string, proxyConfig *ProxyConfig) error
	SetProxyFromStruct(id string, data *SetProxyStruct) error
	RemoveProxy(id string) error
	GetProxyStatus(id string) (*ProxyStatus, error)
	GetRuntime(id string) (*RuntimeDiagnostics, error)
	GetRuntimes() (*RuntimesReport, error)
	ForceReconnect(instanceId string, number string) error
	GetInstanceByToken(token string) (*instance_model.Instance, error)
	GetLogs(instanceId string, startDate, endDate time.Time, level string, limit int) ([]logger_wrapper.LogEntry, error)
	GetAdvancedSettings(instanceId string) (*instance_model.AdvancedSettings, error)
	UpdateAdvancedSettings(instanceId string, settings *instance_model.AdvancedSettings) error
	UpdateIntegrations(instanceId string, data *IntegrationsStruct) (*instance_model.Instance, error)
}

type instances struct {
	instanceRepository instance_repository.InstanceRepository
	config             *config.Config
	killChannel        *safemap.Map[chan bool]
	clientPointer      *safemap.Map[*whatsmeow.Client]
	whatsmeowService   whatsmeow_service.WhatsmeowService
	loggerWrapper      *logger_wrapper.LoggerManager
}

type ProxyConfig struct {
	Protocol string `json:"protocol,omitempty"`
	Port     string `json:"port"`
	Password string `json:"password"`
	Username string `json:"username"`
	Host     string `json:"host"`
}

type CreateStruct struct {
	InstanceId       string                           `json:"instanceId"`
	Name             string                           `json:"name"`
	Token            string                           `json:"token"`
	Proxy            *ProxyConfig                     `json:"proxy"`
	AdvancedSettings *instance_model.AdvancedSettings `json:"advancedSettings"`
}

type ConnectStruct struct {
	WebhookUrl      string   `json:"webhookUrl"`
	Subscribe       []string `json:"subscribe"`
	Immediate       bool     `json:"immediate"`
	Phone           string   `json:"phone"`
	RabbitmqEnable  string   `json:"rabbitmqEnable"`
	WebSocketEnable string   `json:"websocketEnable"`
	NatsEnable      string   `json:"natsEnable"`
}

type StatusStruct struct {
	Connected bool
	LoggedIn  bool
	myJid     *types.JID
	Name      string
}

type QrcodeStruct struct {
	Qrcode string `json:"qrcode"`
	Code   string `json:"code"`
	// Passkey ceremony fields. Populated when the account requires a WebAuthn
	// passkey to finish linking (no QR to scan at that point). The manager uses
	// PasskeyStage to switch its UI and PasskeyOpenUrl for the
	// "Abrir WhatsApp Web" button that launches the passkey ceremony.
	PasskeyStage   string `json:"passkeyStage,omitempty"`
	PasskeyOpenURL string `json:"passkeyOpenUrl,omitempty"`
	PasskeyCode    string `json:"passkeyCode,omitempty"`
}

type PairStruct struct {
	Subscribe []string `json:"subscribe"`
	Phone     string   `json:"phone"`
}

type PairReturnStruct struct {
	PairingCode string
}

type SetProxyStruct struct {
	Protocol string `json:"protocol,omitempty"`
	Host     string `json:"host" validate:"required"`
	Port     string `json:"port" validate:"required"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type ForceReconnectStruct struct {
	Number string `json:"number"`
}

func (i *instances) ensureClientConnected(instanceId string) (*whatsmeow.Client, error) {
	return utils.ClientProvider{Clients: i.clientPointer, Starter: i.whatsmeowService}.Ensure(context.Background(), instanceId, i.loggerWrapper.GetLogger(instanceId))
}

func (i instances) Create(data *CreateStruct) (*instance_model.Instance, error) {
	if data.Proxy != nil {
		data.Proxy.Protocol = utils.NormalizeProxyProtocol(data.Proxy.Protocol, data.Proxy.Port)
	}

	proxyJson, err := json.Marshal(data.Proxy)
	if err != nil {
		return nil, err
	}

	findInstance, _ := i.instanceRepository.GetInstanceByName(data.Name)

	if findInstance != nil {
		return nil, apierror.Conflict("instance already exists")
	}

	// The token identifies the instance on every request; the column is unique, and the
	// database would answer a duplicate with a 500.
	if sameToken, _ := i.instanceRepository.GetInstanceByToken(data.Token); sameToken != nil {
		return nil, apierror.Conflict("token already in use by another instance")
	}

	instance := instance_model.Instance{
		Id:         data.InstanceId,
		Name:       data.Name,
		Token:      data.Token,
		OsName:     i.config.OsName,
		Proxy:      string(proxyJson),
		Connected:  false,
		ClientName: i.config.ClientName,
	}

	// Set advanced settings if provided (nil pointers are left as defaults).
	if data.AdvancedSettings != nil {
		if data.AdvancedSettings.AlwaysOnline != nil {
			instance.AlwaysOnline = *data.AdvancedSettings.AlwaysOnline
		}
		if data.AdvancedSettings.RejectCall != nil {
			instance.RejectCall = *data.AdvancedSettings.RejectCall
		}
		if data.AdvancedSettings.MsgRejectCall != nil {
			instance.MsgRejectCall = *data.AdvancedSettings.MsgRejectCall
		}
		if data.AdvancedSettings.ReadMessages != nil {
			instance.ReadMessages = *data.AdvancedSettings.ReadMessages
		}
		if data.AdvancedSettings.IgnoreGroups != nil {
			instance.IgnoreGroups = *data.AdvancedSettings.IgnoreGroups
		}
		if data.AdvancedSettings.IgnoreStatus != nil {
			instance.IgnoreStatus = *data.AdvancedSettings.IgnoreStatus
		}
		if data.AdvancedSettings.CallsEnabled != nil {
			instance.CallsEnabled = *data.AdvancedSettings.CallsEnabled
		}
	}

	createdInstance, err := i.instanceRepository.Create(instance)
	if err != nil {
		return nil, err
	}

	return createdInstance, nil
}

func (i instances) Connect(data *ConnectStruct, instance *instance_model.Instance) (*instance_model.Instance, string, string, error) {
	i.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] Processing subscribe events: %v", instance.Id, data.Subscribe)

	oldEvents := instance.Events
	oldRabbitmq := instance.RabbitmqEnable

	updates := applyConnectSettings(instance, data)

	if instance.Events != oldEvents {
		i.loggerWrapper.GetLogger(instance.Id).LogWarn("[%s] events changed: %q -> %q", instance.Id, oldEvents, instance.Events)
	}
	if instance.RabbitmqEnable != oldRabbitmq {
		i.loggerWrapper.GetLogger(instance.Id).LogWarn("[%s] rabbitmqEnable changed: %q -> %q", instance.Id, oldRabbitmq, instance.RabbitmqEnable)
	}

	if len(updates) > 0 {
		err := i.instanceRepository.UpdateConnectSettings(instance.Id, updates)
		if err != nil {
			i.loggerWrapper.GetLogger(instance.Id).LogError("[%s] Error updating instance: %s", instance.Id, err)
			return nil, "", "", err
		}
	}

	subscribedEvents := splitSubscribedEvents(instance.Events)
	eventString := instance.Events

	// Sincroniza as configurações na instância em execução (se já estiver conectada)
	var isInstanceRunning bool
	err := i.whatsmeowService.UpdateInstanceSettings(instance.Id)
	if err != nil {
		i.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] Instance not in runtime yet, will be updated when connected", instance.Id)
		isInstanceRunning = false
	} else {
		i.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] Instance settings updated successfully in runtime", instance.Id)
		isInstanceRunning = true
	}

	// Se a instância não estiver rodando, inicia uma nova
	if !isInstanceRunning {
		i.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] Starting new client instance", instance.Id)

		clientData := &whatsmeow_service.ClientData{
			Instance:      instance,
			Subscriptions: subscribedEvents,
			Phone:         data.Phone,
			IsProxy:       false,
		}

		if instance.Proxy != "" || i.config.ProxyHost != "" {
			proxyConfig, err := parseProxyConfig(instance.Proxy)
			if err != nil {
				i.loggerWrapper.GetLogger(instance.Id).LogError("[%s] error unmarshalling proxy config: %v", instance.Id, err)
				return nil, "", "", err
			}

			if proxyConfig.Host != "" || i.config.ProxyHost != "" {
				clientData.IsProxy = true
			}
		}

		go i.whatsmeowService.StartClient(clientData)
	} else {
		i.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] Instance already running, settings updated without restarting client", instance.Id)
	}

	return instance, instance.Jid, eventString, nil
}

func (i instances) Reconnect(instance *instance_model.Instance) error {
	_, err := i.ensureClientConnected(instance.Id)
	if err != nil {
		return err
	}

	return i.whatsmeowService.ReconnectClient(instance.Id)
}

func (i instances) Disconnect(instance *instance_model.Instance) (*instance_model.Instance, error) {
	client, err := i.ensureClientConnected(instance.Id)
	if err != nil {
		return instance, err
	}

	if client.IsConnected() {
		if client.IsLoggedIn() {
			i.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] Disconnection successful", instance.Id)
			// Stop the runtime for good. Sending `true` on the kill channel (what this used
			// to do) is the "restart" signal: the supervisor reported a LoggedOut event that
			// never happened and started the client again, so a disconnect reconnected itself.
			if err := i.whatsmeowService.ClearInstanceCache(instance.Id, instance.Token); err != nil {
				return instance, err
			}

			// Do not clear instance.Events on disconnect (PR #187). Wiping the
			// subscriptions left the instance with an empty events string after the
			// next connect, and CallWebhook then dropped every webhook
			// (strings.Split("", ",") yields [""], which is not an event type).
			instance.Connected = false
			instance.DisconnectReason = instance_repository.DisconnectedByAPIReason
			if err := i.instanceRepository.UpdateConnected(instance.Id, false, instance.DisconnectReason); err != nil {
				return instance, err
			}

			return instance, nil
		}
	}

	i.loggerWrapper.GetLogger(instance.Id).LogWarn("[%s] Ignoring disconnect as it was not connected", instance.Id)
	return instance, nil
}

func (i instances) Logout(instance *instance_model.Instance) (*instance_model.Instance, error) {
	client, err := i.ensureClientConnected(instance.Id)
	if err != nil {
		return instance, err
	}

	if client.IsLoggedIn() && client.IsConnected() {
		err := client.Logout(context.Background())
		if err != nil {
			return instance, err
		}

		instance.Connected = false
		err = i.instanceRepository.UpdateConnected(instance.Id, false, instance.DisconnectReason)
		if err != nil {
			return instance, err
		}

		select {
		case i.killChannel.Get(instance.Id) <- true:
		case <-time.After(5 * time.Second):
		}

		i.clientPointer.Delete(instance.Id)
		i.killChannel.Delete(instance.Id)

		i.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] Logout successful", instance.Id)
		return instance, nil
	}

	if client.IsConnected() {
		client.Disconnect()

		select {
		case i.killChannel.Get(instance.Id) <- true:
		case <-time.After(5 * time.Second):
		}

		i.clientPointer.Delete(instance.Id)
		i.killChannel.Delete(instance.Id)

		i.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] Disconnection successful", instance.Id)
		return instance, nil
	}

	i.loggerWrapper.GetLogger(instance.Id).LogWarn("[%s] Ignoring logout as it was not connected", instance.Id)
	return instance, fmt.Errorf("ignoring logout as it was not connected")
}

func (i instances) Status(instance *instance_model.Instance) (*StatusStruct, error) {
	client := i.clientPointer.Get(instance.Id)

	if client == nil {
		return &StatusStruct{
			Connected: false,
			LoggedIn:  false,
		}, nil
	}

	isConnected := client.IsConnected()
	isLoggedIn := client.IsLoggedIn()

	var myJid *types.JID
	var name string
	if isLoggedIn {
		myJid = client.Store.ID
		name = client.Store.PushName
	}

	return &StatusStruct{
		Connected: isConnected,
		LoggedIn:  isLoggedIn,
		myJid:     myJid,
		Name:      name,
	}, nil
}

// qrWaitTimeout is the longest GetQr waits for the client to produce a QR code.
const qrWaitTimeout = 6 * time.Second

// qrOrLoginReady reports whether GetQr has something to answer with: the client logged in,
// a QR code is stored, or a passkey ceremony is running.
func (i instances) qrOrLoginReady(instanceID string) bool {
	if c := i.clientPointer.Get(instanceID); c != nil && c.IsLoggedIn() {
		return true
	}
	if store := i.whatsmeowService.PasskeyCeremonyStore(); store != nil {
		if _, _, ok := store.StateByInstance(instanceID); ok {
			return true
		}
	}
	inst, err := i.instanceRepository.GetInstanceByID(instanceID)
	return err == nil && inst.Qrcode != ""
}

func (i instances) GetQr(instance *instance_model.Instance) (*QrcodeStruct, error) {
	logger := i.loggerWrapper.GetLogger(instance.Id)
	client := i.clientPointer.Get(instance.Id)

	// Já logado: só informar. Chamar StartInstance numa sessão ativa troca o
	// killChannel e pode derrubar a conexão (ex.: polling do frontend logo após o
	// PairSuccess) — PR #149.
	if client != nil && client.IsLoggedIn() {
		logger.LogInfo("[%s] Client is already logged in — returning 'session already logged in' (StartInstance skipped)", instance.Id)
		return nil, apierror.Conflict("session already logged in")
	}

	// Se não há cliente, precisamos iniciar um novo cliente
	if client == nil {
		logger.LogInfo("[%s] No client found, starting new instance for QR code", instance.Id)

		// Iniciar nova instância para gerar QR code
		err := i.whatsmeowService.StartInstance(instance.Id)
		if err != nil {
			logger.LogError("[%s] Failed to start instance: %v", instance.Id, err)
			return nil, fmt.Errorf("failed to start instance: %w", err)
		}

		// Wait for what this call needs (a QR code, a passkey ceremony, or a session that
		// turned out to be logged in) instead of a fixed 3 s.
		logger.LogInfo("[%s] Waiting for QR code generation...", instance.Id)
		utils.WaitUntilEvery(qrWaitTimeout, 250*time.Millisecond, func() bool { return i.qrOrLoginReady(instance.Id) })

		// Verificar novamente se há cliente
		client = i.clientPointer.Get(instance.Id)
		if client != nil && client.IsLoggedIn() {
			return nil, apierror.Conflict("session already logged in")
		}
	} else if !client.IsConnected() {
		// Se o cliente existe mas não está conectado, pode estar aguardando QR code
		logger.LogInfo("[%s] Client exists but not connected, checking for existing QR code", instance.Id)
	}

	// Buscar instância atualizada do banco para pegar o QR code mais recente
	instance, err := i.instanceRepository.GetInstanceByID(instance.Id)
	if err != nil {
		return nil, err
	}

	// If a passkey ceremony is in progress, there is no QR to scan — return the
	// passkey stage + the #wapk openUrl so the manager can render the
	// "Abrir WhatsApp Web" button. Checked before the empty-QR branch because
	// during a passkey ceremony instance.Qrcode is empty.
	if store := i.whatsmeowService.PasskeyCeremonyStore(); store != nil {
		if token, state, ok := store.StateByInstance(instance.Id); ok {
			logger.LogInfo("[%s] Passkey ceremony active (stage=%s) — returning passkey info instead of QR", instance.Id, state.Stage)
			resp := &QrcodeStruct{
				PasskeyStage:   state.Stage,
				PasskeyCode:    state.Code,
				PasskeyOpenURL: buildPasskeyOpenURL(token),
			}
			// Keep returning the latest QR when there is one, so integrations that
			// render their own QR screen do not lose it while the passkey fields are
			// present (#148). During a ceremony it is normally empty.
			if parts := strings.Split(instance.Qrcode, "|"); len(parts) >= 2 {
				resp.Qrcode, resp.Code = parts[0], parts[1]
			}
			return resp, nil
		}
	}

	code := instance.Qrcode
	if code == "" {
		// Se não há QR code ainda, aguardar um pouco mais e tentar novamente
		logger.LogInfo("[%s] No QR code available yet, waiting a bit more...", instance.Id)
		utils.WaitUntilEvery(qrWaitTimeout, 250*time.Millisecond, func() bool { return i.qrOrLoginReady(instance.Id) })

		instance, err = i.instanceRepository.GetInstanceByID(instance.Id)
		if err != nil {
			return nil, err
		}

		code = instance.Qrcode
		if code == "" {
			return nil, fmt.Errorf("no QR code available. Please wait a moment and try again")
		}
	}

	parts := strings.Split(code, "|")
	if len(parts) < 2 {
		return nil, apierror.Invalid("invalid QR code format")
	}

	qr := &QrcodeStruct{
		Qrcode: parts[0],
		Code:   parts[1],
	}

	return qr, nil
}

// buildPasskeyOpenURL builds the URL the manager opens to start the passkey
// ceremony: https://web.whatsapp.com/#wapk=<base64url({t:token,b:publicBase})>.
// publicBase must be the PUBLICLY reachable API base the browser can hit; set it
// via PASSKEY_PUBLIC_URL. Kept in sync with the event handler in whatsmeow.go.
func buildPasskeyOpenURL(token string) string {
	publicBase := os.Getenv("PASSKEY_PUBLIC_URL")
	if publicBase == "" {
		publicBase = "<SET_PASSKEY_PUBLIC_URL>"
	}
	payload := fmt.Sprintf(`{"t":%q,"b":%q}`, token, publicBase)
	wapk := base64.RawURLEncoding.EncodeToString([]byte(payload))
	return "https://web.whatsapp.com/#wapk=" + wapk
}

func (i instances) Pair(data *PairStruct, instance *instance_model.Instance) (*PairReturnStruct, error) {
	logger := i.loggerWrapper.GetLogger(instance.Id)
	client := i.clientPointer.Get(instance.Id)

	if client == nil || !client.IsConnected() {
		if client != nil && client.IsLoggedIn() {
			return nil, apierror.Conflict("instance is already authenticated")
		}
		logger.LogInfo("[%s] No active connection, starting instance for phone pairing", instance.Id)
		if err := i.whatsmeowService.StartInstance(instance.Id); err != nil {
			logger.LogError("[%s] Failed to start instance for pairing: %v", instance.Id, err)
			return nil, fmt.Errorf("failed to start instance: %w", err)
		}
		// Wait for the WA websocket connection and initial QR generation to establish.
		// PairPhone must be called after the QR event is received per whatsmeow docs. It
		// waited a fixed 3 s; the QR is stored as soon as it arrives, so wait for that.
		utils.WaitUntilEvery(qrWaitTimeout, 250*time.Millisecond, func() bool {
			c := i.clientPointer.Get(instance.Id)
			return c != nil && c.IsConnected() && i.qrOrLoginReady(instance.Id)
		})
		client = i.clientPointer.Get(instance.Id)
		if client == nil {
			return nil, fmt.Errorf("failed to initialize client for pairing")
		}
	}

	if client.IsLoggedIn() {
		return nil, apierror.Conflict("instance is already authenticated")
	}

	code, err := client.PairPhone(context.Background(), data.Phone, true, whatsmeow.PairClientChrome, "Chrome (Linux)")
	if err != nil {
		logger.LogError("[%s] PairPhone failed: %v", instance.Id, err)
		return nil, fmt.Errorf("pairing failed: %w", err)
	}

	return &PairReturnStruct{PairingCode: code}, nil
}

func (i instances) GetAll() ([]*instance_model.Instance, error) {
	instances, err := i.instanceRepository.GetAll(i.config.ClientName)
	if err != nil {
		return nil, err
	}

	for _, instance := range instances {
		if client := i.clientPointer.Get(instance.Id); client != nil {
			instance.Connected = client.IsLoggedIn()
		} else {
			instance.Connected = false
		}

		instance.Proxy = ""
	}

	return instances, nil
}

func (i instances) Info(instanceId string) (*instance_model.Instance, error) {
	instance, err := i.instanceRepository.GetInstanceByID(instanceId)
	if err != nil {
		return nil, err
	}

	// Atualiza o status connected com base no estado real do cliente
	if client := i.clientPointer.Get(instance.Id); client != nil {
		instance.Connected = client.IsLoggedIn()
	} else {
		instance.Connected = false
	}

	instance.Proxy = ""

	return instance, nil
}

func (i instances) Delete(id string) error {
	instance, err := i.instanceRepository.GetInstanceByID(id)
	if err != nil {
		return err
	}

	if i.clientPointer.Get(instance.Id) != nil && i.clientPointer.Get(instance.Id).IsConnected() {
		if i.clientPointer.Get(instance.Id).IsLoggedIn() {
			i.clientPointer.Get(instance.Id).Logout(context.Background())
		}
		i.clientPointer.Get(instance.Id).Disconnect()
	}

	// Limpar todos os recursos da instância antes de deletar
	i.clientPointer.Delete(instance.Id)
	if i.killChannel.Get(instance.Id) != nil {
		close(i.killChannel.Get(instance.Id))
		i.killChannel.Delete(instance.Id)
	}

	// Limpar cache via whatsmeow service
	err = i.whatsmeowService.ClearInstanceCache(instance.Id, instance.Token)
	if err != nil {
		i.loggerWrapper.GetLogger(instance.Id).LogWarn("[%s] Failed to clear instance cache: %v", instance.Id, err)
	}

	// The paired device and the poll votes are not covered by the instance row's delete.
	if err := i.whatsmeowService.PurgeInstanceData(instance.Id, instance.Jid); err != nil {
		i.loggerWrapper.GetLogger(instance.Id).LogWarn("[%s] Failed to purge the instance's stored data: %v", instance.Id, err)
	}

	err = i.instanceRepository.Delete(id)
	if err != nil {
		return err
	}

	// The instance is gone: free its log file descriptor and logger.
	i.loggerWrapper.Release(id)
	if err := i.loggerWrapper.RemoveFiles(id); err != nil {
		i.loggerWrapper.GetLogger(id).LogWarn("[%s] Could not remove the instance's log files: %v", id, err)
	}

	return nil
}

func (i instances) SetProxy(id string, proxyConfig *ProxyConfig) error {
	instance, err := i.instanceRepository.GetInstanceByID(id)
	if err != nil {
		return err
	}

	// Validate proxy configuration
	if proxyConfig == nil {
		return apierror.Invalid("proxy configuration cannot be nil")
	}

	if proxyConfig.Host == "" {
		return apierror.Invalid("proxy host is required")
	}

	if proxyConfig.Port == "" {
		return apierror.Invalid("proxy port is required")
	}

	proxyConfig.Protocol = utils.NormalizeProxyProtocol(proxyConfig.Protocol, proxyConfig.Port)

	// Convert proxy config to JSON
	proxyJSON, err := json.Marshal(proxyConfig)
	if err != nil {
		i.loggerWrapper.GetLogger(id).LogError("[%s] Failed to marshal proxy config: %v", id, err)
		return fmt.Errorf("failed to marshal proxy configuration: %v", err)
	}

	instance.Proxy = string(proxyJSON)

	// Update instance in database
	err = i.instanceRepository.UpdateProxy(id, instance.Proxy)
	if err != nil {
		i.loggerWrapper.GetLogger(id).LogError("[%s] Failed to update instance with proxy: %v", id, err)
		return err
	}

	i.loggerWrapper.GetLogger(id).LogInfo("[%s] Proxy configuration updated: %s://%s:%s", id, proxyConfig.Protocol, proxyConfig.Host, proxyConfig.Port)

	// Reconnect to apply proxy changes
	go i.Reconnect(instance)

	return nil
}

func (i instances) SetProxyFromStruct(id string, data *SetProxyStruct) error {
	if data == nil {
		return apierror.Invalid("proxy data cannot be nil")
	}

	proxyConfig := &ProxyConfig{
		Protocol: data.Protocol,
		Host:     data.Host,
		Port:     data.Port,
		Username: data.Username,
		Password: data.Password,
	}

	return i.SetProxy(id, proxyConfig)
}

// ProxyStatus tells whether an instance's proxy is configured AND actually in use by
// the running client. It never contains credentials.
type ProxyStatus struct {
	Configured bool `json:"configured"`
	// Source is "instance" (POST /instance/proxy), "global" (PROXY_* env) or "none".
	Source   string `json:"source"`
	Protocol string `json:"protocol,omitempty"`
	Host     string `json:"host,omitempty"`
	Port     string `json:"port,omitempty"`
	HasAuth  bool   `json:"hasAuth"`
	// FailClosed mirrors PROXY_FAIL_CLOSED: when true the client never falls back to a direct connection.
	FailClosed           bool       `json:"failClosed"`
	RuntimeEnabled       bool       `json:"runtimeEnabled"`
	FallbackWithoutProxy bool       `json:"fallbackWithoutProxy"`
	LastError            string     `json:"lastError,omitempty"`
	LastAppliedAt        *time.Time `json:"lastAppliedAt,omitempty"`
}

func (i instances) GetProxyStatus(id string) (*ProxyStatus, error) {
	instance, err := i.instanceRepository.GetInstanceByID(id)
	if err != nil {
		return nil, err
	}

	st := &ProxyStatus{Source: "none", FailClosed: i.config.ProxyFailClosed}

	var cfg ProxyConfig
	if instance.Proxy != "" {
		_ = json.Unmarshal([]byte(instance.Proxy), &cfg)
	}
	switch {
	case cfg.Host != "":
		st.Source = "instance"
	case i.config.ProxyHost != "":
		st.Source = "global"
		cfg = ProxyConfig{Protocol: i.config.ProxyProtocol, Host: i.config.ProxyHost, Port: i.config.ProxyPort, Username: i.config.ProxyUsername, Password: i.config.ProxyPassword}
	}
	if st.Source != "none" {
		st.Configured = true
		st.Protocol = utils.NormalizeProxyProtocol(cfg.Protocol, cfg.Port)
		st.Host = cfg.Host
		st.Port = cfg.Port
		st.HasAuth = cfg.Username != "" || cfg.Password != ""
	}

	if rt, ok := i.whatsmeowService.ProxyStatus(id); ok {
		st.RuntimeEnabled = rt.RuntimeEnabled
		st.FallbackWithoutProxy = rt.FallbackWithoutProxy
		st.LastError = rt.LastError
		st.LastAppliedAt = rt.LastAppliedAt
	}

	return st, nil
}

func (i instances) RemoveProxy(id string) error {
	instance, err := i.instanceRepository.GetInstanceByID(id)
	if err != nil {
		return err
	}

	err = i.instanceRepository.UpdateProxy(id, "")
	if err != nil {
		return err
	}

	i.loggerWrapper.GetLogger(id).LogInfo("[%s] Proxy configuration removed", id)

	go i.Reconnect(instance)

	return nil
}

func (i instances) ForceReconnect(instanceId string, number string) error {
	if i.clientPointer.Get(instanceId).IsConnected() && i.clientPointer.Get(instanceId).IsLoggedIn() {
		return apierror.Conflict("client already connected")
	}

	err := i.whatsmeowService.ForceUpdateJid(instanceId, number)
	if err != nil {
		return err
	}

	instance, err := i.instanceRepository.GetInstanceByID(instanceId)
	if err != nil {
		return err
	}

	subscribedEvents := strings.Split(instance.Events, ",")

	clientData := &whatsmeow_service.ClientData{
		Instance:      instance,
		Subscriptions: subscribedEvents,
		Phone:         "",
		IsProxy:       false,
	}

	if instance.Proxy != "" || i.config.ProxyHost != "" {
		proxyConfig, err := parseProxyConfig(instance.Proxy)
		if err != nil {
			i.loggerWrapper.GetLogger(instance.Id).LogError("[%s] error unmarshalling proxy config: %v", instance.Id, err)
			return err
		}

		if proxyConfig.Host != "" || i.config.ProxyHost != "" {
			clientData.IsProxy = true
		}
	}

	if i.clientPointer.Get(instance.Id) != nil {
		client := i.clientPointer.Get(instance.Id)
		client.Disconnect()

		select {
		case i.killChannel.Get(instance.Id) <- true:
		case <-time.After(5 * time.Second):
		}

		i.clientPointer.Delete(instance.Id)
		i.killChannel.Delete(instance.Id)
	}

	go i.whatsmeowService.StartClient(clientData)

	utils.WaitForClient(func() *whatsmeow.Client { return i.clientPointer.Get(instance.Id) }, utils.InstanceStartTimeout)

	if i.clientPointer.Get(instance.Id) != nil {
		if !i.clientPointer.Get(instance.Id).IsConnected() {
			return fmt.Errorf("failed to connect")
		}

		if !i.clientPointer.Get(instance.Id).IsLoggedIn() {
			return fmt.Errorf("failed to login")
		}
	} else {
		return fmt.Errorf("failed to connect")
	}

	return nil
}

func (i instances) GetInstanceByToken(token string) (*instance_model.Instance, error) {
	return i.instanceRepository.GetInstanceByToken(token)
}

func (i instances) GetLogs(instanceId string, startDate, endDate time.Time, level string, limit int) ([]logger_wrapper.LogEntry, error) {
	// Inicializa o slice vazio para garantir que nunca retorne null
	logs := make([]logger_wrapper.LogEntry, 0)

	// Define valores padrão
	if limit <= 0 {
		limit = 100 // Limite padrão de 100 registros
	}

	// Se não foi fornecida data inicial, usa 7 dias atrás
	if startDate.IsZero() {
		startDate = time.Now().AddDate(0, 0, -7)
	}

	// Se não foi fornecida data final, usa data atual
	if endDate.IsZero() {
		endDate = time.Now()
	}

	// Ajusta as datas para início e fim do dia
	startDate = time.Date(startDate.Year(), startDate.Month(), startDate.Day(), 0, 0, 0, 0, time.UTC)
	endDate = time.Date(endDate.Year(), endDate.Month(), endDate.Day(), 23, 59, 59, 999999999, time.UTC)

	// Garante que a data inicial não seja posterior à data final
	if startDate.After(endDate) {
		return logs, fmt.Errorf("data inicial não pode ser posterior à data final")
	}

	// Níveis de log válidos
	validLevels := map[string]bool{
		"INFO":  true,
		"ERROR": true,
		"WARN":  true,
		"DEBUG": true,
	}

	var levelArray []string
	if level == "" {
		// Se nenhum nível foi especificado, usa todos
		levelArray = []string{"INFO", "ERROR", "WARN", "DEBUG"}
	} else {
		// Divide e normaliza os níveis fornecidos
		for _, l := range strings.Split(level, ",") {
			l = strings.TrimSpace(strings.ToUpper(l))
			if !validLevels[l] {
				return logs, fmt.Errorf("nível de log inválido: %s", l)
			}
			levelArray = append(levelArray, l)
		}
	}

	// Lê os logs do arquivo
	logPath := filepath.Join(i.config.LogDirectory, instanceId, "instance.log")
	file, err := os.Open(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return logs, nil // Retorna array vazio se arquivo não existir
		}
		return logs, fmt.Errorf("erro ao abrir arquivo de log: %v", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)

	// Lines can be long (a logged event); the buffer starts small and grows up to 1 MB
	// (it used to allocate the full 1 MB for every request).
	const maxCapacity = 1024 * 1024 // 1MB
	scanner.Buffer(make([]byte, 0, 64*1024), maxCapacity)

	for scanner.Scan() {
		var entry logger_wrapper.LogEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue // Ignora linhas inválidas
		}

		// Ajusta o timestamp da entrada para UTC para comparação correta
		entry.Timestamp = entry.Timestamp.UTC()

		// Aplica os filtros
		if entry.Timestamp.Before(startDate) || entry.Timestamp.After(endDate) {
			continue
		}

		if !slices.Contains(levelArray, entry.Level) {
			continue
		}

		logs = append(logs, entry)

		// Keep only the most recent `limit` entries (the file is chronological). It used
		// to stop at the first `limit` matches, which returned the OLDEST ones of the
		// range, not the latest.
		if len(logs) >= 2*limit {
			logs = append(logs[:0], logs[len(logs)-limit:]...)
		}
	}
	if len(logs) > limit {
		logs = logs[len(logs)-limit:]
	}

	if err := scanner.Err(); err != nil {
		return logs, fmt.Errorf("erro ao ler arquivo de log: %v", err)
	}

	// Ordena os logs por timestamp em ordem decrescente
	sort.Slice(logs, func(i, j int) bool {
		return logs[i].Timestamp.After(logs[j].Timestamp)
	})

	return logs, nil
}

func (i instances) GetAdvancedSettings(instanceId string) (*instance_model.AdvancedSettings, error) {
	i.loggerWrapper.GetLogger(instanceId).LogInfo("[%s] Getting advanced settings", instanceId)

	settings, err := i.instanceRepository.GetAdvancedSettings(instanceId)
	if err != nil {
		i.loggerWrapper.GetLogger(instanceId).LogError("[%s] Error getting advanced settings: %v", instanceId, err)
		return nil, err
	}

	return settings, nil
}

func (i instances) UpdateAdvancedSettings(instanceId string, settings *instance_model.AdvancedSettings) error {
	i.loggerWrapper.GetLogger(instanceId).LogInfo("[%s] Updating advanced settings", instanceId)

	err := i.instanceRepository.UpdateAdvancedSettings(instanceId, settings)
	if err != nil {
		i.loggerWrapper.GetLogger(instanceId).LogError("[%s] Error updating advanced settings: %v", instanceId, err)
		return err
	}

	// Sincroniza as configurações na instância em execução
	err = i.whatsmeowService.UpdateInstanceAdvancedSettings(instanceId)
	if err != nil {
		i.loggerWrapper.GetLogger(instanceId).LogWarn("[%s] Error syncing advanced settings to runtime: %v", instanceId, err)
		// Não falha a operação, apenas loga o warning
	}

	i.loggerWrapper.GetLogger(instanceId).LogInfo("[%s] Advanced settings updated successfully", instanceId)
	return nil
}

// UpdateIntegrations stores webhook, events and producer switches without starting the
// instance. If it is already running, the new settings are applied to it right away.
func (i instances) UpdateIntegrations(instanceId string, data *IntegrationsStruct) (*instance_model.Instance, error) {
	logger := i.loggerWrapper.GetLogger(instanceId)

	instance, err := i.instanceRepository.GetInstanceByID(instanceId)
	if err != nil {
		return nil, err
	}

	updates := applyConnectSettings(instance, &ConnectStruct{
		WebhookUrl:      data.WebhookUrl,
		Subscribe:       data.Subscribe,
		RabbitmqEnable:  data.RabbitmqEnable,
		WebSocketEnable: data.WebSocketEnable,
		NatsEnable:      data.NatsEnable,
	})

	if len(updates) > 0 {
		if err := i.instanceRepository.UpdateConnectSettings(instanceId, updates); err != nil {
			logger.LogError("[%s] Error updating integrations: %v", instanceId, err)
			return nil, err
		}
	}

	// Unlike Connect, a missing runtime is fine here: the stored settings are picked up
	// the next time the instance starts.
	if err := i.whatsmeowService.UpdateInstanceSettings(instanceId); err != nil {
		logger.LogInfo("[%s] Integrations stored; instance not running, nothing to apply", instanceId)
	}

	return instance, nil
}

func NewInstanceService(
	instanceRepository instance_repository.InstanceRepository,
	killChannel *safemap.Map[chan bool],
	clientPointer *safemap.Map[*whatsmeow.Client],
	whatsmeowService whatsmeow_service.WhatsmeowService,
	config *config.Config,
	loggerWrapper *logger_wrapper.LoggerManager,
) InstanceService {
	return &instances{
		instanceRepository: instanceRepository,
		killChannel:        killChannel,
		clientPointer:      clientPointer,
		whatsmeowService:   whatsmeowService,
		config:             config,
		loggerWrapper:      loggerWrapper,
	}
}

// parseProxyConfig reads the proxy JSON stored on an instance. An instance without a
// proxy holds "" (or "null"); that is an empty config, not an error. With a proxy
// configured through the environment (PROXY_HOST...), an instance created before it
// was set has no proxy JSON of its own, and treating "" as malformed made
// connecting it fail with "unexpected end of JSON input".
func parseProxyConfig(raw string) (ProxyConfig, error) {
	var cfg ProxyConfig
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" {
		return cfg, nil
	}
	err := json.Unmarshal([]byte(raw), &cfg)
	return cfg, err
}
