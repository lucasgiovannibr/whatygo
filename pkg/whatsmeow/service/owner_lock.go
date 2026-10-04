package whatsmeow_service

import (
	"context"
	"database/sql"
	"errors"
	"hash/fnv"
	"sync"
	"time"
)

// Instance ownership across replicas.
//
// CLIENT_NAME only decides which instances a replica starts at boot. Nothing stopped a
// second replica from starting an instance too (any request that reaches a replica without
// the client starts it), and two sessions of one WhatsApp account fight each other:
// "stream replaced" in a loop. Each running instance now holds a Postgres advisory lock for
// as long as its runtime lives; a replica that cannot take the lock does not start it.
//
// All the locks of a process are held by ONE dedicated database session (a lock per session
// would pin a connection per instance). Advisory locks belong to the session, so they vanish
// by themselves when the process dies or its connection drops: that is the lease. A watchdog
// pings the session; if it is lost, it is reopened and the locks are taken again, and an
// instance whose lock cannot be taken back (another replica got it meanwhile) is stopped.
//
// INSTANCE_LOCK=false turns it off.

// InstanceLocker decides which process runs an instance.
type InstanceLocker interface {
	// TryLock takes the instance. It reports false when another process holds it.
	TryLock(ctx context.Context, instanceID string) (bool, error)
	// Probe reports whether the instance is free (it is not taken).
	Probe(ctx context.Context, instanceID string) (bool, error)
	// Unlock gives the instance up.
	Unlock(instanceID string)
	// Close releases everything.
	Close()
}

func lockKey(instanceID string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte("whatygo:instance:" + instanceID))
	return int64(h.Sum64())
}

const (
	lockOpTimeout     = 5 * time.Second
	lockWatchInterval = 15 * time.Second
	lockRelockTries   = 3 // watchdog ticks an instance may stay unlocked after a reconnect before it is given up
)

type pgLocker struct {
	db     *sql.DB
	onLost func(instanceID string)

	mu       sync.Mutex
	conn     *sql.Conn
	held     map[string]int64    // instance -> key
	pending  map[string]struct{} // held instances whose lock must be taken again (the session was lost)
	failures map[string]int      // instance -> consecutive failed re-locks
	closed   bool

	stop chan struct{}
	done chan struct{}
}

// NewPostgresLocker returns a locker on the given database. onLost is called, from the
// watchdog, for an instance whose lock was lost and could not be taken back.
func NewPostgresLocker(db *sql.DB, onLost func(instanceID string)) InstanceLocker {
	l := &pgLocker{
		db:       db,
		onLost:   onLost,
		held:     map[string]int64{},
		pending:  map[string]struct{}{},
		failures: map[string]int{},
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
	go l.watch(lockWatchInterval)
	return l
}

// session returns the dedicated session, opening it if needed. Caller holds l.mu.
func (l *pgLocker) session(ctx context.Context) (*sql.Conn, error) {
	if l.closed {
		return nil, errors.New("locker closed")
	}
	if l.conn != nil {
		return l.conn, nil
	}
	c, err := l.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	l.conn = c
	return c, nil
}

// dropSession discards the session (its locks are released by the server). Caller holds l.mu.
func (l *pgLocker) dropSession() {
	if l.conn != nil {
		_ = l.conn.Close()
		l.conn = nil
	}
}

func (l *pgLocker) tryLockLocked(ctx context.Context, key int64) (bool, error) {
	c, err := l.session(ctx)
	if err != nil {
		return false, err
	}
	var ok bool
	if err := c.QueryRowContext(ctx, `SELECT pg_try_advisory_lock($1)`, key).Scan(&ok); err != nil {
		l.dropSession()
		return false, err
	}
	return ok, nil
}

func (l *pgLocker) TryLock(ctx context.Context, instanceID string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, lockOpTimeout)
	defer cancel()
	l.mu.Lock()
	defer l.mu.Unlock()

	if _, mine := l.held[instanceID]; mine {
		return true, nil // advisory locks stack per session: never take it twice
	}
	key := lockKey(instanceID)
	ok, err := l.tryLockLocked(ctx, key)
	if err != nil || !ok {
		return false, err
	}
	l.held[instanceID] = key
	delete(l.failures, instanceID)
	return true, nil
}

func (l *pgLocker) Probe(ctx context.Context, instanceID string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, lockOpTimeout)
	defer cancel()
	l.mu.Lock()
	defer l.mu.Unlock()

	if _, mine := l.held[instanceID]; mine {
		return true, nil
	}
	key := lockKey(instanceID)
	ok, err := l.tryLockLocked(ctx, key)
	if err != nil || !ok {
		return false, err
	}
	// Free: give it back at once.
	var released bool
	if err := l.conn.QueryRowContext(ctx, `SELECT pg_advisory_unlock($1)`, key).Scan(&released); err != nil {
		l.dropSession()
	}
	return true, nil
}

func (l *pgLocker) Unlock(instanceID string) {
	ctx, cancel := context.WithTimeout(context.Background(), lockOpTimeout)
	defer cancel()
	l.mu.Lock()
	defer l.mu.Unlock()

	key, ok := l.held[instanceID]
	if !ok {
		return
	}
	delete(l.held, instanceID)
	delete(l.pending, instanceID)
	delete(l.failures, instanceID)
	if l.conn == nil {
		return // the session is gone, and the lock with it
	}
	var released bool
	if err := l.conn.QueryRowContext(ctx, `SELECT pg_advisory_unlock($1)`, key).Scan(&released); err != nil {
		l.dropSession()
	}
}

// watch pings the session; when it is lost it opens a new one and takes the locks back.
func (l *pgLocker) watch(every time.Duration) {
	defer close(l.done)
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-l.stop:
			return
		case <-t.C:
			l.check()
		}
	}
}

func (l *pgLocker) check() {
	ctx, cancel := context.WithTimeout(context.Background(), lockOpTimeout)
	defer cancel()

	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return
	}
	if l.conn != nil {
		var one int
		if err := l.conn.QueryRowContext(ctx, `SELECT 1`).Scan(&one); err != nil {
			l.dropSession()
		}
	}
	if l.conn == nil { // the session is gone, and with it every lock
		for id := range l.held {
			l.pending[id] = struct{}{}
		}
	}

	// Take the locks back. The server may still consider the old session alive for a while
	// (it keeps the lock until it notices), so a refusal is retried on the next ticks before
	// the instance is given up: that means another replica really has it.
	var lost []string
	for id := range l.pending {
		key, ok := l.held[id]
		if !ok {
			delete(l.pending, id)
			continue
		}
		got, err := l.tryLockLocked(ctx, key)
		if err == nil && got {
			delete(l.pending, id)
			delete(l.failures, id)
			continue
		}
		if err == nil {
			l.failures[id]++
			if l.failures[id] >= lockRelockTries {
				lost = append(lost, id)
			}
		}
	}
	for _, id := range lost {
		delete(l.held, id)
		delete(l.pending, id)
		delete(l.failures, id)
	}
	l.mu.Unlock()

	for _, id := range lost {
		if l.onLost != nil {
			l.onLost(id)
		}
	}
}

func (l *pgLocker) Close() {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return
	}
	l.closed = true
	l.held = map[string]int64{}
	l.dropSession() // releases every lock
	l.mu.Unlock()
	close(l.stop)
	<-l.done
}
