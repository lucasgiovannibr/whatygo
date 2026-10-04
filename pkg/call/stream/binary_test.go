package call_stream

import (
	"encoding/binary"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	call_engine "github.com/lucasgiovannibr/whatygo/pkg/call/engine"
	"github.com/lucasgiovannibr/whatygo/pkg/call/engine/enginetest"
)

// readBinary reads the next message and requires it to be a binary frame.
func readBinary(t *testing.T, conn *websocket.Conn) []byte {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	typ, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if typ != websocket.BinaryMessage {
		t.Fatalf("got a text message %q, want a binary frame", data)
	}
	return data
}

func binaryRig(t *testing.T) (*rig, *enginetest.Fake, *websocket.Conn) {
	t.Helper()
	r := newRig(t, Config{})
	r.binary = true
	call := r.track("inst", "BIN")
	conn := r.mustDial("inst", "BIN")
	if start := read(t, conn); start.Event != "start" || start.Binary == nil || !*start.Binary {
		t.Fatalf("start = %+v: it must say the stream is binary", start)
	}
	return r, call, conn
}

func TestBinaryStreamCarriesAudioAsBinaryFramesBothWays(t *testing.T) {
	_, call, conn := binaryRig(t)
	eventually(t, "the sink", func() bool { return call.Sink() != nil })
	eventually(t, "the source", func() bool { return call.Source() != nil })

	// the peer's audio: 0x01 | seq | timestamp | PCM
	frame := make([]float32, call_engine.FrameSamples)
	frame[0], frame[1] = 0.5, -0.5
	call.Sink().WriteFrame(frame)
	call.Sink().WriteFrame(frame)

	first := readBinary(t, conn)
	if len(first) != 9+call_engine.FrameSamples*2 || first[0] != binAudio || binary.BigEndian.Uint32(first[1:]) != 1 {
		t.Fatalf("first frame: %d bytes, type %#x, seq %d", len(first), first[0], binary.BigEndian.Uint32(first[1:]))
	}
	if got := int16(binary.LittleEndian.Uint16(first[9:])); got != 16384 {
		t.Fatalf("first sample = %d, want 16384 (0.5)", got)
	}
	second := readBinary(t, conn)
	if binary.BigEndian.Uint32(second[1:]) != 2 {
		t.Fatalf("second frame has seq %d", binary.BigEndian.Uint32(second[1:]))
	}

	// the client's audio: 0x01 | PCM, with no header besides the type
	chunk := append([]byte{binAudio}, make([]byte, call_engine.FrameSamples*2)...)
	if err := conn.WriteMessage(websocket.BinaryMessage, chunk); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the client's frame to be playable", func() bool {
		f, _ := call.Source().ReadFrame()
		return len(f) == call_engine.FrameSamples
	})
}

// JSON from the client keeps working on a binary stream: only what the server sends
// changes, and a client may mix.
func TestABinaryStreamStillTakesJSONAudioFromTheClient(t *testing.T) {
	_, call, conn := binaryRig(t)
	eventually(t, "the source", func() bool { return call.Source() != nil })

	conn.WriteJSON(message{Event: "media", Payload: b64(make([]byte, call_engine.FrameSamples*2))})
	eventually(t, "the frame to be playable", func() bool {
		f, _ := call.Source().ReadFrame()
		return len(f) == call_engine.FrameSamples
	})
}

func TestBinaryAndConvertedAudioCombine(t *testing.T) {
	r := newRig(t, Config{})
	r.binary = true
	r.format = AudioFormat{EncodingMulaw, 8000}
	call := r.track("inst", "BM")
	conn := r.mustDial("inst", "BM")
	if start := read(t, conn); start.Encoding != EncodingMulaw || start.Binary == nil || !*start.Binary {
		t.Fatalf("start = %+v", start)
	}
	eventually(t, "the sink", func() bool { return call.Sink() != nil })

	for _, frame := range framesOf(tone(1000, 0.5, 16000, 1)) {
		call.Sink().WriteFrame(frame)
	}
	var last []byte
	for i := 0; i < 16; i++ {
		last = readBinary(t, conn)
	}
	if len(last) != 9+480 {
		t.Fatalf("a 60 ms mu-law frame is %d bytes in a binary frame, want %d", len(last), 9+480)
	}
}

func TestBinaryVideoCarriesTheKeyframeAndOrientationInItsFlags(t *testing.T) {
	r := newRig(t, Config{})
	r.video, r.binary = true, true
	call := r.track("inst", "BV")
	call.SetVideo(true)
	conn := r.mustDial("inst", "BV")
	read(t, conn)
	eventually(t, "the video sink", func() bool { return call.VideoSink() != nil })

	call.VideoSink().(interface{ SetOrientation(int) }).SetOrientation(3) // one clockwise turn
	call.VideoSink().WriteVideo(keyAU())
	call.VideoSink().WriteVideo(pAU())

	first := readBinary(t, conn)
	if first[0] != binVideo || first[1]&1 != 1 || (first[1]>>1)&3 != 1 || binary.BigEndian.Uint32(first[2:]) != 1 {
		t.Fatalf("first video frame header = % x", first[:10])
	}
	if string(first[10:]) != string(keyAU()) {
		t.Fatalf("access unit = %x", first[10:])
	}
	second := readBinary(t, conn)
	if second[1]&1 != 0 || binary.BigEndian.Uint32(second[2:]) != 2 {
		t.Fatalf("second video frame header = % x", second[:10])
	}

	// the client's video: 0x02 | access unit
	if err := conn.WriteMessage(websocket.BinaryMessage, append([]byte{binVideo}, keyAU()...)); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the video to be sent", func() bool { return len(call.SentVideo()) == 1 })
	if string(call.SentVideo()[0].AccessUnit) != string(keyAU()) {
		t.Fatalf("sent = %x", call.SentVideo()[0].AccessUnit)
	}
}

func TestABinaryFrameOnAStreamThatDidNotAskForThemIsAnError(t *testing.T) {
	r := newRig(t, Config{})
	call := r.track("inst", "NB")
	conn := r.mustDial("inst", "NB")
	read(t, conn)
	eventually(t, "the source", func() bool { return call.Source() != nil })

	conn.WriteMessage(websocket.BinaryMessage, append([]byte{binAudio}, make([]byte, 1920)...))
	if m := read(t, conn); m.Event != "error" || m.Code != "binary_not_enabled" {
		t.Fatalf("got %+v", m)
	}
}

func TestBadBinaryFramesAreRefusedWithAnError(t *testing.T) {
	_, _, conn := binaryRig(t)

	conn.WriteMessage(websocket.BinaryMessage, []byte{0x7F, 1, 2, 3}) // not a type
	if m := read(t, conn); m.Event != "error" || m.Code != "bad_binary" {
		t.Fatalf("unknown type: got %+v", m)
	}
	// a video frame on a stream without video says so, as the JSON message does
	conn.WriteMessage(websocket.BinaryMessage, append([]byte{binVideo}, keyAU()...))
	if m := read(t, conn); m.Event != "error" || m.Code != "video_not_enabled" {
		t.Fatalf("video without video: got %+v", m)
	}
}

// What the peer says is stamped when it reached the stream, not when the socket got
// round to writing it, so a recorder can place it in time.
func TestFramesCarryWhenTheyReachedTheStream(t *testing.T) {
	r := newRig(t, Config{})
	call := r.track("inst", "TS")
	conn := r.mustDial("inst", "TS")
	read(t, conn)
	eventually(t, "the sink", func() bool { return call.Sink() != nil })

	frame := make([]float32, call_engine.FrameSamples)
	call.Sink().WriteFrame(frame)
	time.Sleep(250 * time.Millisecond)
	call.Sink().WriteFrame(frame)

	a, b := read(t, conn), read(t, conn)
	if a.Timestamp == nil || b.Timestamp == nil {
		t.Fatalf("media messages lack a timestamp: %+v %+v", a, b)
	}
	if gap := int(*b.Timestamp) - int(*a.Timestamp); gap < 200 || gap > 1000 {
		t.Fatalf("the frames are %d ms apart, want about 250", gap)
	}
}

func TestBinaryFramesCarryTheSameTimestamps(t *testing.T) {
	_, call, conn := binaryRig(t)
	eventually(t, "the sink", func() bool { return call.Sink() != nil })

	frame := make([]float32, call_engine.FrameSamples)
	call.Sink().WriteFrame(frame)
	time.Sleep(250 * time.Millisecond)
	call.Sink().WriteFrame(frame)

	a, b := readBinary(t, conn), readBinary(t, conn)
	gap := int(binary.BigEndian.Uint32(b[5:])) - int(binary.BigEndian.Uint32(a[5:]))
	if gap < 200 || gap > 1000 {
		t.Fatalf("the frames are %d ms apart, want about 250", gap)
	}
}
