package whatsmeow_service

import (
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	instance_repository "github.com/lucasgiovannibr/whatygo/pkg/instance/repository"
	"github.com/lucasgiovannibr/whatygo/pkg/utils"
)

const testInstanceID = "4f3c6282-c561-4056-b530-401a5669ac85"

func autoStartService(t *testing.T, connected bool, reason string) whatsmeowService {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	gdb, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "instances" WHERE id = $1`)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "connected", "disconnect_reason"}).AddRow(testInstanceID, connected, reason))
	return whatsmeowService{instanceRepository: instance_repository.NewInstanceRepository(gdb)}
}

// An instance disconnected through the API stays off: the next request that needs a client
// must not silently reconnect it.
func TestCanAutoStartRefusesInstancesDisconnectedByTheUser(t *testing.T) {
	w := autoStartService(t, false, instance_repository.DisconnectedByAPIReason)
	if err := w.CanAutoStart(testInstanceID); !errors.Is(err, utils.ErrDisconnectedByUser) {
		t.Fatalf("got %v", err)
	}
}

func TestCanAutoStartAllowsOtherStates(t *testing.T) {
	for name, tc := range map[string]struct {
		connected bool
		reason    string
	}{
		"connected":       {true, ""},
		"lost connection": {false, "Disconnected emitted because the websocket is closed by the server."},
		"reconnecting":    {false, instance_repository.ReconnectingReason},
		"never connected": {false, ""},
	} {
		t.Run(name, func(t *testing.T) {
			if err := autoStartService(t, tc.connected, tc.reason).CanAutoStart(testInstanceID); err != nil {
				t.Fatalf("got %v", err)
			}
		})
	}
}
