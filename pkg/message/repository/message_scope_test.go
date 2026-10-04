package message_repository

import (
	"bytes"
	"log"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	message_model "github.com/lucasgiovannibr/whatygo/pkg/message/model"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func dryRunDB(t *testing.T, logBuffer *bytes.Buffer) *gorm.DB {
	t.Helper()
	sqlDB, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, WithoutReturning: true}), &gorm.Config{
		DryRun:                 true,
		SkipDefaultTransaction: true,
		Logger:                 gormlogger.New(log.New(logBuffer, "", 0), gormlogger.Config{LogLevel: gormlogger.Info}),
	})
	if err != nil {
		t.Fatal(err)
	}
	return db
}

// Two instances in the same group receive the same message id: each keeps its own row.
func TestInsertMessageConflictsPerInstance(t *testing.T) {
	var out bytes.Buffer
	repo := NewMessageRepository(dryRunDB(t, &out))

	if err := repo.InsertMessage(message_model.Message{InstanceID: "inst-1", MessageID: "m1", Status: "Received"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `ON CONFLICT ("instance_id","message_id")`) {
		t.Fatalf("the upsert must conflict on (instance_id, message_id), got %q", out.String())
	}
}

// Reproduced on the test stack: /message/status answered with another instance's row.
func TestGetMessageByIDIsScopedToTheInstance(t *testing.T) {
	var out bytes.Buffer
	repo := NewMessageRepository(dryRunDB(t, &out))

	_, _ = repo.GetMessageByID("inst-1", "m1")
	sql := out.String()
	if !strings.Contains(sql, `instance_id = 'inst-1'`) || !strings.Contains(sql, `message_id = 'm1'`) {
		t.Fatalf("the lookup must filter by instance and message id, got %q", sql)
	}
}
