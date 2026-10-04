package call_engine_test

import (
	"sync/atomic"
	"testing"

	call_engine "github.com/lucasgiovannibr/whatygo/pkg/call/engine"
	"github.com/lucasgiovannibr/whatygo/pkg/call/engine/enginetest"
)

// countingSource is a client with audio ready: it counts how often the call took a frame.
type countingSource struct{ reads atomic.Int32 }

func (c *countingSource) ReadFrame() ([]float32, error) {
	c.reads.Add(1)
	return frame(0.2), nil
}
func (c *countingSource) Close() error { return nil }

// The library asks for a frame every 60 ms from the moment a call exists and sends what
// it gets into a call that is not connected: audio handed over while the phone rings is
// lost. So it must not be handed over until the call is active.
func TestTheClientsAudioIsNotTakenUntilTheCallIsActive(t *testing.T) {
	m := call_engine.NewManager(call_engine.Options{MediaStall: -1})
	f := enginetest.NewFake("H1")
	f.SetPhase(call_engine.PhaseCalling)
	if _, err := m.Track("inst", f, call_engine.Outgoing); err != nil {
		t.Fatal(err)
	}
	src := &countingSource{}
	if _, _, err := m.AttachStream("inst", "H1", call_engine.Endpoints{Sink: sink{}, Source: src}, &call_engine.StreamStats{}); err != nil {
		t.Fatal(err)
	}

	for _, phase := range []call_engine.Phase{call_engine.PhaseCalling, call_engine.PhaseRinging, call_engine.PhaseConnecting} {
		f.SetPhase(phase)
		for i := 0; i < 5; i++ { // the library's send loop
			if got, err := f.Source().ReadFrame(); got != nil || err != nil {
				t.Fatalf("phase %s: a frame was handed to the call (%v, %v)", phase, len(got), err)
			}
		}
	}
	if n := src.reads.Load(); n != 0 {
		t.Fatalf("the client's source was read %d times before the call was active", n)
	}

	f.SetPhase(call_engine.PhaseActive)
	if got, _ := f.Source().ReadFrame(); len(got) != call_engine.FrameSamples {
		t.Fatalf("an active call got no audio (%d samples)", len(got))
	}
	if n := src.reads.Load(); n != 1 {
		t.Fatalf("the source was read %d times, want 1", n)
	}
}

func TestAnEndedCallTakesNoMoreAudio(t *testing.T) {
	m := call_engine.NewManager(call_engine.Options{MediaStall: -1})
	f := enginetest.NewFake("H2")
	f.SetPhase(call_engine.PhaseActive)
	if _, err := m.Track("inst", f, call_engine.Incoming); err != nil {
		t.Fatal(err)
	}
	src := &countingSource{}
	if _, _, err := m.AttachStream("inst", "H2", call_engine.Endpoints{Sink: sink{}, Source: src}, &call_engine.StreamStats{}); err != nil {
		t.Fatal(err)
	}
	f.SetPhase(call_engine.PhaseEnded)
	if got, _ := f.Source().ReadFrame(); got != nil || src.reads.Load() != 0 {
		t.Fatal("an ended call was handed audio")
	}
}
