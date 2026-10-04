package call_stream

import (
	"encoding/binary"
	"sync"
	"time"

	call_engine "github.com/lucasgiovannibr/whatygo/pkg/call/engine"
)

const (
	frameSamples = call_engine.FrameSamples

	// toClientFrames is how much of the peer's audio waits for a slow client: 900 ms.
	// Past that the oldest audio is dropped, because a listener that is behind wants the
	// present, not a growing delay. Live, the peer's audio arrives in bursts of up to a
	// dozen frames a second at most, so this leaves room for a client that stalls a
	// moment and no more: it used to be three seconds, which let a lagging client talk
	// to a conversation that was three seconds old.
	toClientFrames = 15

	// maxPendingMarks is how many marks a client may have waiting for its audio to be
	// played. A mark is a few bytes, but they are kept until the audio before them goes.
	maxPendingMarks = 64

	// maxOutboundSamples is how much audio a client may have queued to be sent: thirty
	// seconds. A text-to-speech engine produces a sentence much faster than real time
	// and expects it to be played out at the right pace, so this is deliberately large.
	maxOutboundSamples = 30 * call_engine.SampleRate

	// tailFlushAfter is how long the last, incomplete frame of a burst of client audio
	// waits for more before it is padded with silence and sent.
	tailFlushAfter = 120 * time.Millisecond
)

// bridge is the audio side of one stream: the AudioSink that receives the peer's audio
// and the AudioSource that supplies the client's, both without ever blocking the
// library's goroutines. The library sends a frame every 60 ms and wants silence, not a
// stall, when there is nothing to send: a call that stops transmitting loses its relay.
type bridge struct {
	stats *call_engine.StreamStats

	toClient chan audioFrame // peer audio, read by the socket writer

	mu       sync.Mutex
	pending  []float32 // client audio not yet sent to the peer
	odd      []byte    // a lone byte left over when a chunk had an odd length
	lastPush time.Time
	closed   bool
	done     chan struct{}
	now      func() time.Time

	// Marks. pushed and taken count the samples of client audio accepted and handed to
	// the library since the stream began; a mark waits for taken to reach the pushed
	// count it was set at. Finished marks are collected in order and the socket writer is
	// woken through markReady, so the library's goroutine never waits for the socket.
	pushed, taken uint64
	marks         []pendingMark
	finishedMarks []string
	markReady     chan struct{}
}

type pendingMark struct {
	name string
	at   uint64
}

// audioFrame is a piece of the peer's audio and when it reached the stream.
type audioFrame struct {
	samples []float32
	at      time.Time
}

func newBridge(stats *call_engine.StreamStats) *bridge {
	return &bridge{
		stats:     stats,
		toClient:  make(chan audioFrame, toClientFrames),
		done:      make(chan struct{}),
		now:       time.Now,
		markReady: make(chan struct{}, 1),
	}
}

// WriteFrame receives the peer's audio from the library.
func (b *bridge) WriteFrame(frame []float32) error {
	if len(frame) == 0 {
		return nil
	}
	// The library may reuse its buffer once this returns.
	af := audioFrame{samples: append([]float32(nil), frame...), at: b.now()}

	select {
	case b.toClient <- af:
	default:
		// full: make room by dropping the oldest frame
		select {
		case <-b.toClient:
			b.stats.DroppedToClient.Add(1)
		default:
		}
		select {
		case b.toClient <- af:
		default:
			b.stats.DroppedToClient.Add(1)
		}
	}
	return nil
}

// ReadFrame is called by the library's send loop every 60 ms. It returns one frame of
// the client's audio, or nil (silence) when there is none.
func (b *bridge) ReadFrame() ([]float32, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if len(b.pending) >= frameSamples {
		return b.takeLocked(frameSamples), nil
	}
	// The tail of an utterance is shorter than a frame; without this it would wait for
	// audio that may only come much later.
	if n := len(b.pending); n > 0 && b.now().Sub(b.lastPush) >= tailFlushAfter {
		frame := make([]float32, frameSamples)
		copy(frame, b.pending)
		b.taken += uint64(len(b.pending))
		b.pending = b.pending[:0]
		b.stats.FromClient.Add(1)
		b.finishMarksLocked()
		return frame, nil
	}
	return nil, nil
}

func (b *bridge) takeLocked(n int) []float32 {
	frame := make([]float32, n)
	copy(frame, b.pending)
	b.pending = append(b.pending[:0], b.pending[n:]...)
	b.taken += uint64(n)
	b.stats.FromClient.Add(1)
	b.finishMarksLocked()
	return frame
}

// Push queues a chunk of client audio: signed 16-bit little-endian mono at 16 kHz, of
// any length. Audio that does not fit within maxOutboundSamples is dropped.
func (b *bridge) Push(pcm []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}

	if len(b.odd) > 0 {
		pcm = append(b.odd, pcm...)
		b.odd = nil
	}
	if len(pcm)%2 == 1 {
		b.odd = []byte{pcm[len(pcm)-1]}
		pcm = pcm[:len(pcm)-1]
	}
	samples := make([]float32, len(pcm)/2)
	for i := range samples {
		samples[i] = float32(int16(binary.LittleEndian.Uint16(pcm[2*i:]))) / 32768.0
	}
	b.pushLocked(samples)
}

// PushSamples queues audio for the peer that is already 16 kHz mono floats (the
// stream converts other formats before it gets here). Audio that does not fit within
// maxOutboundSamples is dropped.
func (b *bridge) PushSamples(samples []float32) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.pushLocked(samples)
}

func (b *bridge) pushLocked(samples []float32) {
	if len(samples) == 0 {
		return
	}
	if len(b.pending)+len(samples) > maxOutboundSamples {
		b.stats.DroppedFromClient.Add(uint64((len(samples) + frameSamples - 1) / frameSamples))
		return
	}
	b.pending = append(b.pending, samples...)
	b.pushed += uint64(len(samples))
	b.lastPush = b.now()
}

// Mark asks to be told when the audio queued so far has been handed to the call (the
// library sends it to the peer within one frame, 60 ms). It is how a client that streams
// speech learns how much of it was actually played, and so where an interruption cut it.
// With nothing queued the mark is finished at once. It returns false when too many marks
// are waiting; the mark is then not kept.
func (b *bridge) Mark(name string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return true
	}
	if b.taken >= b.pushed {
		b.finishedMarks = append(b.finishedMarks, name)
		b.wakeLocked()
		return true
	}
	if len(b.marks) >= maxPendingMarks {
		return false
	}
	b.marks = append(b.marks, pendingMark{name: name, at: b.pushed})
	return true
}

// finishMarksLocked moves the marks whose audio has gone out to the finished list.
func (b *bridge) finishMarksLocked() {
	n := 0
	for n < len(b.marks) && b.marks[n].at <= b.taken {
		b.finishedMarks = append(b.finishedMarks, b.marks[n].name)
		n++
	}
	if n > 0 {
		b.marks = append(b.marks[:0], b.marks[n:]...)
		b.wakeLocked()
	}
}

func (b *bridge) wakeLocked() {
	select {
	case b.markReady <- struct{}{}:
	default:
	}
}

// FinishedMarks returns, in order, the marks whose audio has been played since the last
// call.
func (b *bridge) FinishedMarks() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := b.finishedMarks
	b.finishedMarks = nil
	return out
}

// Clear drops the client audio that has not been sent yet (an assistant that is
// interrupted mid-sentence must not finish it).
//
// Like Twilio's, it finishes every waiting mark at once: the audio before them is gone,
// so there is nothing left to wait for, and the client learns the queue was emptied.
func (b *bridge) Clear() {
	b.mu.Lock()
	b.taken = b.pushed
	b.pending = b.pending[:0]
	b.odd = nil
	b.finishMarksLocked()
	b.mu.Unlock()
}

// Close is called by the library when the call ends, and by the socket when it goes
// away. Both may happen, more than once.
func (b *bridge) Close() error {
	b.mu.Lock()
	if !b.closed {
		b.closed = true
		close(b.done)
	}
	b.pending = nil
	b.marks = nil
	b.mu.Unlock()
	return nil
}

// pcm16 converts frames to signed 16-bit little-endian mono, clamping like a recorder
// would.
func pcm16(frame []float32) []byte {
	out := make([]byte, len(frame)*2)
	for i, s := range frame {
		v := s * 32768.0
		switch {
		case v > 32767:
			v = 32767
		case v < -32768:
			v = -32768
		}
		binary.LittleEndian.PutUint16(out[2*i:], uint16(int16(v)))
	}
	return out
}
