package call_engine_test

import (
	"context"
	"errors"
	"testing"
	"time"

	call_engine "github.com/lucasgiovannibr/whatygo/pkg/call/engine"
	"github.com/lucasgiovannibr/whatygo/pkg/call/engine/enginetest"
	"github.com/prometheus/client_golang/prometheus"
)

const stall = 100 * time.Millisecond

// mediaRig is a running call with a stream attached and the media up.
type mediaRig struct {
	m      *call_engine.Manager
	ev     *events
	call   *enginetest.Fake
	tr     *call_engine.Tracked
	stats  *call_engine.StreamStats
	detach func()
}

func newMediaRig(t *testing.T, opts call_engine.Options, attach bool) *mediaRig {
	t.Helper()
	r := &mediaRig{ev: &events{}, call: enginetest.NewFake("M1"), stats: &call_engine.StreamStats{}}
	opts.Notify = r.ev.notify
	r.m = call_engine.NewManager(opts)
	r.call.SetPhase(call_engine.PhaseActive)
	tr, err := r.m.Track("inst", r.call, call_engine.Incoming)
	if err != nil {
		t.Fatal(err)
	}
	r.tr = tr
	r.call.Ready()
	if attach {
		_, detach, err := r.m.AttachStream("inst", "M1", call_engine.Endpoints{Sink: sink{}, Source: source{}}, r.stats)
		if err != nil {
			t.Fatal(err)
		}
		r.detach = detach
	}
	return r
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestMediaStallIsReportedOnceAndWhenItEnds(t *testing.T) {
	r := newMediaRig(t, call_engine.Options{MediaStall: stall}, true)
	r.call.Sink().WriteFrame([]float32{0.1}) // the peer is talking

	waitFor(t, "CallMediaStalled", func() bool { return len(r.ev.named("CallMediaStalled")) == 1 })
	if !r.tr.Info().MediaStalled {
		t.Error("Info does not say the media stalled")
	}
	got := r.ev.named("CallMediaStalled")[0]
	if got["callId"] != "M1" || got["hangup"] != false {
		t.Errorf("event data = %v", got)
	}
	if secs, ok := got["idleSeconds"].(int); !ok || secs < 0 {
		t.Errorf("idleSeconds = %v", got["idleSeconds"])
	}

	// It is said once, not at every check.
	time.Sleep(3 * stall)
	if n := len(r.ev.named("CallMediaStalled")); n != 1 {
		t.Fatalf("%d stall events for one stall", n)
	}
	if r.call.Phase() == call_engine.PhaseEnded {
		t.Fatal("the call was hung up although hanging up is off")
	}

	r.call.Sink().WriteFrame([]float32{0.2})
	waitFor(t, "CallMediaResumed", func() bool { return len(r.ev.named("CallMediaResumed")) == 1 })
	if r.tr.Info().MediaStalled {
		t.Error("Info still says the media is stalled")
	}
}

func TestMediaStallHangsUpWhenAsked(t *testing.T) {
	r := newMediaRig(t, call_engine.Options{MediaStall: stall, MediaStallHangup: true}, true)

	select {
	case <-r.tr.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("the stalled call was not hung up")
	}
	if got := r.tr.Reason(); got != call_engine.ReasonMediaStalled {
		t.Errorf("reason = %q, want %q", got, call_engine.ReasonMediaStalled)
	}
	// Done closes before CallEnded is published
	waitFor(t, "CallEnded", func() bool { return len(r.ev.named("CallEnded")) == 1 })
	if ended := r.ev.named("CallEnded"); ended[0]["reason"] != call_engine.ReasonMediaStalled {
		t.Errorf("CallEnded = %v", ended)
	}
}

func TestACallWithoutAStreamIsNotWatched(t *testing.T) {
	r := newMediaRig(t, call_engine.Options{MediaStall: stall, MediaStallHangup: true}, false)
	time.Sleep(4 * stall)
	if len(r.ev.named("CallMediaStalled")) != 0 || r.call.Phase() == call_engine.PhaseEnded {
		t.Fatal("a call nobody is listening to was reported or hung up")
	}
}

func TestACallThatIsNotActiveYetIsNotWatched(t *testing.T) {
	r := newMediaRig(t, call_engine.Options{MediaStall: stall}, true)
	r.call.SetPhase(call_engine.PhaseConnecting)
	time.Sleep(4 * stall)
	if len(r.ev.named("CallMediaStalled")) != 0 {
		t.Fatal("a call that is still connecting was reported")
	}
}

func TestMediaStallCanBeTurnedOff(t *testing.T) {
	r := newMediaRig(t, call_engine.Options{MediaStall: -1}, true)
	time.Sleep(300 * time.Millisecond)
	if len(r.ev.named("CallMediaStalled")) != 0 {
		t.Fatal("the check ran although it is off")
	}
}

func TestANewStreamStartsTheClockAgain(t *testing.T) {
	r := newMediaRig(t, call_engine.Options{MediaStall: stall}, true)
	waitFor(t, "the first stall", func() bool { return len(r.ev.named("CallMediaStalled")) == 1 })

	r.detach()
	if _, _, err := r.m.AttachStream("inst", "M1", call_engine.Endpoints{Sink: sink{}, Source: source{}}, &call_engine.StreamStats{}); err != nil {
		t.Fatal(err)
	}
	// The call has been silent for longer than the threshold, but the new stream has only
	// just attached: the next check clears the stall instead of blaming the new stream.
	waitFor(t, "the stall to clear", func() bool { return !r.tr.Info().MediaStalled })
	if n := len(r.ev.named("CallMediaStalled")); n != 1 {
		t.Fatalf("%d stall events: the new stream was blamed for the silence before it", n)
	}
}

// --- metrics

// scrape returns the value of the series with these labels (nil labels: the only one).
func scrape(t *testing.T, reg *prometheus.Registry, name string, want map[string]string) float64 {
	t.Helper()
	families, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range families {
		if f.GetName() != name {
			continue
		}
	metric:
		for _, m := range f.GetMetric() {
			have := map[string]string{}
			for _, l := range m.GetLabel() {
				have[l.GetName()] = l.GetValue()
			}
			for k, v := range want {
				if have[k] != v {
					continue metric
				}
			}
			switch {
			case m.GetCounter() != nil:
				return m.GetCounter().GetValue()
			case m.GetGauge() != nil:
				return m.GetGauge().GetValue()
			case m.GetHistogram() != nil:
				return float64(m.GetHistogram().GetSampleCount())
			}
		}
	}
	return 0
}

func registry(t *testing.T, m *call_engine.Manager) *prometheus.Registry {
	t.Helper()
	reg := prometheus.NewRegistry()
	reg.MustRegister(m.Collectors()...)
	return reg
}

func TestCallMetricsFollowACall(t *testing.T) {
	r := newMediaRig(t, call_engine.Options{MediaStall: -1}, true)
	reg := registry(t, r.m)

	if got := scrape(t, reg, "whatygo_calls_started_total", map[string]string{"direction": "incoming", "video": "false"}); got != 1 {
		t.Errorf("started = %v", got)
	}
	if got := scrape(t, reg, "whatygo_calls_active", map[string]string{"phase": "active"}); got != 1 {
		t.Errorf("active calls = %v", got)
	}
	if got := scrape(t, reg, "whatygo_call_streams_attached", nil); got != 1 {
		t.Errorf("streams attached = %v", got)
	}

	r.call.End("") // the peer hangs up: WhatsApp sends no reason
	if got := scrape(t, reg, "whatygo_calls_ended_total", map[string]string{"direction": "incoming", "reason": "peer_hangup"}); got != 1 {
		t.Errorf("ended = %v", got)
	}
	if got := scrape(t, reg, "whatygo_call_talk_seconds", map[string]string{"direction": "incoming"}); got != 1 {
		t.Errorf("talk time observations = %v", got)
	}
	if got := scrape(t, reg, "whatygo_calls_active", map[string]string{"phase": "active"}); got != 0 {
		t.Errorf("active calls after the end = %v", got)
	}
}

func TestACallThatNeverRanIsNotTalkTime(t *testing.T) {
	ev := &events{}
	m := call_engine.NewManager(call_engine.Options{Notify: ev.notify, MediaStall: -1})
	reg := registry(t, m)
	f := enginetest.NewFake("R1")
	if _, err := m.Track("inst", f, call_engine.Incoming); err != nil {
		t.Fatal(err)
	}
	f.Reject()
	if got := scrape(t, reg, "whatygo_calls_ended_total", map[string]string{"reason": "rejected"}); got != 1 {
		t.Errorf("ended = %v", got)
	}
	if got := scrape(t, reg, "whatygo_call_talk_seconds", nil); got != 0 {
		t.Errorf("a call that was never answered has talk time (%v)", got)
	}
}

func TestEndReasonLabelStaysBounded(t *testing.T) {
	m := call_engine.NewManager(call_engine.Options{MediaStall: -1})
	reg := registry(t, m)
	for i, reason := range []string{"server:479", "server:503", "a reason WhatsApp wrote itself", "another one"} {
		f := enginetest.NewFake("B" + string(rune('0'+i)))
		if _, err := m.Track("inst", f, call_engine.Outgoing); err != nil {
			t.Fatal(err)
		}
		f.End(reason)
	}
	if got := scrape(t, reg, "whatygo_calls_ended_total", map[string]string{"reason": "server"}); got != 2 {
		t.Errorf("server reasons = %v, want 2 under one label", got)
	}
	if got := scrape(t, reg, "whatygo_calls_ended_total", map[string]string{"reason": "other"}); got != 2 {
		t.Errorf("free-text reasons = %v, want 2 under \"other\"", got)
	}
}

// A counter that goes down looks like a restart to Prometheus and produces nonsense
// rates, so what a stream moved must not vanish when it detaches or the call ends.
func TestStreamCountersNeverGoDown(t *testing.T) {
	r := newMediaRig(t, call_engine.Options{MediaStall: -1}, true)
	reg := registry(t, r.m)
	toClient := map[string]string{"direction": "to_client", "kind": "audio"}
	const name = "whatygo_call_stream_frames_total"

	r.stats.ToClient.Add(5)
	if got := scrape(t, reg, name, toClient); got != 5 {
		t.Fatalf("while attached = %v, want 5", got)
	}
	r.stats.DroppedToClient.Add(2)
	r.stats.KeyframeRequests.Add(1)

	r.detach()
	if got := scrape(t, reg, name, toClient); got != 5 {
		t.Fatalf("after detach = %v, want 5", got)
	}
	if got := scrape(t, reg, "whatygo_call_streams_attached", nil); got != 0 {
		t.Errorf("streams attached after detach = %v", got)
	}

	second := &call_engine.StreamStats{}
	_, detach, err := r.m.AttachStream("inst", "M1", call_engine.Endpoints{Sink: sink{}, Source: source{}}, second)
	if err != nil {
		t.Fatal(err)
	}
	second.ToClient.Add(3)
	if got := scrape(t, reg, name, toClient); got != 8 {
		t.Fatalf("with a second stream = %v, want 8", got)
	}

	r.call.End("") // the call ends while the stream is still attached
	if got := scrape(t, reg, name, toClient); got != 8 {
		t.Fatalf("after the call ended = %v, want 8", got)
	}
	detach()
	detach() // idempotent
	if got := scrape(t, reg, name, toClient); got != 8 {
		t.Fatalf("after the last detach = %v, want 8", got)
	}
	if got := scrape(t, reg, "whatygo_call_stream_dropped_total", toClient); got != 2 {
		t.Errorf("dropped = %v, want 2", got)
	}
	if got := scrape(t, reg, "whatygo_call_keyframe_requests_total", nil); got != 1 {
		t.Errorf("keyframe requests = %v, want 1", got)
	}
}

func TestDialMetrics(t *testing.T) {
	m := call_engine.NewManager(call_engine.Options{MediaStall: -1})
	reg := registry(t, m)
	if _, err := m.Dial(context.Background(), "nobody", "5511999990000", call_engine.DialOptions{}); !errors.Is(err, call_engine.ErrEngineUnavailable) {
		t.Fatalf("Dial = %v", err)
	}
	if got := scrape(t, reg, "whatygo_call_dials_total", map[string]string{"result": "unavailable"}); got != 1 {
		t.Errorf("unavailable dials = %v", got)
	}
}
