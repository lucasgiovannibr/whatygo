package auth_middleware

import (
	"strconv"
	"sync"
	"time"
)

// Nothing slowed down someone guessing credentials: 2000 requests with a wrong apikey were
// 2000 plain 401s. The global key is long, but the token of an instance is chosen by whoever
// creates it, and it is the credential of that instance.
//
// failLimiter counts the failed authentications of each client address in a fixed window.
// Once an address has failed too many times, every authenticated request from it is refused
// with 429 until the window ends (a correct credential does not get through either: letting it
// would make the limit useless against a guess that happens to be right). Only failures are
// counted; requests that authenticate cost nothing.
//
// The address is the one gin reports, which is the connection's own unless TRUSTED_PROXIES
// names the proxies in front of the server. Behind a proxy that is not listed every client
// shares the proxy's address.

const (
	// DefaultAuthFailLimit is the failed attempts one address may make per window
	// (AUTH_FAIL_LIMIT, 0 turns the limit off).
	DefaultAuthFailLimit = 30
	authFailWindow       = time.Minute
	// maxTrackedAddresses bounds the memory of the limiter: past it the table is emptied.
	maxTrackedAddresses = 100_000
)

type failEntry struct {
	count int
	start time.Time
}

type failLimiter struct {
	max    int
	window time.Duration
	now    func() time.Time

	mu      sync.Mutex
	entries map[string]*failEntry
	swept   time.Time
}

// newFailLimiter returns a limiter that allows max failures per window, or nil (no limit)
// when max is not positive.
func newFailLimiter(max int) *failLimiter {
	if max <= 0 {
		return nil
	}
	return &failLimiter{max: max, window: authFailWindow, now: time.Now, entries: map[string]*failEntry{}}
}

// blocked reports whether the address has used up its failures, and for how many more
// seconds. A nil limiter blocks nobody.
func (l *failLimiter) blocked(addr string) (retryAfter string, blocked bool) {
	if l == nil {
		return "", false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	e := l.entries[addr]
	if e == nil {
		return "", false
	}
	now := l.now()
	if elapsed := now.Sub(e.start); elapsed >= l.window {
		delete(l.entries, addr)
		return "", false
	} else if e.count >= l.max {
		secs := int((l.window - elapsed + time.Second - 1) / time.Second)
		return strconv.Itoa(max(secs, 1)), true
	}
	return "", false
}

// fail records one failed authentication of the address.
func (l *failLimiter) fail(addr string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.sweep(now)
	e := l.entries[addr]
	if e == nil || now.Sub(e.start) >= l.window {
		if len(l.entries) >= maxTrackedAddresses {
			l.entries = map[string]*failEntry{}
		}
		e = &failEntry{start: now}
		l.entries[addr] = e
	}
	e.count++
}

// sweep drops the entries whose window ended, at most once a window (callers hold l.mu).
func (l *failLimiter) sweep(now time.Time) {
	if now.Sub(l.swept) < l.window {
		return
	}
	l.swept = now
	for addr, e := range l.entries {
		if now.Sub(e.start) >= l.window {
			delete(l.entries, addr)
		}
	}
}
