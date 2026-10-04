package instance_service

import (
	"sort"

	producer_interfaces "github.com/lucasgiovannibr/whatygo/pkg/events/interfaces"
	instance_model "github.com/lucasgiovannibr/whatygo/pkg/instance/model"
	whatsmeow_service "github.com/lucasgiovannibr/whatygo/pkg/whatsmeow/service"
)

// DatabaseState is what the database says about the instance.
type DatabaseState struct {
	Connected        bool   `json:"connected"`
	DisconnectReason string `json:"disconnectReason,omitempty"`
	Jid              string `json:"jid,omitempty"`
	AlwaysOnline     bool   `json:"alwaysOnline"`
	CallsEnabled     bool   `json:"callsEnabled"`
}

// RuntimeDiagnostics compares what the database says about an instance with what
// this process is actually running for it.
type RuntimeDiagnostics struct {
	InstanceID string `json:"instanceId"`
	Name       string `json:"name,omitempty"`
	// Database is nil for a runtime whose instance no longer exists.
	Database *DatabaseState                `json:"database"`
	Runtime  whatsmeow_service.RuntimeInfo `json:"runtime"`
	// Warnings are the runtime's own plus the database-vs-runtime mismatches.
	Warnings []whatsmeow_service.Warning `json:"warnings"`
}

// RuntimesReport is the answer of GET /instance/runtimes.
type RuntimesReport struct {
	Process whatsmeow_service.ProcessInfo `json:"process"`
	// Webhook is the state of the webhook delivery queues: what is waiting, and what
	// was dropped because a receiver could not keep up.
	Webhook   *producer_interfaces.WebhookStats `json:"webhook,omitempty"`
	Summary   RuntimesSummary                   `json:"summary"`
	Instances []RuntimeDiagnostics              `json:"instances"`
}

type RuntimesSummary struct {
	Instances    int `json:"instances"`
	Connected    int `json:"connected"`
	WithWarnings int `json:"withWarnings"`
}

// diagnose builds the diagnostics for one instance. instance is nil when the
// process still runs something for an instance that is gone from the database.
func diagnose(instance *instance_model.Instance, rt whatsmeow_service.RuntimeInfo) RuntimeDiagnostics {
	d := RuntimeDiagnostics{InstanceID: rt.InstanceID, Runtime: rt, Warnings: []whatsmeow_service.Warning{}}
	d.Warnings = append(d.Warnings, rt.Warnings...)

	add := func(code, msg string) {
		d.Warnings = append(d.Warnings, whatsmeow_service.Warning{Code: code, Message: msg})
	}

	if instance == nil {
		add("runtime_for_deleted_instance", "this process is still running a client for an instance that does not exist in the database")
		return d
	}

	d.Name = instance.Name
	d.Database = &DatabaseState{
		Connected:        instance.Connected,
		DisconnectReason: instance.DisconnectReason,
		Jid:              instance.Jid,
		AlwaysOnline:     instance.AlwaysOnline,
		CallsEnabled:     instance.CallsEnabled,
	}

	// The call engine is created when the client starts, so a change of the flag only
	// reaches a client that is already running after its next connection.
	if rt.ClientRegistered {
		switch {
		case instance.CallsEnabled && rt.Calls == nil:
			add("calls_enabled_pending_reconnect", "calls are enabled for the instance but the running client started without a call engine (reconnect the instance to apply)")
		case !instance.CallsEnabled && rt.Calls != nil:
			add("calls_disabled_pending_reconnect", "calls are disabled for the instance but the running client still has a call engine (reconnect the instance to apply)")
		}
	}

	switch {
	case instance.Connected && !rt.WebsocketConnected:
		add("db_connected_runtime_offline", "the database says connected but the websocket is down")
	case !instance.Connected && rt.WebsocketConnected && rt.LoggedIn:
		add("db_disconnected_runtime_online", "the database says disconnected but the client is connected and logged in")
	}

	if instance.Jid != "" && rt.ClientRegistered && rt.DeviceJID == "" {
		add("paired_in_db_unpaired_runtime", "the database has a paired JID but the running client is an unpaired device (the session was lost or replaced)")
	}

	if instance.Jid != "" && !rt.ClientRegistered && !rt.RuntimeActive {
		add("paired_without_runtime", "the instance is paired but nothing is running for it (not connected; call /instance/connect)")
	}

	return d
}

// buildReport assembles the report from the database rows and the runtime
// snapshots. Runtimes without a database row are reported too.
func buildReport(rows []*instance_model.Instance, runtimes []whatsmeow_service.RuntimeInfo, process whatsmeow_service.ProcessInfo) *RuntimesReport {
	byID := make(map[string]whatsmeow_service.RuntimeInfo, len(runtimes))
	for _, r := range runtimes {
		byID[r.InstanceID] = r
	}

	report := &RuntimesReport{Process: process, Instances: []RuntimeDiagnostics{}}
	seen := map[string]bool{}

	for _, row := range rows {
		rt, ok := byID[row.Id]
		if !ok {
			rt = whatsmeow_service.RuntimeInfo{InstanceID: row.Id, Warnings: []whatsmeow_service.Warning{}}
		}
		seen[row.Id] = true
		report.Instances = append(report.Instances, diagnose(row, rt))
	}

	orphans := make([]string, 0)
	for id := range byID {
		if !seen[id] {
			orphans = append(orphans, id)
		}
	}
	sort.Strings(orphans)
	for _, id := range orphans {
		report.Instances = append(report.Instances, diagnose(nil, byID[id]))
	}

	for _, d := range report.Instances {
		report.Summary.Instances++
		if d.Runtime.WebsocketConnected && d.Runtime.LoggedIn {
			report.Summary.Connected++
		}
		if len(d.Warnings) > 0 {
			report.Summary.WithWarnings++
		}
	}
	return report
}

func (i instances) GetRuntime(id string) (*RuntimeDiagnostics, error) {
	instance, err := i.instanceRepository.GetInstanceByID(id)
	if err != nil {
		return nil, err
	}
	d := diagnose(instance, i.whatsmeowService.RuntimeInfo(id))
	return &d, nil
}

func (i instances) GetRuntimes() (*RuntimesReport, error) {
	rows, err := i.instanceRepository.GetAll(i.config.ClientName)
	if err != nil {
		return nil, err
	}
	report := buildReport(rows, i.whatsmeowService.RuntimeInfos(), whatsmeow_service.GetProcessInfo())
	report.Webhook = i.whatsmeowService.WebhookStats()
	return report, nil
}
