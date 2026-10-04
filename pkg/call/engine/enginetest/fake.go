// Package enginetest holds a fake call for tests of the code that sits on top of the
// call engine, so they need neither the call library nor a WhatsApp connection.
package enginetest

import (
	"errors"
	"sync"
	"time"

	call_engine "github.com/lucasgiovannibr/whatygo/pkg/call/engine"
	"go.mau.fi/whatsmeow/types"
)

// SentVideo is one access unit the code under test sent to the peer.
type SentVideo struct {
	AccessUnit []byte
	Duration   time.Duration
}

// Fake behaves like the library's call where it matters: Answer moves it to
// connecting, Reject and Hangup end it locally (firing OnEnd) before they try the
// network, and a sink or source attached to it stays until it is replaced.
type Fake struct {
	id string

	mu       sync.Mutex
	phase    call_engine.Phase
	video    bool
	onEnd    func(string)
	onReady  func()
	sink     call_engine.AudioSink
	src      call_engine.AudioSource
	answered int
	ended    int

	videoSink        call_engine.VideoSink
	onVideoState     func(call_engine.VideoState)
	onKeyframe       func()
	sent             []SentVideo
	videoErr         error // what SendVideo answers, to imitate a call with no video media yet
	videoActions     []string
	videoActionErr   error
	sending, recving bool
}

func NewFake(id string) *Fake { return &Fake{id: id, phase: call_engine.PhaseRinging} }

func (f *Fake) ID() string      { return f.id }
func (f *Fake) Peer() types.JID { return types.NewJID("5511999990000", types.DefaultUserServer) }

func (f *Fake) IsVideo() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.video }

func (f *Fake) Phase() call_engine.Phase {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.phase
}

func (f *Fake) SetPhase(p call_engine.Phase) {
	f.mu.Lock()
	f.phase = p
	f.mu.Unlock()
}

func (f *Fake) SetVideo(v bool) { f.mu.Lock(); f.video = v; f.mu.Unlock() }

func (f *Fake) OnReady(fn func())               { f.mu.Lock(); f.onReady = fn; f.mu.Unlock() }
func (f *Fake) OnEnd(fn func(string))           { f.mu.Lock(); f.onEnd = fn; f.mu.Unlock() }
func (f *Fake) Receive(s call_engine.AudioSink) { f.mu.Lock(); f.sink = s; f.mu.Unlock() }
func (f *Fake) Play(s call_engine.AudioSource)  { f.mu.Lock(); f.src = s; f.mu.Unlock() }

func (f *Fake) Answer() error {
	f.mu.Lock()
	f.answered++
	f.phase = call_engine.PhaseConnecting
	f.mu.Unlock()
	return nil
}

// Ready makes the media come up, as the library announces with OnReady.
func (f *Fake) Ready() {
	f.mu.Lock()
	f.phase = call_engine.PhaseActive
	fn := f.onReady
	f.mu.Unlock()
	if fn != nil {
		fn()
	}
}

func (f *Fake) Reject() error { f.End("rejected"); return nil }
func (f *Fake) Hangup() error { f.End("hangup"); return nil }

// End ends the call the way the peer hanging up would.
func (f *Fake) End(reason string) {
	f.mu.Lock()
	f.phase = call_engine.PhaseEnded
	f.ended++
	fn := f.onEnd
	f.mu.Unlock()
	if fn != nil {
		fn(reason)
	}
}

// Sink is the sink the code under test attached with Receive.
func (f *Fake) Sink() call_engine.AudioSink {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sink
}

// Source is the source the code under test attached with Play.
func (f *Fake) Source() call_engine.AudioSource {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.src
}

func (f *Fake) Answered() int { f.mu.Lock(); defer f.mu.Unlock(); return f.answered }

// --- video

func (f *Fake) ReceiveVideo(s call_engine.VideoSink) { f.mu.Lock(); f.videoSink = s; f.mu.Unlock() }

// VideoSink is the sink the code under test attached with ReceiveVideo.
func (f *Fake) VideoSink() call_engine.VideoSink {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.videoSink
}

func (f *Fake) OnVideoState(fn func(call_engine.VideoState)) {
	f.mu.Lock()
	f.onVideoState = fn
	f.mu.Unlock()
}
func (f *Fake) OnVideoKeyframeRequest(fn func()) { f.mu.Lock(); f.onKeyframe = fn; f.mu.Unlock() }

// PeerVideoState makes the peer report its video state.
func (f *Fake) PeerVideoState(v call_engine.VideoState) {
	f.mu.Lock()
	fn := f.onVideoState
	f.mu.Unlock()
	if fn != nil {
		fn(v)
	}
}

// KeyframeRequest makes WhatsApp ask for a keyframe.
func (f *Fake) KeyframeRequest() {
	f.mu.Lock()
	fn := f.onKeyframe
	f.mu.Unlock()
	if fn != nil {
		fn()
	}
}

func (f *Fake) SendVideo(au []byte, d time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.videoErr != nil {
		return f.videoErr
	}
	f.sent = append(f.sent, SentVideo{AccessUnit: append([]byte(nil), au...), Duration: d})
	return nil
}

// RefuseVideo makes SendVideo fail like a call whose video media is not up.
func (f *Fake) RefuseVideo() {
	f.mu.Lock()
	f.videoErr = errors.New("call has no active video media")
	f.mu.Unlock()
}

// AllowVideo undoes RefuseVideo.
func (f *Fake) AllowVideo() { f.mu.Lock(); f.videoErr = nil; f.mu.Unlock() }

// SentVideo is what was sent with SendVideo.
func (f *Fake) SentVideo() []SentVideo {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]SentVideo(nil), f.sent...)
}

func (f *Fake) action(name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.videoActions = append(f.videoActions, name)
	return f.videoActionErr
}

func (f *Fake) StartVideo() error  { return f.action("start") }
func (f *Fake) AcceptVideo() error { return f.action("accept") }
func (f *Fake) StopVideo() error   { return f.action("stop") }
func (f *Fake) SetVideoEnabled(on bool) error {
	if on {
		return f.action("enable")
	}
	return f.action("disable")
}

// orientation actions are recorded as "orientation:N".
func (f *Fake) SetVideoOrientation(o int) error {
	return f.action("orientation:" + string(rune('0'+o)))
}

// VideoActions lists the video controls that were used, in order.
func (f *Fake) VideoActions() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.videoActions...)
}

// FailVideoActions makes the video controls answer err, like the library refusing them.
func (f *Fake) FailVideoActions(err error) { f.mu.Lock(); f.videoActionErr = err; f.mu.Unlock() }

func (f *Fake) IsSendingVideo() bool   { f.mu.Lock(); defer f.mu.Unlock(); return f.sending }
func (f *Fake) IsReceivingVideo() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.recving }
func (f *Fake) SetVideoFlows(sending, receiving bool) {
	f.mu.Lock()
	f.sending, f.recving = sending, receiving
	f.mu.Unlock()
}
