// Package metrics exposes what the process is doing in the Prometheus text format
// (GET /metrics, global API key): HTTP traffic and latency, WhatsApp events, how many
// instances are connected, the webhook queues and the database pools. They are what
// lets a change be measured instead of guessed.
package metrics

import (
	"database/sql"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.mau.fi/whatsmeow"

	producer_interfaces "github.com/lucasgiovannibr/whatygo/pkg/events/interfaces"
	"github.com/lucasgiovannibr/whatygo/pkg/safemap"
)

// Registry is the one the handler serves.
var Registry = prometheus.NewRegistry()

var (
	httpRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "whatygo_http_requests_total",
		Help: "HTTP requests by method, route pattern and status code.",
	}, []string{"method", "route", "status"})

	httpDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "whatygo_http_request_duration_seconds",
		Help:    "HTTP request duration by method and route pattern.",
		Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 30, 60},
	}, []string{"method", "route"})

	httpInFlight = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "whatygo_http_requests_in_flight",
		Help: "HTTP requests being served right now.",
	})

	// Events counts what the WhatsApp clients delivered, by event type (a bounded set).
	Events = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "whatygo_whatsapp_events_total",
		Help: "Events received from WhatsApp, by event type.",
	}, []string{"type"})

	// MessagesDropped counts messages that were not written to the database: the write
	// queue was full or the batch failed.
	MessagesDropped = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "whatygo_messages_dropped_total",
		Help: "Messages not persisted (write queue full or batch failed).",
	})

	// SendThrottled counts sends refused because the instance was over its send limit
	// (answered with 429).
	SendThrottled = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "whatygo_send_throttled_total",
		Help: "Sends refused with 429 because the instance was over its send limit.",
	})

	// CallHistorySaved and CallHistoryFailed count the call records written to the
	// database and the ones that could not be (the call itself is never affected).
	CallHistorySaved = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "whatygo_call_history_saved_total",
		Help: "Call history records saved.",
	})
	CallHistoryFailed = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "whatygo_call_history_failed_total",
		Help: "Call history records that could not be saved.",
	})

	// MediaPending is the number of received messages with media waiting for a worker.
	MediaPending = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "whatygo_media_pending",
		Help: "Received messages with media waiting for a media worker.",
	})

	// MessageBatchSize is how many messages each database write carried.
	MessageBatchSize = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "whatygo_message_batch_size",
		Help:    "Messages per batched database write.",
		Buckets: []float64{1, 2, 5, 10, 25, 50, 100, 200},
	})
)

func init() {
	Registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		httpRequests, httpDuration, httpInFlight, Events, MessagesDropped, MessageBatchSize, SendThrottled, MediaPending,
		CallHistorySaved, CallHistoryFailed,
	)
}

// Handler serves the metrics. Mount it behind the administrative authentication.
func Handler() gin.HandlerFunc {
	h := promhttp.HandlerFor(Registry, promhttp.HandlerOpts{})
	return func(c *gin.Context) { h.ServeHTTP(c.Writer, c.Request) }
}

// Middleware records every request. The route label is the registered pattern
// (/instance/:instanceId/runtime), never the concrete path, so ids and tokens cannot
// blow up the number of series.
func Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		httpInFlight.Inc()
		defer httpInFlight.Dec()

		c.Next()

		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		httpRequests.WithLabelValues(c.Request.Method, route, strconv.Itoa(c.Writer.Status())).Inc()
		httpDuration.WithLabelValues(c.Request.Method, route).Observe(time.Since(start).Seconds())
	}
}

// RegisterDBStats exposes the connection pool of a database under the given name.
func RegisterDBStats(name string, db *sql.DB) {
	if db == nil {
		return
	}
	labels := prometheus.Labels{"db": name}
	gauge := func(metric, help string, value func(sql.DBStats) float64) {
		Registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "whatygo_db_" + metric, Help: help, ConstLabels: labels,
		}, func() float64 { return value(db.Stats()) }))
	}
	gauge("connections_open", "Open connections (in use + idle).", func(s sql.DBStats) float64 { return float64(s.OpenConnections) })
	gauge("connections_in_use", "Connections in use.", func(s sql.DBStats) float64 { return float64(s.InUse) })
	gauge("connections_max", "Pool limit (0 = unlimited).", func(s sql.DBStats) float64 { return float64(s.MaxOpenConnections) })
	gauge("wait_count_total", "Times a request had to wait for a free connection.", func(s sql.DBStats) float64 { return float64(s.WaitCount) })
	gauge("wait_seconds_total", "Time spent waiting for a free connection.", func(s sql.DBStats) float64 { return s.WaitDuration.Seconds() })
}

// RegisterInstances exposes how many instances have a client, are connected and logged in.
func RegisterInstances(clients *safemap.Map[*whatsmeow.Client]) {
	count := func(pred func(*whatsmeow.Client) bool) func() float64 {
		return func() float64 {
			n := 0
			for _, c := range clients.Snapshot() {
				if c != nil && pred(c) {
					n++
				}
			}
			return float64(n)
		}
	}
	for _, g := range []struct {
		name, help string
		pred       func(*whatsmeow.Client) bool
	}{
		{"whatygo_instances_registered", "Instances with a WhatsApp client in this process.", func(*whatsmeow.Client) bool { return true }},
		{"whatygo_instances_connected", "Instances whose websocket is connected.", func(c *whatsmeow.Client) bool { return c.IsConnected() }},
		{"whatygo_instances_logged_in", "Instances logged in to WhatsApp.", func(c *whatsmeow.Client) bool { return c.IsLoggedIn() }},
	} {
		Registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: g.name, Help: g.help}, count(g.pred)))
	}
}

// RegisterWebhookQueues exposes the state of the webhook delivery queues.
func RegisterWebhookQueues(stats func() *producer_interfaces.WebhookStats) {
	read := func(value func(*producer_interfaces.WebhookStats) float64) func() float64 {
		return func() float64 {
			if s := stats(); s != nil {
				return value(s)
			}
			return 0
		}
	}
	gauge := func(name, help string, value func(*producer_interfaces.WebhookStats) float64) {
		Registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: name, Help: help}, read(value)))
	}
	counter := func(name, help string, value func(*producer_interfaces.WebhookStats) float64) {
		Registry.MustRegister(prometheus.NewCounterFunc(prometheus.CounterOpts{Name: name, Help: help}, read(value)))
	}
	gauge("whatygo_webhook_pending_events", "Events waiting in the webhook queues.", func(s *producer_interfaces.WebhookStats) float64 { return float64(s.Pending) })
	gauge("whatygo_webhook_pending_bytes", "Bytes waiting in the webhook queues.", func(s *producer_interfaces.WebhookStats) float64 { return float64(s.PendingBytes) })
	gauge("whatygo_webhook_in_flight", "Webhook deliveries in progress.", func(s *producer_interfaces.WebhookStats) float64 { return float64(s.InFlight) })
	gauge("whatygo_webhook_destinations", "Webhook URLs with events pending or in flight.", func(s *producer_interfaces.WebhookStats) float64 { return float64(s.Destinations) })
	gauge("whatygo_webhook_degraded_destinations", "Webhook URLs whose last event exhausted its retries.", func(s *producer_interfaces.WebhookStats) float64 { return float64(s.DegradedDestinations) })
	counter("whatygo_webhook_sent_total", "Webhook events delivered.", func(s *producer_interfaces.WebhookStats) float64 { return float64(s.Sent) })
	counter("whatygo_webhook_failed_total", "Webhook events that exhausted their retries.", func(s *producer_interfaces.WebhookStats) float64 { return float64(s.Failed) })
	counter("whatygo_webhook_dropped_total", "Webhook events dropped from a full queue.", func(s *producer_interfaces.WebhookStats) float64 { return float64(s.Dropped) })
}
