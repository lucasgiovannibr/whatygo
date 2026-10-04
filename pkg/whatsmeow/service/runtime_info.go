package whatsmeow_service

import (
	call_engine "github.com/lucasgiovannibr/whatygo/pkg/call/engine"
	producer_interfaces "github.com/lucasgiovannibr/whatygo/pkg/events/interfaces"
	"runtime"
	"sort"
	"time"
)

// Warning is one inconsistency found in the runtime state. Code is stable and
// meant for monitors; Message is for humans.
type Warning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// RuntimeInfo is a read-only snapshot of what this process is actually doing for
// one instance. The database says what the instance SHOULD be; this says what it
// IS. Bugs like the duplicated runtime (two clients for one instance) or an
// orphaned supervisor were only visible by reading logs; every inconsistency of
// that kind is reported in Warnings.
type RuntimeInfo struct {
	InstanceID string `json:"instanceId"`

	// A whatsmeow client is registered for the instance.
	ClientRegistered bool `json:"clientRegistered"`
	// The client's websocket is connected / the device is logged in.
	WebsocketConnected bool   `json:"websocketConnected"`
	LoggedIn           bool   `json:"loggedIn"`
	DeviceJID          string `json:"deviceJid,omitempty"`

	// A StartClient run (the supervisor loop) owns the instance.
	RuntimeActive bool `json:"runtimeActive"`
	// The channel that Disconnect / QR teardown use to stop the runtime exists.
	KillChannel bool `json:"killChannel"`
	// The registered supervisor state belongs to the registered client.
	SupervisorCurrent bool `json:"supervisorCurrent"`
	// A ReconnectClient is in flight.
	ReconnectInProgress bool `json:"reconnectInProgress"`

	QRCount               int  `json:"qrCount"`
	QRMax                 int  `json:"qrMax"`
	PasskeyCeremonyActive bool `json:"passkeyCeremonyActive"`

	ConnectedSince *time.Time `json:"connectedSince,omitempty"`
	LastEventType  string     `json:"lastEventType,omitempty"`
	LastEventAt    *time.Time `json:"lastEventAt,omitempty"`
	EventsSeen     uint64     `json:"eventsSeen"`

	Proxy *ProxyRuntimeStatus `json:"proxy,omitempty"`

	// Calls is the state of the call engine; absent when the instance runs without one
	// (calls not enabled for it).
	Calls *call_engine.Status `json:"calls,omitempty"`

	// Operational events reported by WhatsApp (see operational_events.go).
	ReachoutTimelock *ReachoutTimelockStatus `json:"reachoutTimelock,omitempty"`
	LastStreamError  *StreamErrorInfo        `json:"lastStreamError,omitempty"`
	ClientOutdatedAt *time.Time              `json:"clientOutdatedAt,omitempty"`

	Warnings []Warning `json:"warnings"`
}

func nsToTime(ns int64) *time.Time {
	if ns == 0 {
		return nil
	}
	t := time.Unix(0, ns)
	return &t
}

// RuntimeInfo returns the snapshot for one instance.
func (w *whatsmeowService) RuntimeInfo(instanceID string) RuntimeInfo {
	info := RuntimeInfo{InstanceID: instanceID, Warnings: []Warning{}}

	client := w.clientPointer.Get(instanceID)
	mycli := w.myClientPointer.Get(instanceID)

	info.ClientRegistered = client != nil
	info.RuntimeActive = runtimeActive(instanceID)
	info.KillChannel = w.killChannel.Get(instanceID) != nil
	_, info.ReconnectInProgress = reconnecting.Load(instanceID)
	info.QRMax = w.config.QrcodeMaxCount

	if client != nil {
		info.WebsocketConnected = client.IsConnected()
		info.LoggedIn = client.IsLoggedIn()
		if client.Store != nil && client.Store.ID != nil {
			info.DeviceJID = client.Store.ID.String()
		}
	}

	info.SupervisorCurrent = client != nil && mycli != nil && mycli.WAClient == client
	if mycli != nil {
		info.QRCount = int(mycli.qrcodeCount.Load())
		info.ConnectedSince = nsToTime(mycli.connectedAt.Load())
		info.LastEventAt = nsToTime(mycli.lastEventAt.Load())
		if v, ok := mycli.lastEventType.Load().(string); ok {
			info.LastEventType = v
		}
		info.EventsSeen = mycli.eventCount.Load()
		info.ReachoutTimelock = mycli.reachoutTimelock.Load()
		info.LastStreamError = mycli.lastStreamError.Load()
		info.ClientOutdatedAt = nsToTime(mycli.clientOutdatedAt.Load())
		if mycli.passkeyCeremony != nil {
			info.PasskeyCeremonyActive = mycli.passkeyCeremony.HasActiveByInstance(instanceID)
		}
	}

	if st, ok := GetProxyRuntimeStatus(instanceID); ok {
		info.Proxy = &st
	}

	if st, ok := w.callEngine.Status(instanceID); ok {
		info.Calls = &st
	}

	info.Warnings = runtimeWarnings(info)
	return info
}

// RuntimeInfos returns the snapshot of every instance this process knows about: a
// registered client, a supervisor state, or a running supervisor.
func (w *whatsmeowService) RuntimeInfos() []RuntimeInfo {
	ids := map[string]struct{}{}
	for id := range w.clientPointer.Snapshot() {
		ids[id] = struct{}{}
	}
	for id := range w.myClientPointer.Snapshot() {
		ids[id] = struct{}{}
	}
	runtimeSlots.Range(func(k, _ interface{}) bool {
		ids[k.(string)] = struct{}{}
		return true
	})

	sorted := make([]string, 0, len(ids))
	for id := range ids {
		sorted = append(sorted, id)
	}
	sort.Strings(sorted)

	out := make([]RuntimeInfo, 0, len(sorted))
	for _, id := range sorted {
		out = append(out, w.RuntimeInfo(id))
	}
	return out
}

// runtimeWarnings derives the inconsistencies from a snapshot. Pure function so it
// can be unit tested.
func runtimeWarnings(i RuntimeInfo) []Warning {
	return runtimeWarningsAt(i, time.Now())
}

// recentEventWindow is how long a stream error or a version refusal keeps being
// reported as a warning.
const recentEventWindow = 30 * time.Minute

func runtimeWarningsAt(i RuntimeInfo, now time.Time) []Warning {
	out := []Warning{}
	add := func(code, msg string) { out = append(out, Warning{Code: code, Message: msg}) }

	switch {
	case i.RuntimeActive && !i.ClientRegistered:
		add("runtime_without_client", "a runtime owns the instance but no client is registered (normal for a few seconds while starting; if it persists the start is stuck)")
	case i.ClientRegistered && !i.RuntimeActive:
		add("client_without_runtime", "a client is registered but no supervisor loop is running for it (orphaned client: kill/QR teardown will not reach it)")
	}

	if i.RuntimeActive && i.ClientRegistered && !i.SupervisorCurrent {
		add("supervisor_mismatch", "the registered supervisor state does not belong to the registered client")
	}

	if i.RuntimeActive && !i.KillChannel {
		add("no_kill_channel", "the runtime has no kill channel: Disconnect and QR teardown cannot stop it")
	}

	if i.ClientRegistered && i.DeviceJID != "" && !i.WebsocketConnected && !i.ReconnectInProgress {
		add("paired_but_offline", "the device is paired but the websocket is down and no reconnect is in progress")
	}

	if i.ClientRegistered && i.DeviceJID == "" && i.QRMax > 0 && i.QRCount >= i.QRMax-1 && !i.PasskeyCeremonyActive {
		add("qr_limit_near", "waiting for a QR scan and the QR limit is about to be reached (the runtime will restart)")
	}

	if i.ReachoutTimelock.InEffect(now) {
		add("reachout_timelock_active", "WhatsApp restricted this account from starting conversations with new contacts: sends to contacts that never wrote to it fail with error 463")
	}

	if i.LastStreamError != nil && now.Sub(i.LastStreamError.At) < recentEventWindow {
		add("recent_stream_error", "WhatsApp sent an unknown stream error recently (code "+i.LastStreamError.Code+"); the connection may drop")
	}

	if i.Calls != nil {
		switch i.Calls.State {
		case call_engine.StateHookFailed:
			add("calls_hook_failed", "calls are enabled but the call engine could not hook into whatsmeow, so calls get no media: "+i.Calls.Error)
		case call_engine.StateBlockedProxy:
			add("calls_blocked_by_proxy", "calls are enabled but the instance uses a proxy and call media would bypass it, so calls are off for this instance")
		}
	}

	if i.ClientOutdatedAt != nil && now.Sub(*i.ClientOutdatedAt) < recentEventWindow {
		add("client_outdated", "WhatsApp refused the client version (405) recently; check WHATSAPP_VERSION_* or update the image")
	}

	return out
}

// ProcessInfo describes the process itself. A goroutine count that only ever grows
// is how a leak (one supervisor per reconnect, once) shows up.
type ProcessInfo struct {
	UptimeSeconds int64  `json:"uptimeSeconds"`
	Goroutines    int    `json:"goroutines"`
	HeapAllocMB   uint64 `json:"heapAllocMb"`
	SysMB         uint64 `json:"sysMb"`
	NumGC         uint32 `json:"numGc"`
	GoVersion     string `json:"goVersion"`
}

var processStart = time.Now()

// GetProcessInfo returns the current process statistics.
func GetProcessInfo() ProcessInfo {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return ProcessInfo{
		UptimeSeconds: int64(time.Since(processStart).Seconds()),
		Goroutines:    runtime.NumGoroutine(),
		HeapAllocMB:   m.HeapAlloc / (1 << 20),
		SysMB:         m.Sys / (1 << 20),
		NumGC:         m.NumGC,
		GoVersion:     runtime.Version(),
	}
}

// WebhookStats reports the webhook delivery queues, or nil when the producer does not
// keep any.
func (w *whatsmeowService) WebhookStats() *producer_interfaces.WebhookStats {
	sp, ok := w.webhookProducer.(producer_interfaces.StatsProducer)
	if !ok {
		return nil
	}
	st := sp.WebhookStats()
	return &st
}
