package call_engine

import (
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// callMetrics are what the Manager measures about calls. They belong to the Manager, not
// to the process, so tests can build as many Managers as they like; Collectors hands
// them over to be registered once.
//
// Labels are bounded on purpose: the instance id and the peer are never labels (a busy
// server would grow a series per number), and a reason WhatsApp sends in free text is
// folded into "other".
type callMetrics struct {
	started *prometheus.CounterVec   // direction, video
	ended   *prometheus.CounterVec   // direction, reason
	talk    *prometheus.HistogramVec // direction: from media ready to the end
	dials   *prometheus.CounterVec   // result
	stalls  prometheus.Counter

	streamMu sync.Mutex
	live     map[*StreamStats]struct{} // streams attached right now
	finished streamSum                 // streams that have detached
}

// streamSum is the total of what streams moved.
type streamSum struct {
	toClient, fromClient, droppedToClient, droppedFromClient                          uint64
	videoToClient, videoFromClient, videoDroppedToClient, videoDroppedFromClient, key uint64
}

func (s *streamSum) add(st *StreamStats) {
	s.toClient += st.ToClient.Load()
	s.fromClient += st.FromClient.Load()
	s.droppedToClient += st.DroppedToClient.Load()
	s.droppedFromClient += st.DroppedFromClient.Load()
	s.videoToClient += st.VideoToClient.Load()
	s.videoFromClient += st.VideoFromClient.Load()
	s.videoDroppedToClient += st.VideoDroppedToClient.Load()
	s.videoDroppedFromClient += st.VideoDroppedFromClient.Load()
	s.key += st.KeyframeRequests.Load()
}

func newCallMetrics() *callMetrics {
	return &callMetrics{
		started: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "whatygo_calls_started_total",
			Help: "Calls the engine started following, by direction and whether they began with video.",
		}, []string{"direction", "video"}),
		ended: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "whatygo_calls_ended_total",
			Help: "Calls that ended, by direction and reason (peer_hangup, hangup, rejected, rejected_busy, ring_timeout, stream_closed, media_stalled, max_duration, silence_timeout, instance_stopped, server, other).",
		}, []string{"direction", "reason"}),
		talk: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "whatygo_call_talk_seconds",
			Help:    "How long answered calls lasted, from the media being ready to the end.",
			Buckets: []float64{5, 15, 30, 60, 120, 300, 600, 1800, 3600},
		}, []string{"direction"}),
		dials: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "whatygo_call_dials_total",
			Help: "Outgoing call attempts by result (ok, failed, rate_limited, busy, unavailable).",
		}, []string{"result"}),
		stalls: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "whatygo_call_media_stalls_total",
			Help: "Times an active call with a stream stopped receiving the peer's audio.",
		}),
		live: make(map[*StreamStats]struct{}),
	}
}

var knownReasons = map[string]bool{
	ReasonPeerHangup: true, "hangup": true, "rejected": true, "rejected_busy": true,
	"ring_timeout": true, "stream_closed": true, ReasonMediaStalled: true,
	ReasonMaxDuration: true, ReasonSilenceTimeout: true,
	"instance_stopped": true, "ended": true,
}

// metricReason keeps the reason label to a small set.
func metricReason(reason string) string {
	switch {
	case knownReasons[reason]:
		return reason
	case strings.HasPrefix(reason, "server:"):
		return "server"
	}
	return "other"
}

func (c *callMetrics) callStarted(dir Direction, video bool) {
	c.started.WithLabelValues(string(dir), strconv.FormatBool(video)).Inc()
}

func (c *callMetrics) callEnded(dir Direction, reason string, talk time.Duration, answered bool) {
	c.ended.WithLabelValues(string(dir), metricReason(reason)).Inc()
	if answered {
		c.talk.WithLabelValues(string(dir)).Observe(talk.Seconds())
	}
}

// streamAttached and streamDetached keep the counters of the audio stream continuous:
// a stream counts while it is attached, and its totals are folded in when it goes, under
// one lock so a scrape never sees a number go down.
func (c *callMetrics) streamAttached(st *StreamStats) {
	if st == nil {
		return
	}
	c.streamMu.Lock()
	c.live[st] = struct{}{}
	c.streamMu.Unlock()
}

func (c *callMetrics) streamDetached(st *StreamStats) {
	if st == nil {
		return
	}
	c.streamMu.Lock()
	if _, ok := c.live[st]; ok {
		delete(c.live, st)
		c.finished.add(st)
	}
	c.streamMu.Unlock()
}

func (c *callMetrics) streamTotals() (sum streamSum, attached int) {
	c.streamMu.Lock()
	defer c.streamMu.Unlock()
	sum = c.finished
	for st := range c.live {
		sum.add(st)
	}
	return sum, len(c.live)
}

// Collectors are the metrics of the call engine, to register once in the process
// registry (see pkg/metrics).
func (m *Manager) Collectors() []prometheus.Collector {
	c := m.metrics
	out := []prometheus.Collector{c.started, c.ended, c.talk, c.dials, c.stalls}

	gauge := func(name, help string, labels prometheus.Labels, value func() float64) {
		out = append(out, prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: name, Help: help, ConstLabels: labels}, value))
	}
	counter := func(name, help string, labels prometheus.Labels, value func(streamSum) uint64) {
		out = append(out, prometheus.NewCounterFunc(prometheus.CounterOpts{Name: name, Help: help, ConstLabels: labels},
			func() float64 { s, _ := c.streamTotals(); return float64(value(s)) }))
	}

	for _, phase := range []Phase{PhaseCalling, PhaseRinging, PhaseConnecting, PhaseActive} {
		gauge("whatygo_calls_active", "Calls being followed right now, by phase.", prometheus.Labels{"phase": string(phase)},
			func() float64 { return float64(m.countCalls(func(t *Tracked) bool { return t.call.Phase() == phase })) })
	}
	for _, state := range []State{StateActive, StateHookFailed, StateBlockedProxy} {
		gauge("whatygo_call_engines", "Instances with a call engine, by state.", prometheus.Labels{"state": string(state)},
			func() float64 { return float64(m.countEngines(state)) })
	}
	gauge("whatygo_call_media_stalled", "Active calls that are not receiving the peer's audio right now.", nil,
		func() float64 { return float64(m.countCalls(func(t *Tracked) bool { return t.stalled.Load() })) })
	gauge("whatygo_call_streams_attached", "Audio streams attached to a call right now.", nil,
		func() float64 { _, n := c.streamTotals(); return float64(n) })

	for _, kind := range []string{"audio", "video"} {
		kind := kind
		to, from := prometheus.Labels{"direction": "to_client", "kind": kind}, prometheus.Labels{"direction": "from_client", "kind": kind}
		pick := func(a, v func(streamSum) uint64) func(streamSum) uint64 {
			if kind == "audio" {
				return a
			}
			return v
		}
		counter("whatygo_call_stream_frames_total", "Audio frames (60 ms) and video access units moved by the streams, to or from the client.",
			to, pick(func(s streamSum) uint64 { return s.toClient }, func(s streamSum) uint64 { return s.videoToClient }))
		counter("whatygo_call_stream_frames_total", "Audio frames (60 ms) and video access units moved by the streams, to or from the client.",
			from, pick(func(s streamSum) uint64 { return s.fromClient }, func(s streamSum) uint64 { return s.videoFromClient }))
		counter("whatygo_call_stream_dropped_total", "Audio frames and video access units the streams dropped: the client read too slowly (to_client) or the call refused them (from_client).",
			to, pick(func(s streamSum) uint64 { return s.droppedToClient }, func(s streamSum) uint64 { return s.videoDroppedToClient }))
		counter("whatygo_call_stream_dropped_total", "Audio frames and video access units the streams dropped: the client read too slowly (to_client) or the call refused them (from_client).",
			from, pick(func(s streamSum) uint64 { return s.droppedFromClient }, func(s streamSum) uint64 { return s.videoDroppedFromClient }))
	}
	counter("whatygo_call_keyframe_requests_total", "Times WhatsApp asked for a video keyframe.", nil,
		func(s streamSum) uint64 { return s.key })
	return out
}

// countCalls counts the tracked calls that satisfy pred. The predicate may call into the
// library, whose callbacks take m.mu (finish), so it runs after the lock is released.
func (m *Manager) countCalls(pred func(*Tracked) bool) int {
	m.mu.RLock()
	var all []*Tracked
	for _, per := range m.calls {
		for _, t := range per {
			all = append(all, t)
		}
	}
	m.mu.RUnlock()

	n := 0
	for _, t := range all {
		if pred(t) {
			n++
		}
	}
	return n
}

func (m *Manager) countEngines(state State) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	n := 0
	for _, rt := range m.runtimes {
		if rt.status.State == state {
			n++
		}
	}
	return n
}

// unixNano is an atomic timestamp that reads as the zero time until it is first set.
type unixNano struct{ v atomic.Int64 }

func (u *unixNano) set(t time.Time) { u.v.Store(t.UnixNano()) }

func (u *unixNano) get() time.Time {
	n := u.v.Load()
	if n == 0 {
		return time.Time{}
	}
	return time.Unix(0, n)
}
