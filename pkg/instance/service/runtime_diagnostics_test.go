package instance_service

import (
	"testing"

	call_engine "github.com/lucasgiovannibr/whatygo/pkg/call/engine"
	instance_model "github.com/lucasgiovannibr/whatygo/pkg/instance/model"
	whatsmeow_service "github.com/lucasgiovannibr/whatygo/pkg/whatsmeow/service"
)

func codesOf(d RuntimeDiagnostics) map[string]bool {
	m := map[string]bool{}
	for _, w := range d.Warnings {
		m[w.Code] = true
	}
	return m
}

func rt(id string, mod func(*whatsmeow_service.RuntimeInfo)) whatsmeow_service.RuntimeInfo {
	r := whatsmeow_service.RuntimeInfo{InstanceID: id, Warnings: []whatsmeow_service.Warning{}}
	if mod != nil {
		mod(&r)
	}
	return r
}

func TestDiagnoseConsistentInstance(t *testing.T) {
	inst := &instance_model.Instance{Id: "a", Name: "a", Connected: true, Jid: "5511:1@s.whatsapp.net"}
	d := diagnose(inst, rt("a", func(r *whatsmeow_service.RuntimeInfo) {
		r.ClientRegistered, r.RuntimeActive, r.KillChannel, r.SupervisorCurrent = true, true, true, true
		r.WebsocketConnected, r.LoggedIn, r.DeviceJID = true, true, "5511:1@s.whatsapp.net"
	}))
	if len(d.Warnings) != 0 {
		t.Fatalf("a consistent instance must have no warnings: %v", d.Warnings)
	}
}

// The state seen in the first live test: paired in the database, but the running
// client was a brand-new unpaired device and the database said "disconnected".
func TestDiagnoseLostSessionAfterDuplicateRuntime(t *testing.T) {
	inst := &instance_model.Instance{Id: "a", Connected: false, Jid: "5511:1@s.whatsapp.net"}
	d := diagnose(inst, rt("a", func(r *whatsmeow_service.RuntimeInfo) {
		r.ClientRegistered, r.RuntimeActive, r.KillChannel, r.SupervisorCurrent = true, true, true, true
		r.WebsocketConnected = true // connected, but as a new device: no JID, not logged in
	}))
	if !codesOf(d)["paired_in_db_unpaired_runtime"] {
		t.Fatalf("got %v", d.Warnings)
	}
}

func TestDiagnoseMismatches(t *testing.T) {
	up := rt("a", func(r *whatsmeow_service.RuntimeInfo) {
		r.ClientRegistered, r.RuntimeActive, r.KillChannel, r.SupervisorCurrent = true, true, true, true
		r.WebsocketConnected, r.LoggedIn, r.DeviceJID = true, true, "5511:1@s.whatsapp.net"
	})
	if c := codesOf(diagnose(&instance_model.Instance{Id: "a", Connected: false, Jid: "5511:1@s.whatsapp.net"}, up)); !c["db_disconnected_runtime_online"] {
		t.Fatalf("got %v", c)
	}

	down := rt("a", func(r *whatsmeow_service.RuntimeInfo) {
		r.ClientRegistered, r.RuntimeActive, r.KillChannel, r.SupervisorCurrent = true, true, true, true
		r.ReconnectInProgress = true
	})
	if c := codesOf(diagnose(&instance_model.Instance{Id: "a", Connected: true}, down)); !c["db_connected_runtime_offline"] {
		t.Fatalf("got %v", c)
	}

	if c := codesOf(diagnose(&instance_model.Instance{Id: "a", Jid: "5511:1@s.whatsapp.net"}, rt("a", nil))); !c["paired_without_runtime"] {
		t.Fatalf("got %v", c)
	}
}

func TestBuildReportIncludesRuntimesOfDeletedInstances(t *testing.T) {
	rows := []*instance_model.Instance{{Id: "a", Name: "a"}}
	runtimes := []whatsmeow_service.RuntimeInfo{
		rt("a", nil),
		rt("ghost", func(r *whatsmeow_service.RuntimeInfo) { r.ClientRegistered, r.RuntimeActive = true, true }),
	}

	report := buildReport(rows, runtimes, whatsmeow_service.ProcessInfo{Goroutines: 10})
	if len(report.Instances) != 2 || report.Summary.Instances != 2 {
		t.Fatalf("unexpected report: %#v", report)
	}

	ghost := report.Instances[1]
	if ghost.InstanceID != "ghost" || ghost.Database != nil || !codesOf(ghost)["runtime_for_deleted_instance"] {
		t.Fatalf("a runtime without a database row must be flagged: %#v", ghost)
	}
	if report.Summary.WithWarnings != 1 || report.Process.Goroutines != 10 {
		t.Fatalf("unexpected summary: %#v", report.Summary)
	}
}

func TestDiagnoseCallsFlagVersusRunningClient(t *testing.T) {
	running := func(mod func(*whatsmeow_service.RuntimeInfo)) whatsmeow_service.RuntimeInfo {
		return rt("a", func(r *whatsmeow_service.RuntimeInfo) {
			r.ClientRegistered, r.RuntimeActive, r.KillChannel, r.SupervisorCurrent = true, true, true, true
			r.WebsocketConnected, r.LoggedIn, r.DeviceJID = true, true, "5511:1@s.whatsapp.net"
			if mod != nil {
				mod(r)
			}
		})
	}
	inst := func(calls bool) *instance_model.Instance {
		return &instance_model.Instance{Id: "a", Connected: true, Jid: "5511:1@s.whatsapp.net", CallsEnabled: calls}
	}

	// The flag was switched on after the client started: it only applies on the next connection.
	if c := codesOf(diagnose(inst(true), running(nil))); !c["calls_enabled_pending_reconnect"] {
		t.Fatalf("got %v", c)
	}

	// ... and the other way round.
	active := running(func(r *whatsmeow_service.RuntimeInfo) { r.Calls = &call_engine.Status{State: call_engine.StateActive} })
	if c := codesOf(diagnose(inst(false), active)); !c["calls_disabled_pending_reconnect"] {
		t.Fatalf("got %v", c)
	}

	// Flag and engine agree: nothing to report.
	if d := diagnose(inst(true), active); len(d.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", d.Warnings)
	}
	if d := diagnose(inst(false), running(nil)); len(d.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", d.Warnings)
	}

	// An instance that is not running has nothing to apply the flag to yet.
	if c := codesOf(diagnose(inst(true), rt("a", nil))); c["calls_enabled_pending_reconnect"] {
		t.Fatalf("a stopped instance must not report a pending flag: %v", c)
	}

	if d := diagnose(inst(true), active); d.Database == nil || !d.Database.CallsEnabled {
		t.Fatalf("database state must carry callsEnabled: %+v", d.Database)
	}
}
