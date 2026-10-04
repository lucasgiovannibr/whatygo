package call_stream

import (
	"math"
	"time"

	call_engine "github.com/lucasgiovannibr/whatygo/pkg/call/engine"
)

// The voice detector behind the speech_start and speech_end events: an energy detector
// with a floor that follows the room, a minimum length before speech starts and a
// hangover before it ends. It is not a speech recogniser: a steady noise louder than
// the floor it started with is taken for speech until it stops, and what it is good at is
// the thing an agent needs, knowing when the person began and stopped talking.
const (
	// voicedFloor is the level (rms, full scale 1) below which a frame is never speech:
	// about -42 dBFS. An ordinary voice is 0.03 and up.
	voicedFloor = 0.008
	// noiseMargin is how many times above the room's noise a frame has to be to count
	// as speech.
	noiseMargin = 4.0
	// minSpeech is how much speech, in a row, makes it speech and not a click.
	minSpeech = 120 * time.Millisecond
	// speechHangover is how long after the last speech the person is taken to have
	// stopped: long enough to bridge the pause between two words.
	speechHangover = 600 * time.Millisecond
	// speechTick is how often a stream with speech events looks for the end of speech
	// when no audio is arriving. A peer that has gone quiet sends almost nothing (two or
	// three frames a second), so the end cannot wait for the next frame.
	speechTick = 100 * time.Millisecond

	noiseStart = 0.003
	noiseMin   = 0.0003
	noiseMax   = 0.03
)

// speechEvent is a change in whether the peer is speaking.
type speechEvent struct {
	start bool
	// at is when speech began, or when it last was heard for an end.
	at time.Time
	// length is how long the speech lasted (only on an end).
	length time.Duration
}

// vad follows whether the peer is speaking. It is for one goroutine.
type vad struct {
	noise float64 // the room's level, following what is not speech

	speaking  bool
	run       time.Duration // speech heard in a row, before it counts
	runStart  time.Time
	startedAt time.Time // when the current speech began
	lastVoice time.Time // the end of the last frame with speech in it
}

func newVAD() *vad { return &vad{noise: noiseStart} }

// Frame takes the next piece of the peer's audio, 16 kHz, that reached the stream at at.
func (v *vad) Frame(at time.Time, samples []float32) *speechEvent {
	if len(samples) == 0 {
		return nil
	}
	length := time.Duration(len(samples)) * time.Second / time.Duration(call_engine.SampleRate)
	level := frameLevel(samples)

	if level > math.Max(voicedFloor, v.noise*noiseMargin) {
		v.lastVoice = at.Add(length)
		if v.speaking {
			return nil
		}
		if v.run == 0 {
			v.runStart = at
		}
		v.run += length
		if v.run < minSpeech {
			return nil
		}
		v.speaking = true
		v.startedAt = v.runStart
		return &speechEvent{start: true, at: v.startedAt}
	}

	// not speech: the room is this loud. It comes down quickly, since a quiet room is
	// quiet at once, and goes up slowly, so that a word is not taken for the room.
	rate := 0.02
	if level < v.noise {
		rate = 0.2
	}
	v.noise = math.Min(math.Max(v.noise+(level-v.noise)*rate, noiseMin), noiseMax)

	v.run = 0
	return v.expire(at)
}

// Tick looks for the end of speech when no audio is arriving.
func (v *vad) Tick(now time.Time) *speechEvent { return v.expire(now) }

// Close ends the speech there is, because the call ended.
func (v *vad) Close() *speechEvent {
	if !v.speaking {
		return nil
	}
	return v.end()
}

func (v *vad) expire(now time.Time) *speechEvent {
	if v.speaking && now.Sub(v.lastVoice) >= speechHangover {
		return v.end()
	}
	return nil
}

func (v *vad) end() *speechEvent {
	v.speaking, v.run = false, 0
	return &speechEvent{at: v.lastVoice, length: v.lastVoice.Sub(v.startedAt)}
}

// frameLevel is the root mean square of a frame.
func frameLevel(samples []float32) float64 {
	var sum float64
	for _, s := range samples {
		sum += float64(s) * float64(s)
	}
	return math.Sqrt(sum / float64(len(samples)))
}
