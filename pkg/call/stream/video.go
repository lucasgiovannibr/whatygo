package call_stream

import (
	"errors"
	"sync"
	"sync/atomic"
	"time"

	call_engine "github.com/lucasgiovannibr/whatygo/pkg/call/engine"
)

const (
	// videoQueueFrames is how much of the peer's video waits for a slow client: about
	// two seconds at 30 frames a second.
	videoQueueFrames = 60

	// A frame's duration is the time since the client sent the previous one, kept in a
	// sane range so that a pause (or a burst) does not throw the RTP clock off.
	minFrameDuration = 5 * time.Millisecond
	maxFrameDuration = 200 * time.Millisecond

	// maxAccessUnitBytes bounds one video access unit from a client. A keyframe of a
	// 1080p stream is a few hundred kilobytes.
	maxAccessUnitBytes = 768 * 1024
)

// ErrAccessUnitTooLarge: one video access unit is bigger than maxAccessUnitBytes.
var ErrAccessUnitTooLarge = errors.New("video access unit too large")

// ErrNotAnnexB: the client's video is not H.264 in Annex-B framing.
var ErrNotAnnexB = errors.New("video must be H.264 access units in Annex-B framing (start codes 00 00 01 / 00 00 00 01), not length-prefixed (MP4/AVCC)")

// videoFrame is one access unit of the peer's video on its way to the client.
type videoFrame struct {
	data        []byte
	keyframe    bool
	orientation int
	at          time.Time // when it reached the stream
}

// videoIn is the peer-to-client side of the video of a stream: the VideoSink the
// library writes to and the queue the socket writer reads.
//
// Video cannot be dropped like audio: a frame that is missing breaks every frame after
// it until the next keyframe. So when the client falls behind, the whole queue goes and
// nothing is delivered until a keyframe comes, and a client always starts on one.
type videoIn struct {
	stats *call_engine.StreamStats

	frames chan videoFrame

	mu      sync.Mutex
	waitKey bool

	orientation atomic.Int32
}

func newVideoIn(stats *call_engine.StreamStats) *videoIn {
	return &videoIn{stats: stats, frames: make(chan videoFrame, videoQueueFrames), waitKey: true}
}

// WriteVideo receives the peer's video from the library. It never blocks.
func (v *videoIn) WriteVideo(au []byte) error {
	if len(au) == 0 {
		return nil
	}
	key := isKeyframe(au)

	v.mu.Lock()
	defer v.mu.Unlock()

	if v.waitKey && !key {
		v.stats.VideoDroppedToClient.Add(1)
		return nil
	}
	frame := videoFrame{data: append([]byte(nil), au...), keyframe: key, orientation: int(v.orientation.Load()), at: time.Now()}

	select {
	case v.frames <- frame:
		if key {
			v.waitKey = false
		}
		return nil
	default:
	}

	// The client is behind: what is queued is now a broken sequence.
	for {
		select {
		case <-v.frames:
			v.stats.VideoDroppedToClient.Add(1)
			continue
		default:
		}
		break
	}
	if key {
		v.frames <- frame
		v.waitKey = false
		return nil
	}
	v.stats.VideoDroppedToClient.Add(1)
	v.waitKey = true
	return nil
}

// SetOrientation is told by the library the rotation the peer's frames carry in their
// RTP header (CVO). The library documents it as clockwise quarter turns, but live tests
// against an iPhone show it counts counter-clockwise: value 3 needed one clockwise turn
// to come out upright, 2 needed two, 0 none. So a client gets the clockwise quarter
// turns that make the picture upright, (4 - value) mod 4.
func (v *videoIn) SetOrientation(orientation int) {
	v.orientation.Store(int32(uprightTurns(orientation)))
}

// uprightTurns turns the library's rotation value into the clockwise quarter turns a
// client must apply to show the picture upright.
func uprightTurns(libraryValue int) int { return (4 - libraryValue&3) & 3 }

// Close is called by the library when the call ends; the socket writer stops with it.
func (v *videoIn) Close() error { return nil }

// videoOut is the client-to-peer side: it checks and repairs the client's access units
// and sends them through the call.
type videoOut struct {
	send  func([]byte, time.Duration) error
	stats *call_engine.StreamStats
	now   func() time.Time

	mu      sync.Mutex
	headers headerRepair
	last    time.Time
}

func newVideoOut(send func([]byte, time.Duration) error, stats *call_engine.StreamStats) *videoOut {
	return &videoOut{send: send, stats: stats, now: time.Now}
}

// Send sends one access unit from the client to the peer. An access unit that is not
// Annex-B H.264, or too large, is refused with an error the client can act on. One the
// library refuses (the call has no video media yet, or the peer has not accepted video)
// is dropped and counted: it is normal for a client to start sending a little early.
func (o *videoOut) Send(au []byte) error {
	if len(au) > maxAccessUnitBytes {
		return ErrAccessUnitTooLarge
	}

	o.mu.Lock()
	repaired, ok := o.headers.repair(au)
	var duration time.Duration
	now := o.now()
	if !o.last.IsZero() {
		duration = min(max(now.Sub(o.last), minFrameDuration), maxFrameDuration)
	}
	o.last = now
	o.mu.Unlock()

	if !ok {
		return ErrNotAnnexB
	}
	if err := o.send(repaired, duration); err != nil {
		o.stats.VideoDroppedFromClient.Add(1)
		return err
	}
	o.stats.VideoFromClient.Add(1)
	return nil
}
