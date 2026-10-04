package call_engine_test

import (
	"testing"
	"time"

	call_engine "github.com/lucasgiovannibr/whatygo/pkg/call/engine"
	"github.com/lucasgiovannibr/whatygo/pkg/call/engine/enginetest"
)

const limit = 150 * time.Millisecond

// frame is one 60 ms frame at the given level (a constant, so its rms is the level).
func frame(level float32) []float32 {
	f := make([]float32, call_engine.FrameSamples)
	for i := range f {
		f[i] = level
	}
	return f
}

// loudSource is a client that is always speaking: every frame the library asks for has sound.
type loudSource struct{}

func (loudSource) ReadFrame() ([]float32, error) { return frame(0.2), nil }
func (loudSource) Close() error                  { return nil }

func ended(t *testing.T, r *mediaRig, within time.Duration) bool {
	t.Helper()
	select {
	case <-r.tr.Done():
		return true
	case <-time.After(within):
		return false
	}
}

func TestACallIsHungUpWhenItRunsPastItsMaximumDuration(t *testing.T) {
	r := newMediaRig(t, call_engine.Options{MediaStall: -1, MaxDuration: limit}, false) // no stream needed
	if !ended(t, r, 3*time.Second) {
		t.Fatal("a call past its maximum duration was not hung up")
	}
	if got := r.tr.Reason(); got != call_engine.ReasonMaxDuration {
		t.Fatalf("reason = %q, want %q", got, call_engine.ReasonMaxDuration)
	}
	waitFor(t, "CallEnded", func() bool { return len(r.ev.named("CallEnded")) == 1 })
	if got := r.ev.named("CallEnded")[0]["reason"]; got != call_engine.ReasonMaxDuration {
		t.Fatalf("CallEnded reason = %v", got)
	}
}

func TestTheMaximumDurationCountsFromTheMediaBeingReady(t *testing.T) {
	// An answered call whose media never came up is not this limit's business (the
	// stream grace and WhatsApp itself deal with it): the clock starts at CallReady.
	r := &mediaRig{ev: &events{}, call: enginetest.NewFake("NR")}
	r.m = call_engine.NewManager(call_engine.Options{Notify: r.ev.notify, MediaStall: -1, MaxDuration: limit})
	r.call.SetPhase(call_engine.PhaseActive)
	tr, err := r.m.Track("inst", r.call, call_engine.Incoming)
	if err != nil {
		t.Fatal(err)
	}
	r.tr = tr

	if ended(t, r, 4*limit) {
		t.Fatal("a call whose media was never ready was hung up by the maximum duration")
	}
	r.call.Ready()
	if !ended(t, r, 3*time.Second) {
		t.Fatal("the call was not hung up after its media came up")
	}
}

func TestThereIsNoLimitUnlessOneIsAskedFor(t *testing.T) {
	r := newMediaRig(t, call_engine.Options{MediaStall: -1}, true)
	if ended(t, r, 400*time.Millisecond) {
		t.Fatal("a call was hung up with no limit configured")
	}
}

func TestTheSilenceTimeoutHangsUpACallNobodyIsTalkingIn(t *testing.T) {
	r := newMediaRig(t, call_engine.Options{MediaStall: -1, SilenceTimeout: limit}, true)
	if !ended(t, r, 3*time.Second) {
		t.Fatal("a call with nobody talking was not hung up")
	}
	if got := r.tr.Reason(); got != call_engine.ReasonSilenceTimeout {
		t.Fatalf("reason = %q, want %q", got, call_engine.ReasonSilenceTimeout)
	}
}

func TestSpeechFromThePeerKeepsACallAlive(t *testing.T) {
	r := newMediaRig(t, call_engine.Options{MediaStall: -1, SilenceTimeout: limit}, true)

	// speaking for well over the timeout
	deadline := time.Now().Add(5 * limit)
	for time.Now().Before(deadline) {
		r.call.Sink().WriteFrame(frame(0.1))
		time.Sleep(limit / 5)
	}
	select {
	case <-r.tr.Done():
		t.Fatal("a call with the peer talking was hung up")
	default:
	}

	// once they stop, the timeout runs out
	if !ended(t, r, 3*time.Second) {
		t.Fatal("the call was not hung up after the peer went quiet")
	}
}

// A peer that is silent or muted still sends comfort noise a couple of times a second:
// that is the media watchdog's business (the audio is arriving) but it is not somebody
// speaking, and must not keep an abandoned call alive.
func TestComfortNoiseDoesNotCountAsSpeech(t *testing.T) {
	r := newMediaRig(t, call_engine.Options{MediaStall: -1, SilenceTimeout: limit}, true)

	stop := make(chan struct{})
	defer close(stop)
	go func() {
		for {
			select {
			case <-stop:
				return
			case <-time.After(limit / 6):
				r.call.Sink().WriteFrame(frame(0.0002)) // about -74 dBFS
			}
		}
	}()
	if !ended(t, r, 3*time.Second) {
		t.Fatal("comfort noise kept a call alive")
	}
}

func TestAQuietSpeakerIsNotTakenForSilence(t *testing.T) {
	r := newMediaRig(t, call_engine.Options{MediaStall: -1, SilenceTimeout: limit}, true)

	deadline := time.Now().Add(4 * limit)
	for time.Now().Before(deadline) {
		r.call.Sink().WriteFrame(frame(0.01)) // a soft voice, -40 dBFS
		time.Sleep(limit / 5)
	}
	select {
	case <-r.tr.Done():
		t.Fatal("a quiet speaker was hung up on")
	default:
	}
}

// The client speaking counts too: an agent that is telling the caller something while
// the caller listens is a live call.
func TestSpeechFromTheClientKeepsACallAlive(t *testing.T) {
	r := newMediaRig(t, call_engine.Options{MediaStall: -1, SilenceTimeout: limit}, false)
	if _, _, err := r.m.AttachStream("inst", "M1", call_engine.Endpoints{Sink: sink{}, Source: loudSource{}}, r.stats); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(5 * limit)
	for time.Now().Before(deadline) {
		r.call.Source().ReadFrame() // the library asking for the next 60 ms
		time.Sleep(limit / 5)
	}
	select {
	case <-r.tr.Done():
		t.Fatal("a call with the client talking was hung up")
	default:
	}
}

func TestTheSilenceTimeoutNeedsAStream(t *testing.T) {
	r := newMediaRig(t, call_engine.Options{MediaStall: -1, SilenceTimeout: limit}, false)
	if ended(t, r, 4*limit) {
		t.Fatal("a call without a stream was hung up by the silence timeout")
	}
}

func TestTheSilenceTimeoutWaitsForTheCallToBeActive(t *testing.T) {
	r := newMediaRig(t, call_engine.Options{MediaStall: -1, SilenceTimeout: limit}, true)
	r.call.SetPhase(call_engine.PhaseConnecting)
	if ended(t, r, 4*limit) {
		t.Fatal("a call that is still connecting was hung up for silence")
	}
}

func TestTheReasonsOfTheLimitsAreMetricsLabels(t *testing.T) {
	r := newMediaRig(t, call_engine.Options{MediaStall: -1, MaxDuration: limit}, false)
	reg := registry(t, r.m)
	if !ended(t, r, 3*time.Second) {
		t.Fatal("not hung up")
	}
	waitFor(t, "the metric", func() bool {
		return scrape(t, reg, "whatygo_calls_ended_total", map[string]string{"reason": "max_duration"}) == 1
	})
}
