package whatsmeow_service

import (
	"testing"

	call_engine "github.com/lucasgiovannibr/whatygo/pkg/call/engine"
	"github.com/lucasgiovannibr/whatygo/pkg/config"
	"github.com/lucasgiovannibr/whatygo/pkg/safemap"
	"go.mau.fi/whatsmeow"
)

func codes(ws []Warning) map[string]bool {
	m := map[string]bool{}
	for _, w := range ws {
		m[w.Code] = true
	}
	return m
}

func TestRuntimeWarnings(t *testing.T) {
	t.Run("healthy connected instance has no warnings", func(t *testing.T) {
		w := runtimeWarnings(RuntimeInfo{ClientRegistered: true, RuntimeActive: true, KillChannel: true, SupervisorCurrent: true, WebsocketConnected: true, LoggedIn: true, DeviceJID: "5511@s.whatsapp.net", QRMax: 5})
		if len(w) != 0 {
			t.Fatalf("unexpected warnings: %v", w)
		}
	})

	t.Run("runtime without client", func(t *testing.T) {
		c := codes(runtimeWarnings(RuntimeInfo{RuntimeActive: true, KillChannel: true}))
		if !c["runtime_without_client"] {
			t.Fatalf("got %v", c)
		}
	})

	t.Run("orphaned client and mismatched supervisor", func(t *testing.T) {
		if c := codes(runtimeWarnings(RuntimeInfo{ClientRegistered: true})); !c["client_without_runtime"] {
			t.Fatalf("got %v", c)
		}
		if c := codes(runtimeWarnings(RuntimeInfo{ClientRegistered: true, RuntimeActive: true, KillChannel: true})); !c["supervisor_mismatch"] {
			t.Fatalf("got %v", c)
		}
	})

	t.Run("no kill channel", func(t *testing.T) {
		if c := codes(runtimeWarnings(RuntimeInfo{ClientRegistered: true, RuntimeActive: true, SupervisorCurrent: true})); !c["no_kill_channel"] {
			t.Fatalf("got %v", c)
		}
	})

	t.Run("paired but offline, unless reconnecting", func(t *testing.T) {
		base := RuntimeInfo{ClientRegistered: true, RuntimeActive: true, KillChannel: true, SupervisorCurrent: true, DeviceJID: "5511@s.whatsapp.net"}
		if c := codes(runtimeWarnings(base)); !c["paired_but_offline"] {
			t.Fatalf("got %v", c)
		}
		base.ReconnectInProgress = true
		if c := codes(runtimeWarnings(base)); c["paired_but_offline"] {
			t.Fatalf("must not warn while a reconnect is in flight: %v", c)
		}
	})

	t.Run("qr limit near, unless a passkey ceremony is running", func(t *testing.T) {
		base := RuntimeInfo{ClientRegistered: true, RuntimeActive: true, KillChannel: true, SupervisorCurrent: true, WebsocketConnected: true, QRCount: 4, QRMax: 5}
		if c := codes(runtimeWarnings(base)); !c["qr_limit_near"] {
			t.Fatalf("got %v", c)
		}
		base.PasskeyCeremonyActive = true
		if c := codes(runtimeWarnings(base)); c["qr_limit_near"] {
			t.Fatalf("got %v", c)
		}
	})
}

func newInfoService() *whatsmeowService {
	return &whatsmeowService{
		config:          &config.Config{QrcodeMaxCount: 5},
		clientPointer:   safemap.New[*whatsmeow.Client](),
		myClientPointer: safemap.New[*MyClient](),
		killChannel:     safemap.New[chan bool](),
	}
}

// The runtime slot is what prevents two runtimes per instance; the snapshot must
// show it, and show the instance even though no client is registered yet.
func TestRuntimeInfoSeesTheRuntimeSlot(t *testing.T) {
	const id = "info-test"
	releaseRuntime(id)
	w := newInfoService()

	if got := w.RuntimeInfo(id); got.RuntimeActive || got.ClientRegistered || len(got.Warnings) != 0 {
		t.Fatalf("an unknown instance must be empty and clean: %#v", got)
	}

	acquireRuntime(id)
	defer releaseRuntime(id)

	info := w.RuntimeInfo(id)
	if !info.RuntimeActive || info.ClientRegistered {
		t.Fatalf("unexpected snapshot: %#v", info)
	}
	c := codes(info.Warnings)
	if !c["runtime_without_client"] || !c["no_kill_channel"] {
		t.Fatalf("expected the startup inconsistencies, got %v", c)
	}

	found := false
	for _, i := range w.RuntimeInfos() {
		if i.InstanceID == id {
			found = true
		}
	}
	if !found {
		t.Fatal("RuntimeInfos must list instances known only by their runtime slot")
	}
}

func TestProcessInfo(t *testing.T) {
	p := GetProcessInfo()
	if p.Goroutines < 1 || p.GoVersion == "" || p.SysMB == 0 {
		t.Fatalf("unexpected process info: %#v", p)
	}
}

func TestRuntimeWarningsCallEngine(t *testing.T) {
	up := RuntimeInfo{ClientRegistered: true, RuntimeActive: true, KillChannel: true, SupervisorCurrent: true, WebsocketConnected: true, LoggedIn: true, DeviceJID: "5511@s.whatsapp.net", QRMax: 5}

	with := func(st call_engine.Status) RuntimeInfo {
		i := up
		i.Calls = &st
		return i
	}

	if w := runtimeWarnings(with(call_engine.Status{State: call_engine.StateActive})); len(w) != 0 {
		t.Fatalf("an active call engine must not warn: %v", w)
	}
	if c := codes(runtimeWarnings(with(call_engine.Status{State: call_engine.StateHookFailed, Error: "layout changed"}))); !c["calls_hook_failed"] {
		t.Fatalf("got %v", c)
	}
	if c := codes(runtimeWarnings(with(call_engine.Status{State: call_engine.StateBlockedProxy}))); !c["calls_blocked_by_proxy"] {
		t.Fatalf("got %v", c)
	}
}

// Services built without a call engine (every test in this package) still report a
// runtime, with no calls section.
func TestRuntimeInfoWithoutACallEngine(t *testing.T) {
	info := newInfoService().RuntimeInfo("no-engine")
	if info.Calls != nil {
		t.Fatalf("Calls = %+v, want nil", info.Calls)
	}
}
