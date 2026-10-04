package call_engine_test

import (
	"errors"
	"strings"
	"sync"
	"testing"

	call_engine "github.com/lucasgiovannibr/whatygo/pkg/call/engine"
	"github.com/lucasgiovannibr/whatygo/pkg/call/engine/enginetest"
)

type events struct {
	mu   sync.Mutex
	list []map[string]interface{}
}

func (e *events) notify(_ string, name string, data map[string]interface{}) {
	e.mu.Lock()
	defer e.mu.Unlock()
	d := map[string]interface{}{"event": name}
	for k, v := range data {
		d[k] = v
	}
	e.list = append(e.list, d)
}

func (e *events) named(name string) []map[string]interface{} {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []map[string]interface{}
	for _, d := range e.list {
		if d["event"] == name {
			out = append(out, d)
		}
	}
	return out
}

type rig struct {
	m      *call_engine.Manager
	ev     *events
	call   *enginetest.Fake
	tr     *call_engine.Tracked
	states []call_engine.VideoState
	keys   int
}

// newRig tracks a running call ("C1" of "inst").
func newRig(t *testing.T) *rig {
	t.Helper()
	ev := &events{}
	m := call_engine.NewManager(call_engine.Options{Notify: ev.notify})
	f := enginetest.NewFake("C1")
	f.SetPhase(call_engine.PhaseActive)
	tr, err := m.Track("inst", f, call_engine.Incoming)
	if err != nil {
		t.Fatal(err)
	}
	return &rig{m: m, ev: ev, call: f, tr: tr}
}

type sink struct{}

func (sink) WriteFrame([]float32) error { return nil }
func (sink) Close() error               { return nil }

type source struct{}

func (source) ReadFrame() ([]float32, error) { return nil, nil }
func (source) Close() error                  { return nil }

type vsink struct{}

func (vsink) WriteVideo([]byte) error { return nil }
func (vsink) Close() error            { return nil }

func (r *rig) attach(withVideo bool) (*call_engine.StreamStats, func()) {
	stats := &call_engine.StreamStats{}
	ep := call_engine.Endpoints{
		Sink: sink{}, Source: source{},
		OnVideoState:      func(v call_engine.VideoState) { r.states = append(r.states, v) },
		OnKeyframeRequest: func() { r.keys++ },
	}
	if withVideo {
		ep.Video = vsink{}
	}
	_, detach, err := r.m.AttachStream("inst", "C1", ep, stats)
	if err != nil {
		panic(err)
	}
	return stats, detach
}

func TestEveryVideoActionReachesTheCall(t *testing.T) {
	r := newRig(t)
	cases := []struct {
		action call_engine.VideoAction
		want   string
	}{
		{call_engine.VideoStart, "start"},
		{call_engine.VideoAccept, "accept"},
		{call_engine.VideoStop, "stop"},
		{call_engine.VideoEnable, "enable"},
		{call_engine.VideoDisable, "disable"},
	}
	for _, c := range cases {
		if _, err := r.m.Video("inst", "C1", c.action, 0); err != nil {
			t.Fatalf("%s: %v", c.action, err)
		}
	}
	if _, err := r.m.Video("inst", "C1", call_engine.VideoOrientation, 3); err != nil {
		t.Fatal(err)
	}

	got := r.call.VideoActions()
	want := []string{"start", "accept", "stop", "enable", "disable", "orientation:3"}
	if len(got) != len(want) {
		t.Fatalf("actions = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("actions = %v, want %v", got, want)
		}
	}
}

// An iPhone ignores "camera on" on a call that never had video (checked live: the server
// answered 200, sent video and nothing showed), so the server refuses it and says what
// to do instead.
func TestEnableIsRefusedOnACallThatNeverHadVideo(t *testing.T) {
	r := newRig(t)

	_, err := r.m.Video("inst", "C1", call_engine.VideoEnable, 0)
	if !errors.Is(err, call_engine.ErrWrongState) || !strings.Contains(err.Error(), "start") {
		t.Fatalf("err = %v, want a wrong-state error that points to start", err)
	}
	if got := r.call.VideoActions(); len(got) != 0 {
		t.Fatalf("the call was touched: %v", got)
	}
}

func TestEnableWorksOnceTheCallHasHadVideo(t *testing.T) {
	enableWorks := func(t *testing.T, r *rig) {
		t.Helper()
		if _, err := r.m.Video("inst", "C1", call_engine.VideoEnable, 0); err != nil {
			t.Fatalf("enable: %v", err)
		}
	}

	t.Run("a call that started with video, after muting it", func(t *testing.T) {
		m := call_engine.NewManager(call_engine.Options{Notify: (&events{}).notify})
		f := enginetest.NewFake("V1")
		f.SetPhase(call_engine.PhaseActive)
		f.SetVideo(true)
		if _, err := m.Track("inst", f, call_engine.Outgoing); err != nil {
			t.Fatal(err)
		}
		if _, err := m.Video("inst", "V1", call_engine.VideoDisable, 0); err != nil {
			t.Fatal(err)
		}
		f.SetVideo(false) // muted: no camera is on any more
		if _, err := m.Video("inst", "V1", call_engine.VideoEnable, 0); err != nil {
			t.Fatalf("unmuting video that started with the call: %v", err)
		}
	})

	t.Run("after we asked for video", func(t *testing.T) {
		r := newRig(t)
		if _, err := r.m.Video("inst", "C1", call_engine.VideoStart, 0); err != nil {
			t.Fatal(err)
		}
		enableWorks(t, r)
	})

	t.Run("after we accepted the peer's request", func(t *testing.T) {
		r := newRig(t)
		if _, err := r.m.Video("inst", "C1", call_engine.VideoAccept, 0); err != nil {
			t.Fatal(err)
		}
		enableWorks(t, r)
	})

	t.Run("after the peer accepted our request", func(t *testing.T) {
		r := newRig(t)
		r.call.PeerVideoState(call_engine.VideoState{State: call_engine.VideoStateUpgradeAccepted, StateCode: 4})
		enableWorks(t, r)
	})

	t.Run("after the peer turned its camera on", func(t *testing.T) {
		r := newRig(t)
		r.call.PeerVideoState(call_engine.VideoState{Active: true, State: call_engine.VideoStateEnabled, StateCode: 1})
		enableWorks(t, r)
	})

	t.Run("a refused start does not count", func(t *testing.T) {
		r := newRig(t)
		r.call.FailVideoActions(errors.New("call is not active"))
		if _, err := r.m.Video("inst", "C1", call_engine.VideoStart, 0); err == nil {
			t.Fatal("start should have been refused")
		}
		r.call.FailVideoActions(nil)
		if _, err := r.m.Video("inst", "C1", call_engine.VideoEnable, 0); !errors.Is(err, call_engine.ErrWrongState) {
			t.Fatalf("err = %v, want the enable to be refused", err)
		}
	})
}

func TestBadVideoRequestsNeverReachTheCall(t *testing.T) {
	r := newRig(t)

	for _, tc := range []struct {
		name        string
		action      call_engine.VideoAction
		orientation int
	}{
		{"unknown action", "explode", 0},
		{"empty action", "", 0},
		{"orientation too high", call_engine.VideoOrientation, 4},
		{"negative orientation", call_engine.VideoOrientation, -1},
	} {
		if _, err := r.m.Video("inst", "C1", tc.action, tc.orientation); !errors.Is(err, call_engine.ErrInvalidVideoRequest) {
			t.Errorf("%s: err = %v", tc.name, err)
		}
	}
	if _, err := r.m.Video("inst", "nope", call_engine.VideoStart, 0); !errors.Is(err, call_engine.ErrCallNotFound) {
		t.Errorf("unknown call: err = %v", err)
	}
	if _, err := r.m.Video("other", "C1", call_engine.VideoStart, 0); !errors.Is(err, call_engine.ErrCallNotFound) {
		t.Errorf("another instance's call: err = %v", err)
	}
	if got := r.call.VideoActions(); len(got) != 0 {
		t.Fatalf("the call was touched: %v", got)
	}
}

func TestVideoCannotBeChangedOnACallThatIsStillRinging(t *testing.T) {
	r := newRig(t)
	r.call.SetPhase(call_engine.PhaseRinging)

	if _, err := r.m.Video("inst", "C1", call_engine.VideoStart, 0); !errors.Is(err, call_engine.ErrWrongState) {
		t.Fatalf("err = %v", err)
	}
	if got := r.call.VideoActions(); len(got) != 0 {
		t.Fatalf("the call was touched: %v", got)
	}
}

func TestTheLibrarysRefusalComesBackAsWrongStateWithItsReason(t *testing.T) {
	r := newRig(t)
	r.call.FailVideoActions(errors.New("no pending peer video upgrade"))

	_, err := r.m.Video("inst", "C1", call_engine.VideoAccept, 0)

	if !errors.Is(err, call_engine.ErrWrongState) || err.Error() == call_engine.ErrWrongState.Error() {
		t.Fatalf("err = %v", err)
	}
	if want := "no pending peer video upgrade"; !contains(err.Error(), want) {
		t.Fatalf("err = %q, want it to say %q", err, want)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestThePeersVideoStateIsKeptPublishedAndHandedToTheStream(t *testing.T) {
	r := newRig(t)
	if got := r.tr.Info().PeerVideo; got != nil {
		t.Fatalf("PeerVideo = %+v before the peer said anything", got)
	}
	r.attach(false)

	r.call.PeerVideoState(call_engine.VideoState{Active: true, Upgrade: true, Orientation: 1, State: call_engine.VideoStateUpgradeRequest, StateCode: 11})

	info := r.tr.Info()
	if info.PeerVideo == nil || !info.PeerVideo.Active || !info.PeerVideo.Upgrade || info.PeerVideo.Orientation != 1 {
		t.Fatalf("PeerVideo = %+v", info.PeerVideo)
	}
	published := r.ev.named("CallVideoState")
	if len(published) != 1 || published[0]["callId"] != "C1" || published[0]["upgrade"] != true || published[0]["orientation"] != 1 ||
		published[0]["state"] != call_engine.VideoStateUpgradeRequest || published[0]["stateCode"] != 11 {
		t.Fatalf("published = %+v", published)
	}
	if len(r.states) != 1 || !r.states[0].Upgrade || r.states[0].State != call_engine.VideoStateUpgradeRequest {
		t.Fatalf("the stream got %+v", r.states)
	}
}

func TestAStreamThatLeftGetsNoMoreVideoEventsButTheStateIsStillKept(t *testing.T) {
	r := newRig(t)
	_, detach := r.attach(false)
	detach()

	r.call.PeerVideoState(call_engine.VideoState{Active: true})
	r.call.KeyframeRequest()

	if len(r.states) != 0 || r.keys != 0 {
		t.Fatalf("a detached stream was called: states=%v keys=%d", r.states, r.keys)
	}
	if r.tr.Info().PeerVideo == nil || len(r.ev.named("CallVideoState")) != 1 {
		t.Fatal("the state must be kept and published whether or not a stream listens")
	}
}

func TestAKeyframeRequestIsCountedAndReachesTheStream(t *testing.T) {
	r := newRig(t)
	stats, _ := r.attach(true)

	r.call.KeyframeRequest()
	r.call.KeyframeRequest()

	if r.keys != 2 || stats.KeyframeRequests.Load() != 2 {
		t.Fatalf("keys=%d counted=%d, want 2 and 2", r.keys, stats.KeyframeRequests.Load())
	}
}

func TestAKeyframeRequestWithNoStreamIsHarmless(t *testing.T) {
	r := newRig(t)
	r.call.KeyframeRequest() // nobody listens: must not panic
}

func TestVideoIsOnlyAttachedWhenTheStreamAsksForIt(t *testing.T) {
	r := newRig(t)
	_, detach := r.attach(false)
	if r.call.VideoSink() != nil {
		t.Fatal("a video sink was attached to an audio-only stream")
	}
	detach()

	_, detach = r.attach(true)
	if r.call.VideoSink() == nil {
		t.Fatal("the video sink was not attached")
	}
	detach()
	if r.call.VideoSink() != nil {
		t.Fatal("the video sink stays attached after detach")
	}
}

func TestInfoReportsTheVideoFlows(t *testing.T) {
	r := newRig(t)
	if info := r.tr.Info(); info.VideoSending || info.VideoReceiving {
		t.Fatalf("info = %+v", info)
	}
	r.call.SetVideoFlows(true, true)
	if info := r.tr.Info(); !info.VideoSending || !info.VideoReceiving {
		t.Fatalf("info = %+v", info)
	}
}

func TestVideoStreamStatsAreReported(t *testing.T) {
	r := newRig(t)
	stats, _ := r.attach(true)
	stats.VideoToClient.Add(5)
	stats.VideoDroppedToClient.Add(2)
	stats.KeyframeRequests.Add(1)

	got := r.tr.Info().Stream
	if got == nil || got.VideoToClient != 5 || got.VideoDroppedToClient != 2 || got.KeyframeRequests != 1 {
		t.Fatalf("stream = %+v", got)
	}
}
