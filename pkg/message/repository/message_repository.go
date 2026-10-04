package message_repository

import (
	message_model "github.com/lucasgiovannibr/whatygo/pkg/message/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type MessageRepository interface {
	InsertMessage(message message_model.Message) error
	// InsertMessages writes many messages in one statement (upsert, status only moves forward).
	InsertMessages(messages []message_model.Message) error
	// QueueMessage hands a message to the background batch writer and returns at once. It
	// reports false when the queue is full and the message was dropped.
	QueueMessage(message message_model.Message) bool
	GetMessageByID(instanceID, messageID string) (*message_model.Message, error)
	// Close writes what is still queued and stops the batch writer.
	Close()
}

type messageRepository struct {
	db     *gorm.DB
	writer *batchWriter
}

// statusRank orders the states a message goes through. Receipts arrive out of order (and
// are retried), and every write used to overwrite the previous status, so a late
// "Delivered" could turn a message that was already "Read" back into "Delivered".
var statusRank = map[string]int{"Received": 1, "Delivered": 2, "Read": 3}

func rank(status string) int { return statusRank[status] }

// rankSQL is statusRank as a SQL expression over the given column.
func rankSQL(column string) string {
	return "CASE " + column + " WHEN 'Read' THEN 3 WHEN 'Delivered' THEN 2 WHEN 'Received' THEN 1 ELSE 0 END"
}

// onConflict updates an existing row only when the new status is not behind the stored one;
// a missing referral never erases the stored one.
func onConflict() clause.OnConflict {
	return clause.OnConflict{
		Columns: []clause.Column{{Name: "instance_id"}, {Name: "message_id"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"timestamp": gorm.Expr("excluded.timestamp"),
			"status":    gorm.Expr("excluded.status"),
			"source":    gorm.Expr("excluded.source"),
			"referral":  gorm.Expr("COALESCE(excluded.referral, messages.referral)"),
		}),
		Where: clause.Where{Exprs: []clause.Expression{
			clause.Expr{SQL: rankSQL("messages.status") + " <= " + rankSQL("excluded.status")},
		}},
	}
}

func (m *messageRepository) InsertMessage(message message_model.Message) error {
	return m.InsertMessages([]message_model.Message{message})
}

func (m *messageRepository) InsertMessages(messages []message_model.Message) error {
	messages = coalesce(messages)
	if len(messages) == 0 {
		return nil
	}
	return m.db.Clauses(onConflict()).Create(&messages).Error
}

// coalesce collapses messages that share a key: one INSERT .. ON CONFLICT cannot touch the
// same row twice. The furthest status wins; a referral is kept from whichever had one.
func coalesce(messages []message_model.Message) []message_model.Message {
	if len(messages) < 2 {
		return messages
	}
	type key struct{ instance, id string }
	index := make(map[key]int, len(messages))
	out := make([]message_model.Message, 0, len(messages))
	for _, msg := range messages {
		k := key{msg.InstanceID, msg.MessageID}
		at, seen := index[k]
		if !seen {
			index[k] = len(out)
			out = append(out, msg)
			continue
		}
		kept := out[at]
		referral := kept.Referral
		if rank(msg.Status) >= rank(kept.Status) {
			kept = msg
			if len(kept.Referral) == 0 {
				kept.Referral = referral
			}
		} else if len(kept.Referral) == 0 {
			kept.Referral = msg.Referral
		}
		out[at] = kept
	}
	return out
}

func (m *messageRepository) QueueMessage(message message_model.Message) bool {
	return m.writer.enqueue(message)
}

func (m *messageRepository) Close() { m.writer.close() }

// GetMessageByID looks a message up inside one instance: it used to search every
// instance's rows, so any instance token could read the status of another one's messages.
func (m *messageRepository) GetMessageByID(instanceID, messageID string) (*message_model.Message, error) {
	var message message_model.Message
	err := m.db.Where("instance_id = ? AND message_id = ?", instanceID, messageID).First(&message).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}

	return &message, nil
}

func NewMessageRepository(db *gorm.DB) MessageRepository {
	m := &messageRepository{db: db}
	m.writer = newBatchWriter(m.InsertMessages)
	return m
}
