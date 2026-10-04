package call_stream

import (
	"bytes"
	"errors"
	"testing"
	"time"

	call_engine "github.com/lucasgiovannibr/whatygo/pkg/call/engine"
)

func keyAU() []byte { return annexB(false, sps, pps, idr) }
func pAU() []byte   { return annexB(false, slice) }

func TestAClientStartsOnAKeyframe(t *testing.T) {
	stats := &call_engine.StreamStats{}
	v := newVideoIn(stats)

	// the stream attached in the middle of a GOP: these are useless without their keyframe
	v.WriteVideo(pAU())
	v.WriteVideo(pAU())
	if len(v.frames) != 0 || stats.VideoDroppedToClient.Load() != 2 {
		t.Fatalf("queued=%d dropped=%d, want 0 and 2", len(v.frames), stats.VideoDroppedToClient.Load())
	}

	v.WriteVideo(keyAU())
	v.WriteVideo(pAU())
	if len(v.frames) != 2 {
		t.Fatalf("queued = %d, want the keyframe and the frame after it", len(v.frames))
	}
	first := <-v.frames
	if !first.keyframe || !bytes.Equal(first.data, keyAU()) {
		t.Fatalf("first frame = %+v", first)
	}
	if second := <-v.frames; second.keyframe {
		t.Fatal("the P frame was marked as a keyframe")
	}
}

func TestASlowClientLosesTheWholeSequenceAndResumesOnAKeyframe(t *testing.T) {
	stats := &call_engine.StreamStats{}
	v := newVideoIn(stats)
	v.WriteVideo(keyAU())
	for i := 0; i < videoQueueFrames-1; i++ {
		v.WriteVideo(pAU())
	}
	if len(v.frames) != videoQueueFrames {
		t.Fatalf("queue = %d, want it full", len(v.frames))
	}

	v.WriteVideo(pAU()) // does not fit

	if len(v.frames) != 0 {
		t.Fatalf("queue = %d: a queue with a hole in it must be dropped whole", len(v.frames))
	}
	if got := stats.VideoDroppedToClient.Load(); got != videoQueueFrames+1 {
		t.Fatalf("dropped = %d, want %d", got, videoQueueFrames+1)
	}

	// P frames are refused until the next keyframe...
	v.WriteVideo(pAU())
	if len(v.frames) != 0 {
		t.Fatal("a P frame was queued while waiting for a keyframe")
	}
	// ... which restarts the sequence
	v.WriteVideo(keyAU())
	v.WriteVideo(pAU())
	if len(v.frames) != 2 || !(<-v.frames).keyframe {
		t.Fatal("the stream did not resume on the keyframe")
	}
}

func TestAKeyframeMakesRoomWhenTheQueueIsFull(t *testing.T) {
	v := newVideoIn(&call_engine.StreamStats{})
	v.WriteVideo(keyAU())
	for i := 0; i < videoQueueFrames-1; i++ {
		v.WriteVideo(pAU())
	}

	v.WriteVideo(keyAU())

	if len(v.frames) != 1 || !(<-v.frames).keyframe {
		t.Fatal("a keyframe arriving at a full queue must replace it")
	}
}

func TestWriteVideoNeverBlocks(t *testing.T) {
	v := newVideoIn(&call_engine.StreamStats{})
	done := make(chan struct{})
	go func() {
		for i := 0; i < videoQueueFrames*3; i++ {
			v.WriteVideo(keyAU())
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("WriteVideo blocked on a client that is not reading")
	}
}

func TestWriteVideoCopiesTheLibrarysBufferAndCarriesTheOrientation(t *testing.T) {
	v := newVideoIn(&call_engine.StreamStats{})
	v.SetOrientation(3)
	buf := keyAU()
	v.WriteVideo(buf)
	buf[5] = 0xff

	got := <-v.frames
	if !bytes.Equal(got.data, keyAU()) {
		t.Fatal("the frame changed after the library reused its buffer")
	}
	// The library's value 3 is one clockwise turn (seen live on an iPhone).
	if got.orientation != 1 {
		t.Fatalf("orientation = %d, want 1", got.orientation)
	}
}

// What the library reports for the camera is counter-clockwise quarter turns; a client
// is given the clockwise turns that make the picture upright.
func TestTheLibrarysRotationBecomesClockwiseTurnsToShowTheVideoUpright(t *testing.T) {
	for library, want := range map[int]int{0: 0, 1: 3, 2: 2, 3: 1} {
		v := newVideoIn(&call_engine.StreamStats{})
		v.SetOrientation(library)
		v.WriteVideo(keyAU())
		if got := (<-v.frames).orientation; got != want {
			t.Errorf("library %d: client gets %d, want %d", library, got, want)
		}
	}
}

type sentAU struct {
	au       []byte
	duration time.Duration
}

func newTestOut(now *time.Time, sendErr *error) (*videoOut, *[]sentAU, *call_engine.StreamStats) {
	stats := &call_engine.StreamStats{}
	var sent []sentAU
	o := newVideoOut(func(au []byte, d time.Duration) error {
		if sendErr != nil && *sendErr != nil {
			return *sendErr
		}
		sent = append(sent, sentAU{append([]byte(nil), au...), d})
		return nil
	}, stats)
	o.now = func() time.Time { return *now }
	return o, &sent, stats
}

func TestClientVideoIsSentWithTheTimeBetweenFrames(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	o, sent, stats := newTestOut(&now, nil)

	o.Send(keyAU())
	now = now.Add(66 * time.Millisecond)
	o.Send(pAU())
	now = now.Add(2 * time.Second) // a pause
	o.Send(pAU())
	now = now.Add(time.Millisecond) // a burst
	o.Send(pAU())

	got := []time.Duration{}
	for _, s := range *sent {
		got = append(got, s.duration)
	}
	want := []time.Duration{0, 66 * time.Millisecond, maxFrameDuration, minFrameDuration}
	if len(got) != len(want) {
		t.Fatalf("durations = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("durations = %v, want %v", got, want)
		}
	}
	if stats.VideoFromClient.Load() != 4 {
		t.Fatalf("VideoFromClient = %d", stats.VideoFromClient.Load())
	}
}

func TestABareKeyframeIsSentWithTheHeadersOfTheFirstOne(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	o, sent, _ := newTestOut(&now, nil)
	o.Send(keyAU())

	o.Send(annexB(false, idr))

	if got := (*sent)[1].au; !bytes.Equal(got, keyAU()) {
		t.Fatalf("sent %x, want the IDR with the SPS and PPS put back", got)
	}
}

func TestVideoThatIsNotAnnexBIsRefusedAndNeverSent(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	o, sent, _ := newTestOut(&now, nil)

	err := o.Send([]byte{0, 0, 0, 5, 0x65, 1, 2, 3, 4}) // AVCC

	if !errors.Is(err, ErrNotAnnexB) || len(*sent) != 0 {
		t.Fatalf("err=%v sent=%d", err, len(*sent))
	}
}

func TestAnOversizedAccessUnitIsRefused(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	o, sent, _ := newTestOut(&now, nil)

	if err := o.Send(make([]byte, maxAccessUnitBytes+1)); err == nil || len(*sent) != 0 {
		t.Fatalf("err=%v sent=%d", err, len(*sent))
	}
}

func TestVideoTheLibraryRefusesIsCountedAsDropped(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	refuse := errors.New("call has no active video media")
	o, sent, stats := newTestOut(&now, &refuse)

	err := o.Send(keyAU())

	if err == nil || len(*sent) != 0 {
		t.Fatalf("err=%v sent=%d", err, len(*sent))
	}
	if stats.VideoDroppedFromClient.Load() != 1 || stats.VideoFromClient.Load() != 0 {
		t.Fatalf("dropped=%d sent=%d", stats.VideoDroppedFromClient.Load(), stats.VideoFromClient.Load())
	}
}
