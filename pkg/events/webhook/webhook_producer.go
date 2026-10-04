package webhook_producer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	producer_interfaces "github.com/lucasgiovannibr/whatygo/pkg/events/interfaces"
	logger_wrapper "github.com/lucasgiovannibr/whatygo/pkg/logger"
	"github.com/lucasgiovannibr/whatygo/pkg/utils"
)

// Webhook delivery goes through one bounded queue per destination URL.
//
// It used to start a goroutine per event, each retrying up to 5 times with a 30 s wait.
// A receiver that was down during heavy traffic therefore piled up goroutines (and the
// event payloads they held, media included) without limit, and nothing bounded how many
// requests hit a receiver that had just come back.
//
// Now every destination has a queue with a limit on events and on bytes and at most a
// few workers. When the queue is full the OLDEST event is dropped (the newest describes
// the current state) and counted, so an outage costs a bounded amount of memory and
// the loss is visible in GET /instance/runtimes. An event is retried with a growing,
// jittered wait; once one has exhausted its retries the destination is treated as down
// and the events behind it get a single attempt each until one gets through, so a dead
// receiver drains quickly instead of costing minutes per event.
//
// Tuning (environment): WEBHOOK_QUEUE_MAX_EVENTS (default 1000), WEBHOOK_QUEUE_MAX_MB
// (default 64, per destination), WEBHOOK_QUEUE_GLOBAL_MB (default 256, all destinations
// together) and WEBHOOK_QUEUE_WORKERS (default 4; 1 delivers strictly in order).
//
// The limit per destination alone allowed N x 64 MB for N destinations: a server whose
// instances each have their own, dead, receiver could hold gigabytes of events. The global
// limit takes the oldest events of the destination that holds the most.

const (
	// webhookRequestTimeout bounds one delivery attempt. The client used to have no
	// timeout at all: a receiver that accepts the connection and never answers held its
	// goroutine (and the event payload it carries) forever, and the retries never got to
	// run. Receivers that are slow but alive (n8n, Make) answer well inside this.
	webhookRequestTimeout = 30 * time.Second

	// maxLoggedResponse is how much of a receiver's answer goes to the log and is read
	// at all: a large or endless body must not be pulled into memory just to be logged.
	maxLoggedResponse = 4 << 10

	defaultMaxEvents = 1000
	defaultMaxMB     = 64
	defaultGlobalMB  = 256
	defaultWorkers   = 4
)

// retryable reports whether a failed delivery is worth trying again. A refusal that says "this is
// wrong" (400, 404, 410, 422...) will say the same next time: it used to be retried five times,
// two and a half minutes per event, and then the destination was marked as down. Timeouts, rate
// limits and server errors, and failures to connect (status 0), are retried.
func retryable(status int) bool {
	if status >= 400 && status < 500 {
		return status == http.StatusRequestTimeout || status == http.StatusTooEarly || status == http.StatusTooManyRequests
	}
	return true
}

// defaultBackoff is the wait before each retry: 5 attempts in total, as before, but
// growing instead of a flat 30 s.
var defaultBackoff = []time.Duration{1 * time.Second, 5 * time.Second, 30 * time.Second, 2 * time.Minute}

type webhookEvent struct {
	body   []byte
	userID string
}

// destQueue holds the pending events of one destination. All fields are guarded by
// webhookProducer.mu.
type destQueue struct {
	events   []webhookEvent
	bytes    int64
	running  int  // workers alive
	inFlight int  // events being delivered right now
	degraded bool // the last event exhausted its retries: single attempts until one succeeds
}

type webhookProducer struct {
	url           string
	loggerWrapper *logger_wrapper.LoggerManager
	httpClient    *http.Client

	maxEvents int
	maxBytes  int64
	// globalMaxBytes bounds the events waiting in every destination together (0: no limit).
	globalMaxBytes int64
	workers        int
	backoff        []time.Duration

	mu         sync.Mutex
	queues     map[string]*destQueue
	totalBytes int64 // bytes waiting in all queues

	sent    uint64
	failed  uint64
	dropped uint64

	// draining is closed when Close starts: retries no longer wait between attempts (the
	// remaining ones run back to back), so a receiver that is down is given up on in
	// moments instead of holding the shutdown for the whole backoff.
	// stop is closed when Close gives up waiting: retries fail at once.
	draining  chan struct{}
	drainOnce sync.Once
	stop      chan struct{}
	stopOnce  sync.Once
}

func envInt(name string, def int) int {
	if v, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name))); err == nil && v > 0 {
		return v
	}
	return def
}

func NewWebhookProducer(
	url string,
	loggerWrapper *logger_wrapper.LoggerManager,
) producer_interfaces.Producer {
	return &webhookProducer{
		url:            url,
		loggerWrapper:  loggerWrapper,
		httpClient:     utils.NewWebhookClient(webhookRequestTimeout),
		maxEvents:      envInt("WEBHOOK_QUEUE_MAX_EVENTS", defaultMaxEvents),
		maxBytes:       int64(envInt("WEBHOOK_QUEUE_MAX_MB", defaultMaxMB)) << 20,
		globalMaxBytes: int64(envInt("WEBHOOK_QUEUE_GLOBAL_MB", defaultGlobalMB)) << 20,
		workers:        envInt("WEBHOOK_QUEUE_WORKERS", defaultWorkers),
		backoff:        defaultBackoff,
		queues:         map[string]*destQueue{},
		draining:       make(chan struct{}),
		stop:           make(chan struct{}),
	}
}

// Close waits for the queued events to be delivered, until ctx expires; then it stops the
// retries and reports what was left. A deploy used to lose every event still queued.
func (p *webhookProducer) Close(ctx context.Context) error {
	p.drainOnce.Do(func() { close(p.draining) })
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		st := p.WebhookStats()
		if st.Pending+st.InFlight == 0 {
			return nil
		}
		select {
		case <-tick.C:
		case <-ctx.Done():
			p.stopOnce.Do(func() { close(p.stop) })
			return fmt.Errorf("%d webhook event(s) not delivered before shutdown", st.Pending+st.InFlight)
		}
	}
}

func (p *webhookProducer) Produce(
	queueName string,
	payload []byte,
	webhookUrl string,
	userID string,
) error {
	splitQueue := strings.Split(queueName, ".")

	if len(splitQueue) < 2 {
		return nil
	}

	if p.url != "" {
		p.enqueue(p.url, payload, userID)
	}
	// The same URL as the global one would receive every event twice.
	if webhookUrl != "" && webhookUrl != p.url {
		p.enqueue(webhookUrl, payload, userID)
	}

	return nil
}

// enqueue adds an event to the destination's queue, dropping the oldest events when
// the queue is over its limits, and makes sure a worker is running.
func (p *webhookProducer) enqueue(url string, body []byte, userID string) {
	size := int64(len(body))

	p.mu.Lock()
	q := p.queues[url]
	if q == nil {
		q = &destQueue{}
		p.queues[url] = q
	}

	var droppedNow int
	for len(q.events) > 0 && (len(q.events) >= p.maxEvents || q.bytes+size > p.maxBytes) {
		p.dropOldest(q)
		droppedNow++
	}
	// All destinations together: the oldest events of the one that holds the most make room.
	for p.globalMaxBytes > 0 && p.totalBytes+size > p.globalMaxBytes {
		biggest := p.biggestQueue()
		if biggest == nil || len(biggest.events) == 0 {
			break
		}
		p.dropOldest(biggest)
		droppedNow++
	}
	q.events = append(q.events, webhookEvent{body: body, userID: userID})
	q.bytes += size
	p.totalBytes += size
	p.dropped += uint64(droppedNow)

	// One more worker per waiting event, up to the limit.
	start := q.running < p.workers && q.running < len(q.events)+q.inFlight
	if start {
		q.running++
	}
	p.mu.Unlock()

	if droppedNow > 0 {
		p.loggerWrapper.GetLogger(userID).LogWarn("[%s] webhook queue for %s is full: dropped %d oldest event(s)", userID, url, droppedNow)
	}
	if start {
		go p.worker(url, q)
	}
}

// dropOldest discards the oldest event of the queue (callers hold p.mu).
func (p *webhookProducer) dropOldest(q *destQueue) {
	n := int64(len(q.events[0].body))
	q.bytes -= n
	p.totalBytes -= n
	q.events[0] = webhookEvent{} // release the payload
	q.events = q.events[1:]
}

// biggestQueue is the destination that holds the most bytes (callers hold p.mu).
func (p *webhookProducer) biggestQueue() *destQueue {
	var biggest *destQueue
	for _, q := range p.queues {
		if len(q.events) > 0 && (biggest == nil || q.bytes > biggest.bytes) {
			biggest = q
		}
	}
	return biggest
}

// worker delivers events of one destination until its queue is empty.
func (p *webhookProducer) worker(url string, q *destQueue) {
	for {
		p.mu.Lock()
		if len(q.events) == 0 {
			q.running--
			if q.running == 0 && q.inFlight == 0 {
				delete(p.queues, url)
			}
			p.mu.Unlock()
			return
		}
		ev := q.events[0]
		q.events[0] = webhookEvent{}
		q.events = q.events[1:]
		q.bytes -= int64(len(ev.body))
		p.totalBytes -= int64(len(ev.body))
		if len(q.events) == 0 {
			q.events = nil // let the backing array go
		}
		q.inFlight++
		degraded := q.degraded
		p.mu.Unlock()

		ok := p.deliver(url, ev, degraded)

		p.mu.Lock()
		q.inFlight--
		if ok {
			q.degraded = false
			p.sent++
		} else {
			q.degraded = true
			p.failed++
		}
		p.mu.Unlock()
	}
}

// deliver sends one event, retrying with the backoff unless the destination is
// already known to be down, in which case it tries once.
func (p *webhookProducer) deliver(url string, ev webhookEvent, degraded bool) bool {
	attempts := len(p.backoff) + 1
	if degraded {
		attempts = 1
	}
	return p.sendWithRetry(url, ev.body, attempts, ev.userID)
}

func (p *webhookProducer) sendWithRetry(url string, body []byte, attempts int, userID string) bool {
	for i := 0; i < attempts; i++ {
		err, _, statusCode := p.sendWebhook(url, body, userID)
		if err == nil {
			// Only at debug level, and without the body of the answer (up to 4 KB of whatever the
			// receiver said, on every delivery).
			p.loggerWrapper.GetLogger(userID).LogDebug("[%s] webhook sent successfully - url: %s, status: %d", userID, url, statusCode)
			return true
		}
		p.loggerWrapper.GetLogger(userID).LogWarn("[%s] webhook failed - url: %s, attempt: %d, error: %v", userID, url, i+1, err)

		if !retryable(statusCode) {
			p.loggerWrapper.GetLogger(userID).LogError("[%s] webhook refused with status %d, not retrying - url: %s", userID, statusCode, url)
			return false
		}

		// No point waiting after the last attempt.
		if i < attempts-1 {
			t := time.NewTimer(p.wait(i))
			select {
			case <-t.C:
			case <-p.draining: // shutting down: next attempt now
				t.Stop()
			case <-p.stop:
				t.Stop()
				return false
			}
		}
	}
	p.loggerWrapper.GetLogger(userID).LogError("[%s] webhook failed after %d attempt(s) - url: %s", userID, attempts, url)
	return false
}

// wait is the pause after failed attempt i (0-based): the backoff step with up to 20%
// added, so that many events do not retry in lockstep.
func (p *webhookProducer) wait(i int) time.Duration {
	if len(p.backoff) == 0 {
		return 0
	}
	if i >= len(p.backoff) {
		i = len(p.backoff) - 1
	}
	d := p.backoff[i]
	return d + time.Duration(float64(d)*0.2*rand.Float64())
}

func (p *webhookProducer) sendWebhook(url string, body []byte, userID string) (error, []byte, int) {
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(body))
	if err != nil {
		return err, nil, 0
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return err, nil, 0
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, maxLoggedResponse))
	if err != nil {
		return fmt.Errorf("erro ao ler resposta: %v", err), nil, 0
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errors.New("received non-2xx response: " + resp.Status), responseBody, resp.StatusCode
	}

	return nil, responseBody, resp.StatusCode
}

// WebhookStats reports the state of the delivery queues.
func (p *webhookProducer) WebhookStats() producer_interfaces.WebhookStats {
	p.mu.Lock()
	defer p.mu.Unlock()

	st := producer_interfaces.WebhookStats{
		Destinations: len(p.queues),
		MaxEvents:    p.maxEvents,
		Workers:      p.workers,
		Sent:         p.sent,
		Failed:       p.failed,
		Dropped:      p.dropped,
	}
	for _, q := range p.queues {
		st.Pending += len(q.events)
		st.InFlight += q.inFlight
		st.PendingBytes += q.bytes
		if q.degraded {
			st.DegradedDestinations++
		}
	}
	return st
}

// CreateGlobalQueues não faz nada para webhook producer
func (p *webhookProducer) CreateGlobalQueues() error {
	return nil
}
