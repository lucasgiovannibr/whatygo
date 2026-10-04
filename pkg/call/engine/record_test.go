package call_engine_test

import (
	"sync"
	"testing"
	"time"

	call_engine "github.com/lucasgiovannibr/whatygo/pkg/call/engine"
	"github.com/lucasgiovannibr/whatygo/pkg/call/engine/enginetest"
)

func TestClassify(t *testing.T) {
	in, out := call_engine.Incoming, call_engine.Outgoing
	for _, c := range []struct {
		dir      call_engine.Direction
		answered bool
		reason   string
		want     string
	}{
		// whatever the reason, a call whose media came up was a conversation
		{in, true, "peer_hangup", call_engine.OutcomeAnswered},
		{out, true, "hangup", call_engine.OutcomeAnswered},
		{in, true, "max_duration", call_engine.OutcomeAnswered},

		{in, false, call_engine.ReasonPeerHangup, call_engine.OutcomeMissed},
		{in, false, "ring_timeout", call_engine.OutcomeMissed},
		{in, false, "rejected", call_engine.OutcomeRejected},
		{in, false, "rejected_busy", call_engine.OutcomeBusy},

		{out, false, "hangup", call_engine.OutcomeCancelled},
		{out, false, "ring_timeout", call_engine.OutcomeUnanswered},
		{out, false, call_engine.ReasonPeerHangup, call_engine.OutcomeUnanswered},
		{out, false, "rejected", call_engine.OutcomeRejected},

		{in, false, "server:503", call_engine.OutcomeFailed},
		{out, false, "instance_stopped", call_engine.OutcomeFailed},
		{in, false, "something nobody has seen", call_engine.OutcomeFailed},
	} {
		if got := call_engine.Classify(c.dir, c.answered, c.reason); got != c.want {
			t.Errorf("Classify(%s, answered=%v, %q) = %q, want %q", c.dir, c.answered, c.reason, got, c.want)
		}
	}
}

func TestEveryOutcomeIsListed(t *testing.T) {
	listed := map[string]bool{}
	for _, o := range call_engine.Outcomes {
		listed[o] = true
	}
	for _, o := range []string{call_engine.OutcomeAnswered, call_engine.OutcomeMissed, call_engine.OutcomeRejected,
		call_engine.OutcomeCancelled, call_engine.OutcomeUnanswered, call_engine.OutcomeBusy, call_engine.OutcomeFailed} {
		if !listed[o] {
			t.Errorf("outcome %q is not in Outcomes", o)
		}
	}
}

func TestRecordDurations(t *testing.T) {
	start := time.Date(2026, 10, 2, 10, 0, 0, 0, time.UTC)
	answered := call_engine.Record{StartedAt: start, AnsweredAt: start.Add(8 * time.Second), EndedAt: start.Add(68 * time.Second)}
	if answered.Ring() != 8*time.Second || answered.Talk() != 60*time.Second {
		t.Errorf("answered: ring %v talk %v", answered.Ring(), answered.Talk())
	}
	missed := call_engine.Record{StartedAt: start, EndedAt: start.Add(20 * time.Second)}
	if missed.Ring() != 20*time.Second || missed.Talk() != 0 {
		t.Errorf("missed: ring %v talk %v", missed.Ring(), missed.Talk())
	}
	// a clock that went backwards must not give a negative duration
	odd := call_engine.Record{StartedAt: start, AnsweredAt: start.Add(time.Minute), EndedAt: start.Add(time.Second)}
	if odd.Ring() < 0 || odd.Talk() < 0 {
		t.Errorf("negative duration: ring %v talk %v", odd.Ring(), odd.Talk())
	}
}

type recorded struct {
	mu   sync.Mutex
	list []call_engine.Record
}

func (r *recorded) add(rec call_engine.Record) {
	r.mu.Lock()
	r.list = append(r.list, rec)
	r.mu.Unlock()
}

func (r *recorded) all() []call_engine.Record {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]call_engine.Record(nil), r.list...)
}

func newRecordingManager(t *testing.T) (*call_engine.Manager, *recorded) {
	t.Helper()
	m := call_engine.NewManager(call_engine.Options{MediaStall: -1})
	got := &recorded{}
	m.SetOnFinished(got.add)
	return m, got
}

func TestAnAnsweredCallIsRecordedWithItsTimes(t *testing.T) {
	m, got := newRecordingManager(t)
	f := enginetest.NewFake("A1")
	f.SetPhase(call_engine.PhaseActive)
	if _, err := m.Track("inst", f, call_engine.Incoming); err != nil {
		t.Fatal(err)
	}
	f.Ready()
	time.Sleep(30 * time.Millisecond)
	f.End("") // the peer hangs up

	recs := got.all()
	if len(recs) != 1 {
		t.Fatalf("%d records, want 1", len(recs))
	}
	r := recs[0]
	if r.InstanceID != "inst" || r.CallID != "A1" || r.Peer != "5511999990000@s.whatsapp.net" ||
		r.Direction != call_engine.Incoming || r.Outcome != call_engine.OutcomeAnswered || r.Reason != call_engine.ReasonPeerHangup {
		t.Fatalf("record = %+v", r)
	}
	if r.AnsweredAt.IsZero() || r.Talk() < 20*time.Millisecond || r.EndedAt.Before(r.AnsweredAt) {
		t.Fatalf("times: answered %v ended %v talk %v", r.AnsweredAt, r.EndedAt, r.Talk())
	}
}

func TestACallThatWasNeverAnsweredIsRecordedAsSuch(t *testing.T) {
	m, got := newRecordingManager(t)

	missed := enginetest.NewFake("M1")
	if _, err := m.Track("inst", missed, call_engine.Incoming); err != nil {
		t.Fatal(err)
	}
	missed.End("") // the caller gave up while it rang

	rejected := enginetest.NewFake("R1")
	if _, err := m.Track("inst", rejected, call_engine.Incoming); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Reject("inst", "R1"); err != nil {
		t.Fatal(err)
	}

	cancelled := enginetest.NewFake("C1")
	cancelled.SetPhase(call_engine.PhaseCalling)
	if _, err := m.Track("inst", cancelled, call_engine.Outgoing); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Hangup("inst", "C1"); err != nil {
		t.Fatal(err)
	}

	outcome := map[string]string{}
	for _, r := range got.all() {
		outcome[r.CallID] = r.Outcome
		if !r.AnsweredAt.IsZero() || r.Talk() != 0 {
			t.Errorf("%s was never answered but has an answer time", r.CallID)
		}
	}
	want := map[string]string{"M1": call_engine.OutcomeMissed, "R1": call_engine.OutcomeRejected, "C1": call_engine.OutcomeCancelled}
	for id, w := range want {
		if outcome[id] != w {
			t.Errorf("call %s: outcome %q, want %q (all: %v)", id, outcome[id], w, outcome)
		}
	}
}

func TestTheVideoOfACallIsRecorded(t *testing.T) {
	m, got := newRecordingManager(t)
	f := enginetest.NewFake("V1")
	f.SetVideo(true)
	f.SetPhase(call_engine.PhaseActive)
	if _, err := m.Track("inst", f, call_engine.Outgoing); err != nil {
		t.Fatal(err)
	}
	f.End("hangup")
	if recs := got.all(); len(recs) != 1 || !recs[0].Video {
		t.Fatalf("records = %+v, want one with video", recs)
	}
}

func TestACallIsRecordedOnce(t *testing.T) {
	m, got := newRecordingManager(t)
	f := enginetest.NewFake("O1")
	if _, err := m.Track("inst", f, call_engine.Incoming); err != nil {
		t.Fatal(err)
	}
	f.End("")
	f.End("") // the library may report the end more than once
	if n := len(got.all()); n != 1 {
		t.Fatalf("%d records for one call", n)
	}
}

func TestNothingIsRecordedUnlessAskedFor(t *testing.T) {
	m := call_engine.NewManager(call_engine.Options{MediaStall: -1})
	f := enginetest.NewFake("N1")
	if _, err := m.Track("inst", f, call_engine.Incoming); err != nil {
		t.Fatal(err)
	}
	f.End("") // no consumer: nothing to call, and no panic

	got := &recorded{}
	m.SetOnFinished(got.add)
	m.SetOnFinished(nil)
	g := enginetest.NewFake("N2")
	if _, err := m.Track("inst", g, call_engine.Incoming); err != nil {
		t.Fatal(err)
	}
	g.End("")
	if n := len(got.all()); n != 0 {
		t.Fatalf("%d records after the consumer was removed", n)
	}
}

func TestAFailingConsumerDoesNotTakeTheCallDown(t *testing.T) {
	ev := &events{}
	m2 := call_engine.NewManager(call_engine.Options{MediaStall: -1, Notify: ev.notify})
	m2.SetOnFinished(func(call_engine.Record) { panic("database on fire") })

	f := enginetest.NewFake("P1")
	if _, err := m2.Track("inst", f, call_engine.Incoming); err != nil {
		t.Fatal(err)
	}
	f.End("")
	if len(ev.named("CallEnded")) != 1 {
		t.Fatal("the CallEnded event was lost because the consumer panicked")
	}
}
