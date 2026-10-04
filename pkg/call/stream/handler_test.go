package call_stream

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	call_engine "github.com/lucasgiovannibr/whatygo/pkg/call/engine"
	"github.com/lucasgiovannibr/whatygo/pkg/call/engine/enginetest"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

type rig struct {
	video   bool        // whether the tickets the rig issues ask for video
	format  AudioFormat // the audio format the tickets ask for (zero: the default)
	binary  bool        // whether the tickets ask for binary frames
	speech  bool        // whether the tickets ask for speech events
	t       *testing.T
	engine  *call_engine.Manager
	tickets *Tickets
	server  *httptest.Server
}

func newRig(t *testing.T, cfg Config) *rig {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := &rig{t: t, engine: call_engine.NewManager(call_engine.Options{}), tickets: NewTickets()}
	engine := gin.New()
	RegisterRoutes(engine, r.engine, r.tickets, cfg)
	r.server = httptest.NewServer(engine)
	t.Cleanup(r.server.Close)
	return r
}

// track adds a running incoming call to the instance.
func (r *rig) track(instance, id string) *enginetest.Fake {
	f := enginetest.NewFake(id)
	f.SetPhase(call_engine.PhaseActive)
	if _, err := r.engine.Track(instance, f, call_engine.Incoming); err != nil {
		r.t.Fatal(err)
	}
	return f
}

func (r *rig) url(callID, ticket string) string {
	return "ws" + strings.TrimPrefix(r.server.URL, "http") + "/call/stream/" + callID + "?ticket=" + ticket
}

func (r *rig) dial(instance, callID string, header http.Header) (*websocket.Conn, *http.Response, error) {
	token, _, err := r.tickets.IssueWith(instance, callID, StreamOptions{Video: r.video, Format: r.format, Binary: r.binary, Speech: r.speech})
	if err != nil {
		r.t.Fatal(err)
	}
	return websocket.DefaultDialer.Dial(r.url(callID, token), header)
}

func (r *rig) mustDial(instance, callID string) *websocket.Conn {
	r.t.Helper()
	conn, _, err := r.dial(instance, callID, nil)
	if err != nil {
		r.t.Fatalf("dial: %v", err)
	}
	r.t.Cleanup(func() { conn.Close() })
	return conn
}

func read(t *testing.T, conn *websocket.Conn) message {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var m message
	if err := conn.ReadJSON(&m); err != nil {
		t.Fatalf("read: %v", err)
	}
	return m
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestTheStreamCarriesAudioBothWaysAndReportsTheEnd(t *testing.T) {
	r := newRig(t, Config{})
	call := r.track("inst", "C1")
	conn := r.mustDial("inst", "C1")

	start := read(t, conn)
	if start.Event != "start" || start.CallID != "C1" || start.SampleRate != 16000 || start.Channels != 1 ||
		start.Encoding != "audio/pcm-s16le" || start.FrameMs != 60 || start.Direction != "incoming" {
		t.Fatalf("start = %+v", start)
	}

	// the peer's audio reaches the client
	eventually(t, "the sink to be attached", func() bool { return call.Sink() != nil })
	call.Sink().WriteFrame([]float32{0.5, -0.5})
	media := read(t, conn)
	pcm, err := base64.StdEncoding.DecodeString(media.Payload)
	if media.Event != "media" || media.Track != "inbound" || media.Seq != 1 || err != nil || len(pcm) != 4 {
		t.Fatalf("media = %+v (%v)", media, err)
	}

	// the client's audio reaches the call
	eventually(t, "the source to be attached", func() bool { return call.Source() != nil })
	chunk := make([]byte, call_engine.FrameSamples*2)
	if err := conn.WriteJSON(message{Event: "media", Payload: base64.StdEncoding.EncodeToString(chunk)}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the client's frame to be playable", func() bool {
		frame, _ := call.Source().ReadFrame()
		return len(frame) == call_engine.FrameSamples
	})

	// the end of the call is reported, then the socket closes
	call.End("terminate")
	stop := read(t, conn)
	if stop.Event != "stop" || stop.Reason != "terminate" {
		t.Fatalf("stop = %+v", stop)
	}
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("the socket stayed open after the call ended")
	}
}

// WhatsApp sends no reason when the other side hangs up; the stream says so instead of
// closing with a stop that explains nothing.
func TestTheStopOfACallThePeerEndedSaysSo(t *testing.T) {
	r := newRig(t, Config{})
	call := r.track("inst", "C1")
	conn := r.mustDial("inst", "C1")
	read(t, conn) // start

	call.End("")

	if stop := read(t, conn); stop.Event != "stop" || stop.Reason != call_engine.ReasonPeerHangup {
		t.Fatalf("stop = %+v", stop)
	}
}

func TestPeerAudioBeforeTheEndIsNotLost(t *testing.T) {
	r := newRig(t, Config{})
	call := r.track("inst", "C1")
	conn := r.mustDial("inst", "C1")
	read(t, conn) // start
	eventually(t, "the sink", func() bool { return call.Sink() != nil })

	call.Sink().WriteFrame([]float32{0.25})
	call.End("terminate")

	got := []string{}
	for {
		m := read(t, conn)
		got = append(got, m.Event)
		if m.Event == "stop" {
			break
		}
	}
	if len(got) != 2 || got[0] != "media" {
		t.Fatalf("events = %v, want the last audio before the stop", got)
	}
}

func TestATicketIsRequiredAndWorksOnce(t *testing.T) {
	r := newRig(t, Config{})
	r.track("inst", "C1")

	_, resp, err := websocket.DefaultDialer.Dial(r.url("C1", ""), nil)
	if err == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no ticket: err=%v status=%v", err, resp)
	}

	token, _, _ := r.tickets.Issue("inst", "C1", false)
	conn, _, err := websocket.DefaultDialer.Dial(r.url("C1", token), nil)
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
	_, resp, err = websocket.DefaultDialer.Dial(r.url("C1", token), nil)
	if err == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("reused ticket: err=%v status=%v", err, resp)
	}

	other, _, _ := r.tickets.Issue("inst", "C2", false)
	_, resp, err = websocket.DefaultDialer.Dial(r.url("C1", other), nil)
	if err == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("ticket of another call: err=%v status=%v", err, resp)
	}
}

func TestATicketDoesNotOpenACallOfAnotherInstance(t *testing.T) {
	r := newRig(t, Config{})
	r.track("inst", "C1")

	_, resp, err := r.dial("intruder", "C1", nil)

	if err == nil || resp.StatusCode != http.StatusNotFound {
		t.Fatalf("err=%v status=%v, want 404", err, resp)
	}
}

func TestASecondStreamIsRefusedAndTheFirstKeepsWorking(t *testing.T) {
	r := newRig(t, Config{})
	call := r.track("inst", "C1")
	first := r.mustDial("inst", "C1")
	read(t, first) // start
	eventually(t, "the sink", func() bool { return call.Sink() != nil })

	second := r.mustDial("inst", "C1")
	if m := read(t, second); m.Event != "error" || m.Code != "stream_busy" {
		t.Fatalf("second stream got %+v", m)
	}

	call.Sink().WriteFrame([]float32{0.1})
	if m := read(t, first); m.Event != "media" {
		t.Fatalf("the first stream was disturbed: %+v", m)
	}
}

func TestAStreamThatStopsLeavesTheCallAndFreesTheSlot(t *testing.T) {
	r := newRig(t, Config{})
	call := r.track("inst", "C1")
	call.SetPhase(call_engine.PhaseRinging) // not answered: no stream clock
	conn := r.mustDial("inst", "C1")
	read(t, conn)

	if err := conn.WriteJSON(message{Event: "stop"}); err != nil {
		t.Fatal(err)
	}

	eventually(t, "the stream to detach", func() bool {
		info := r.engine.List("inst")
		return len(info) == 1 && info[0].Stream != nil && !info[0].Stream.Attached
	})
	if len(r.engine.List("inst")) != 1 {
		t.Fatal("the call ended with its stream")
	}
	r.mustDial("inst", "C1") // a new stream can attach
}

func TestBadMessagesGetOneErrorEach(t *testing.T) {
	r := newRig(t, Config{})
	r.track("inst", "C1")
	conn := r.mustDial("inst", "C1")
	read(t, conn)

	for i := 0; i < 3; i++ {
		conn.WriteJSON(message{Event: "media", Payload: "not base64!!"})
	}
	conn.WriteMessage(websocket.TextMessage, []byte("not json"))
	conn.WriteMessage(websocket.TextMessage, []byte("not json"))

	got := map[string]int{}
	for i := 0; i < 2; i++ {
		got[read(t, conn).Code]++
	}
	if got["bad_payload"] != 1 || got["bad_message"] != 1 {
		t.Fatalf("errors = %v, want one of each kind", got)
	}

	// and the stream still works
	conn.WriteJSON(message{Event: "unknown-thing"})                                                                        // ignored
	conn.WriteJSON(message{Event: "media", Track: "inbound", Payload: base64.StdEncoding.EncodeToString(make([]byte, 4))}) // an echo: ignored
}

func TestOriginPolicy(t *testing.T) {
	cases := []struct {
		name    string
		cfg     Config
		origin  string
		allowed bool
	}{
		{"no origin (a server)", Config{}, "", true},
		{"foreign website", Config{}, "https://evil.example", false},
		{"listed origin", Config{AllowedOrigins: []string{"https://app.example"}}, "https://app.example", true},
		{"listed origin, other case", Config{AllowedOrigins: []string{"https://app.example"}}, "https://APP.example", true},
		{"unlisted origin", Config{AllowedOrigins: []string{"https://app.example"}}, "https://evil.example", false},
		{"wildcard", Config{AllowedOrigins: []string{"*"}}, "https://anything.example", true},
		{"garbage origin", Config{AllowedOrigins: []string{"*"}}, "://", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newRig(t, c.cfg)
			r.track("inst", "C1")
			header := http.Header{}
			if c.origin != "" {
				header.Set("Origin", c.origin)
			}

			conn, resp, err := r.dial("inst", "C1", header)
			if conn != nil {
				conn.Close()
			}
			if c.allowed && err != nil {
				t.Fatalf("refused: %v", err)
			}
			if !c.allowed && (err == nil || resp.StatusCode != http.StatusForbidden) {
				t.Fatalf("err=%v resp=%v, want 403", err, resp)
			}
		})
	}
}

func TestTheServersOwnOriginIsAllowed(t *testing.T) {
	r := newRig(t, Config{})
	r.track("inst", "C1")
	header := http.Header{}
	header.Set("Origin", r.server.URL) // same host as the request

	conn, _, err := r.dial("inst", "C1", header)
	if err != nil {
		t.Fatalf("the server's own origin was refused: %v", err)
	}
	conn.Close()
}

func TestAStreamThatIsClosedHangsUpARunningCallAfterTheGrace(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := call_engine.NewManager(call_engine.Options{StreamGrace: 30 * time.Millisecond})
	tickets := NewTickets()
	g := gin.New()
	RegisterRoutes(g, engine, tickets, Config{})
	server := httptest.NewServer(g)
	defer server.Close()

	call := enginetest.NewFake("C1")
	call.SetPhase(call_engine.PhaseActive)
	tracked, _ := engine.Track("inst", call, call_engine.Incoming)

	token, _, _ := tickets.Issue("inst", "C1", false)
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/call/stream/C1?ticket="+token, nil)
	if err != nil {
		t.Fatal(err)
	}
	read(t, conn)
	conn.Close() // the consumer goes away

	select {
	case <-tracked.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("the call was never hung up")
	}
	if tracked.Reason() != "stream_closed" {
		t.Fatalf("reason = %q", tracked.Reason())
	}
}

// ---- video

func videoRig(t *testing.T) (*rig, *enginetest.Fake, *websocket.Conn) {
	t.Helper()
	r := newRig(t, Config{})
	r.video = true
	call := r.track("inst", "C1")
	call.SetVideo(true)
	conn := r.mustDial("inst", "C1")
	return r, call, conn
}

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

func TestAVideoStreamTellsTheClientVideoWillFollow(t *testing.T) {
	_, call, conn := videoRig(t)

	start := read(t, conn)

	if start.Video == nil || !*start.Video || start.VideoStream == nil || !*start.VideoStream {
		t.Fatalf("start = %+v", start)
	}
	eventually(t, "the video sink", func() bool { return call.VideoSink() != nil })
}

func TestAnAudioOnlyStreamDoesNotCarryVideo(t *testing.T) {
	r := newRig(t, Config{})
	call := r.track("inst", "C1")
	call.SetVideo(true) // the call has video, the stream did not ask for it
	conn := r.mustDial("inst", "C1")

	start := read(t, conn)
	if start.Video == nil || !*start.Video || start.VideoStream == nil || *start.VideoStream {
		t.Fatalf("start = %+v: the call has video but the stream does not", start)
	}
	eventually(t, "the audio sink", func() bool { return call.Sink() != nil })
	if call.VideoSink() != nil {
		t.Fatal("a video sink was attached to a stream that did not ask for video")
	}

	conn.WriteJSON(message{Event: "video", Payload: b64(keyAU())})
	if m := read(t, conn); m.Event != "error" || m.Code != "video_not_enabled" {
		t.Fatalf("got %+v", m)
	}
	if len(call.SentVideo()) != 0 {
		t.Fatal("video from a stream without video was sent to the peer")
	}
}

func TestThePeersVideoReachesTheClientStartingOnAKeyframe(t *testing.T) {
	_, call, conn := videoRig(t)
	read(t, conn) // start
	eventually(t, "the video sink", func() bool { return call.VideoSink() != nil })

	call.VideoSink().WriteVideo(pAU()) // the middle of a GOP: nobody can decode it
	call.VideoSink().(interface{ SetOrientation(int) }).SetOrientation(3)
	call.VideoSink().WriteVideo(keyAU())
	call.VideoSink().WriteVideo(pAU())

	first := read(t, conn)
	if first.Event != "video" || first.Track != "inbound" || first.Seq != 1 ||
		first.Keyframe == nil || !*first.Keyframe || first.Orientation == nil || *first.Orientation != 1 { // library 3 = one clockwise turn
		t.Fatalf("first = %+v", first)
	}
	got, err := base64.StdEncoding.DecodeString(first.Payload)
	if err != nil || string(got) != string(keyAU()) {
		t.Fatalf("payload = %x (%v)", got, err)
	}
	second := read(t, conn)
	if second.Seq != 2 || second.Keyframe == nil || *second.Keyframe {
		t.Fatalf("second = %+v", second)
	}
}

func TestTheClientsVideoReachesThePeerWithItsHeaders(t *testing.T) {
	_, call, conn := videoRig(t)
	read(t, conn)

	conn.WriteJSON(message{Event: "video", Payload: b64(keyAU())})
	conn.WriteJSON(message{Event: "video", Payload: b64(pAU())})
	conn.WriteJSON(message{Event: "video", Payload: b64(annexB(false, idr))}) // a keyframe asked for later, bare

	eventually(t, "three access units", func() bool { return len(call.SentVideo()) == 3 })
	sent := call.SentVideo()
	if string(sent[0].AccessUnit) != string(keyAU()) || string(sent[1].AccessUnit) != string(pAU()) {
		t.Fatalf("sent = %x", sent)
	}
	if string(sent[2].AccessUnit) != string(keyAU()) {
		t.Fatalf("the bare keyframe went out as %x, want it with the SPS and PPS of the first", sent[2].AccessUnit)
	}
}

func TestVideoThatIsNotH264AnnexBIsRefusedOnce(t *testing.T) {
	_, call, conn := videoRig(t)
	read(t, conn)

	for i := 0; i < 3; i++ {
		conn.WriteJSON(message{Event: "video", Payload: b64([]byte{0, 0, 0, 5, 0x65, 1, 2, 3, 4})}) // AVCC
	}
	conn.WriteJSON(message{Event: "video", Payload: "not base64!!"})

	got := map[string]int{}
	for i := 0; i < 2; i++ {
		got[read(t, conn).Code]++
	}
	if got["bad_video"] != 1 || got["bad_payload"] != 1 {
		t.Fatalf("errors = %v, want one of each", got)
	}
	if len(call.SentVideo()) != 0 {
		t.Fatal("refused video was sent to the peer")
	}
}

func TestVideoSentBeforeTheCallIsReadyIsDroppedAndCounted(t *testing.T) {
	r, call, conn := videoRig(t)
	read(t, conn)
	call.RefuseVideo()

	conn.WriteJSON(message{Event: "video", Payload: b64(keyAU())})
	conn.WriteJSON(message{Event: "video", Payload: b64(keyAU())})

	if m := read(t, conn); m.Event != "error" || m.Code != "video_not_ready" {
		t.Fatalf("got %+v", m)
	}
	eventually(t, "both to be counted", func() bool {
		info := r.engine.List("inst")[0].Stream
		return info != nil && info.VideoDroppedFromClient == 2
	})

	// and it works as soon as the call can take it
	call.AllowVideo()
	conn.WriteJSON(message{Event: "video", Payload: b64(keyAU())})
	eventually(t, "the video to be sent", func() bool { return len(call.SentVideo()) == 1 })
}

func TestAKeyframeRequestReachesAVideoStream(t *testing.T) {
	_, call, conn := videoRig(t)
	read(t, conn)

	call.KeyframeRequest()

	if m := read(t, conn); m.Event != "keyframe_request" {
		t.Fatalf("got %+v", m)
	}
}

func TestAnAudioOnlyStreamIsNotToldAboutKeyframes(t *testing.T) {
	r := newRig(t, Config{})
	call := r.track("inst", "C1")
	conn := r.mustDial("inst", "C1")
	read(t, conn)
	eventually(t, "the sink", func() bool { return call.Sink() != nil })

	call.KeyframeRequest()
	call.Sink().WriteFrame([]float32{0.1})

	if m := read(t, conn); m.Event != "media" {
		t.Fatalf("got %+v: a keyframe request means nothing to a stream without video", m)
	}
}

func TestThePeersVideoStateReachesEveryStream(t *testing.T) {
	for _, video := range []bool{true, false} {
		r := newRig(t, Config{})
		r.video = video
		call := r.track("inst", "C1")
		conn := r.mustDial("inst", "C1")
		read(t, conn)
		eventually(t, "the stream to attach", func() bool { return call.Sink() != nil })

		call.PeerVideoState(call_engine.VideoState{Active: true, Upgrade: true, Orientation: 2, State: call_engine.VideoStateUpgradeRequest, StateCode: 11})

		m := read(t, conn)
		if m.Event != "video_state" || m.Active == nil || !*m.Active || m.Upgrade == nil || !*m.Upgrade ||
			m.Orientation == nil || *m.Orientation != 2 || m.State != call_engine.VideoStateUpgradeRequest ||
			m.StateCode == nil || *m.StateCode != 11 {
			t.Fatalf("video=%v: got %+v", video, m)
		}
	}
}

func TestAStreamThatLeavesTakesItsVideoSinkAway(t *testing.T) {
	_, call, conn := videoRig(t)
	read(t, conn)
	eventually(t, "the video sink", func() bool { return call.VideoSink() != nil })

	conn.WriteJSON(message{Event: "stop"})

	eventually(t, "the video sink to go", func() bool { return call.VideoSink() == nil })
}

func TestMarksComeBackWhenTheAudioBeforeThemHasBeenPlayed(t *testing.T) {
	r := newRig(t, Config{})
	call := r.track("inst", "MK1")
	conn := r.mustDial("inst", "MK1")
	read(t, conn) // start
	eventually(t, "the source", func() bool { return call.Source() != nil })

	chunk := make([]byte, call_engine.FrameSamples*2*2) // two frames
	if err := conn.WriteJSON(message{Event: "media", Payload: base64.StdEncoding.EncodeToString(chunk)}); err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteJSON(message{Event: "mark", Name: "sentence-1"}); err != nil {
		t.Fatal(err)
	}
	// the library's send loop takes the two frames
	eventually(t, "the first frame", func() bool { f, _ := call.Source().ReadFrame(); return f != nil })
	eventually(t, "the second frame", func() bool { f, _ := call.Source().ReadFrame(); return f != nil })

	if m := read(t, conn); m.Event != "mark" || m.Name != "sentence-1" {
		t.Fatalf("got %+v, want the mark sentence-1", m)
	}
}

func TestClearGivesTheMarksBackAtOnce(t *testing.T) {
	r := newRig(t, Config{})
	call := r.track("inst", "MK2")
	conn := r.mustDial("inst", "MK2")
	read(t, conn)
	eventually(t, "the source", func() bool { return call.Source() != nil })

	chunk := make([]byte, call_engine.FrameSamples*2*10)
	_ = conn.WriteJSON(message{Event: "media", Payload: base64.StdEncoding.EncodeToString(chunk)})
	_ = conn.WriteJSON(message{Event: "mark", Name: "interrupted"})
	_ = conn.WriteJSON(message{Event: "clear"})

	if m := read(t, conn); m.Event != "mark" || m.Name != "interrupted" {
		t.Fatalf("got %+v, want the mark interrupted", m)
	}
}

func TestAClientThatReadsTooSlowlyIsToldOnce(t *testing.T) {
	r := newRig(t, Config{})
	call := r.track("inst", "LAG1")
	conn := r.mustDial("inst", "LAG1")
	read(t, conn)
	eventually(t, "the sink", func() bool { return call.Sink() != nil })

	// far more frames than the queue holds, faster than the socket can write them
	frame := make([]float32, call_engine.FrameSamples)
	for i := 0; i < 3000; i++ {
		call.Sink().WriteFrame(frame)
	}

	warnings := 0
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		var m message
		if err := conn.ReadJSON(&m); err != nil {
			break
		}
		if m.Event == "error" && m.Code == "inbound_overflow" {
			warnings++
		}
	}
	if warnings != 1 {
		t.Fatalf("the client was warned %d times, want once", warnings)
	}
}

// formatCase runs the stream in a non-default format against a call, with a tone going
// each way, and checks what the client receives and what the call is given.
func TestTheStreamConvertsToTheFormatOfTheTicket(t *testing.T) {
	for _, c := range []struct {
		name      string
		format    AudioFormat
		frameSize int // bytes of a 60 ms frame in steady state
	}{
		{"mu-law 8 kHz", AudioFormat{EncodingMulaw, 8000}, 480},
		{"A-law 8 kHz", AudioFormat{EncodingAlaw, 8000}, 480},
		{"PCM 8 kHz", AudioFormat{EncodingPCM, 8000}, 960},
		{"PCM 24 kHz", AudioFormat{EncodingPCM, 24000}, 2880},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := newRig(t, Config{})
			r.format = c.format
			call := r.track("inst", "FMT")
			conn := r.mustDial("inst", "FMT")

			start := read(t, conn)
			if start.Encoding != c.format.Encoding || start.SampleRate != c.format.SampleRate || start.FrameMs != 60 || start.Channels != 1 {
				t.Fatalf("start = %+v, want %+v at 60 ms", start, c.format)
			}

			// the peer's audio: a second of a 1 kHz tone, in the library's 60 ms frames
			eventually(t, "the sink", func() bool { return call.Sink() != nil })
			for _, frame := range framesOf(tone(1000, 0.5, 16000, 1)) {
				call.Sink().WriteFrame(frame)
			}
			conv := newConverter(c.format)
			var heard []float32
			sizes := map[int]int{}
			for i := 0; i < 16; i++ {
				m := read(t, conn)
				if m.Event != "media" {
					t.Fatalf("got %+v", m)
				}
				data, err := base64.StdEncoding.DecodeString(m.Payload)
				if err != nil {
					t.Fatal(err)
				}
				sizes[len(data)]++
				heard = append(heard, conv.decode(data)...)
			}
			if sizes[c.frameSize] < 12 {
				t.Errorf("frame sizes %v: most frames should be %d bytes", sizes, c.frameSize)
			}
			if level, want := rms(heard, 1000), 0.5/1.4142; level < 0.9*want || level > 1.1*want {
				t.Errorf("the client heard a tone with rms %.3f, want about %.3f", level, want)
			}

			// the client's audio: the same tone in the stream's format reaches the call
			eventually(t, "the source", func() bool { return call.Source() != nil })
			sent := newConverter(c.format)
			var payload []byte
			for _, frame := range framesOf(tone(1000, 0.5, 16000, 1)) {
				payload = append(payload, sent.encode(frame)...)
			}
			if err := conn.WriteJSON(message{Event: "media", Payload: base64.StdEncoding.EncodeToString(payload)}); err != nil {
				t.Fatal(err)
			}
			var played []float32
			eventually(t, "the call to be given the audio", func() bool {
				frame, _ := call.Source().ReadFrame()
				if len(frame) == call_engine.FrameSamples {
					played = append(played, frame...)
				}
				return len(played) >= 12*call_engine.FrameSamples
			})
			if level, want := rms(played, 1000), 0.5/1.4142; level < 0.9*want || level > 1.1*want {
				t.Errorf("the call was given a tone with rms %.3f, want about %.3f", level, want)
			}
		})
	}
}

func TestTheDefaultFormatIsReportedAndNotConverted(t *testing.T) {
	r := newRig(t, Config{})
	call := r.track("inst", "DEF")
	conn := r.mustDial("inst", "DEF")

	if start := read(t, conn); start.Encoding != EncodingPCM || start.SampleRate != 16000 {
		t.Fatalf("start = %+v", start)
	}
	eventually(t, "the sink", func() bool { return call.Sink() != nil })
	call.Sink().WriteFrame([]float32{0.5, -0.5})
	m := read(t, conn)
	if data, _ := base64.StdEncoding.DecodeString(m.Payload); len(data) != 4 {
		t.Fatalf("a 2-sample frame became %d bytes: the default format must pass through unchanged", len(data))
	}
}
