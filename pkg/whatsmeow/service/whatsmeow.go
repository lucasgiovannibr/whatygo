package whatsmeow_service

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"io"
	"math/rand"
	"os"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/image/webp"
	"google.golang.org/protobuf/proto"

	_ "time/tzdata" // the presence schedule needs America/Sao_Paulo even where the system has no zone files

	_ "github.com/lib/pq"
	"github.com/patrickmn/go-cache"
	"github.com/skip2/go-qrcode"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/proto/waCompanionReg"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"

	"github.com/lucasgiovannibr/whatygo/pkg/apierror"
	call_engine "github.com/lucasgiovannibr/whatygo/pkg/call/engine"
	"github.com/lucasgiovannibr/whatygo/pkg/config"
	producer_interfaces "github.com/lucasgiovannibr/whatygo/pkg/events/interfaces"
	instance_model "github.com/lucasgiovannibr/whatygo/pkg/instance/model"
	instance_repository "github.com/lucasgiovannibr/whatygo/pkg/instance/repository"
	"github.com/lucasgiovannibr/whatygo/pkg/internal/event_types"
	label_model "github.com/lucasgiovannibr/whatygo/pkg/label/model"
	label_repository "github.com/lucasgiovannibr/whatygo/pkg/label/repository"
	logger_wrapper "github.com/lucasgiovannibr/whatygo/pkg/logger"
	message_model "github.com/lucasgiovannibr/whatygo/pkg/message/model"
	message_repository "github.com/lucasgiovannibr/whatygo/pkg/message/repository"
	"github.com/lucasgiovannibr/whatygo/pkg/metrics"
	"github.com/lucasgiovannibr/whatygo/pkg/passkey/ceremony"
	poll_service "github.com/lucasgiovannibr/whatygo/pkg/poll/service"
	"github.com/lucasgiovannibr/whatygo/pkg/safemap"
	storage_interfaces "github.com/lucasgiovannibr/whatygo/pkg/storage/interfaces"
	"github.com/lucasgiovannibr/whatygo/pkg/utils"
)

type WhatsmeowService interface {
	StartClient(clientData *ClientData)
	ConnectOnStartup(clientName string)
	// Shutdown disconnects every client and stops anything from (re)starting one; the
	// stored connection state is kept so the instances come back on the next start.
	Shutdown(ctx context.Context)
	StartInstance(instanceId string) error
	// EnableInstanceLock makes the instances this process runs exclusive across replicas
	// (see owner_lock.go). Call it before any instance is started.
	EnableInstanceLock(db *sql.DB)
	// CanAutoStart reports whether a request may start the instance by itself (nil) or why
	// not: an instance disconnected through the API stays off until it is connected again.
	CanAutoStart(instanceId string) error
	ReconnectClient(instanceId string) error
	// RequestReconnect asks, on behalf of a request that found the instance disconnected, for
	// it to be reconnected. It goes through the same backoff as the reconnections the process
	// starts by itself, and joins one that is already scheduled. It reports whether the
	// instance has a runtime that will do it; the caller waits for the connection.
	RequestReconnect(instanceId string) bool
	ClearInstanceCache(instanceId string, token string) error
	PurgeInstanceData(instanceId string, jid string) error
	CallWebhook(instance *instance_model.Instance, queueName string, jsonData []byte)
	// EventWanted reports whether anyone would receive the event for the instance, so the
	// caller can skip building an expensive payload nobody gets.
	EventWanted(instance *instance_model.Instance, eventType, chat string) bool
	SendToGlobalQueues(event string, jsonData []byte, userId string)
	ForceUpdateJid(instanceId string, number string) error
	UpdateInstanceSettings(instanceId string) error
	UpdateInstanceAdvancedSettings(instanceId string) error
	ProxyStatus(instanceId string) (ProxyRuntimeStatus, bool)
	ReachoutTimelock(instanceId string) *ReachoutTimelockStatus
	RuntimeInfo(instanceId string) RuntimeInfo
	RuntimeInfos() []RuntimeInfo
	WebhookStats() *producer_interfaces.WebhookStats
	ChatDisappearingSeconds(instanceId string, chat types.JID) (uint32, bool)
	RememberChatDisappearing(instanceId string, chat types.JID, seconds uint32)
	GetPollService() poll_service.PollService // NOVO: Acesso ao serviço de polls

	// Passkey (WebAuthn) pairing bridge — read by the public ceremony endpoint,
	// written by the whatsmeow event goroutine.
	PasskeyCeremonyStore() *ceremony.Store
	SubmitPasskeyResponse(instanceId string, resp *types.WebAuthnResponse) error
	ConfirmPasskey(instanceId string) error

	// CallEngine is the per-instance WhatsApp call engine registry (calls are opt-in
	// per instance, see Instance.CallsEnabled).
	CallEngine() *call_engine.Manager
}

type clientVersion struct {
	Major int
	Minor int
	Patch int
}

type whatsmeowService struct {
	instanceRepository instance_repository.InstanceRepository
	authDB             *sql.DB
	messageRepository  message_repository.MessageRepository
	labelRepository    label_repository.LabelRepository
	pollService        poll_service.PollService // NOVO: Serviço de enquetes
	config             *config.Config
	killChannel        *safemap.Map[chan bool]
	userInfoCache      *cache.Cache
	clientPointer      *safemap.Map[*whatsmeow.Client]
	myClientPointer    *safemap.Map[*MyClient]
	rabbitmqProducer   producer_interfaces.Producer
	webhookProducer    producer_interfaces.Producer
	websocketProducer  producer_interfaces.Producer
	sqliteDB           *sql.DB
	exPath             string
	mediaStorage       storage_interfaces.MediaStorage
	processedMessages  *cache.Cache
	natsProducer       producer_interfaces.Producer
	loggerWrapper      *logger_wrapper.LoggerManager
	passkeyCeremony    *ceremony.Store
	callEngine         *call_engine.Manager
	// owner decides which replica runs an instance; nil when INSTANCE_LOCK is off.
	owner InstanceLocker
}

// EnableInstanceLock turns on the cross-replica ownership of instances on db.
func (w *whatsmeowService) EnableInstanceLock(db *sql.DB) {
	if db == nil || w.owner != nil {
		return
	}
	w.owner = NewPostgresLocker(db, w.ownershipLost)
	w.loggerWrapper.GetLogger("system").LogInfo("Instance ownership lock enabled (INSTANCE_LOCK=false turns it off)")
}

// ownershipLost stops an instance whose lock was lost and could not be taken back: another
// replica runs it now.
func (w *whatsmeowService) ownershipLost(instanceID string) {
	w.loggerWrapper.GetLogger(instanceID).LogError("[%s] The instance is now owned by another replica: stopping it here", instanceID)
	token := ""
	if inst, err := w.instanceRepository.GetInstanceByID(instanceID); err == nil {
		token = inst.Token
	}
	_ = w.ClearInstanceCache(instanceID, token)
}

type MyClient struct {
	service        WhatsmeowService
	WAClient       *whatsmeow.Client
	eventHandlerID uint32
	userID         string
	token          string
	// instance is the record the running client works with. UpdateInstanceSettings
	// replaces it from another goroutine while the event handler reads it, so it is only
	// accessed through inst() / setInst().
	instance           atomic.Pointer[instance_model.Instance]
	instanceRepository instance_repository.InstanceRepository
	messageRepository  message_repository.MessageRepository
	labelRepository    label_repository.LabelRepository
	pollService        poll_service.PollService // NOVO: Serviço de enquetes
	clientPointer      *safemap.Map[*whatsmeow.Client]
	myClientPointer    *safemap.Map[*MyClient]
	killChannel        *safemap.Map[chan bool]
	userInfoCache      *cache.Cache
	config             *config.Config
	historySyncID      int32
	rabbitmqProducer   producer_interfaces.Producer
	webhookProducer    producer_interfaces.Producer
	websocketProducer  producer_interfaces.Producer
	mediaStorage       storage_interfaces.MediaStorage
	processedMessages  *cache.Cache
	natsProducer       producer_interfaces.Producer
	loggerWrapper      *logger_wrapper.LoggerManager
	// presenceRunning is set while this client's presence scheduler goroutine lives,
	// so switching alwaysOnline on at runtime cannot start a second one.
	presenceRunning atomic.Bool
	// qrcodeCount is written by the QR rotation goroutine and read by diagnostics.
	qrcodeCount atomic.Int32
	// Observability (RuntimeInfo): last event seen, when it was connected, total events.
	lastEventAt   atomic.Int64 // unix nanoseconds
	lastEventType atomic.Value // string
	connectedAt   atomic.Int64 // unix nanoseconds of the last events.Connected
	eventCount    atomic.Uint64
	// State reported by operational events (see operational_events.go).
	reachoutTimelock atomic.Pointer[ReachoutTimelockStatus]
	lastStreamError  atomic.Pointer[StreamErrorInfo]
	clientOutdatedAt atomic.Int64 // unix nanoseconds of the last events.ClientOutdated
	// lastUndecryptReconnect is when an undecryptable message last forced a reconnect.
	lastUndecryptReconnect atomic.Int64 // unix nanoseconds
	passkeyCeremony        *ceremony.Store
	appStateRecoveryMu     sync.Mutex
	appStateRecovery       map[appstate.WAPatchName]appStateRecoveryAttempt
}

// inst is the instance record of the running client (never nil once the client is set up).
func (mycli *MyClient) inst() *instance_model.Instance { return mycli.instance.Load() }

func (mycli *MyClient) setInst(i *instance_model.Instance) { mycli.instance.Store(i) }

type appStateRecoveryAttempt struct {
	fullSyncAt        time.Time
	recoveryRequestAt time.Time
}

const appStateRecoveryCooldown = 15 * time.Minute

// undecryptReconnectCooldown is the minimum time between two reconnects forced by
// undecryptable messages.
const undecryptReconnectCooldown = 5 * time.Minute

// allowUndecryptReconnect reports whether an undecryptable message may force a reconnect
// now, and records it when it may.
func (mycli *MyClient) allowUndecryptReconnect(now time.Time) bool {
	for {
		last := mycli.lastUndecryptReconnect.Load()
		if last != 0 && now.Sub(time.Unix(0, last)) < undecryptReconnectCooldown {
			return false
		}
		if mycli.lastUndecryptReconnect.CompareAndSwap(last, now.UnixNano()) {
			return true
		}
	}
}

func (mycli *MyClient) reserveAppStateRecovery(name appstate.WAPatchName, recoveryRequest bool) bool {
	mycli.appStateRecoveryMu.Lock()
	defer mycli.appStateRecoveryMu.Unlock()

	if mycli.appStateRecovery == nil {
		mycli.appStateRecovery = make(map[appstate.WAPatchName]appStateRecoveryAttempt)
	}

	now := time.Now()
	attempt := mycli.appStateRecovery[name]
	lastAttempt := attempt.fullSyncAt
	if recoveryRequest {
		lastAttempt = attempt.recoveryRequestAt
	}
	if !lastAttempt.IsZero() && now.Sub(lastAttempt) < appStateRecoveryCooldown {
		return false
	}

	if recoveryRequest {
		attempt.recoveryRequestAt = now
	} else {
		attempt.fullSyncAt = now
	}
	mycli.appStateRecovery[name] = attempt
	return true
}

func (mycli *MyClient) handleAppStateSyncError(evt *events.AppStateSyncError) {
	if evt == nil || mycli.WAClient == nil {
		return
	}

	// A failed incremental sync is retried once as a full sync. If the full
	// snapshot also fails verification, ask the primary phone for a recovery
	// snapshot. This follows the recovery sequence documented by whatsmeow and
	// deliberately avoids the fatal recovery notification, which unlinks every
	// companion device and would require a new login/QR.
	recoveryRequest := evt.FullSync
	if !mycli.reserveAppStateRecovery(evt.Name, recoveryRequest) {
		mycli.loggerWrapper.GetLogger(mycli.userID).LogInfo(
			"[%s] App-state recovery already attempted recently for %s (fullSync=%t)",
			mycli.userID, evt.Name, evt.FullSync,
		)
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if !recoveryRequest {
			mycli.loggerWrapper.GetLogger(mycli.userID).LogWarn(
				"[%s] App-state incremental sync failed for %s; starting controlled full sync",
				mycli.userID, evt.Name,
			)
			if err := mycli.WAClient.FetchAppState(ctx, evt.Name, true, false); err != nil {
				mycli.loggerWrapper.GetLogger(mycli.userID).LogWarn(
					"[%s] App-state full sync did not complete for %s; recovery request will be used if the full-sync error event is emitted: %v",
					mycli.userID, evt.Name, err,
				)
			}
			return
		}

		mycli.loggerWrapper.GetLogger(mycli.userID).LogWarn(
			"[%s] App-state full sync failed for %s; requesting recovery snapshot from primary device",
			mycli.userID, evt.Name,
		)
		if _, err := mycli.WAClient.SendPeerMessage(ctx, whatsmeow.BuildAppStateRecoveryRequest(evt.Name)); err != nil {
			mycli.loggerWrapper.GetLogger(mycli.userID).LogError(
				"[%s] Failed to send app-state recovery request for %s: %v",
				mycli.userID, evt.Name, err,
			)
			return
		}
		mycli.loggerWrapper.GetLogger(mycli.userID).LogInfo(
			"[%s] App-state recovery request sent for %s",
			mycli.userID, evt.Name,
		)
	}()
}

func (mycli *MyClient) persistMessageAsync(message message_model.Message) {
	if mycli == nil || mycli.messageRepository == nil {
		return
	}

	// The repository batches the writes of every instance (and never blocks this handler).
	if !mycli.messageRepository.QueueMessage(message) {
		mycli.loggerWrapper.GetLogger(mycli.userID).LogError("[%s] Message %s was not persisted: the write queue is full or closed", mycli.userID, message.MessageID)
	}
}

type ClientData struct {
	Instance      *instance_model.Instance
	Subscriptions []string
	Phone         string
	IsProxy       bool
}

type Values struct {
	m map[string]string
}

func (v Values) Get(key string) string {
	return v.m[key]
}

type UserCollection struct {
	Users map[types.JID]types.UserInfo
}

type ProxyConfig struct {
	Protocol string `json:"protocol,omitempty"`
	Host     string `json:"host"`
	Password string `json:"password"`
	Port     string `json:"port"`
	Username string `json:"username"`
}

// recoverAndLog must be used as `defer recoverAndLog(...)` at the top of code that
// runs in its own goroutine. whatsmeow recovers panics in its event dispatch, but
// goroutines started by this service (webhook fan-out, reconnects, client
// supervisors) are outside that safety net: a panic in any of them killed the
// whole process, and with it every instance.
func recoverAndLog(lw *logger_wrapper.LoggerManager, instanceID, where string) {
	if r := recover(); r != nil {
		lw.GetLogger(instanceID).LogError("[%s] panic recovered in %s: %v\n%s", instanceID, where, r, debug.Stack())
	}
}

// reconnecting holds the instances that currently have a ReconnectClient in
// flight. whatsmeow can deliver two Disconnected events for the same drop within
// the same second; each one used to start its own reconnect, and the two raced
// on the shared maps (nil dereference in ReconnectClient, issue #188) and could
// leave two runtimes for one instance.
var reconnecting sync.Map

func (w whatsmeowService) ReconnectClient(instanceId string) error {
	defer recoverAndLog(w.loggerWrapper, instanceId, "ReconnectClient")
	if _, busy := reconnecting.LoadOrStore(instanceId, struct{}{}); busy {
		w.loggerWrapper.GetLogger(instanceId).LogInfo("[%s] Reconnection already in progress, ignoring duplicate request", instanceId)
		return nil
	}
	defer reconnecting.Delete(instanceId)

	w.loggerWrapper.GetLogger(instanceId).LogInfo("[%s] Starting reconnection process - simulating restart", instanceId)

	// Passo 1: Limpar conexão existente se houver
	if client, exists := w.clientPointer.Lookup(instanceId); exists {
		w.loggerWrapper.GetLogger(instanceId).LogInfo("[%s] Disconnecting existing client", instanceId)

		// Desconectar o cliente WebSocket
		if client.IsConnected() {
			client.Disconnect()
			w.loggerWrapper.GetLogger(instanceId).LogInfo("[%s] WebSocket disconnected", instanceId)
		}

		// Remover event handler se existir
		if mycli, ok := w.myClientPointer.Lookup(instanceId); ok && mycli != nil {
			if mycli.eventHandlerID != 0 {
				client.RemoveEventHandler(mycli.eventHandlerID)
				w.loggerWrapper.GetLogger(instanceId).LogInfo("[%s] Event handler removed", instanceId)
			}
		}
	}

	// Passo 2: Limpar todos os recursos da instância
	w.loggerWrapper.GetLogger(instanceId).LogInfo("[%s] Cleaning up resources", instanceId)

	// Enviar sinal de kill se o canal existir
	if killChan, exists := w.killChannel.Lookup(instanceId); exists {
		select {
		case killChan <- true:
			w.loggerWrapper.GetLogger(instanceId).LogInfo("[%s] Kill signal sent", instanceId)
		default:
			// Canal pode estar bloqueado, continua
		}
	}

	// Remover das estruturas
	w.clientPointer.Delete(instanceId)
	w.myClientPointer.Delete(instanceId)
	w.callEngine.Detach(instanceId)
	w.killChannel.Delete(instanceId)

	// Limpar cache de userInfo para esta instância
	if instance, err := w.instanceRepository.GetInstanceByID(instanceId); err == nil {
		w.userInfoCache.Delete(instance.Token)
		w.loggerWrapper.GetLogger(instanceId).LogInfo("[%s] UserInfo cache cleared", instanceId)
	}

	// Passo 3: Atualizar status no banco
	instance, err := w.instanceRepository.GetInstanceByID(instanceId)
	if err != nil {
		return fmt.Errorf("failed to get instance: %v", err)
	}

	instance.Connected = false
	instance.DisconnectReason = instance_repository.ReconnectingReason
	err = w.instanceRepository.UpdateConnected(instanceId, false, instance_repository.ReconnectingReason)
	if err != nil {
		w.loggerWrapper.GetLogger(instanceId).LogWarn("[%s] Failed to update disconnect status: %v", instanceId, err)
	}

	// Passo 4: Aguardar um pouco para garantir limpeza completa
	time.Sleep(2 * time.Second)

	// The old supervisor loop notices it was replaced within ~1s; the new run cannot
	// start while it still owns the instance.
	if !waitRuntimeReleased(instanceId, 10*time.Second) {
		w.loggerWrapper.GetLogger(instanceId).LogWarn("[%s] Previous runtime still active after 10s, starting anyway", instanceId)
	}

	// Passo 5: Iniciar nova instância como se fosse a primeira vez
	w.loggerWrapper.GetLogger(instanceId).LogInfo("[%s] Starting fresh instance", instanceId)
	return w.StartInstance(instanceId)
}

// RequestReconnect is how a request that found its instance disconnected gets it
// reconnected. It used to call ReconnectClient itself, which is not paced: with traffic
// arriving, an instance that WhatsApp keeps dropping (banned, replaced, bad proxy) was
// disconnected, cleaned up and started again every ~15 s, the pattern the backoff of the
// automatic reconnections exists to avoid. Here it is one more reason for the same paced
// reconnection (the first is immediate, then 5 s, 10 s, 20 s... up to the maximum), and it
// joins one already scheduled.
func (w whatsmeowService) RequestReconnect(instanceId string) bool {
	mycli, ok := w.myClientPointer.Lookup(instanceId)
	if !ok || mycli == nil {
		return false
	}
	mycli.scheduleAutoReconnect("a request found the instance disconnected")
	return true
}

func (w whatsmeowService) ForceUpdateJid(instanceId string, number string) error {
	instance, err := w.instanceRepository.GetInstanceByID(instanceId)
	if err != nil {
		w.loggerWrapper.GetLogger(instanceId).LogError("[%s] Error getting instance: %v", instanceId, err)
		return err
	}

	if instance.Jid == "" && number != "" {
		if w.authDB == nil {
			return errors.New("ForceUpdateJid requires the PostgreSQL auth database")
		}

		// `number` comes from the request body: keep only digits and pass it as a
		// bound parameter. It used to be formatted straight into the SQL string.
		digits := strings.Map(func(r rune) rune {
			if r >= '0' && r <= '9' {
				return r
			}
			return -1
		}, number)
		if digits == "" {
			return apierror.Invalid("invalid number")
		}

		rows, err := w.authDB.Query("SELECT jid FROM whatsmeow_device WHERE jid LIKE $1", "%"+digits+"%")
		if err != nil {
			w.loggerWrapper.GetLogger(instanceId).LogError("[%s] Error getting device: %v", instanceId, err)
			return err
		}

		defer rows.Close()

		var latestJid string
		var latestSession int

		for rows.Next() {
			type deviceStruct struct {
				Jid string `json:"jid"`
			}
			var device deviceStruct
			err := rows.Scan(&device.Jid)
			if err != nil {
				w.loggerWrapper.GetLogger(instanceId).LogError("[%s] Error getting device: %v", instanceId, err)
				return err
			}

			// Extrair o número da sessão do JID
			parts := strings.Split(device.Jid, ":")
			if len(parts) == 2 {
				sessionPart := strings.Split(parts[1], "@")[0]
				session, err := strconv.Atoi(sessionPart)
				if err != nil {
					w.loggerWrapper.GetLogger(instanceId).LogError("[%s] Error parsing session number: %v", instanceId, err)
					return err
				}

				// Atualizar se for a sessão mais recente
				if session > latestSession {
					latestSession = session
					latestJid = device.Jid
				}
			}
		}

		// Atualizar a instância com o JID mais recente
		if latestJid != "" {
			instance.Jid = latestJid
			err = w.instanceRepository.UpdateJid(instanceId, latestJid)
			if err != nil {
				w.loggerWrapper.GetLogger(instanceId).LogError("[%s] Error updating instance: %v", instanceId, err)
			}
			w.loggerWrapper.GetLogger(instanceId).LogInfo("[%s] Updated instance with latest JID: %s (session: %d)", instanceId, latestJid, latestSession)
		}
	}

	return nil
}

// sharedAuthContainer is the single whatsmeow store container of the process.
//
// StartClient used to call sqlstore.New on every invocation — first connect AND
// every reconnect — and never closed the result. Each sqlstore.New opens its own
// *sql.DB, so every websocket drop, QR timeout or /instance/connect leaked a
// whole connection pool until Postgres answered "too many clients already".
// The DSN is the same for every instance, so one container serves all of them.
//
// A failed creation is NOT memoized (a Mutex rather than sync.Once): a database
// that is briefly unreachable must not poison the process for its lifetime.
var (
	sharedAuthContainer   *sqlstore.Container
	sharedAuthContainerMu sync.Mutex
)

// getAuthContainer returns the process-wide whatsmeow store container, creating
// it on first use. On Postgres it reuses the already-bounded authDB pool instead
// of opening a new one.
func (w whatsmeowService) getAuthContainer() (*sqlstore.Container, error) {
	sharedAuthContainerMu.Lock()
	defer sharedAuthContainerMu.Unlock()

	if sharedAuthContainer != nil {
		return sharedAuthContainer, nil
	}

	var dbLog waLog.Logger
	if w.config.WaDebug != "" {
		dbLog = waLog.Stdout("Database", w.config.WaDebug, true)
	}

	var container *sqlstore.Container
	var err error
	switch {
	case w.config.PostgresAuthDB != "" && w.authDB != nil:
		container = sqlstore.NewWithDB(w.authDB, "postgres", dbLog)
		err = container.Upgrade(context.Background())
	case w.config.PostgresAuthDB != "":
		container, err = sqlstore.New(context.Background(), "postgres", w.config.PostgresAuthDB, dbLog)
	default:
		dsn := fmt.Sprintf("file:%s/dbdata/main.db?_pragma=foreign_keys(1)&_busy_timeout=5000&cache=shared&mode=rwc&_journal_mode=WAL", w.exPath)
		container, err = sqlstore.New(context.Background(), "sqlite", dsn, dbLog)
	}
	if err != nil {
		return nil, err
	}

	sharedAuthContainer = container
	return container, nil
}

func (w whatsmeowService) StartClient(cd *ClientData) {
	defer recoverAndLog(w.loggerWrapper, cd.Instance.Id, "StartClient")

	// One runtime per instance (see runtime_slot.go): a duplicate start, e.g. the
	// GET /instance/qr that follows POST /instance/connect, must not create a second
	// client for the same instance.
	if !acquireRuntime(cd.Instance.Id) {
		w.loggerWrapper.GetLogger(cd.Instance.Id).LogInfo("[%s] A runtime is already starting or running for this instance, ignoring duplicate start", cd.Instance.Id)
		return
	}

	// The kill channel belongs to this run: created here (it used to be replaced
	// by every caller, orphaning the loop that was listening on the old one).
	kill := make(chan bool)
	w.killChannel.Set(cd.Instance.Id, kill)

	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() {
			if w.killChannel.Get(cd.Instance.Id) == kill {
				w.killChannel.Delete(cd.Instance.Id)
			}
			releaseRuntime(cd.Instance.Id)
		})
	}
	defer release()

	// One replica per instance (see owner_lock.go). The lock lives as long as the runtime.
	if w.owner != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		ok, err := w.owner.TryLock(ctx, cd.Instance.Id)
		cancel()
		if err != nil {
			w.loggerWrapper.GetLogger(cd.Instance.Id).LogError("[%s] Could not take the instance lock, not starting: %v", cd.Instance.Id, err)
			return
		}
		if !ok {
			w.loggerWrapper.GetLogger(cd.Instance.Id).LogWarn("[%s] Another replica runs this instance, not starting it here", cd.Instance.Id)
			return
		}
		defer w.owner.Unlock(cd.Instance.Id)
	}

	w.loggerWrapper.GetLogger(cd.Instance.Id).LogInfo("Starting websocket connection to Whatsapp for user '%s'", cd.Instance.Id)

	var deviceStore *store.Device

	if w.clientPointer.Get(cd.Instance.Id) != nil {
		if w.clientPointer.Get(cd.Instance.Id).IsConnected() {
			return
		}
	}

	container, err := w.getAuthContainer()
	if err != nil {
		w.loggerWrapper.GetLogger(cd.Instance.Id).LogError("[%s] Failed to create container: %v", cd.Instance.Id, err)
		return
	}

	if cd.Instance.Jid != "" {
		jid, _ := utils.ParseJID(cd.Instance.Jid)
		w.loggerWrapper.GetLogger(cd.Instance.Id).LogInfo("[%s] Jid found. Getting device store for jid: %s", cd.Instance.Id, jid)
		deviceStore, err = container.GetDevice(context.Background(), jid)
		if err != nil {
			w.loggerWrapper.GetLogger(cd.Instance.Id).LogError("[%s] Erro ao obter device store: %v", cd.Instance.Id, err)
			return
		}
	} else {
		w.loggerWrapper.GetLogger(cd.Instance.Id).LogWarn("[%s] No jid found. Creating new device", cd.Instance.Id)
		deviceStore = container.NewDevice()
	}

	if deviceStore == nil {
		w.loggerWrapper.GetLogger(cd.Instance.Id).LogWarn("[%s] No store found. Creating new one", cd.Instance.Id)
		deviceStore = container.NewDevice()

		cd.Instance.Connected = false
		err := w.instanceRepository.UpdateConnected(cd.Instance.Id, cd.Instance.Connected, cd.Instance.DisconnectReason)
		if err != nil {
			w.loggerWrapper.GetLogger(cd.Instance.Id).LogError("[%s] Error updating instance: %s", cd.Instance.Id, err)
		}
	}

	w.configureWAIdentity(cd, deviceStore.ID == nil)

	// 🔒 FIX: Sempre criar logger, mesmo que WaDebug esteja vazio
	// Usar "INFO" como nível mínimo para garantir que logs importantes apareçam
	minLevel := w.config.WaDebug
	if minLevel == "" {
		minLevel = "INFO" // Nível mínimo para garantir que logs INFO apareçam
	}
	clientLog := waLog.Stdout("Client", minLevel, true)
	client := whatsmeow.NewClient(deviceStore, clientLog)

	w.clientPointer.Set(cd.Instance.Id, client)

	// The call engine must be attached before Connect(): it installs its handling of
	// raw <call> stanzas when it is created. Only instances that asked for calls get
	// one, because it answers every incoming offer with a preaccept.
	if cd.Instance.CallsEnabled {
		w.callEngine.Attach(cd.Instance.Id, client, cd.IsProxy, w.loggerWrapper.GetLogger(cd.Instance.Id))
	} else {
		w.callEngine.Detach(cd.Instance.Id)
	}

	if cd.IsProxy {
		proxyConfig, err := parseProxyConfig(cd.Instance.Proxy)
		if err != nil {
			w.loggerWrapper.GetLogger(cd.Instance.Id).LogError("[%s] error unmarshalling proxy config", cd.Instance.Id)
			return
		}

		proxyProtocol := proxyConfig.Protocol
		proxyHost := proxyConfig.Host
		proxyPort := proxyConfig.Port
		proxyUsername := proxyConfig.Username
		proxyPassword := proxyConfig.Password

		if proxyConfig.Host == "" {
			proxyHost = w.config.ProxyHost
		}

		if proxyConfig.Port == "" {
			proxyPort = w.config.ProxyPort
		}

		if proxyConfig.Protocol == "" {
			proxyProtocol = w.config.ProxyProtocol
		}

		if proxyConfig.Username == "" {
			proxyUsername = w.config.ProxyUsername
		}

		if proxyConfig.Password == "" {
			proxyPassword = w.config.ProxyPassword
		}

		proxyAddress, err := utils.BuildProxyAddress(proxyProtocol, proxyHost, proxyPort, proxyUsername, proxyPassword)
		if err == nil {
			err = client.SetProxyAddress(proxyAddress)
		}
		if err != nil {
			// The error text may echo the address (with credentials): keep it out of the status.
			proxyFailed(cd.Instance.Id, "invalid proxy configuration", !w.config.ProxyFailClosed)
			if w.config.ProxyFailClosed {
				w.loggerWrapper.GetLogger(cd.Instance.Id).LogError("[%s] Proxy error and PROXY_FAIL_CLOSED is set; not connecting directly: %v", cd.Instance.Id, err)
				w.clientPointer.Delete(cd.Instance.Id)
				return
			}
			w.loggerWrapper.GetLogger(cd.Instance.Id).LogWarn("[%s] Proxy error, continuing without proxy: %v", cd.Instance.Id, err)
		} else {
			proxyEnabled(cd.Instance.Id)
			w.loggerWrapper.GetLogger(cd.Instance.Id).LogInfo("[%s] Proxy enabled (%s)", cd.Instance.Id, utils.NormalizeProxyProtocol(proxyProtocol, proxyPort))
		}
	} else {
		proxyRuntime.Delete(cd.Instance.Id)
	}

	client.EnableAutoReconnect = false
	client.AutoTrustIdentity = true
	// Re-request messages that fail to decrypt from the phone instead of dropping them.
	client.AutomaticMessageRerequestFromPhone = w.config.RerequestFromPhone

	mycli := &MyClient{
		service:            &w,
		WAClient:           client,
		eventHandlerID:     1,
		userID:             cd.Instance.Id,
		token:              cd.Instance.Token,
		instanceRepository: w.instanceRepository,
		messageRepository:  w.messageRepository,
		labelRepository:    w.labelRepository,
		pollService:        w.pollService, // NOVO: Serviço de enquetes
		userInfoCache:      w.userInfoCache,
		clientPointer:      w.clientPointer,
		myClientPointer:    w.myClientPointer,
		killChannel:        w.killChannel,
		config:             w.config,
		historySyncID:      0,
		rabbitmqProducer:   w.rabbitmqProducer,
		webhookProducer:    w.webhookProducer,
		websocketProducer:  w.websocketProducer,
		mediaStorage:       w.mediaStorage,
		processedMessages:  w.processedMessages,
		natsProducer:       w.natsProducer,
		loggerWrapper:      w.loggerWrapper,
		passkeyCeremony:    w.passkeyCeremony,
	}

	mycli.setInst(cd.Instance)
	mycli.eventHandlerID = mycli.WAClient.AddEventHandler(mycli.myEventHandler)

	// Armazena o MyClient no map para permitir atualizações posteriores
	w.myClientPointer.Set(cd.Instance.Id, mycli)

	if client.Store.ID != nil {
		w.loggerWrapper.GetLogger(cd.Instance.Id).LogInfo("[%s] Already logged in with JID: %s", cd.Instance.Id, client.Store.ID.String())
		err = client.Connect()
		if err != nil {
			if strings.Contains(err.Error(), "EOF") {
				w.loggerWrapper.GetLogger(cd.Instance.Id).LogError("[%s] Erro de conexão WebSocket (EOF). Tentando reconectar em 5 segundos...", cd.Instance.Id)
				time.Sleep(5 * time.Second)
				err = client.Connect()
				if err != nil {
					w.loggerWrapper.GetLogger(cd.Instance.Id).LogError("[%s] Falha na segunda tentativa de conexão: %v", cd.Instance.Id, err)
					return
				}
			} else if strings.Contains(err.Error(), "username/password authentication failed") {
				w.loggerWrapper.GetLogger(cd.Instance.Id).LogWarn("[%s] Proxy authentication failed, attempting to connect without proxy", cd.Instance.Id)

				// Desabilita o proxy (ou aborta com PROXY_FAIL_CLOSED)
				if !w.fallbackWithoutProxy(cd.Instance.Id, client, err) {
					return
				}

				// Tenta conectar sem proxy
				err = client.Connect()
				if err != nil {
					w.loggerWrapper.GetLogger(cd.Instance.Id).LogError("[%s] Failed to connect even without proxy: %v", cd.Instance.Id, err)
					return
				}
				w.loggerWrapper.GetLogger(cd.Instance.Id).LogInfo("[%s] Successfully connected without proxy", cd.Instance.Id)
			} else {
				w.loggerWrapper.GetLogger(cd.Instance.Id).LogError("[%s] Failed to connect: %v", cd.Instance.Id, err)
				return
			}
		}
	} else {
		// New-device pairing. We intentionally do NOT use client.GetQRChannel:
		// in the installed whatsmeow its qrChannel handler auto-confirms a
		// passkey ceremony (SkipHandoffUX) and Disconnects the socket when the
		// QR codes run out, both of which break passkey pairing (DOC2 §4.3/§4.4).
		// Instead we Connect() directly and consume *events.QR in myEventHandler
		// (see handleQRCodes), which pair.go dispatches to every handler anyway.
		err = client.Connect()
		if err != nil {
			if strings.Contains(err.Error(), "EOF") {
				w.loggerWrapper.GetLogger(cd.Instance.Id).LogError("[%s] Erro de conexão WebSocket (EOF). Tentando reconectar em 5 segundos...", cd.Instance.Id)
				time.Sleep(5 * time.Second)
				err = client.Connect()
				if err != nil {
					w.loggerWrapper.GetLogger(cd.Instance.Id).LogError("[%s] Falha na segunda tentativa de conexão: %v", cd.Instance.Id, err)
					return
				}
			} else if strings.Contains(err.Error(), "username/password authentication failed") {
				w.loggerWrapper.GetLogger(cd.Instance.Id).LogWarn("[%s] Proxy authentication failed during QR connection, attempting without proxy", cd.Instance.Id)

				// Desabilita o proxy (ou aborta com PROXY_FAIL_CLOSED)
				if !w.fallbackWithoutProxy(cd.Instance.Id, client, err) {
					return
				}

				// Tenta conectar sem proxy
				err = client.Connect()
				if err != nil {
					w.loggerWrapper.GetLogger(cd.Instance.Id).LogError("[%s] Failed to connect even without proxy: %v", cd.Instance.Id, err)
					return
				}
				w.loggerWrapper.GetLogger(cd.Instance.Id).LogInfo("[%s] Successfully connected without proxy", cd.Instance.Id)
			} else {
				w.loggerWrapper.GetLogger(cd.Instance.Id).LogError("[%s] Failed to connect: %v", cd.Instance.Id, err)
				return
			}
		}

	}

	// Removed auto-reconnect logic to prevent infinite loops

	for {
		select {
		case _, open := <-kill:
			// A CLOSED channel means the instance was deleted or stopped for good
			// (Delete/ClearInstanceCache): shut down without restarting. Only an
			// explicit `true` (QR timeout, disconnect...) restarts the client. Treating
			// both alike resurrected deleted instances in an endless QR loop.
			if !open {
				w.loggerWrapper.GetLogger(cd.Instance.Id).LogInfo("[%s] Kill channel closed, shutting the runtime down without restart", cd.Instance.Id)
				client.Disconnect()
				if w.clientPointer.Get(cd.Instance.Id) == client {
					w.clientPointer.Delete(cd.Instance.Id)
					w.myClientPointer.Delete(cd.Instance.Id)
					w.callEngine.Detach(cd.Instance.Id)
				}
				w.userInfoCache.Delete(cd.Instance.Token)
				return
			}

			w.loggerWrapper.GetLogger(cd.Instance.Id).LogInfo("Received kill signal for user '%s'", cd.Instance.Id)
			client.Disconnect()

			w.clientPointer.Delete(cd.Instance.Id)
			w.myClientPointer.Delete(cd.Instance.Id)
			w.callEngine.Detach(cd.Instance.Id)

			// Limpar cache de userInfo para esta instância
			w.userInfoCache.Delete(cd.Instance.Token)
			w.loggerWrapper.GetLogger(cd.Instance.Id).LogInfo("[%s] UserInfo cache cleared", cd.Instance.Id)

			cd.Instance.Connected = false

			err := w.instanceRepository.UpdateConnected(cd.Instance.Id, cd.Instance.Connected, cd.Instance.DisconnectReason)
			if err != nil {
				w.loggerWrapper.GetLogger(cd.Instance.Id).LogError("[%s] Error updating instance: %s", cd.Instance.Id, err)
			}

			postMap := make(map[string]interface{})

			postMap["event"] = "LoggedOut"

			dataMap := make(map[string]interface{})

			dataMap["reason"] = "Logged out"

			postMap["data"] = dataMap

			mycli.config.AddInstanceToken(postMap, mycli.token)
			postMap["instanceId"] = mycli.userID
			postMap["instanceName"] = cd.Instance.Name

			var queueName string

			if _, ok := postMap["event"]; ok {
				queueName = strings.ToLower(fmt.Sprintf("%s.%s", cd.Instance.Id, postMap["event"]))
			}

			values, err := json.Marshal(postMap)
			if err != nil {
				w.loggerWrapper.GetLogger(cd.Instance.Id).LogError("[%s] Failed to marshal JSON for queue", cd.Instance.Id)
				return
			}

			go w.CallWebhook(cd.Instance, queueName, values)

			if mycli.config.AmqpGlobalEnabled || mycli.config.NatsGlobalEnabled {
				go mycli.service.SendToGlobalQueues(postMap["event"].(string), values, mycli.userID)
			}

			// restart client
			w.loggerWrapper.GetLogger(cd.Instance.Id).LogInfo("[%s] Restarting client", cd.Instance.Id)
			// Not a direct (recursive) call: every restart used to stack another
			// StartClient frame on this goroutine (visible as the repeated
			// whatsmeow.go:629 frames in issue #203).
			// Never restart an instance whose row is gone.
			if _, err := w.instanceRepository.GetInstanceByID(cd.Instance.Id); err != nil {
				w.loggerWrapper.GetLogger(cd.Instance.Id).LogInfo("[%s] Instance no longer exists, not restarting", cd.Instance.Id)
				return
			}

			release() // free the slot so the restarted run can take it
			go w.StartClient(cd)
			return
		default:
			// This client was replaced (ReconnectClient removed it and started a new
			// one): nothing left to supervise, end quietly without touching the
			// state that now belongs to the new client.
			if w.clientPointer.Get(cd.Instance.Id) != client {
				w.loggerWrapper.GetLogger(cd.Instance.Id).LogInfo("[%s] Client was replaced, ending its supervisor loop", cd.Instance.Id)
				return
			}
			time.Sleep(1000 * time.Millisecond)
		}
	}
}

// waIdentityMu serializes the writes to whatsmeow's process-wide identity (store.DeviceProps
// and the WhatsApp version). Both are globals every client reads while it connects, and
// every StartClient used to rewrite them: starting instances in parallel was a data race,
// and the version was re-applied (and re-fetched) for each one.
var (
	waIdentityMu      sync.Mutex
	appliedWAVersion  clientVersion
	waIdentityApplied bool
)

// configureWAIdentity resolves the WhatsApp version and applies the identity this process
// presents. The values are the same for every instance, so the globals are only written when
// they actually change. The OS name is the one thing that is per instance, and it is only
// used when a device is paired (QR / pair code), so it is only set for a device that is about
// to be.
func (w whatsmeowService) configureWAIdentity(cd *ClientData, newDevice bool) {
	log := w.loggerWrapper.GetLogger(cd.Instance.Id)

	if cd.Instance.OsName == "" {
		cd.Instance.OsName = utils.WhatsAppGetUserOS()
	}

	// Resolve the version first (it may hit the network, cached for an hour), outside the lock.
	var version clientVersion
	if w.config.WhatsappVersionMajor != 0 && w.config.WhatsappVersionMinor != 0 && w.config.WhatsappVersionPatch != 0 {
		version = clientVersion{Major: w.config.WhatsappVersionMajor, Minor: w.config.WhatsappVersionMinor, Patch: w.config.WhatsappVersionPatch}
	} else if webVersion, err := fetchWhatsAppWebVersion(); err != nil {
		log.LogError("[%s] Failed to fetch WhatsApp Web version: %v", cd.Instance.Id, err)
	} else {
		version = *webVersion
	}

	waIdentityMu.Lock()
	defer waIdentityMu.Unlock()

	if platformID, ok := waCompanionReg.DeviceProps_PlatformType_value[strings.ToUpper("chrome")]; ok {
		if want := waCompanionReg.DeviceProps_PlatformType(platformID); store.DeviceProps.GetPlatformType() != want {
			store.DeviceProps.PlatformType = want.Enum()
		}
	}
	if !store.DeviceProps.GetRequireFullSync() {
		store.DeviceProps.RequireFullSync = proto.Bool(true)
	}
	if newDevice {
		osName := cd.Instance.OsName
		store.DeviceProps.Os = &osName
	}

	if version.Major == 0 && version.Minor == 0 && version.Patch == 0 {
		return
	}
	if waIdentityApplied && appliedWAVersion == version {
		return
	}
	log.LogInfo("[%s] Setting whatsapp version to %d.%d.%d", cd.Instance.Id, version.Major, version.Minor, version.Patch)
	// DeviceProps.Version is what is advertised while pairing; SetWAVersion is the version of
	// the connection handshake (without it WhatsApp ended up refusing the one compiled into
	// whatsmeow with "Client outdated (405)", PR #199).
	if store.DeviceProps.Version == nil {
		store.DeviceProps.Version = &waCompanionReg.DeviceProps_AppVersion{}
	}
	store.DeviceProps.Version.Primary = proto.Uint32(uint32(version.Major))
	store.DeviceProps.Version.Secondary = proto.Uint32(uint32(version.Minor))
	store.DeviceProps.Version.Tertiary = proto.Uint32(uint32(version.Patch))
	store.SetWAVersion(store.WAVersionContainer{uint32(version.Major), uint32(version.Minor), uint32(version.Patch)})
	appliedWAVersion = version
	waIdentityApplied = true
}

// startPresenceUpdates starts the periodic presence goroutine unless this client
// already has one. It reports whether it started one.
func startPresenceUpdates(mycli *MyClient) bool {
	if !mycli.presenceRunning.CompareAndSwap(false, true) {
		return false
	}
	go func() {
		defer mycli.presenceRunning.Store(false)
		// A panic here is outside whatsmeow's safety net and used to end the whole process.
		defer recoverAndLog(mycli.loggerWrapper, mycli.userID, "presence scheduler")
		schedulePresenceUpdates(mycli)
	}()
	return true
}

// applyAlwaysOnlineChange makes a runtime change of alwaysOnline take effect now, the
// same way the Connected event does. Turning it off needs no goroutine handling: the
// scheduler notices the flag on its next tick and ends.
func applyAlwaysOnlineChange(mycli *MyClient, alwaysOnline bool) {
	if mycli.WAClient == nil || !mycli.WAClient.IsConnected() {
		return
	}
	presence := types.PresenceUnavailable
	if alwaysOnline {
		presence = types.PresenceAvailable
		startPresenceUpdates(mycli)
	}
	if err := mycli.WAClient.SendPresence(context.Background(), presence); err != nil {
		mycli.loggerWrapper.GetLogger(mycli.userID).LogWarn("[%s] Failed to send %s presence after alwaysOnline changed: %v", mycli.userID, presence, err)
		return
	}
	mycli.loggerWrapper.GetLogger(mycli.userID).LogInfo("[%s] alwaysOnline switched to %v at runtime: presence now %s", mycli.userID, alwaysOnline, presence)
}

func schedulePresenceUpdates(mycli *MyClient) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	// Bound to the channel of the run that started it (see StartClient).
	kill := mycli.killChannel.Get(mycli.userID)

	for {
		select {
		case <-ticker.C:
			// Verificar se a instância ainda existe
			_, err := mycli.instanceRepository.GetInstanceByID(mycli.userID)
			if err != nil {
				mycli.loggerWrapper.GetLogger(mycli.userID).LogInfo("[%s] Instance no longer exists, stopping presence updates", mycli.userID)
				return // Encerra a goroutine se a instância não existir mais
			}

			// Stop when this client was replaced by a reconnect (each reconnect used to
			// leave one of these goroutines behind, still holding the old flag values)
			// or when alwaysOnline was switched off at runtime (#55).
			if current, ok := mycli.myClientPointer.Lookup(mycli.userID); !ok || current != mycli {
				mycli.loggerWrapper.GetLogger(mycli.userID).LogInfo("[%s] Client was replaced, stopping presence updates", mycli.userID)
				return
			}
			if !mycli.inst().AlwaysOnline {
				mycli.loggerWrapper.GetLogger(mycli.userID).LogInfo("[%s] alwaysOnline disabled, stopping presence updates", mycli.userID)
				return
			}

			processPresenceUpdates(mycli)

			ticker.Stop()
			randomInterval := time.Duration(1+rand.Intn(3)) * time.Hour
			ticker = time.NewTicker(randomInterval)

		case <-kill:
			mycli.loggerWrapper.GetLogger(mycli.userID).LogInfo("[%s] Received kill signal, stopping presence updates", mycli.userID)
			return // Encerra a goroutine quando receber sinal de kill
		}
	}
}

func processPresenceUpdates(mycli *MyClient) {
	now := time.Now()
	location, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		location = time.UTC // a nil location makes Time.In panic
	}
	nowSp := now.In(location)

	if nowSp.Hour() >= 1 && nowSp.Hour() < 24 {
		err := mycli.WAClient.SendPresence(context.Background(), types.PresenceUnavailable)
		if err != nil {
			mycli.loggerWrapper.GetLogger(mycli.userID).LogError("[%s] Failed to set presence as unavailable %v", mycli.userID, err)
		} else {
			mycli.loggerWrapper.GetLogger(mycli.userID).LogInfo("[%s] Marked self as unavailable", mycli.userID)
		}

		time.Sleep(time.Duration(1+rand.Intn(5)) * time.Second)

		err = mycli.WAClient.SendPresence(context.Background(), types.PresenceAvailable)
		if err != nil {
			mycli.loggerWrapper.GetLogger(mycli.userID).LogError("[%s] Failed to set presence as available %v", mycli.userID, err)
		} else {
			mycli.loggerWrapper.GetLogger(mycli.userID).LogInfo("[%s] Marked self as available", mycli.userID)
		}
	}
}

// handleQRCodes forwards a batch of QR codes (events.QR.Codes) to the manager,
// rotating them with whatsmeow's native timing (first code ~60s, the rest ~20s)
// WITHOUT using GetQRChannel. GetQRChannel is deliberately avoided for new-device
// pairing because, in the installed whatsmeow, its qrChannel handler both
// auto-confirms PairPasskeyConfirmation when SkipHandoffUX is set (racing our
// own confirm flow) and Disconnects the socket when codes run out — either of
// which breaks an in-flight passkey ceremony (DOC2 §4.3/§4.4). Consuming
// events.QR here keeps the socket alive for as long as pairing (QR or passkey)
// needs, since events.QR is dispatched to every handler by pair.go regardless.
//
// This preserves the original GetQRChannel-loop behavior byte-for-byte for the
// per-code work (max-count enforcement, PNG encode, DB persist, webhook/queue
// fan-out) and the timeout teardown; only the trigger (batch vs. per-code) and
// the rotation/self-timer are new. Runs in its own goroutine so it never blocks
// the whatsmeow event dispatch.
func (mycli *MyClient) handleQRCodes(codes []string) {
	go func() {
		instanceID := mycli.userID
		for i, code := range codes {
			// A successful pair (Store.ID set) or an in-flight passkey ceremony
			// supersedes QR — stop rotating WITHOUT tearing down. Store.ID stays
			// nil throughout a passkey ceremony (it is only set at PairSuccess),
			// so we must also consult the ceremony store, otherwise a ceremony
			// that outlasts QR rotation would have its socket/client torn down.
			if mycli.WAClient == nil || mycli.WAClient.Store.ID != nil {
				return
			}
			// This client is no longer the instance's runtime (deleted, replaced or
			// shut down): its QR codes are useless.
			if !mycli.isCurrentRuntime() {
				return
			}
			if mycli.passkeyCeremony != nil && mycli.passkeyCeremony.HasActiveByInstance(instanceID) {
				mycli.loggerWrapper.GetLogger(instanceID).LogInfo("[%s] Passkey ceremony in progress — pausing QR rotation, keeping socket alive", instanceID)
				return
			}

			mycli.qrcodeCount.Add(1)

			if mycli.config.QrcodeMaxCount > 0 {
				mycli.loggerWrapper.GetLogger(instanceID).LogInfo("[%s] QR code generated #%d (max: %d)", instanceID, mycli.qrcodeCount.Load(), mycli.config.QrcodeMaxCount)
			} else {
				mycli.loggerWrapper.GetLogger(instanceID).LogInfo("[%s] QR code generated #%d (limit disabled)", instanceID, mycli.qrcodeCount.Load())
			}

			// Max-count reached: force logout + teardown + QRTimeout (0 = disabled).
			// But never tear down while a passkey ceremony is in flight.
			if mycli.config.QrcodeMaxCount > 0 && int(mycli.qrcodeCount.Load()) >= mycli.config.QrcodeMaxCount {
				if mycli.passkeyCeremony != nil && mycli.passkeyCeremony.HasActiveByInstance(instanceID) {
					mycli.loggerWrapper.GetLogger(instanceID).LogInfo("[%s] QR max-count reached but passkey ceremony active — not tearing down", instanceID)
					return
				}
				mycli.loggerWrapper.GetLogger(instanceID).LogWarn("[%s] Maximum QR code count reached (%d), forcing logout and QRTimeout", instanceID, mycli.config.QrcodeMaxCount)

				if mycli.WAClient.IsConnected() {
					if err := mycli.WAClient.Logout(context.Background()); err != nil {
						mycli.loggerWrapper.GetLogger(instanceID).LogWarn("[%s] Error during forced logout: %v", instanceID, err)
					}
				}
				mycli.teardownQR(fmt.Sprintf("Maximum QR code count (%d) reached", mycli.config.QrcodeMaxCount), true)
				return
			}

			if mycli.config.LogType != "json" {
				fmt.Println("QR code:\n", code)
			}

			image, _ := qrcode.Encode(code, qrcode.Medium, 256)
			base64qrcode := "data:image/png;base64," + base64.StdEncoding.EncodeToString(image)
			base64WithCode := base64qrcode + "|" + code

			if err := mycli.instanceRepository.UpdateQrcode(instanceID, base64WithCode); err != nil {
				mycli.loggerWrapper.GetLogger(instanceID).LogError("[%s] Error updating instance: %s", instanceID, err)
			}

			postMap := map[string]interface{}{
				"event": "QRCode",
				"data": map[string]interface{}{
					"qrcode":   base64qrcode,
					"code":     code,
					"count":    mycli.qrcodeCount.Load(),
					"maxCount": mycli.config.QrcodeMaxCount,
				},
				"instanceId":   instanceID,
				"instanceName": mycli.inst().Name,
			}
			mycli.config.AddInstanceToken(postMap, mycli.token)
			queueName := strings.ToLower(fmt.Sprintf("%s.%s", instanceID, "QRCode"))
			if values, err := json.Marshal(postMap); err == nil {
				go mycli.service.CallWebhook(mycli.inst(), queueName, values)
				if mycli.config.AmqpGlobalEnabled || mycli.config.NatsGlobalEnabled {
					go mycli.service.SendToGlobalQueues("QRCode", values, instanceID)
				}
			} else {
				mycli.loggerWrapper.GetLogger(instanceID).LogError("[%s] Failed to marshal JSON for queue", instanceID)
			}

			// Rotation timing: first code lives ~60s, subsequent ~20s (whatsmeow native).
			timeout := 20 * time.Second
			if i == 0 {
				timeout = 60 * time.Second
			}
			time.Sleep(timeout)
		}

		// Ran out of codes without a PairSuccess. Treat as QR timeout (mirrors
		// GetQRChannel's "timeout") — UNLESS a passkey ceremony is in flight, in
		// which case the socket must stay alive for the ceremony to complete.
		if mycli.WAClient != nil && mycli.WAClient.Store.ID == nil && mycli.isCurrentRuntime() {
			if mycli.passkeyCeremony != nil && mycli.passkeyCeremony.HasActiveByInstance(instanceID) {
				mycli.loggerWrapper.GetLogger(instanceID).LogInfo("[%s] QR codes exhausted but passkey ceremony active — keeping socket alive", instanceID)
				return
			}
			mycli.teardownQR("", false)
		}
	}()
}

// isCurrentRuntime reports whether this client is still the one registered for its
// instance.
func (mycli *MyClient) isCurrentRuntime() bool {
	cur, ok := mycli.myClientPointer.Lookup(mycli.userID)
	return ok && cur == mycli
}

// teardownQR clears the QR state and emits a QRTimeout event, then signals the
// kill channel so StartClient's select loop performs the actual disconnect and
// map cleanup. IMPORTANT: this method must NOT delete from the shared
// clientPointer/myClientPointer/killChannel maps itself — those are unsynchronized
// service-wide maps and this runs in the handleQRCodes goroutine; doing the
// delete()s here (concurrent with other instances' goroutines and the whatsmeow
// dispatch) risks a `fatal error: concurrent map writes`. The kill-channel send
// is blocking (like the original GetQRChannel timeout branch) so the signal is
// never dropped and the socket can't be orphaned. Cleanup happens in the
// StartClient goroutine, the single writer of those maps for this instance.
// If reason is non-empty it is included in the QRTimeout payload (max-count path).
func (mycli *MyClient) teardownQR(reason string, forceLogout bool) {
	instanceID := mycli.userID

	if err := mycli.instanceRepository.UpdateQrcode(instanceID, ""); err != nil {
		mycli.loggerWrapper.GetLogger(instanceID).LogError("[%s] Error updating instance: %s", instanceID, err)
	}

	// With a QR limit the instance gives up (below); without one (QRCODE_MAX_COUNT=0) it keeps
	// restarting for new codes, as it always did.
	giveUp := mycli.config.QrcodeMaxCount > 0
	if giveUp && reason == "" {
		reason = instance_repository.QRTimeoutReason
	}
	storedReason := reason
	if reason != "" || giveUp {
		if err := mycli.instanceRepository.UpdateConnected(instanceID, false, storedReason); err != nil {
			mycli.loggerWrapper.GetLogger(instanceID).LogError("[%s] Error updating instance status: %v", instanceID, err)
		}
	}

	data := map[string]interface{}{}
	if reason != "" && reason != instance_repository.QRTimeoutReason {
		data["reason"] = reason
		data["qrcount"] = mycli.qrcodeCount.Load()
		data["maxCount"] = mycli.config.QrcodeMaxCount
		data["forceLogout"] = forceLogout
	}
	postMap := map[string]interface{}{
		"event":        "QRTimeout",
		"data":         data,
		"instanceId":   instanceID,
		"instanceName": mycli.inst().Name,
	}
	mycli.config.AddInstanceToken(postMap, mycli.token)
	queueName := strings.ToLower(fmt.Sprintf("%s.%s", instanceID, "QRTimeout"))
	if values, err := json.Marshal(postMap); err == nil {
		go mycli.service.CallWebhook(mycli.inst(), queueName, values)
		if mycli.config.AmqpGlobalEnabled || mycli.config.NatsGlobalEnabled {
			go mycli.service.SendToGlobalQueues("QRTimeout", values, instanceID)
		}
	}

	if giveUp {
		// The QR codes ran out unscanned: stop for good. The kill channel used to be sent
		// `true`, which means "restart": the supervisor reported a LoggedOut that never happened
		// and started a new client with a new QR, and the code counter started again at #1, so
		// QRCODE_MAX_COUNT never gave up and every ~140 s the instance reconnected to WhatsApp and
		// told the webhook it had been logged out, for ever. Now the runtime ends (closing the
		// kill channel) and the instance stays off until someone connects it again.
		mycli.loggerWrapper.GetLogger(instanceID).LogWarn("[%s] QR codes ran out: stopping the instance until it is connected again", instanceID)
		_ = mycli.service.ClearInstanceCache(instanceID, mycli.token)
		return
	}

	// Signal StartClient's select loop to disconnect and clean up the shared
	// maps (it is the single writer for this instance). Blocking send mirrors
	// the original timeout branch so the signal is never dropped.
	mycli.loggerWrapper.GetLogger(instanceID).LogWarn("[%s] QR timeout — signaling kill channel", instanceID)
	if killChan, exists := mycli.killChannel.Lookup(instanceID); exists {
		// Bounded: if the supervisor loop already ended (client replaced) nobody is
		// receiving and an unbounded send would block this goroutine forever.
		// The channel may be closed concurrently by Delete: a send on a closed
		// channel panics, so contain it.
		func() {
			defer recoverAndLog(mycli.loggerWrapper, instanceID, "teardownQR kill signal")
			select {
			case killChan <- true:
			case <-time.After(10 * time.Second):
				mycli.loggerWrapper.GetLogger(instanceID).LogWarn("[%s] Kill signal not received (client already replaced?), giving up", instanceID)
			}
		}()
	}
}

// handleEvent processes one event (see myEventHandler for how events get here).
func (mycli *MyClient) handleEvent(rawEvt interface{}) {
	mycli.lastEventAt.Store(time.Now().UnixNano())
	eventType := strings.TrimPrefix(fmt.Sprintf("%T", rawEvt), "*events.")
	mycli.lastEventType.Store(eventType)
	metrics.Events.WithLabelValues(eventType).Inc()
	mycli.eventCount.Add(1)

	userID := mycli.userID
	postMap := make(map[string]interface{})
	postMap["data"] = rawEvt
	doWebhook := false
	// eventChat is the chat of the event, for the subscription check at the end.
	eventChat := ""

	switch evt := rawEvt.(type) {
	case *events.QR:
		// New-device pairing emits QR codes here (we connect without GetQRChannel
		// so the socket survives a passkey ceremony). Forward + rotate them.
		mycli.handleQRCodes(evt.Codes)
		return
	case *events.AppStateSyncError:
		mycli.handleAppStateSyncError(evt)
		return
	case *events.AppStateSyncComplete:
		if evt.Recovery {
			mycli.loggerWrapper.GetLogger(mycli.userID).LogInfo(
				"[%s] App-state recovery completed for %s at version %d",
				mycli.userID, evt.Name, evt.Version,
			)
		}
		if len(mycli.WAClient.Store.PushName) > 0 && evt.Name == appstate.WAPatchCriticalBlock {
			err := mycli.WAClient.SendPresence(context.Background(), types.PresenceUnavailable)
			if err != nil {
				mycli.loggerWrapper.GetLogger(mycli.userID).LogWarn("[%s] Failed to send unavailable presence %v", mycli.userID, err)
			} else {
				mycli.loggerWrapper.GetLogger(mycli.userID).LogWarn("[%s] Marked self as unavailable", mycli.userID)
			}
		}
	case *events.Connected, *events.PushNameSetting:
		if _, isConnected := rawEvt.(*events.Connected); isConnected {
			mycli.connectedAt.Store(time.Now().UnixNano())
			autoReconnect.connected(mycli.userID, time.Now())
		}
		mycli.loggerWrapper.GetLogger(mycli.userID).LogInfo("[%s] events.Connected to Whatsapp for user '%s'", mycli.userID, mycli.WAClient.Store.PushName)
		if len(mycli.WAClient.Store.PushName) > 0 {
			doWebhook = true
			postMap["event"] = "Connected"

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
			dataMap["pushName"] = mycli.WAClient.Store.PushName

			// jid, ok := utils.ParseJID(mycli.WAClient.Store.ID.ToNonAD().User)
			// if ok {
			// 	profilePicUrl, err := mycli.clientPointer.Get(mycli.userID).GetProfilePictureInfo(jid, &whatsmeow.GetProfilePictureParams{
			// 		Preview: false,
			// 	})
			// 	if err != nil {
			// 		w.loggerWrapper.GetLogger(instanceId).LogError("[%s] Failed to get profile picture info: %v", mycli.userID, err)
			// 	} else {
			// 		dataMap["profilePicUrl"] = profilePicUrl.URL
			// 	}
			// }

			postMap["data"] = dataMap

			// Respect the alwaysOnline instance flag. Previously the device was marked
			// online unconditionally on every connect (and the periodic presence job was
			// started), which kept the linked device permanently "available". WhatsApp then
			// delivers messages to that active session and suppresses push notifications on
			// the user's phone. When alwaysOnline is false we now send Unavailable instead.
			var err error
			if mycli.inst().AlwaysOnline {
				startPresenceUpdates(mycli)

				err = mycli.WAClient.SendPresence(context.Background(), types.PresenceAvailable)
				if err != nil {
					mycli.loggerWrapper.GetLogger(mycli.userID).LogWarn("[%s] Failed to send available presence %v", mycli.userID, err)
				} else {
					mycli.loggerWrapper.GetLogger(mycli.userID).LogWarn("[%s] Marked self as available", mycli.userID)
				}
			} else {
				err = mycli.WAClient.SendPresence(context.Background(), types.PresenceUnavailable)
				if err != nil {
					mycli.loggerWrapper.GetLogger(mycli.userID).LogWarn("[%s] Failed to send unavailable presence %v", mycli.userID, err)
				} else {
					mycli.loggerWrapper.GetLogger(mycli.userID).LogInfo("[%s] Marked self as unavailable (alwaysOnline=false)", mycli.userID)
				}
			}

			mycli.inst().Connected = true
			mycli.inst().DisconnectReason = ""
			err = mycli.instanceRepository.UpdateConnected(mycli.inst().Id, mycli.inst().Connected, mycli.inst().DisconnectReason)
			if err != nil {
				mycli.loggerWrapper.GetLogger(mycli.userID).LogError("[%s] Error updating instance: %s", mycli.inst().Id, err)
			}

			err = mycli.instanceRepository.UpdateQrcode(mycli.inst().Id, "")
			if err != nil {
				mycli.loggerWrapper.GetLogger(mycli.userID).LogError("[%s] Error updating instance: %s", mycli.inst().Id, err)
			}
		}
	case *events.PairSuccess:
		if dispatch, chat := mycli.handlePairSuccess(evt, postMap); !dispatch {
			return
		} else {
			doWebhook, eventChat = true, chat
		}
	case *events.PairPasskeyRequest:
		// The server demands a WebAuthn passkey to finish linking. We CANNOT
		// produce the assertion here (it needs the user's authenticator on the
		// web.whatsapp.com origin) — we only forward the challenge. The browser
		// extension (tools/passkey-helper) runs navigator.credentials.get() and
		// POSTs the assertion back to /passkey-ceremony/{token}/response, which
		// is where SendPasskeyResponse is actually called.
		doWebhook = true
		postMap["event"] = "PasskeyRequest"

		pkJSON, err := json.Marshal(evt.PublicKey)
		if err != nil {
			mycli.loggerWrapper.GetLogger(mycli.userID).LogError("[%s] Failed to marshal passkey publicKey: %v", mycli.userID, err)
			mycli.passkeyCeremony.SetError(mycli.userID, "failed to encode passkey challenge")
			return
		}

		token := mycli.passkeyCeremony.Start(mycli.userID, pkJSON)

		// Build the #wapk payload the extension consumes: base64url({t,b}).
		// `b` must be the PUBLICLY reachable API base the browser can hit
		// (a tunnel / LAN IP in dev) — set PASSKEY_PUBLIC_URL to that base.
		publicBase := os.Getenv("PASSKEY_PUBLIC_URL")
		if publicBase == "" {
			publicBase = "<SET_PASSKEY_PUBLIC_URL>"
		}
		payload := fmt.Sprintf(`{"t":%q,"b":%q}`, token, publicBase)
		wapk := base64.RawURLEncoding.EncodeToString([]byte(payload))
		openURL := "https://web.whatsapp.com/#wapk=" + wapk

		mycli.loggerWrapper.GetLogger(mycli.userID).LogInfo(
			"[%s] Passkey required. Open this URL in a browser with the WhatyGo Passkey Helper extension:\n%s\n(ceremony token=%s, base=%s)",
			mycli.userID, openURL, token, publicBase,
		)

		// Surface the ceremony info to webhooks/queues so the manager UI can
		// render the "Abrir WhatsApp Web" button.
		postMap["data"] = map[string]interface{}{
			"ceremonyToken": token,
			"openUrl":       openURL,
			"stage":         "challenge",
		}
	case *events.PairPasskeyConfirmation:
		// The server returned a confirmation code. Per DOC2 §4.2 we NEVER
		// auto-confirm on SkipHandoffUX — we always force skipHandoffUX=false so
		// the extension shows the manual "Confirmar" button, and the actual
		// SendPasskeyConfirmation happens from /passkey-ceremony/{token}/confirm.
		doWebhook = true
		postMap["event"] = "PasskeyConfirmation"
		mycli.passkeyCeremony.SetConfirmation(mycli.userID, evt.Code, false)
		mycli.loggerWrapper.GetLogger(mycli.userID).LogInfo(
			"[%s] Passkey confirmation code=%s (skipHandoffUX from server=%v, forced to manual)",
			mycli.userID, evt.Code, evt.SkipHandoffUX,
		)
		postMap["data"] = map[string]interface{}{
			"code":  evt.Code,
			"stage": "confirmation",
		}
	case *events.PairPasskeyError:
		doWebhook = true
		postMap["event"] = "PasskeyError"
		msg := "unknown passkey error"
		if evt.Error != nil {
			msg = evt.Error.Error()
		}
		mycli.passkeyCeremony.SetError(mycli.userID, msg)
		mycli.loggerWrapper.GetLogger(mycli.userID).LogError(
			"[%s] Passkey pairing error (continuation=%v): %s", mycli.userID, evt.Continuation, msg,
		)
		postMap["data"] = map[string]interface{}{
			"error": msg,
			"stage": "error",
		}
	case *events.StreamReplaced:
		mycli.loggerWrapper.GetLogger(mycli.userID).LogInfo("[%s] Received StreamReplaced event", mycli.userID)
		return
	case *events.TemporaryBan:
		mycli.loggerWrapper.GetLogger(mycli.userID).LogInfo("[%s] User received temporary ban for %s", mycli.userID, evt.Code.String())
		doWebhook = true
		postMap["event"] = "TemporaryBan"

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

		dataMap["reason"] = evt.Code.String()
		dataMap["expire"] = evt.Expire

		postMap["data"] = dataMap
	case *events.Message:
		if dispatch, chat := mycli.handleMessage(evt, postMap); !dispatch {
			return
		} else {
			doWebhook, eventChat = true, chat
		}
	case *events.Receipt:
		if dispatch, chat := mycli.handleReceipt(evt, postMap); !dispatch {
			return
		} else {
			doWebhook, eventChat = true, chat
		}
	case *events.Presence:
		doWebhook = true
		postMap["event"] = "Presence"
		// Explicit top-level fields so consumers don't depend on types.JID/time marshaling.
		postMap["from"] = evt.From.String()

		if evt.Unavailable {
			postMap["state"] = "offline"
			if evt.LastSeen.IsZero() {
				mycli.loggerWrapper.GetLogger(mycli.userID).LogInfo("[%s] User is now offline", mycli.userID)
			} else {
				postMap["lastSeen"] = evt.LastSeen.Unix()
				mycli.loggerWrapper.GetLogger(mycli.userID).LogInfo("[%s] User is now offline since %s", mycli.userID, evt.LastSeen.Format("2006-01-02 15:04:05"))
			}
		} else {
			postMap["state"] = "online"
			mycli.loggerWrapper.GetLogger(mycli.userID).LogInfo("[%s] User is now online", mycli.userID)
		}
	case *events.Archive:
		doWebhook = true
		postMap["event"] = "Archive"

		// postMap["data"] still holds the raw *events.Archive here, so asserting it
		// to a map panicked on every archive/unarchive (issues #95, #101). Build
		// the payload explicitly instead.
		postMap["data"] = map[string]interface{}{
			"JID":          evt.JID,
			"Timestamp":    evt.Timestamp,
			"Action":       evt.Action,
			"FromFullSync": evt.FromFullSync,
		}

		mycli.loggerWrapper.GetLogger(mycli.userID).LogInfo("[%s] Chat archived", mycli.userID)
	case *events.HistorySync:
		doWebhook = true
		postMap["event"] = "HistorySync"

		mycli.loggerWrapper.GetLogger(mycli.userID).LogDebug("[%s] History sync event received %+v", mycli.userID, evt.Data.SyncType)
	case *events.AppState:
		mycli.loggerWrapper.GetLogger(mycli.userID).LogDebug("[%s] App state event received %+v", mycli.userID, evt)
	case *events.LoggedOut:
		if dispatch, chat := mycli.handleLoggedOut(evt, postMap); !dispatch {
			return
		} else {
			doWebhook, eventChat = true, chat
		}
	case *events.ChatPresence:
		doWebhook = true
		postMap["event"] = "ChatPresence"
		mycli.loggerWrapper.GetLogger(mycli.userID).LogDebug("[%s] Chat presence received %+v", mycli.userID, evt)
	case *events.CallOffer:
		doWebhook = true
		postMap["event"] = "CallOffer"

		// Verifica se deve rejeitar chamadas automaticamente
		if mycli.inst().RejectCall {
			mycli.loggerWrapper.GetLogger(mycli.userID).LogInfo("[%s] Auto-rejecting call from %s", mycli.userID, evt.CallCreator.String())

			// An instance with a call engine has already preaccepted this offer, so the
			// engine has to reject it or it keeps the call in its own state.
			if tracked, err := mycli.service.CallEngine().Reject(mycli.userID, evt.CallID); tracked {
				if err != nil {
					mycli.loggerWrapper.GetLogger(mycli.userID).LogWarn("[%s] Auto-reject of call %s ended it here but the peer may not have been told: %v", mycli.userID, evt.CallID, err)
				}
			} else {
				// Rejeita a chamada
				mycli.WAClient.RejectCall(context.Background(), evt.CallCreator, evt.CallID)
			}

			// Envia mensagem de rejeição se configurada
			if mycli.inst().MsgRejectCall != "" {
				msg := &waE2E.Message{
					ExtendedTextMessage: &waE2E.ExtendedTextMessage{
						Text: &mycli.inst().MsgRejectCall,
					},
				}

				_, err := mycli.WAClient.SendMessage(context.Background(), evt.CallCreator, msg)
				if err != nil {
					mycli.loggerWrapper.GetLogger(mycli.userID).LogError("[%s] Failed to send reject call message: %v", mycli.userID, err)
				} else {
					mycli.loggerWrapper.GetLogger(mycli.userID).LogInfo("[%s] Sent reject call message to %s", mycli.userID, evt.CallCreator.String())
				}
			}
			return
		}

		mycli.loggerWrapper.GetLogger(mycli.userID).LogDebug("[%s] Got call offer %+v", mycli.userID, evt)
	case *events.CallAccept:
		doWebhook = true
		postMap["event"] = "CallAccept"
		mycli.loggerWrapper.GetLogger(mycli.userID).LogDebug("[%s] Got call accept %+v", mycli.userID, evt)
	case *events.CallTerminate:
		doWebhook = true
		postMap["event"] = "CallTerminate"
		mycli.loggerWrapper.GetLogger(mycli.userID).LogDebug("[%s] Got call terminate %+v", mycli.userID, evt)
	case *events.CallOfferNotice:
		doWebhook = true
		postMap["event"] = "CallOfferNotice"
		mycli.loggerWrapper.GetLogger(mycli.userID).LogDebug("[%s] Got call offer notice %+v", mycli.userID, evt)
	case *events.CallRelayLatency:
		doWebhook = true
		postMap["event"] = "CallRelayLatency"
		mycli.loggerWrapper.GetLogger(mycli.userID).LogDebug("[%s] Got call relay latency %+v", mycli.userID, evt)
	case *events.OfflineSyncCompleted:
		doWebhook = true
		postMap["event"] = "OfflineSyncCompleted"
	case *events.NotifyAccountReachoutTimelock:
		status := reachoutTimelockFromEvent(evt, time.Now())
		mycli.reachoutTimelock.Store(status)
		if status.Active {
			until := "unknown"
			if status.EndsAt != nil {
				until = status.EndsAt.Format(time.RFC3339)
			}
			mycli.loggerWrapper.GetLogger(mycli.userID).LogWarn("[%s] WhatsApp restricted this account from starting conversations with new contacts (%s) until %s; sends to contacts that never wrote to it fail with error 463", mycli.userID, status.EnforcementType, until)
		} else {
			mycli.loggerWrapper.GetLogger(mycli.userID).LogInfo("[%s] Reachout restriction lifted (%s)", mycli.userID, status.EnforcementType)
		}
		doWebhook = true
		postMap["event"] = "ReachoutTimelock"
		postMap["data"] = status.webhookData()
	case *events.StreamError:
		info := streamErrorFromEvent(evt, time.Now())
		mycli.lastStreamError.Store(info)
		mycli.loggerWrapper.GetLogger(mycli.userID).LogWarn("[%s] WhatsApp sent an unknown stream error (code %q): %s", mycli.userID, info.Code, info.Raw)
		doWebhook = true
		postMap["event"] = "StreamError"
		postMap["data"] = info.webhookData()
	case *events.ClientOutdated:
		mycli.clientOutdatedAt.Store(time.Now().UnixNano())
		// The cached version is the one that was just refused: drop it so the next
		// reconnection looks the current one up instead of retrying the stale one for
		// up to an hour.
		invalidateWebVersionCache()
		pinned := mycli.config.WhatsappVersionMajor != 0 && mycli.config.WhatsappVersionMinor != 0 && mycli.config.WhatsappVersionPatch != 0
		if pinned {
			mycli.loggerWrapper.GetLogger(mycli.userID).LogError("[%s] WhatsApp refused the client version (405) and WHATSAPP_VERSION_* pins it: update or remove those variables", mycli.userID)
		} else {
			mycli.loggerWrapper.GetLogger(mycli.userID).LogError("[%s] WhatsApp refused the client version (405); the version cache was cleared and the next reconnection fetches the current one", mycli.userID)
		}
		doWebhook = true
		postMap["event"] = "ClientOutdated"
		postMap["data"] = map[string]interface{}{"versionPinned": pinned}
	case *events.PairError, *events.QRScannedWithoutMultidevice, *events.CATRefreshError,
		*events.Mute, *events.Pin, *events.Star, *events.MarkChatAsRead, *events.ClearChat,
		*events.DeleteChat, *events.DeleteForMe, *events.UnarchiveChatsSetting, *events.UserStatusMute:
		name, data, publish, _ := pairAndChatEventData(rawEvt)
		switch rawEvt.(type) {
		case *events.PairError:
			mycli.loggerWrapper.GetLogger(mycli.userID).LogError("[%s] Pairing failed: %v", mycli.userID, data["error"])
		case *events.QRScannedWithoutMultidevice:
			mycli.loggerWrapper.GetLogger(mycli.userID).LogWarn("[%s] The QR code was scanned by a phone without multi-device enabled; the same code can be scanned again after enabling it", mycli.userID)
		case *events.CATRefreshError:
			mycli.loggerWrapper.GetLogger(mycli.userID).LogWarn("[%s] CAT refresh failed: %v", mycli.userID, data["error"])
		}
		if !publish {
			// app state replayed by a full sync (e.g. right after pairing): not a change
			return
		}
		doWebhook = true
		postMap["event"] = name
		postMap["data"] = data
	case *events.Blocklist, *events.PrivacySettings, *events.BusinessName,
		*events.CallPreAccept, *events.CallTransport, *events.CallReject, *events.UnknownCallEvent,
		*events.MediaRetry, *events.NewsletterLiveUpdate, *events.NewsletterMuteChange,
		*events.OfflineSyncPreview, *events.RotateADVSecret, *events.ManualLoginReconnect:
		name, data, publish, _ := remainingEventData(rawEvt)
		switch e := rawEvt.(type) {
		case *events.RotateADVSecret:
			mycli.loggerWrapper.GetLogger(mycli.userID).LogInfo("[%s] The session's ADV secret was rotated by WhatsApp", mycli.userID)
		case *events.ManualLoginReconnect:
			mycli.loggerWrapper.GetLogger(mycli.userID).LogWarn("[%s] ManualLoginReconnect received: this project keeps login auto-reconnect on, so this is unexpected", mycli.userID)
		case *events.OfflineSyncPreview:
			mycli.loggerWrapper.GetLogger(mycli.userID).LogInfo("[%s] Offline sync preview: %d items waiting (%d messages, %d notifications, %d receipts, %d app data changes)", mycli.userID, e.Total, e.Messages, e.Notifications, e.Receipts, e.AppDataChanges)
		case *events.MediaRetry:
			mycli.loggerWrapper.GetLogger(mycli.userID).LogWarn("[%s] The sender has to upload media of message %s again (media retry)", mycli.userID, e.MessageID)
		}
		if !publish {
			return
		}
		doWebhook = true
		postMap["event"] = name
		postMap["data"] = data
	case *events.ConnectFailure:
		doWebhook = true
		postMap["event"] = "ConnectFailure"
		mycli.loggerWrapper.GetLogger(mycli.userID).LogInfo("[%s] Connection failed with reason %s", mycli.userID, evt.Reason.String())

		// Limpar cache de userInfo para esta instância
		mycli.userInfoCache.Delete(mycli.inst().Token)
		mycli.loggerWrapper.GetLogger(mycli.userID).LogInfo("[%s] UserInfo cache cleared", mycli.userID)

		mycli.inst().DisconnectReason = evt.Reason.String()
		mycli.inst().Connected = false
		err := mycli.instanceRepository.UpdateConnected(mycli.inst().Id, mycli.inst().Connected, mycli.inst().DisconnectReason)
		if err != nil {
			mycli.loggerWrapper.GetLogger(mycli.userID).LogError("[%s] Error updating instance: %s", mycli.inst().Id, err)
		}
	case *events.Disconnected:
		if shuttingDown.Load() {
			// We are the ones closing it: the instance must stay marked connected so the
			// next start brings it back, and nothing may reconnect it now.
			return
		}
		doWebhook = true
		postMap["event"] = "Disconnected"

		// Limpar cache de userInfo para esta instância (mas não para reconexão automática)
		mycli.userInfoCache.Delete(mycli.inst().Token)
		mycli.loggerWrapper.GetLogger(mycli.userID).LogInfo("[%s] UserInfo cache cleared", mycli.userID)

		mycli.inst().DisconnectReason = "Disconnected emitted because the websocket is closed by the server."
		mycli.inst().Connected = false
		err := mycli.instanceRepository.UpdateConnected(mycli.inst().Id, mycli.inst().Connected, mycli.inst().DisconnectReason)
		if err != nil {
			mycli.loggerWrapper.GetLogger(mycli.userID).LogError("[%s] Error updating instance: %s", mycli.inst().Id, err)
		}

		// Restart the instance (paced by the reconnect backoff, non-blocking)
		mycli.scheduleAutoReconnect("Disconnected detected")
	case *events.KeepAliveTimeout:
		doWebhook = true
		postMap["event"] = "KeepAliveTimeout"
		postMap["data"] = map[string]interface{}{
			"ErrorCount":  evt.ErrorCount,
			"LastSuccess": evt.LastSuccess,
		}
		mycli.loggerWrapper.GetLogger(mycli.userID).LogWarn("[%s] Keepalive ping timed out (%d consecutive, last success: %s)", mycli.userID, evt.ErrorCount, evt.LastSuccess.Format(time.RFC3339))

		// With EnableAutoReconnect disabled, a TCP connection that silently dies
		// keeps timing out keepalives forever without ever emitting
		// events.Disconnected — the instance stays "connected" but is a zombie
		// (PR #126, see also #185). whatsmeow's docs suggest using this event to
		// force a faster disconnect+reconnect, so after 3 consecutive timeouts
		// restart the instance through the same path the Disconnected handler
		// uses. Exact match keeps counts 4, 5, ... from piling up restarts while
		// one is already underway (ReconnectClient also ignores duplicates).
		if evt.ErrorCount == 3 {
			mycli.scheduleAutoReconnect("3 consecutive keepalive timeouts")
		}
	case *events.KeepAliveRestored:
		doWebhook = true
		postMap["event"] = "KeepAliveRestored"
		mycli.loggerWrapper.GetLogger(mycli.userID).LogInfo("[%s] Keepalive pings restored", mycli.userID)
	case *events.LabelEdit:
		doWebhook = true
		postMap["event"] = "LabelEdit"
		mycli.loggerWrapper.GetLogger(mycli.userID).LogDebug("[%s] Got label edit %+v", mycli.userID, evt.Action)

		// A label deleted on the phone used to stay in the local table (and in
		// GET /label/list) forever, because only the upsert existed.
		if evt.Action.GetDeleted() {
			if err := mycli.labelRepository.DeleteLabelByLabelID(mycli.userID, evt.LabelID); err != nil {
				mycli.loggerWrapper.GetLogger(mycli.userID).LogError("[%s] Failed to delete label: %v", mycli.userID, err)
			}
			break
		}

		label := label_model.Label{
			InstanceID:   mycli.userID,
			LabelID:      evt.LabelID,
			LabelName:    evt.Action.GetName(),
			LabelColor:   fmt.Sprintf("%d", evt.Action.GetColor()),
			PredefinedId: fmt.Sprintf("%d", evt.Action.GetPredefinedID()),
		}

		err := mycli.labelRepository.UpsertLabel(label)
		if err != nil {
			mycli.loggerWrapper.GetLogger(mycli.userID).LogError("[%s] Failed to upsert label: %v", mycli.userID, err)
		}
	case *events.LabelAssociationChat:
		doWebhook = true
		postMap["event"] = "LabelAssociationChat"

		mycli.loggerWrapper.GetLogger(mycli.userID).LogDebug("[%s] Label association chat received %+v", mycli.userID, evt)
	case *events.LabelAssociationMessage:
		doWebhook = true
		postMap["event"] = "LabelAssociationMessage"

		mycli.loggerWrapper.GetLogger(mycli.userID).LogDebug("[%s] Label association message received %+v", mycli.userID, evt)
	case *events.Contact:
		doWebhook = true
		postMap["event"] = "Contact"
	case *events.PushName:
		doWebhook = true
		postMap["event"] = "PushName"
	case *events.Picture:
		doWebhook = true
		postMap["event"] = "Picture"
	case *events.UserAbout:
		doWebhook = true
		postMap["event"] = "UserAbout"
	case *events.IdentityChange:
		doWebhook = false
	case *events.GroupInfo:
		doWebhook = true
		postMap["event"] = "GroupInfo"
		groupInfos.forget(groupInfoKey(mycli.userID, evt.JID))
		learnChatTimerFromGroup(mycli.userID, evt.JID, evt.Ephemeral, time.Now())
	case *events.JoinedGroup:
		doWebhook = true
		postMap["event"] = "JoinedGroup"
		groupInfos.forget(groupInfoKey(mycli.userID, evt.JID))
		learnChatTimerFromGroup(mycli.userID, evt.JID, &evt.GroupEphemeral, time.Now())
	case *events.NewsletterJoin:
		doWebhook = true
		postMap["event"] = "NewsletterJoin"
	case *events.NewsletterLeave:
		doWebhook = true
		postMap["event"] = "NewsletterLeave"
	case *events.UndecryptableMessage:
		mycli.loggerWrapper.GetLogger(mycli.userID).LogWarn("[%s] Undecryptable message received: %s (%s)", mycli.userID, evt.Info.ID, evt.UnavailableType)

		if evt.UnavailableType == "view_once" {
			mycli.loggerWrapper.GetLogger(mycli.userID).LogWarn("[%s] Undecryptable message received view_once: %s", mycli.userID, evt.Info.ID)

			doWebhook = true
			postMap["event"] = "Message"

			postMap["data"] = evt
		} else if strings.HasPrefix(evt.Info.ID, "66") || strings.HasPrefix(evt.Info.ID, "67") {
			// The message id is chosen by the sender, so anyone who can message this number
			// could force a reconnect with every such message: at most one per cooldown.
			if !mycli.allowUndecryptReconnect(time.Now()) {
				mycli.loggerWrapper.GetLogger(mycli.userID).LogWarn("[%s] ID 66 or 67 found, but a reconnect was forced less than %s ago: not reconnecting again", mycli.userID, undecryptReconnectCooldown)
			} else {
				mycli.loggerWrapper.GetLogger(mycli.userID).LogError("[%s] ID 66 or 67 found, reconnecting client", mycli.userID)
				mycli.WAClient.Disconnect()
				err := mycli.WAClient.Connect()
				if err != nil {
					mycli.loggerWrapper.GetLogger(mycli.userID).LogError("[%s] Error reconnecting client: %s", mycli.userID, err)
				}
			}
		} else {
			mycli.loggerWrapper.GetLogger(mycli.userID).LogWarn("[%s] ID is not 66 or 67 or view_once, skipping", mycli.userID)
		}

		// Tell the subscriber (view_once is already published above as a Message).
		// Its id/chat/sender are what POST /message/rerequest needs.
		if evt.UnavailableType != "view_once" {
			doWebhook = true
			postMap["event"] = "UndecryptableMessage"
			postMap["data"] = undecryptableEventData(evt)
		}
	default:
		mycli.loggerWrapper.GetLogger(mycli.userID).LogWarn("[%s] Unhandled event %T", mycli.userID, evt)
		return
	}

	if doWebhook {
		// Serializing an event (a history sync, a message with its media) is the costly part;
		// when nobody would receive it, do not.
		if name, _ := postMap["event"].(string); name != "" && !mycli.service.EventWanted(mycli.inst(), name, eventChat) {
			return
		}

		mycli.config.AddInstanceToken(postMap, mycli.token)
		postMap["instanceId"] = mycli.userID
		postMap["instanceName"] = mycli.inst().Name

		values, err := json.Marshal(postMap)
		if err != nil {
			mycli.loggerWrapper.GetLogger(mycli.userID).LogError("[%s] Failed to marshal JSON for queue", mycli.userID)
			return
		}

		var queueName string
		if _, ok := postMap["event"]; ok {
			queueName = strings.ToLower(fmt.Sprintf("%s.%s", userID, postMap["event"]))
		}

		// Log webhook dispatch
		eventType := "unknown"
		if event, ok := postMap["event"].(string); ok {
			eventType = event
		}

		dataSize := len(values)
		mycli.loggerWrapper.GetLogger(mycli.userID).LogDebug("[%s] ===== DISPATCHING WEBHOOK ===== Event: %s, Queue: %s, DataSize: %d bytes", mycli.userID, eventType, queueName, dataSize)

		go mycli.service.CallWebhook(mycli.inst(), queueName, values)

		if mycli.config.AmqpGlobalEnabled || mycli.config.NatsGlobalEnabled {
			mycli.loggerWrapper.GetLogger(mycli.userID).LogDebug("[%s] Sending to global queues - Event: %s, AMQP: %v, NATS: %v", mycli.userID, eventType, mycli.config.AmqpGlobalEnabled, mycli.config.NatsGlobalEnabled)
			go mycli.service.SendToGlobalQueues(postMap["event"].(string), values, mycli.userID)
		}
	} else {
		mycli.loggerWrapper.GetLogger(mycli.userID).LogDebug("[%s] ===== WEBHOOK SKIPPED ===== doWebhook=false", mycli.userID)
	}
}

// CallWebhook delivers an event to the outputs of the instance, if the instance subscribed
// to it (see eventSubscribed).
func (w *whatsmeowService) CallWebhook(instance *instance_model.Instance, queueName string, jsonData []byte) {
	defer recoverAndLog(w.loggerWrapper, instance.Id, "CallWebhook")

	env, ok := readEnvelope(jsonData)
	if !ok {
		return
	}
	if !eventSubscribed(cachedSubscriptions(instance.Events), env.Event, env.chat()) {
		return
	}

	w.loggerWrapper.GetLogger(instance.Id).LogDebug("[%s] Event received of type %s", instance.Id, env.Event)
	w.sendToQueueOrWebhook(instance, queueName, jsonData)
}

func contains(subscriptions []string, event string) bool {
	for _, sub := range subscriptions {
		if strings.EqualFold(sub, event) {
			return true
		}
	}
	return false
}

// sendToQueueOrWebhook hands one event to every output the instance enabled: RabbitMQ,
// NATS, the websocket and the webhook.
//
// The outputs are independent. It used to return at the first error, so a RabbitMQ
// that was down (or a websocket subscriber that could not take the frame) kept the
// webhook from ever being tried; reproduced with rabbitmqEnable set and no broker: the
// webhook got nothing. They also run side by side now, so a producer that is slow (the
// RabbitMQ one reconnects for seconds) does not delay the others.
func (w *whatsmeowService) sendToQueueOrWebhook(instance *instance_model.Instance, queueName string, jsonData []byte) {
	type output struct {
		name string
		send func() error
	}

	var outputs []output
	if instance.RabbitmqEnable == "enabled" || instance.RabbitmqEnable == "true" {
		outputs = append(outputs, output{"rabbitmq", func() error {
			return w.rabbitmqProducer.Produce(queueName, jsonData, instance.RabbitmqEnable, instance.Id)
		}})
	}
	if instance.NatsEnable == "enabled" || instance.NatsEnable == "true" {
		outputs = append(outputs, output{"nats", func() error {
			return w.natsProducer.Produce(queueName, jsonData, instance.NatsEnable, instance.Id)
		}})
	}
	if instance.WebSocketEnable == "enabled" || instance.WebSocketEnable == "true" {
		outputs = append(outputs, output{"websocket", func() error {
			return w.websocketProducer.Produce(queueName, jsonData, instance.Id, instance.Token)
		}})
	}
	if instance.Webhook != "" && instance.Webhook != "disabled" {
		outputs = append(outputs, output{"webhook", func() error {
			return w.webhookProducer.Produce(queueName, jsonData, instance.Webhook, instance.Id)
		}})
	}

	var wg sync.WaitGroup
	for _, out := range outputs {
		wg.Add(1)
		go func(out output) {
			defer wg.Done()
			defer recoverAndLog(w.loggerWrapper, instance.Id, "producer "+out.name)
			if err := out.send(); err != nil {
				w.loggerWrapper.GetLogger(instance.Id).LogError("[%s] Failed to send message to %s: %s", instance.Id, out.name, err)
				return
			}
			w.loggerWrapper.GetLogger(instance.Id).LogDebug("[%s] Message sent to %s successfully", instance.Id, out.name)
		}(out)
	}
	wg.Wait()
}

func (w whatsmeowService) StartInstance(instanceId string) error {
	instance, err := w.instanceRepository.GetInstanceByID(instanceId)
	if err != nil {
		return err
	}

	// Say so right away when another replica runs it (StartClient would only log it).
	if w.owner != nil && !runtimeActive(instanceId) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		free, err := w.owner.Probe(ctx, instanceId)
		cancel()
		if err == nil && !free {
			return fmt.Errorf("%w: %s", utils.ErrOwnedElsewhere, instanceId)
		}
	}

	if instance.Proxy == "" && w.config.ProxyHost != "" && w.config.ProxyPort != "" && w.config.ProxyUsername != "" && w.config.ProxyPassword != "" {
		proxyConfig := ProxyConfig{
			Protocol: utils.NormalizeProxyProtocol(w.config.ProxyProtocol, w.config.ProxyPort),
			Host:     w.config.ProxyHost,
			Port:     w.config.ProxyPort,
			Username: w.config.ProxyUsername,
			Password: w.config.ProxyPassword,
		}

		proxyJSON, err := json.Marshal(proxyConfig)
		if err != nil {
			w.loggerWrapper.GetLogger(instanceId).LogError("[%s] Failed to marshal proxy config: %v", instanceId, err)
			return err
		}

		instance.Proxy = string(proxyJSON)

		err = w.instanceRepository.UpdateProxy(instance.Id, instance.Proxy)
		if err != nil {
			w.loggerWrapper.GetLogger(instanceId).LogError("[%s] Failed to update instance: %s", instanceId, err)
			return err
		}
	}

	w.loggerWrapper.GetLogger(instanceId).LogInfo("[%s] Starting client", instance.Id)

	v := Values{map[string]string{
		"Id":     instance.Id,
		"Jid":    instance.Jid,
		"Token":  instance.Token,
		"Events": instance.Events,
		"osName": instance.OsName,
		"Proxy":  instance.Proxy,
	}}

	w.userInfoCache.Set(instance.Token, v, cache.NoExpiration)

	eventArray := strings.Split(instance.Events, ",")

	var subscribedEvents []string

	if len(eventArray) < 1 {
		subscribedEvents = append(subscribedEvents, event_types.MESSAGE)
	} else {
		for _, arg := range eventArray {
			if !event_types.IsEventType(arg) {
				w.loggerWrapper.GetLogger(instanceId).LogWarn("[%s] Message type discarded: %s", instanceId, arg)
				continue
			}
			if !utils.Find(subscribedEvents, arg) {
				subscribedEvents = append(subscribedEvents, arg)
			}

		}
	}

	clientData := &ClientData{
		Instance:      instance,
		Subscriptions: subscribedEvents,
		Phone:         "",
		IsProxy:       false,
	}

	if instance.Proxy != "" {
		var proxyConfig ProxyConfig
		err := json.Unmarshal([]byte(instance.Proxy), &proxyConfig)
		if err != nil {
			w.loggerWrapper.GetLogger(instanceId).LogError("[%s] error unmarshalling proxy config", instanceId)
			return err
		}

		if proxyConfig.Host != "" {
			clientData.IsProxy = true
		}
	}

	go w.StartClient(clientData)

	return nil
}

func (w whatsmeowService) CanAutoStart(instanceId string) error {
	instance, err := w.instanceRepository.GetInstanceByID(instanceId)
	if err != nil {
		return nil // StartInstance reports a missing instance
	}
	if !instance.Connected && instance.DisconnectReason == instance_repository.DisconnectedByAPIReason {
		return utils.ErrDisconnectedByUser
	}
	// No paired device (never paired, QR codes that ran out, logged out from the phone): a
	// request cannot use it, and starting it would only begin a new round of QR codes.
	if !instance.Connected && instance.Jid == "" {
		return utils.ErrNotLoggedIn
	}
	return nil
}

func (w whatsmeowService) ConnectOnStartup(clientName string) {
	w.loggerWrapper.GetLogger(clientName).LogInfo("Connecting all instances on startup")
	var instances []*instance_model.Instance
	var err error

	if clientName != "" {
		instances, err = w.instanceRepository.GetAllConnectedInstancesByClientName(clientName)
		if err != nil {
			w.loggerWrapper.GetLogger(clientName).LogError("[%s] Error getting all connected instances: %s", clientName, err)
			return
		}
	} else {
		instances, err = w.instanceRepository.GetAllConnectedInstances()
		if err != nil {
			w.loggerWrapper.GetLogger(clientName).LogError("[%s] Error getting all connected instances: %s", clientName, err)
			return
		}
	}

	w.loggerWrapper.GetLogger(clientName).LogInfo("[%s] Found %d connected instances", clientName, len(instances))

	stagger := startupStagger()
	for i, instance := range instances {
		// Pace the boot: see startupStagger.
		if i > 0 && !sleepOrShutdown(staggerWithJitter(stagger)) {
			w.loggerWrapper.GetLogger(clientName).LogInfo("[%s] Shutting down, the remaining instances are not started", clientName)
			return
		}
		w.loggerWrapper.GetLogger(clientName).LogInfo("[%s] Starting client for user '%s'", clientName, instance.Id)

		err := w.StartInstance(instance.Id)
		if err != nil {
			w.loggerWrapper.GetLogger(clientName).LogError("[%s] Error starting client: %s", clientName, err)
		}
	}
}

func getExtensionFromMimeType(mimeType string) string {
	switch mimeType {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "video/mp4":
		return ".mp4"
	case "audio/ogg":
		return ".ogg"
	case "audio/mpeg":
		return ".mp3"
	case "application/pdf":
		return ".pdf"
	case "application/vnd.openxmlformats-officedocument.wordprocessingml.document":
		return ".docx"
	case "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":
		return ".xlsx"
	case "application/vnd.openxmlformats-officedocument.presentationml.presentation":
		return ".pptx"
	default:
		// Se não encontrar um tipo conhecido, extrai a extensão do mimetype
		parts := strings.Split(mimeType, "/")
		if len(parts) > 1 {
			return "." + parts[1]
		}
		return ".bin"
	}
}

// globalEventTypeFor maps a whatsmeow event name (the "event" field of the
// payload) to the global event group used by AMQP_GLOBAL_EVENTS and
// NATS_GLOBAL_EVENTS. It returns "" for events that have no group.
//
// AMQP and NATS used to carry two hand-copied switches that drifted apart:
// PICTURE, USER_ABOUT and BUTTON_CLICK were accepted by NATS_GLOBAL_EVENTS but
// silently never published (issue #193). Keep a single source of truth here.
func globalEventTypeFor(eventType string) string {
	switch eventType {
	case "Message", "UndecryptableMessage", "MediaRetry":
		return "MESSAGE"
	case "SendMessage":
		return "SEND_MESSAGE"
	case "Receipt":
		return "READ_RECEIPT"
	case "Presence":
		return "PRESENCE"
	case "HistorySync":
		return "HISTORY_SYNC"
	case "ChatPresence", "Archive", "Mute", "Pin", "Star", "MarkChatAsRead", "ClearChat", "DeleteChat", "DeleteForMe", "UnarchiveChatsSetting", "UserStatusMute":
		return "CHAT_PRESENCE"
	case "CallOffer", "CallAccept", "CallTerminate", "CallOfferNotice", "CallRelayLatency", "CallPreAccept", "CallReject", "CallTransport", "UnknownCallEvent", "CallReady", "CallEnded", "CallVideoState", "CallMediaStalled", "CallMediaResumed":
		return "CALL"
	case "Connected", "PairSuccess", "TemporaryBan", "LoggedOut", "ConnectFailure", "Disconnected", "KeepAliveTimeout", "KeepAliveRestored", "ReachoutTimelock", "StreamError", "ClientOutdated", "CATRefreshError", "OfflineSyncPreview":
		return "CONNECTION"
	case "LabelEdit", "LabelAssociationChat", "LabelAssociationMessage":
		return "LABEL"
	case "Contact", "PushName", "Blocklist", "PrivacySettings", "BusinessName":
		return "CONTACT"
	case "Picture":
		return "PICTURE"
	case "UserAbout":
		return "USER_ABOUT"
	case "GroupInfo", "JoinedGroup":
		return "GROUP"
	case "NewsletterJoin", "NewsletterLeave", "NewsletterLiveUpdate", "NewsletterMuteChange":
		return "NEWSLETTER"
	case "QRCode", "QRTimeout", "QRSuccess", "PasskeyRequest", "PasskeyConfirmation", "PasskeyError", "PairError", "QRScannedWithoutMultidevice":
		return "QRCODE"
	case "ButtonClick":
		return "BUTTON_CLICK"
	default:
		return ""
	}
}

func (w *whatsmeowService) SendToGlobalQueues(eventType string, payload []byte, userId string) {
	defer recoverAndLog(w.loggerWrapper, userId, "SendToGlobalQueues")
	w.loggerWrapper.GetLogger(userId).LogDebug("[%s] Starting sendToGlobalQueues for event: %s", userId, eventType)

	// AMQP: AMQP_SPECIFIC_EVENTS tem prioridade sobre AMQP_GLOBAL_EVENTS
	if w.config.AmqpGlobalEnabled {
		var shouldSendToAmqp bool
		var amqpQueueName string

		// Se AMQP_SPECIFIC_EVENTS estiver configurada, ela tem prioridade
		if len(w.config.AmqpSpecificEvents) > 0 {
			w.loggerWrapper.GetLogger(userId).LogDebug("[%s] Using AMQP_SPECIFIC_EVENTS (priority over AMQP_GLOBAL_EVENTS)", userId)
			// Verifica se o evento específico está na lista
			if utils.Find(w.config.AmqpSpecificEvents, eventType) {
				shouldSendToAmqp = true
				amqpQueueName = strings.ToLower(eventType)
				w.loggerWrapper.GetLogger(userId).LogDebug("[%s] Event %s found in AMQP_SPECIFIC_EVENTS", userId, eventType)
			}
		} else {
			// Fallback para AMQP_GLOBAL_EVENTS (modo antigo com grupos de eventos)
			w.loggerWrapper.GetLogger(userId).LogDebug("[%s] Using AMQP_GLOBAL_EVENTS (fallback mode)", userId)

			// Mapeia o evento do Whatsmeow para o tipo de evento global
			globalEventType := globalEventTypeFor(eventType)
			if globalEventType == "" {
				w.loggerWrapper.GetLogger(userId).LogDebug("[%s] Event %s not mapped to global event type", userId, eventType)
				return
			}

			// Verifica se o grupo de eventos está na lista
			if utils.Find(w.config.AmqpGlobalEvents, globalEventType) {
				shouldSendToAmqp = true
				amqpQueueName = strings.ToLower(eventType)
				w.loggerWrapper.GetLogger(userId).LogDebug("[%s] Event group %s found in AMQP_GLOBAL_EVENTS", userId, globalEventType)
			}
		}

		// Envia para RabbitMQ se necessário
		if shouldSendToAmqp {
			w.loggerWrapper.GetLogger(userId).LogDebug("[%s] Sending to AMQP queue: %s", userId, amqpQueueName)
			err := w.rabbitmqProducer.Produce(amqpQueueName, payload, "global", userId)
			if err != nil {
				w.loggerWrapper.GetLogger(userId).LogError("[%s] Failed to send message to RabbitMQ global queue %s: %v", userId, amqpQueueName, err)
			} else {
				w.loggerWrapper.GetLogger(userId).LogDebug("[%s] Successfully sent message to RabbitMQ global queue %s", userId, amqpQueueName)
			}
		} else {
			w.loggerWrapper.GetLogger(userId).LogDebug("[%s] Event %s not configured for AMQP", userId, eventType)
		}
	}

	// NATS: Mantém o comportamento original por enquanto (só NATS_GLOBAL_EVENTS)
	if w.config.NatsGlobalEnabled {
		// Mapeia o evento para grupo (necessário para NATS por enquanto)
		globalEventType := globalEventTypeFor(eventType)

		// Verifica se o evento está na lista de eventos globais NATS
		if globalEventType != "" && utils.Find(w.config.NatsGlobalEvents, globalEventType) {
			queueName := strings.ToLower(eventType)
			w.loggerWrapper.GetLogger(userId).LogDebug("[%s] Sending to NATS subject: %s", userId, queueName)

			err := w.natsProducer.Produce(queueName, payload, "global", userId)
			if err != nil {
				w.loggerWrapper.GetLogger(userId).LogError("[%s] Failed to send message to NATS global subject %s: %v", userId, queueName, err)
			} else {
				w.loggerWrapper.GetLogger(userId).LogDebug("[%s] Successfully sent message to NATS global subject %s", userId, queueName)
			}
		}
	}
}

var (
	cachedWebVersion   *clientVersion
	cachedWebVersionAt time.Time
	cachedWebVersionMu sync.Mutex
	webVersionCacheTTL = 1 * time.Hour
)

func fetchWhatsAppWebVersion() (*clientVersion, error) {
	cachedWebVersionMu.Lock()
	defer cachedWebVersionMu.Unlock()

	if cachedWebVersion != nil && time.Since(cachedWebVersionAt) < webVersionCacheTTL {
		return cachedWebVersion, nil
	}

	resp, err := utils.FixedURLClient.Get("https://web.whatsapp.com/sw.js")
	if err != nil {
		return nil, fmt.Errorf("failed to fetch WhatsApp Web version: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %v", err)
	}

	content := string(body)

	// Múltiplas estratégias para encontrar client_revision
	patterns := []string{
		`"client_revision":\s*(\d+)`,              // Formato direto
		`\\"client_revision\\":\s*(\d+)`,          // Formato escaped
		`client_revision\\?\\"?:[\s]*(\d+)`,       // Formato mais flexível
		`["']client_revision["'][\s]*:[\s]*(\d+)`, // Com aspas variadas
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindStringSubmatch(content)

		if len(matches) >= 2 {
			clientRevision, err := strconv.Atoi(matches[1])
			if err != nil {
				continue // Tenta próximo padrão
			}

			// Log qual padrão funcionou
			if clientRevision > 0 {
				cachedWebVersion = &clientVersion{
					Major: 2,
					Minor: 3000,
					Patch: clientRevision,
				}
				cachedWebVersionAt = time.Now()
				return cachedWebVersion, nil
			}
		}
	}

	// Se chegou aqui, nenhum padrão funcionou - log do conteúdo para debug
	// Mostra apenas uma parte para não logar muito
	previewLength := 500
	if len(content) > previewLength {
		content = content[:previewLength] + "..."
	}

	return nil, fmt.Errorf("could not find client revision in the fetched content. Content preview: %s", content)
}

func (w whatsmeowService) UpdateInstanceSettings(instanceId string) error {
	w.loggerWrapper.GetLogger(instanceId).LogInfo("[%s] Updating instance settings in runtime", instanceId)

	// Busca a instância atualizada do banco
	instance, err := w.instanceRepository.GetInstanceByID(instanceId)
	if err != nil {
		w.loggerWrapper.GetLogger(instanceId).LogError("[%s] Error getting instance from DB: %v", instanceId, err)
		return err
	}

	// Verifica se o MyClient existe
	myClient, exists := w.myClientPointer.Lookup(instanceId)
	if !exists {
		w.loggerWrapper.GetLogger(instanceId).LogWarn("[%s] MyClient not found in runtime, instance may not be connected", instanceId)
		return apierror.Invalid(fmt.Sprintf("instance %s not found in runtime", instanceId))
	}

	// Atualiza as configurações no MyClient em execução
	myClient.setInst(instance)

	// Atualiza o cache do userInfo com as novas configurações
	v := Values{map[string]string{
		"Id":     instance.Id,
		"Jid":    instance.Jid,
		"Token":  instance.Token,
		"Events": instance.Events,
		"osName": instance.OsName,
		"Proxy":  instance.Proxy,
	}}
	w.userInfoCache.Set(instance.Token, v, cache.NoExpiration)

	w.loggerWrapper.GetLogger(instanceId).LogInfo("[%s] Instance settings and cache updated in runtime successfully", instanceId)
	return nil
}

func (w whatsmeowService) UpdateInstanceAdvancedSettings(instanceId string) error {
	w.loggerWrapper.GetLogger(instanceId).LogInfo("[%s] Updating advanced settings in runtime", instanceId)

	// Busca a instância atualizada do banco
	instance, err := w.instanceRepository.GetInstanceByID(instanceId)
	if err != nil {
		w.loggerWrapper.GetLogger(instanceId).LogError("[%s] Error getting instance from DB: %v", instanceId, err)
		return err
	}

	// Verifica se o MyClient existe
	myClient, exists := w.myClientPointer.Lookup(instanceId)
	if !exists {
		w.loggerWrapper.GetLogger(instanceId).LogWarn("[%s] MyClient not found in runtime, instance may not be connected", instanceId)
		return apierror.Invalid(fmt.Sprintf("instance %s not found in runtime", instanceId))
	}

	// Atualiza a instância no MyClient com as advanced settings atualizadas
	previous := myClient.inst()
	wasAlwaysOnline := previous != nil && previous.AlwaysOnline
	myClient.setInst(instance)

	// The presence scheduler and the "available"/"unavailable" mark used to happen only
	// on the Connected event, so switching alwaysOnline at runtime did nothing until the
	// next reconnect.
	if wasAlwaysOnline != instance.AlwaysOnline {
		applyAlwaysOnlineChange(myClient, instance.AlwaysOnline)
	}

	w.loggerWrapper.GetLogger(instanceId).LogInfo("[%s] Advanced settings updated in runtime successfully", instanceId)
	return nil
}

func (w whatsmeowService) ClearInstanceCache(instanceId string, token string) error {
	w.loggerWrapper.GetLogger(instanceId).LogInfo("[%s] Clearing instance cache", instanceId)

	// Limpar userInfoCache
	w.userInfoCache.Delete(token)
	forgetInstanceChatTimers(instanceId)
	groupInfos.forgetInstance(instanceId)
	autoReconnect.forget(instanceId)

	// Limpar myClientPointer se existir
	if _, exists := w.myClientPointer.Lookup(instanceId); exists {
		w.myClientPointer.Delete(instanceId)
		w.loggerWrapper.GetLogger(instanceId).LogInfo("[%s] MyClient pointer cleared", instanceId)
	}
	w.callEngine.Detach(instanceId)

	// Limpar clientPointer se existir
	if _, exists := w.clientPointer.Lookup(instanceId); exists {
		w.clientPointer.Delete(instanceId)
		w.loggerWrapper.GetLogger(instanceId).LogInfo("[%s] Client pointer cleared", instanceId)
	}

	// Limpar killChannel se existir
	if killChan, exists := w.killChannel.Lookup(instanceId); exists {
		// Closing is the "stop for good" signal; sending `true` would make the
		// supervisor restart the client.
		close(killChan)
		w.killChannel.Delete(instanceId)
		w.loggerWrapper.GetLogger(instanceId).LogInfo("[%s] Kill channel cleared", instanceId)
	}

	w.loggerWrapper.GetLogger(instanceId).LogInfo("[%s] Instance cache completely cleared", instanceId)
	return nil
}

func NewWhatsmeowService(
	instanceRepository instance_repository.InstanceRepository,
	authDB *sql.DB,
	messageRepository message_repository.MessageRepository,
	labelRepository label_repository.LabelRepository,
	config *config.Config,
	killChannel *safemap.Map[chan bool],
	clientPointer *safemap.Map[*whatsmeow.Client],
	rabbitmqProducer producer_interfaces.Producer,
	webhookProducer producer_interfaces.Producer,
	websocketProducer producer_interfaces.Producer,
	sqliteDB *sql.DB,
	exPath string,
	mediaStorage storage_interfaces.MediaStorage,
	natsProducer producer_interfaces.Producer,
	loggerWrapper *logger_wrapper.LoggerManager,
) WhatsmeowService {
	// Inicializar PollService de forma segura
	pollSvc := poll_service.NewPollService(authDB, loggerWrapper)

	svc := &whatsmeowService{
		instanceRepository: instanceRepository,
		authDB:             authDB,
		messageRepository:  messageRepository,
		labelRepository:    labelRepository,
		pollService:        pollSvc, // NOVO: Serviço de enquetes
		config:             config,
		killChannel:        killChannel,
		userInfoCache:      cache.New(5*time.Minute, 10*time.Minute),
		clientPointer:      clientPointer,
		myClientPointer:    safemap.New[*MyClient](),
		rabbitmqProducer:   rabbitmqProducer,
		webhookProducer:    webhookProducer,
		websocketProducer:  websocketProducer,
		sqliteDB:           sqliteDB,
		exPath:             exPath,
		mediaStorage:       mediaStorage,
		processedMessages:  cache.New(30*time.Minute, 1*time.Hour),
		natsProducer:       natsProducer,
		loggerWrapper:      loggerWrapper,
		passkeyCeremony:    ceremony.NewStore(),
	}
	svc.callEngine = call_engine.NewManager(call_engine.Options{
		MaxConcurrent:    config.CallMaxConcurrent,
		RingTimeout:      time.Duration(config.CallRingTimeout) * time.Second,
		StreamGrace:      time.Duration(config.CallStreamGrace) * time.Second,
		DialsPerMinute:   config.CallDialLimit,
		MediaStall:       time.Duration(config.CallMediaStall) * time.Second,
		MediaStallHangup: config.CallMediaStallHangup,
		MaxDuration:      time.Duration(config.CallMaxDuration) * time.Second,
		SilenceTimeout:   time.Duration(config.CallSilenceTimeout) * time.Second,
		Notify:           svc.publishCallEvent,
	})
	return svc
}

// GetPollService retorna o serviço de polls (evita dupla inicialização)
// ProxyStatus reports what the running client did with its proxy.
func (w *whatsmeowService) ProxyStatus(instanceId string) (ProxyRuntimeStatus, bool) {
	return GetProxyRuntimeStatus(instanceId)
}

func (w *whatsmeowService) GetPollService() poll_service.PollService {
	return w.pollService
}

// CallEngine exposes the per-instance call engine registry.
func (w *whatsmeowService) CallEngine() *call_engine.Manager { return w.callEngine }

// PasskeyCeremonyStore exposes the shared ceremony store so the public HTTP
// polling endpoint can read the current stage for a given ceremony token.
func (w *whatsmeowService) PasskeyCeremonyStore() *ceremony.Store {
	return w.passkeyCeremony
}

// SubmitPasskeyResponse forwards the browser's WebAuthn assertion to WhatsApp
// for the given instance. Called by POST /passkey-ceremony/{token}/response.
func (w *whatsmeowService) SubmitPasskeyResponse(instanceId string, resp *types.WebAuthnResponse) error {
	client, ok := w.clientPointer.Lookup(instanceId)
	if !ok || client == nil {
		return fmt.Errorf("no active client for instance %s", instanceId)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := client.SendPasskeyResponse(ctx, resp); err != nil {
		w.passkeyCeremony.SetError(instanceId, err.Error())
		return err
	}
	// Server will asynchronously emit PairPasskeyConfirmation (or Error) into
	// the event handler; move to the waiting stage in the meantime.
	w.passkeyCeremony.SetAwaitingConfirmation(instanceId)
	return nil
}

// ConfirmPasskey finishes the pairing after the user verified the code.
// Called by POST /passkey-ceremony/{token}/confirm.
func (w *whatsmeowService) ConfirmPasskey(instanceId string) error {
	client, ok := w.clientPointer.Lookup(instanceId)
	if !ok || client == nil {
		return fmt.Errorf("no active client for instance %s", instanceId)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := client.SendPasskeyConfirmation(ctx); err != nil {
		w.passkeyCeremony.SetError(instanceId, err.Error())
		return err
	}
	w.passkeyCeremony.SetConfirmed(instanceId)
	return nil
}

// cleanSenderID remove a parte ":numero" do sender ID para exibir apenas o remoteJid correto
// Exemplo: "557499879409:3@s.whatsapp.net" -> "557499879409@s.whatsapp.net"
func cleanSenderID(senderID string) string {
	// Procura pelo padrão ":numero" antes do @
	if colonIndex := strings.Index(senderID, ":"); colonIndex != -1 {
		if atIndex := strings.Index(senderID, "@"); atIndex != -1 && colonIndex < atIndex {
			// Remove a parte ":numero" mantendo apenas o número principal e o domínio
			return senderID[:colonIndex] + senderID[atIndex:]
		}
	}
	return senderID
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

// stickerAsPNG converts a received WebP sticker to PNG for the webhook payload. The sticker
// comes from any WhatsApp contact, and a crafted one can declare enormous dimensions (a
// decoder allocates for what the header says), so its size is checked before it is decoded.
func stickerAsPNG(data []byte) ([]byte, error) {
	if err := utils.CheckImageDimensions(data, utils.MaxStickerPixels); err != nil {
		return nil, err
	}
	img, err := webp.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
