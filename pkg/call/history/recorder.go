package call_history

import (
	"strings"
	"time"

	call_engine "github.com/lucasgiovannibr/whatygo/pkg/call/engine"
	"github.com/lucasgiovannibr/whatygo/pkg/metrics"
)

// PhoneResolver turns the peer of a call into a phone number (digits only), or "" when it
// cannot. The peer is often a @lid, which says nothing to whoever reads the history.
type PhoneResolver func(instanceID, peer string) string

// Logger is where a failed save is reported.
type Logger interface {
	LogError(format string, args ...interface{})
}

// Recorder saves a Record for every call the engine ends. Its Handle is what the engine's
// SetOnFinished takes.
type Recorder struct {
	repo    Repository
	resolve PhoneResolver
	log     Logger
	// async runs the save away from the goroutine that ended the call; tests make it
	// synchronous.
	async func(func())
}

func NewRecorder(repo Repository, resolve PhoneResolver, log Logger) *Recorder {
	return &Recorder{repo: repo, resolve: resolve, log: log, async: func(f func()) { go f() }}
}

// Handle saves one call. It returns at once: the call ended on the library's goroutine,
// which must not wait for the database, and a database that is down loses the record, not
// the call.
func (r *Recorder) Handle(rec call_engine.Record) {
	r.async(func() {
		defer func() {
			if p := recover(); p != nil {
				metrics.CallHistoryFailed.Inc()
				r.log.LogError("[%s] Saving the history of call %s panicked: %v", rec.InstanceID, rec.CallID, p)
			}
		}()
		if err := r.repo.Save(r.row(rec)); err != nil {
			metrics.CallHistoryFailed.Inc()
			r.log.LogError("[%s] Could not save the history of call %s: %v", rec.InstanceID, rec.CallID, err)
			return
		}
		metrics.CallHistorySaved.Inc()
	})
}

func (r *Recorder) row(rec call_engine.Record) Record {
	row := Record{
		InstanceID:  rec.InstanceID,
		CallID:      rec.CallID,
		Peer:        rec.Peer,
		Direction:   string(rec.Direction),
		Video:       rec.Video,
		Outcome:     rec.Outcome,
		Reason:      rec.Reason,
		StartedAt:   rec.StartedAt.UTC(),
		EndedAt:     rec.EndedAt.UTC(),
		TalkSeconds: int(rec.Talk().Seconds()),
		RingSeconds: int(rec.Ring().Seconds()),
	}
	if !rec.AnsweredAt.IsZero() {
		at := rec.AnsweredAt.UTC()
		row.AnsweredAt = &at
	}
	if r.resolve != nil {
		row.PeerPhone = r.resolve(rec.InstanceID, rec.Peer)
	}
	return row
}

// PhoneOf is the phone number (digits) in a user JID that is a phone number, "" for any
// other (a @lid, a group).
func PhoneOf(jid string) string {
	user, server, ok := strings.Cut(jid, "@")
	if !ok || server != "s.whatsapp.net" {
		return ""
	}
	user, _, _ = strings.Cut(user, ":") // a device suffix
	return digits(user)
}

// Retain deletes what is older than keep, once now and then every interval, until stop is
// closed. It is what makes the history expire; keep <= 0 keeps everything and starts
// nothing.
func Retain(repo Repository, keep, interval time.Duration, log interface {
	LogInfo(format string, args ...interface{})
	LogError(format string, args ...interface{})
}, stop <-chan struct{}) {
	if keep <= 0 {
		return
	}
	purge := func() {
		n, err := repo.Purge(time.Now().Add(-keep))
		switch {
		case err != nil:
			log.LogError("[CALL HISTORY] Could not delete old records: %v", err)
		case n > 0:
			log.LogInfo("[CALL HISTORY] Deleted %d records older than %s", n, keep)
		}
	}
	go func() {
		purge()
		tick := time.NewTicker(interval)
		defer tick.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tick.C:
				purge()
			}
		}
	}()
}
