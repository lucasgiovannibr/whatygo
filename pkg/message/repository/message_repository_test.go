package message_repository

import (
	"bytes"
	"encoding/json"
	"log"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	message_model "github.com/lucasgiovannibr/whatygo/pkg/message/model"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func TestInsertMessagePreservesReferralOnStatusUpdate(t *testing.T) {
	sqlDB, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open sqlmock db: %v", err)
	}
	defer sqlDB.Close()

	var logBuffer bytes.Buffer
	gormDB, err := gorm.Open(postgres.New(postgres.Config{
		Conn:             sqlDB,
		WithoutReturning: true,
	}), &gorm.Config{
		DryRun:                 true,
		SkipDefaultTransaction: true,
		Logger: gormlogger.New(
			log.New(&logBuffer, "", 0),
			gormlogger.Config{LogLevel: gormlogger.Info, Colorful: false},
		),
	})
	if err != nil {
		t.Fatalf("open gorm db: %v", err)
	}

	repo := NewMessageRepository(gormDB)
	referral := json.RawMessage(`{"ctwaClid":"abc123","showAdAttribution":true}`)

	initial := message_model.Message{
		InstanceID: "inst-1",
		MessageID:  "msg-1",
		Timestamp:  "2026-05-09 10:00:00",
		Status:     "Received",
		Source:     "1551999999999",
		Referral:   referral,
	}

	if err := repo.InsertMessage(initial); err != nil {
		t.Fatalf("insert initial message: %v", err)
	}

	sqlText := logBuffer.String()
	// A message without a referral must not erase the stored one, and one with a referral
	// must be able to set it.
	if !strings.Contains(sqlText, `"referral"=COALESCE(excluded.referral, messages.referral)`) {
		t.Fatalf("expected upsert to keep the stored referral, got %q", sqlText)
	}
	if !strings.Contains(sqlText, `"timestamp"=excluded.timestamp`) || !strings.Contains(sqlText, `"status"=excluded.status`) {
		t.Fatalf("expected upsert to keep core columns, got %q", sqlText)
	}
	// The status only moves forward.
	if !strings.Contains(sqlText, `WHERE CASE messages.status`) || !strings.Contains(sqlText, `<= CASE excluded.status`) {
		t.Fatalf("expected the upsert to be guarded by the status rank, got %q", sqlText)
	}
}
