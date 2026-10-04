package call_stream

import (
	"encoding/binary"
	"testing"
	"time"

	call_engine "github.com/lucasgiovannibr/whatygo/pkg/call/engine"
)

func testBridge() (*bridge, *call_engine.StreamStats, *time.Time) {
	stats := &call_engine.StreamStats{}
	b := newBridge(stats)
	now := time.Unix(1_000_000, 0)
	b.now = func() time.Time { return now }
	return b, stats, &now
}

func pcmOf(samples int, value int16) []byte {
	out := make([]byte, samples*2)
	for i := 0; i < samples; i++ {
		binary.LittleEndian.PutUint16(out[2*i:], uint16(value))
	}
	return out
}

// The library asks for a frame every 60 ms from its send loop. Blocking there would
// stall the call, so an empty queue is answered with nothing (silence) at once.
func TestReadFrameNeverBlocksAndAnswersSilenceWhenThereIsNothing(t *testing.T) {
	b, _, _ := testBridge()

	done := make(chan struct{})
	go func() {
		frame, err := b.ReadFrame()
		if frame != nil || err != nil {
			t.Errorf("ReadFrame() = %v, %v; want nil, nil", frame, err)
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("ReadFrame blocked")
	}
}

func TestClientAudioComesOutInWholeFrames(t *testing.T) {
	b, stats, _ := testBridge()

	// 2.5 frames in one chunk, then the rest of the third in another
	b.Push(pcmOf(frameSamples*2+frameSamples/2, 16384))
	for i := 0; i < 2; i++ {
		frame, _ := b.ReadFrame()
		if len(frame) != frameSamples {
			t.Fatalf("frame %d has %d samples, want %d", i, len(frame), frameSamples)
		}
		if frame[0] != 0.5 {
			t.Fatalf("sample = %v, want 0.5 (16384/32768)", frame[0])
		}
	}
	if frame, _ := b.ReadFrame(); frame != nil {
		t.Fatalf("an incomplete frame was sent right away: %d samples", len(frame))
	}

	b.Push(pcmOf(frameSamples/2, 16384))
	frame, _ := b.ReadFrame()
	if len(frame) != frameSamples {
		t.Fatalf("the completed third frame has %d samples", len(frame))
	}
	if got := stats.FromClient.Load(); got != 3 {
		t.Fatalf("FromClient = %d, want 3", got)
	}
}

func TestTheTailOfAnUtteranceIsPaddedAndSent(t *testing.T) {
	b, _, now := testBridge()
	b.Push(pcmOf(100, 8192))

	if frame, _ := b.ReadFrame(); frame != nil {
		t.Fatal("the tail was sent before it had a chance to be completed")
	}

	*now = now.Add(tailFlushAfter)
	frame, _ := b.ReadFrame()
	if len(frame) != frameSamples {
		t.Fatalf("tail frame has %d samples, want %d", len(frame), frameSamples)
	}
	if frame[99] != 0.25 || frame[100] != 0 {
		t.Fatalf("frame[99]=%v frame[100]=%v: the audio must be kept and the rest silence", frame[99], frame[100])
	}
	if next, _ := b.ReadFrame(); next != nil {
		t.Fatal("the tail was sent twice")
	}
}

func TestAChunkOfOddLengthKeepsItsLastByteForTheNextOne(t *testing.T) {
	b, _, _ := testBridge()
	chunk := pcmOf(frameSamples, 1000)

	b.Push(chunk[:len(chunk)-1]) // one byte short of a whole frame
	if frame, _ := b.ReadFrame(); frame != nil {
		t.Fatal("a frame came out of an incomplete sample")
	}
	b.Push(chunk[len(chunk)-1:])

	frame, _ := b.ReadFrame()
	if len(frame) != frameSamples {
		t.Fatalf("got %d samples, want %d", len(frame), frameSamples)
	}
	if want := float32(1000) / 32768; frame[frameSamples-1] != want {
		t.Fatalf("last sample = %v, want %v: the split sample was damaged", frame[frameSamples-1], want)
	}
}

func TestClearDropsTheAudioNotYetSent(t *testing.T) {
	b, _, _ := testBridge()
	b.Push(pcmOf(frameSamples*5, 100))

	b.Clear()

	if frame, _ := b.ReadFrame(); frame != nil {
		t.Fatal("audio survived Clear")
	}
}

func TestTooMuchClientAudioIsDroppedAndCounted(t *testing.T) {
	b, stats, _ := testBridge()

	b.Push(pcmOf(maxOutboundSamples-frameSamples, 1))
	if got := stats.DroppedFromClient.Load(); got != 0 {
		t.Fatalf("dropped %d frames while under the limit", got)
	}

	b.Push(pcmOf(frameSamples*3, 1)) // does not fit
	if got := stats.DroppedFromClient.Load(); got != 3 {
		t.Fatalf("DroppedFromClient = %d, want 3", got)
	}
}

func TestPeerAudioIsDroppedOldestFirstWhenTheClientIsSlow(t *testing.T) {
	b, stats, _ := testBridge()

	for i := 0; i < toClientFrames+10; i++ {
		b.WriteFrame([]float32{float32(i)})
	}

	if got := stats.DroppedToClient.Load(); got != 10 {
		t.Fatalf("DroppedToClient = %d, want 10", got)
	}
	first := <-b.toClient
	if first.samples[0] != 10 {
		t.Fatalf("oldest kept frame is #%v, want #10: the oldest audio must go first", first.samples[0])
	}
}

func TestWriteFrameNeverBlocks(t *testing.T) {
	b, _, _ := testBridge()
	done := make(chan struct{})
	go func() {
		for i := 0; i < toClientFrames*4; i++ {
			b.WriteFrame([]float32{1})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("WriteFrame blocked on a client that is not reading")
	}
}

func TestWriteFrameCopiesTheLibrarysBuffer(t *testing.T) {
	b, _, _ := testBridge()
	buf := []float32{1, 2, 3}
	b.WriteFrame(buf)
	buf[0] = 99
	if got := (<-b.toClient).samples[0]; got != 1 {
		t.Fatalf("frame changed after the library reused its buffer: %v", got)
	}
}

func TestCloseIsIdempotentAndStopsClientAudio(t *testing.T) {
	b, _, _ := testBridge()
	b.Push(pcmOf(frameSamples, 1))

	b.Close()
	b.Close()

	b.Push(pcmOf(frameSamples, 1))
	if frame, _ := b.ReadFrame(); frame != nil {
		t.Fatal("audio came out of a closed bridge")
	}
	select {
	case <-b.done:
	default:
		t.Fatal("done was not closed")
	}
}

func TestPCM16RoundTripAndClamping(t *testing.T) {
	got := pcm16([]float32{0, 0.5, -0.5, 2, -2})
	want := []int16{0, 16384, -16384, 32767, -32768}
	for i, w := range want {
		if v := int16(binary.LittleEndian.Uint16(got[2*i:])); v != w {
			t.Errorf("sample %d = %d, want %d", i, v, w)
		}
	}
}

// --- marks

func markNames(b *bridge) []string { return b.FinishedMarks() }

func sameNames(got []string, want ...string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestAMarkWaitsForTheAudioBeforeIt(t *testing.T) {
	b, _, _ := testBridge()
	b.Push(pcmOf(frameSamples*2, 1000))
	if !b.Mark("a") {
		t.Fatal("the mark was refused")
	}
	if got := markNames(b); len(got) != 0 {
		t.Fatalf("the mark came back with all its audio still queued: %v", got)
	}

	b.ReadFrame()
	if got := markNames(b); len(got) != 0 {
		t.Fatalf("the mark came back after one of two frames: %v", got)
	}
	b.ReadFrame()
	if got := markNames(b); !sameNames(got, "a") {
		t.Fatalf("marks after the audio went out = %v, want [a]", got)
	}
	select {
	case <-b.markReady:
	default:
		t.Fatal("the writer was not woken")
	}
}

func TestAMarkWithNothingQueuedComesBackAtOnce(t *testing.T) {
	b, _, _ := testBridge()
	b.Mark("now")
	if got := markNames(b); !sameNames(got, "now") {
		t.Fatalf("marks = %v, want [now]", got)
	}
}

func TestMarksComeBackInOrderAsTheirAudioGoesOut(t *testing.T) {
	b, _, _ := testBridge()
	b.Push(pcmOf(frameSamples, 1000))
	b.Mark("a")
	b.Push(pcmOf(frameSamples, 1000))
	b.Mark("b")

	b.ReadFrame()
	if got := markNames(b); !sameNames(got, "a") {
		t.Fatalf("after the first frame = %v, want [a]", got)
	}
	b.ReadFrame()
	if got := markNames(b); !sameNames(got, "b") {
		t.Fatalf("after the second frame = %v, want [b]", got)
	}
}

func TestTheShortTailOfAnUtteranceFinishesItsMarkWhenItIsFlushed(t *testing.T) {
	b, _, now := testBridge()
	b.Push(pcmOf(frameSamples/2, 1000))
	b.Mark("end")

	if frame, _ := b.ReadFrame(); frame != nil {
		t.Fatal("the tail was sent before it had waited for more")
	}
	if got := markNames(b); len(got) != 0 {
		t.Fatalf("mark before the tail went out: %v", got)
	}
	*now = now.Add(tailFlushAfter)
	if frame, _ := b.ReadFrame(); frame == nil {
		t.Fatal("the tail was never sent")
	}
	if got := markNames(b); !sameNames(got, "end") {
		t.Fatalf("marks = %v, want [end]", got)
	}
}

func TestClearGivesBackEveryWaitingMarkAndStartsOver(t *testing.T) {
	b, _, _ := testBridge()
	b.Push(pcmOf(frameSamples*3, 1000))
	b.Mark("one")
	b.Push(pcmOf(frameSamples, 1000))
	b.Mark("two")

	b.Clear()
	if got := markNames(b); !sameNames(got, "one", "two") {
		t.Fatalf("marks after clear = %v, want [one two]", got)
	}

	// what is pushed afterwards is counted from the cleared position, not from before
	b.Push(pcmOf(frameSamples, 1000))
	b.Mark("three")
	if got := markNames(b); len(got) != 0 {
		t.Fatalf("a mark on new audio came back before it was played: %v", got)
	}
	b.ReadFrame()
	if got := markNames(b); !sameNames(got, "three") {
		t.Fatalf("marks = %v, want [three]", got)
	}
}

func TestMarksAreBoundedAndRefusedBeyondTheLimit(t *testing.T) {
	b, _, _ := testBridge()
	b.Push(pcmOf(frameSamples, 1000))
	for i := 0; i < maxPendingMarks; i++ {
		if !b.Mark("m") {
			t.Fatalf("mark %d was refused", i)
		}
	}
	if b.Mark("one too many") {
		t.Fatal("a mark beyond the limit was accepted")
	}
	b.ReadFrame()
	if got := markNames(b); len(got) != maxPendingMarks {
		t.Fatalf("%d marks came back, want %d", len(got), maxPendingMarks)
	}
}

// Audio refused because the client sent too much is not queued, so a mark after it must
// not wait for audio that will never be played.
func TestAudioThatWasDroppedDoesNotHoldBackAMark(t *testing.T) {
	b, stats, _ := testBridge()
	b.Push(pcmOf(maxOutboundSamples, 1000))
	b.Push(pcmOf(frameSamples, 1000)) // over the limit: dropped
	if stats.DroppedFromClient.Load() == 0 {
		t.Fatal("the test did not overflow the queue")
	}
	b.Mark("end")
	for i := 0; i < maxOutboundSamples/frameSamples; i++ {
		b.ReadFrame()
	}
	if got := markNames(b); !sameNames(got, "end") {
		t.Fatalf("marks = %v, want [end]", got)
	}
}

func TestMarksStopWhenTheBridgeCloses(t *testing.T) {
	b, _, _ := testBridge()
	b.Push(pcmOf(frameSamples, 1000))
	b.Mark("a")
	b.Close()
	if !b.Mark("late") {
		t.Fatal("a mark on a closed bridge was refused")
	}
	if got := markNames(b); len(got) != 0 {
		t.Fatalf("marks of a closed bridge came back: %v", got)
	}
}
