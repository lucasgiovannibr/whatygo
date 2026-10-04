package send_service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lucasgiovannibr/whatygo/pkg/metrics"
)

// Sends are limited per instance, not per process: an account is what WhatsApp restricts.
// A CRM or bot that fires hundreds of requests at once used to push them all to the socket
// at the same moment, the classic trigger of a restriction (and of "rate-overlimit" errors).
//
//   - SEND_MAX_CONCURRENT (default 4): requests of one instance inside the network call at
//     once. The rest wait their turn; 0 turns the limit off.
//   - SEND_RATE_PER_MIN (default 0 = off): sustained messages per minute per instance, with a
//     burst of about six seconds' worth.
//   - SEND_QUEUE_WAIT_SEC (default 30): how long a request waits for its turn before it is
//     refused with 429 and a Retry-After.
//
// Only the network call is limited; preparing the message (download, conversion, a typing
// delay) happens outside, so a slow upload never blocks other sends.

// ErrSendThrottled is returned when a send could not get its turn in time. Handlers
// answer it with 429 and the Retry-After header.
type ErrSendThrottled struct {
	RetryAfter time.Duration
}

func (e *ErrSendThrottled) Error() string {
	return fmt.Sprintf("too many messages for this instance, try again in %d s", e.seconds())
}

func (e *ErrSendThrottled) seconds() int {
	s := int(math.Ceil(e.RetryAfter.Seconds()))
	if s < 1 {
		s = 1
	}
	return s
}

// RetryAfterSeconds is the value of the Retry-After header.
func (e *ErrSendThrottled) RetryAfterSeconds() int { return e.seconds() }

// AsThrottled reports whether err is (or wraps) an ErrSendThrottled.
func AsThrottled(err error) (*ErrSendThrottled, bool) {
	var t *ErrSendThrottled
	if errors.As(err, &t) {
		return t, true
	}
	return nil, false
}

type throttleConfig struct {
	maxConcurrent int
	ratePerMin    int
	maxWait       time.Duration
}

func throttleConfigFromEnv() throttleConfig {
	geti := func(name string, def int) int {
		if v, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name))); err == nil && v >= 0 {
			return v
		}
		return def
	}
	return throttleConfig{
		maxConcurrent: geti("SEND_MAX_CONCURRENT", 4),
		ratePerMin:    geti("SEND_RATE_PER_MIN", 0),
		maxWait:       time.Duration(geti("SEND_QUEUE_WAIT_SEC", 30)) * time.Second,
	}
}

type instanceLimiter struct {
	sem    chan struct{} // nil: no concurrency limit
	mu     sync.Mutex
	tokens float64 // may go negative: reserved ahead
	last   time.Time
}

type sendThrottle struct {
	cfg throttleConfig
	now func() time.Time

	mu    sync.Mutex
	insts map[string]*instanceLimiter
}

func newSendThrottle(cfg throttleConfig) *sendThrottle {
	return &sendThrottle{cfg: cfg, now: time.Now, insts: map[string]*instanceLimiter{}}
}

func (t *sendThrottle) enabled() bool {
	return t.cfg.maxConcurrent > 0 || t.cfg.ratePerMin > 0
}

func (t *sendThrottle) burst() float64 {
	b := math.Ceil(float64(t.cfg.ratePerMin) / 10)
	if b < 1 {
		b = 1
	}
	return b
}

func (t *sendThrottle) limiter(id string) *instanceLimiter {
	t.mu.Lock()
	defer t.mu.Unlock()
	l := t.insts[id]
	if l == nil {
		l = &instanceLimiter{tokens: t.burst(), last: t.now()}
		if t.cfg.maxConcurrent > 0 {
			l.sem = make(chan struct{}, t.cfg.maxConcurrent)
		}
		t.insts[id] = l
	}
	return l
}

// reserve takes a token and returns how long to wait before using it (0: now).
func (t *sendThrottle) reserve(l *instanceLimiter) time.Duration {
	if t.cfg.ratePerMin <= 0 {
		return 0
	}
	perSec := float64(t.cfg.ratePerMin) / 60
	l.mu.Lock()
	defer l.mu.Unlock()
	now := t.now()
	l.tokens = math.Min(t.burst(), l.tokens+now.Sub(l.last).Seconds()*perSec)
	l.last = now
	if l.tokens >= 1 {
		l.tokens--
		return 0
	}
	wait := time.Duration((1 - l.tokens) / perSec * float64(time.Second))
	l.tokens-- // reserved: the next caller queues behind this one
	return wait
}

// unreserve gives a token back (the caller gave up waiting).
func (t *sendThrottle) unreserve(l *instanceLimiter) {
	l.mu.Lock()
	l.tokens++
	l.mu.Unlock()
}

// acquire waits for the instance's turn. The returned function releases it.
func (t *sendThrottle) acquire(ctx context.Context, id string) (func(), error) {
	if t == nil || !t.enabled() { // nil: a service built without NewSendService
		return func() {}, nil
	}
	l := t.limiter(id)

	if wait := t.reserve(l); wait > 0 {
		if wait > t.cfg.maxWait {
			t.unreserve(l)
			metrics.SendThrottled.Inc()
			return nil, &ErrSendThrottled{RetryAfter: wait}
		}
		timer := time.NewTimer(wait)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			t.unreserve(l)
			return nil, ctx.Err()
		}
	}

	if l.sem == nil {
		return func() {}, nil
	}
	timer := time.NewTimer(t.cfg.maxWait)
	defer timer.Stop()
	select {
	case l.sem <- struct{}{}:
		return func() { <-l.sem }, nil
	case <-timer.C:
		metrics.SendThrottled.Inc()
		return nil, &ErrSendThrottled{RetryAfter: time.Second}
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
