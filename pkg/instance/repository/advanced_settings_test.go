package instance_repository

import (
	"testing"

	instance_model "github.com/lucasgiovannibr/whatygo/pkg/instance/model"
)

func TestBuildAdvancedSettingsUpdates(t *testing.T) {
	trueVal := true

	t.Run("only AlwaysOnline", func(t *testing.T) {
		updates := buildAdvancedSettingsUpdates(&instance_model.AdvancedSettings{
			AlwaysOnline: &trueVal,
		})
		if len(updates) != 1 {
			t.Fatalf("expected only always_online, got %#v", updates)
		}
		if updates["always_online"] != true {
			t.Fatalf("always_online = %#v, want true", updates["always_online"])
		}
	})

	t.Run("explicit false is written", func(t *testing.T) {
		falseVal := false
		updates := buildAdvancedSettingsUpdates(&instance_model.AdvancedSettings{
			IgnoreGroups: &falseVal,
		})
		if len(updates) != 1 {
			t.Fatalf("expected only ignore_groups, got %#v", updates)
		}
		if updates["ignore_groups"] != false {
			t.Fatalf("ignore_groups = %#v, want false", updates["ignore_groups"])
		}
	})

	t.Run("msgRejectCall non-empty", func(t *testing.T) {
		updates := buildAdvancedSettingsUpdates(&instance_model.AdvancedSettings{
			MsgRejectCall: "busy",
		})
		if updates["msg_reject_call"] != "busy" {
			t.Fatalf("msg_reject_call = %#v, want busy", updates["msg_reject_call"])
		}
	})

	t.Run("nil settings", func(t *testing.T) {
		updates := buildAdvancedSettingsUpdates(nil)
		if len(updates) != 0 {
			t.Fatalf("expected empty map, got %#v", updates)
		}
	})
}

func TestBuildAdvancedSettingsUpdatesCallsEnabled(t *testing.T) {
	on, off := true, false

	updates := buildAdvancedSettingsUpdates(&instance_model.AdvancedSettings{CallsEnabled: &on})
	if len(updates) != 1 || updates["calls_enabled"] != true {
		t.Fatalf("got %#v, want only calls_enabled=true", updates)
	}

	updates = buildAdvancedSettingsUpdates(&instance_model.AdvancedSettings{CallsEnabled: &off})
	if len(updates) != 1 || updates["calls_enabled"] != false {
		t.Fatalf("explicit false must be written, got %#v", updates)
	}

	if updates = buildAdvancedSettingsUpdates(&instance_model.AdvancedSettings{}); len(updates) != 0 {
		t.Fatalf("an omitted callsEnabled must not touch the column, got %#v", updates)
	}
}
