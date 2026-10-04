package poll_service

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/lucasgiovannibr/whatygo/pkg/config"
	logger_wrapper "github.com/lucasgiovannibr/whatygo/pkg/logger"
	"github.com/lucasgiovannibr/whatygo/pkg/poll/model"
)

func newMockPollService(t *testing.T) (*pollService, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	cfg := &config.Config{LogDirectory: t.TempDir()}
	return &pollService{db: db, loggerWrapper: logger_wrapper.NewLoggerManagerForTest(t, cfg)}, mock
}

// A vote is unique per instance: two instances that receive the same poll must each
// keep their own row.
func TestSavePollVoteConflictsPerInstance(t *testing.T) {
	s, mock := newMockPollService(t)
	mock.ExpectExec(`ON CONFLICT \(instance_id, poll_message_id, voter_jid\)`).WillReturnResult(sqlmock.NewResult(0, 1))

	err := s.SavePollVote(context.Background(), &model.PollVote{
		InstanceID: "inst-1", PollMessageID: "P1", VoterJid: "5511999990001@s.whatsapp.net",
		SelectedOptions: []string{"h1"}, VotedAt: time.Now(), ReceivedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMigrationMakesTheUniquenessPerInstance(t *testing.T) {
	s, mock := newMockPollService(t)
	mock.ExpectExec(`(?s)unique_vote_per_poll_instance ON poll_votes\(instance_id, poll_message_id, voter_jid\).*DROP CONSTRAINT IF EXISTS unique_vote_per_poll`).
		WillReturnResult(sqlmock.NewResult(0, 0))

	if err := s.autoMigrate(); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteInstanceVotes(t *testing.T) {
	s, mock := newMockPollService(t)
	mock.ExpectExec(`DELETE FROM poll_votes WHERE instance_id = \$1`).WithArgs("inst-1").WillReturnResult(sqlmock.NewResult(0, 3))

	if err := s.DeleteInstanceVotes(context.Background(), "inst-1"); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}

	// nothing to do without an id or a database
	if err := s.DeleteInstanceVotes(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	var nilSvc *pollService
	if err := nilSvc.DeleteInstanceVotes(context.Background(), "x"); err != nil {
		t.Fatal(err)
	}
}
