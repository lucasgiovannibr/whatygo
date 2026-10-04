package message_repository

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/gomessguii/logger"

	message_model "github.com/lucasgiovannibr/whatygo/pkg/message/model"
	"github.com/lucasgiovannibr/whatygo/pkg/metrics"
)

const (
	// writerQueueSize bounds the memory a database outage can pin. It used to be one
	// goroutine (and one connection) per received message, without any limit.
	writerQueueSize = 8192
	// writerBatchMax is the most rows sent in one statement.
	writerBatchMax = 200
	// writerFlushEvery is how long a message waits for company before it is written.
	writerFlushEvery = 250 * time.Millisecond
)

// batchWriter collects messages from every instance and writes them in few multi-row
// statements, from a single goroutine: a burst of receipts costs a handful of round
// trips instead of one connection each.
type batchWriter struct {
	write func([]message_model.Message) error
	ch    chan message_model.Message

	mu        sync.RWMutex // held for reading by enqueue, for writing by close: never sends on a closed channel
	closed    bool
	startOnce sync.Once
	started   atomic.Bool
	done      chan struct{}
	lastDrop  atomic.Int64 // unix seconds of the last "dropped" log line
}

func newBatchWriter(write func([]message_model.Message) error) *batchWriter {
	return &batchWriter{
		write: write,
		ch:    make(chan message_model.Message, writerQueueSize),
		done:  make(chan struct{}),
	}
}

func (w *batchWriter) enqueue(m message_model.Message) bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.closed {
		return false
	}
	w.startOnce.Do(func() {
		w.started.Store(true)
		go w.run()
	})
	select {
	case w.ch <- m:
		return true
	default:
		metrics.MessagesDropped.Inc()
		if now := time.Now().Unix(); now-w.lastDrop.Swap(now) >= 10 {
			logger.LogError("[MESSAGES] write queue full (%d): dropping messages, is the database reachable?", writerQueueSize)
		}
		return false
	}
}

func (w *batchWriter) run() {
	defer close(w.done)
	ticker := time.NewTicker(writerFlushEvery)
	defer ticker.Stop()

	batch := make([]message_model.Message, 0, writerBatchMax)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := w.write(batch); err != nil {
			metrics.MessagesDropped.Add(float64(len(batch)))
			logger.LogError("[MESSAGES] failed to persist %d messages: %v", len(batch), err)
		} else {
			metrics.MessageBatchSize.Observe(float64(len(batch)))
		}
		batch = batch[:0]
	}

	for {
		select {
		case m, ok := <-w.ch:
			if !ok {
				flush()
				return
			}
			batch = append(batch, m)
			if len(batch) >= writerBatchMax {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

// close stops accepting messages and writes the ones still queued.
func (w *batchWriter) close() {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return
	}
	w.closed = true
	close(w.ch)
	w.mu.Unlock()
	if w.started.Load() {
		<-w.done
	}
}
