package call_history

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	call_engine "github.com/lucasgiovannibr/whatygo/pkg/call/engine"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	// DefaultLimit and MaxLimit are the page sizes of List.
	DefaultLimit = 50
	MaxLimit     = 200
)

// ErrInvalidQuery: a filter or the cursor of a list request is not valid.
var ErrInvalidQuery = errors.New("invalid call history query")

// Query is a list request. The zero value is the newest page, unfiltered.
type Query struct {
	// Direction is "incoming" or "outgoing".
	Direction string
	// Outcome is one of call_engine.Outcomes.
	Outcome string
	// Peer matches a call by the JID WhatsApp reported or by the phone number (with or
	// without the + and punctuation).
	Peer string
	// Limit is the page size; zero is DefaultLimit.
	Limit int
	// Cursor is the Next of the previous page.
	Cursor string
}

// Page is a page of the history, newest first. Next is empty on the last page.
type Page struct {
	Records []Record `json:"records"`
	Next    string   `json:"next,omitempty"`
}

// Repository stores and reads the history.
type Repository interface {
	// Save stores a call; saving the same call of the same instance again is ignored.
	Save(r Record) error
	// List returns a page of the history of one instance, newest first.
	List(instanceID string, q Query) (Page, error)
	// Delete erases the history of one instance: all of it, or what started before the
	// given time when it is not zero. It returns how many records went.
	Delete(instanceID string, before time.Time) (int64, error)
	// Purge erases what started before the given time, for every instance.
	Purge(before time.Time) (int64, error)
}

type repository struct{ db *gorm.DB }

func NewRepository(db *gorm.DB) Repository { return &repository{db: db} }

func (r *repository) Save(rec Record) error {
	return r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "instance_id"}, {Name: "call_id"}},
		DoNothing: true,
	}).Create(&rec).Error
}

func (r *repository) List(instanceID string, q Query) (Page, error) {
	if q.Direction != "" && q.Direction != string(call_engine.Incoming) && q.Direction != string(call_engine.Outgoing) {
		return Page{}, fmt.Errorf("%w: direction must be incoming or outgoing", ErrInvalidQuery)
	}
	if q.Outcome != "" && !validOutcome(q.Outcome) {
		return Page{}, fmt.Errorf("%w: outcome must be one of %s", ErrInvalidQuery, strings.Join(call_engine.Outcomes, ", "))
	}
	limit := q.Limit
	switch {
	case limit < 0 || limit > MaxLimit:
		return Page{}, fmt.Errorf("%w: limit must be between 1 and %d", ErrInvalidQuery, MaxLimit)
	case limit == 0:
		limit = DefaultLimit
	}

	tx := r.db.Where("instance_id = ?", instanceID)
	if q.Direction != "" {
		tx = tx.Where("direction = ?", q.Direction)
	}
	if q.Outcome != "" {
		tx = tx.Where("outcome = ?", q.Outcome)
	}
	if q.Peer != "" {
		tx = tx.Where("peer = ? OR peer_phone = ?", strings.TrimSpace(q.Peer), digits(q.Peer))
	}
	if q.Cursor != "" {
		at, id, err := decodeCursor(q.Cursor)
		if err != nil {
			return Page{}, err
		}
		// keyset pagination: stable while calls are being added at the top
		tx = tx.Where("(started_at, id) < (?, ?)", at, id)
	}

	var rows []Record
	// one more than asked for tells whether there is a next page
	if err := tx.Order("started_at DESC, id DESC").Limit(limit + 1).Find(&rows).Error; err != nil {
		return Page{}, err
	}
	page := Page{Records: rows}
	if len(rows) > limit {
		page.Records = rows[:limit]
		last := page.Records[limit-1]
		page.Next = encodeCursor(last.StartedAt, last.Id)
	}
	if page.Records == nil {
		page.Records = []Record{}
	}
	return page, nil
}

func (r *repository) Delete(instanceID string, before time.Time) (int64, error) {
	tx := r.db.Where("instance_id = ?", instanceID)
	if !before.IsZero() {
		tx = tx.Where("started_at < ?", before)
	}
	res := tx.Delete(&Record{})
	return res.RowsAffected, res.Error
}

func (r *repository) Purge(before time.Time) (int64, error) {
	res := r.db.Where("started_at < ?", before).Delete(&Record{})
	return res.RowsAffected, res.Error
}

func validOutcome(o string) bool {
	for _, v := range call_engine.Outcomes {
		if v == o {
			return true
		}
	}
	return false
}

// digits keeps the digits of a phone number typed any way (+55 (31) 9...).
func digits(s string) string {
	var b strings.Builder
	for _, c := range s {
		if c >= '0' && c <= '9' {
			b.WriteRune(c)
		}
	}
	return b.String()
}

func encodeCursor(startedAt time.Time, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(startedAt.UnixMicro(), 10) + ":" + id))
}

func decodeCursor(cursor string) (time.Time, string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("%w: cursor", ErrInvalidQuery)
	}
	micro, id, ok := strings.Cut(string(raw), ":")
	if !ok || id == "" {
		return time.Time{}, "", fmt.Errorf("%w: cursor", ErrInvalidQuery)
	}
	n, err := strconv.ParseInt(micro, 10, 64)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("%w: cursor", ErrInvalidQuery)
	}
	return time.UnixMicro(n).UTC(), id, nil
}
