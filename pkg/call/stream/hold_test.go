package call_stream

import (
	"testing"
	"time"

	call_engine "github.com/lucasgiovannibr/whatygo/pkg/call/engine"
	"github.com/lucasgiovannibr/whatygo/pkg/call/engine/enginetest"
)

// A greeting queued while the other phone rings plays when it answers, and a mark set
// after it comes back only then: not as "played" for audio the call threw away.
func TestAudioQueuedWhileTheCallRingsPlaysWhenItIsAnswered(t *testing.T) {
	r := newRig(t, Config{})
	call := enginetest.NewFake("HOLD") // rings: the phase a call starts in
	call.SetPhase(call_engine.PhaseCalling)
	if _, err := r.engine.Track("inst", call, call_engine.Outgoing); err != nil {
		t.Fatal(err)
	}
	conn := r.mustDial("inst", "HOLD")
	read(t, conn) // start
	eventually(t, "the source", func() bool { return call.Source() != nil })

	// the greeting, and a mark right after it
	two := make([]byte, call_engine.FrameSamples*2*2)
	conn.WriteJSON(message{Event: "media", Payload: b64(two)})
	conn.WriteJSON(message{Event: "mark", Name: "greeting"})

	tr, _ := r.engine.Get("inst", "HOLD")
	// give the socket a moment to queue what was sent; nothing reads the queue meanwhile
	time.Sleep(150 * time.Millisecond)
	// the library keeps asking, every 60 ms, while the phone rings
	for i := 0; i < 20; i++ {
		if frame, _ := call.Source().ReadFrame(); frame != nil {
			t.Fatal("audio was handed to a call that is still ringing")
		}
	}
	if got := tr.Info().Stream.FromClient; got != 0 {
		t.Fatalf("%d frames of the client's audio were consumed while the call rang", got)
	}

	// the other side answers: now it plays, and then the mark comes back
	call.SetPhase(call_engine.PhaseActive)
	played := 0
	eventually(t, "the greeting to play", func() bool {
		if frame, _ := call.Source().ReadFrame(); len(frame) == call_engine.FrameSamples {
			played++
		}
		return played >= 2
	})
	if m := read(t, conn); m.Event != "mark" || m.Name != "greeting" {
		t.Fatalf("got %+v, want the mark greeting", m)
	}
}
