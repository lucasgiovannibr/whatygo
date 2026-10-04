package call_stream

import (
	"encoding/json"
	"testing"
	"time"

	call_engine "github.com/lucasgiovannibr/whatygo/pkg/call/engine"
	"github.com/lucasgiovannibr/whatygo/pkg/call/engine/enginetest"
	"github.com/gorilla/websocket"
)

func speechRig(t *testing.T, binary bool) (*rig, *enginetest.Fake, *websocket.Conn) {
	t.Helper()
	r := newRig(t, Config{})
	r.speech, r.binary = true, binary
	call := r.track("inst", "SP")
	conn := r.mustDial("inst", "SP")
	if start := read(t, conn); start.SpeechEvents == nil || !*start.SpeechEvents {
		t.Fatalf("start = %+v: it must say the stream reports speech", start)
	}
	eventually(t, "the sink", func() bool { return call.Sink() != nil })
	return r, call, conn
}

// nextEvent reads until a message that is not media (audio frames are interleaved).
func nextEvent(t *testing.T, conn *websocket.Conn) message {
	t.Helper()
	for {
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		typ, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if typ == websocket.BinaryMessage {
			continue
		}
		m := message{}
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatal(err)
		}
		if m.Event != "media" {
			return m
		}
	}
}

func voice(call *enginetest.Fake, frames int, level float32) {
	f := make([]float32, call_engine.FrameSamples)
	for i := range f {
		f[i] = level
	}
	for i := 0; i < frames; i++ {
		call.Sink().WriteFrame(f)
		time.Sleep(10 * time.Millisecond)
	}
}

func TestTheStreamTellsWhenThePeerBeginsAndStopsTalking(t *testing.T) {
	_, call, conn := speechRig(t, false)

	voice(call, 8, 0.1)
	started := nextEvent(t, conn)
	if started.Event != "speech_start" || started.Timestamp == nil {
		t.Fatalf("got %+v, want speech_start with a timestamp", started)
	}

	// the peer goes quiet: nothing arrives, and the end still comes (by the clock)
	ended := nextEvent(t, conn)
	if ended.Event != "speech_end" || ended.Timestamp == nil || ended.DurationMs == nil {
		t.Fatalf("got %+v, want speech_end", ended)
	}
	if *ended.Timestamp < *started.Timestamp || *ended.DurationMs <= 0 || *ended.DurationMs > 3000 {
		t.Fatalf("start at %d ms, end at %d ms, lasted %d ms", *started.Timestamp, *ended.Timestamp, *ended.DurationMs)
	}
}

func TestSpeechEventsComeAsJSONOnABinaryStream(t *testing.T) {
	_, call, conn := speechRig(t, true)
	voice(call, 8, 0.1)
	if m := nextEvent(t, conn); m.Event != "speech_start" {
		t.Fatalf("got %+v", m)
	}
}

func TestACallThatEndsWhileThePeerTalksGetsTheEndOfSpeechBeforeTheStop(t *testing.T) {
	_, call, conn := speechRig(t, false)
	voice(call, 8, 0.1)
	if m := nextEvent(t, conn); m.Event != "speech_start" {
		t.Fatalf("got %+v", m)
	}

	call.End("terminate")
	if m := nextEvent(t, conn); m.Event != "speech_end" {
		t.Fatalf("got %+v, want the speech_end first", m)
	}
	if m := nextEvent(t, conn); m.Event != "stop" {
		t.Fatalf("got %+v, want stop", m)
	}
}

func TestAStreamThatDidNotAskForSpeechEventsGetsNone(t *testing.T) {
	r := newRig(t, Config{})
	call := r.track("inst", "NS")
	conn := r.mustDial("inst", "NS")
	start := read(t, conn)
	if start.SpeechEvents != nil && *start.SpeechEvents {
		t.Fatalf("start = %+v", start)
	}
	eventually(t, "the sink", func() bool { return call.Sink() != nil })

	voice(call, 8, 0.1)
	time.Sleep(900 * time.Millisecond)
	call.End("terminate")
	for {
		m := nextEvent(t, conn)
		if m.Event == "speech_start" || m.Event == "speech_end" {
			t.Fatalf("a stream that did not ask got %+v", m)
		}
		if m.Event == "stop" {
			return
		}
	}
}

func TestSilenceOnASpeechStreamIsSilent(t *testing.T) {
	_, call, conn := speechRig(t, false)
	voice(call, 10, 0.0002) // comfort noise
	call.End("terminate")
	for {
		m := nextEvent(t, conn)
		if m.Event == "speech_start" || m.Event == "speech_end" {
			t.Fatalf("silence was reported as speech: %+v", m)
		}
		if m.Event == "stop" {
			return
		}
	}
}
