package whatsmeow_service

import (
	"os"
	"strconv"
	"strings"
	"sync"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types/events"

	"github.com/lucasgiovannibr/whatygo/pkg/metrics"
)

// whatsmeow calls the event handler of a client one event at a time. A received message
// with media downloads the file (up to 5 minutes), converts stickers, uploads to S3 or
// base64-encodes it, so one large video held back every message and receipt behind it
// (and a full handler queue makes whatsmeow warn that ordering is no longer guaranteed).
//
// Messages with media that somebody is going to receive are therefore processed on their
// own goroutine, a bounded number at a time:
//
//   - MEDIA_WORKERS (default 4): messages with media being processed at once, process-wide;
//   - MEDIA_WORKERS_PER_INSTANCE (default 2): the share of one instance, so a flood on one
//     account cannot take every slot;
//   - MEDIA_ORDERED=true: process them inline again (strict arrival order).
//
// Webhook delivery was already not strictly ordered (each event is dispatched on its own
// goroutine); events carry their timestamp. Everything else (text, receipts, presence,
// connection events) stays inline and in order.

const mediaPendingCap = 512 // messages waiting for a slot; beyond it the handler works inline (back-pressure)

type mediaLimiter struct {
	global      chan struct{}
	perInstance int

	mu      sync.Mutex
	inst    map[string]chan struct{}
	pending int
}

func newMediaLimiter() *mediaLimiter {
	geti := func(name string, def int) int {
		if v, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name))); err == nil && v > 0 {
			return v
		}
		return def
	}
	return &mediaLimiter{
		global:      make(chan struct{}, geti("MEDIA_WORKERS", 4)),
		perInstance: geti("MEDIA_WORKERS_PER_INSTANCE", 2),
		inst:        map[string]chan struct{}{},
	}
}

var (
	mediaSlots   = newMediaLimiter()
	mediaOrdered = strings.EqualFold(strings.TrimSpace(os.Getenv("MEDIA_ORDERED")), "true")
)

func (l *mediaLimiter) instSem(id string) chan struct{} {
	l.mu.Lock()
	defer l.mu.Unlock()
	s := l.inst[id]
	if s == nil {
		s = make(chan struct{}, l.perInstance)
		l.inst[id] = s
	}
	return s
}

// run executes fn on a new goroutine once the instance and the process have a free slot.
// It reports false (and does nothing) when too many are already waiting.
func (l *mediaLimiter) run(instanceID string, fn func()) bool {
	l.mu.Lock()
	if l.pending >= mediaPendingCap {
		l.mu.Unlock()
		return false
	}
	l.pending++
	l.mu.Unlock()
	metrics.MediaPending.Inc()

	sem := l.instSem(instanceID)
	go func() {
		sem <- struct{}{} // the instance's share first, so a flooded instance waits alone
		l.global <- struct{}{}
		l.mu.Lock()
		l.pending--
		l.mu.Unlock()
		metrics.MediaPending.Dec()
		defer func() {
			<-l.global
			<-sem
		}()
		fn()
	}()
	return true
}

// messageHasMedia reports whether the message (or the message it carries) is a file the
// handler would download.
func messageHasMedia(m *waE2E.Message) bool {
	_, ok := pickMedia(m)
	return ok
}

// myEventHandler is the handler registered with whatsmeow. It hands messages with media to
// the bounded workers (see above) and handles everything else right here.
func (mycli *MyClient) myEventHandler(rawEvt interface{}) {
	if evt, ok := rawEvt.(*events.Message); ok && !mediaOrdered && mycli.config.WebhookFiles &&
		messageHasMedia(evt.Message) && mycli.service.EventWanted(mycli.inst(), "Message", evt.Info.Chat.String()) {
		// A copy: the handler rewrites evt.Info (JID clean-up) and whatsmeow owns the original.
		cp := *evt
		if mediaSlots.run(mycli.userID, func() {
			defer recoverAndLog(mycli.loggerWrapper, mycli.userID, "media message")
			mycli.handleEvent(&cp)
		}) {
			return
		}
	}
	mycli.handleEvent(rawEvt)
}
