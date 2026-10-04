package call_stream

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/lucasgiovannibr/whatygo/pkg/apierror"
	call_engine "github.com/lucasgiovannibr/whatygo/pkg/call/engine"
)

const (
	writeWait  = 10 * time.Second
	pongWait   = 60 * time.Second
	pingPeriod = 20 * time.Second

	// A second of audio is 43 kB in base64, and a keyframe of a 1080p stream a few
	// hundred kilobytes; this leaves room for both and no more.
	maxMessageBytes = 1 << 20

	// controlQueue is how many events (video state, keyframe requests) wait for the
	// socket writer. They are rare; a client so slow that this fills up loses them.
	controlQueue = 16
)

// Config is what the stream needs from the configuration.
type Config struct {
	// AllowedOrigins are the browser origins (scheme://host[:port]) allowed to open the
	// stream besides the server's own. "*" allows every origin. A client that sends no
	// Origin header (a server, a script) is always allowed: the ticket is what protects
	// the stream, the origin check only keeps other websites from using a logged-in
	// browser's ticket.
	AllowedOrigins []string
}

// message is the JSON envelope of the socket, modelled on Twilio Media Streams so
// existing voice integrations need little adapting.
//
// Server to client:
//
//	{"event":"start", "callId", "sampleRate":16000, "channels":1, "encoding":"audio/pcm-s16le", "frameMs":60,
//	                  "direction", "video":<the call has video>, "videoStream":<video messages will follow>}
//	{"event":"media", "track":"inbound", "seq":N, "timestamp":ms, "payload":"<base64 pcm>"}   the peer's audio
//	{"event":"video", "track":"inbound", "seq":N, "timestamp":ms, "keyframe":bool, "orientation":0..3,
//	                  "payload":"<base64 H.264 access unit, Annex-B>"}                  the peer's video;
//	                  "orientation" is the clockwise quarter turns to rotate the picture by
//	                  to show it upright. It follows the camera, so it is the one to use.
//	{"event":"video_state", "active", "upgrade", "orientation", "state", "stateCode"}   the peer's camera or upgrade request;
//	                  "state" is enabled, disabled, stopped, upgrade_request, upgrade_accepted,
//	                  upgrade_rejected, upgrade_cancelled or unknown; "stateCode" is the number
//	                  WhatsApp sent, the only thing that tells unknown states apart; its
//	                  "orientation" is the device's as the peer reports it and does not follow
//	                  the camera in use
//	{"event":"keyframe_request"}                                                        the next video you send must be an IDR
//	{"event":"speech_start", "timestamp":ms}                                            the peer began to talk (streams that asked for speechEvents)
//	{"event":"speech_end", "timestamp":ms, "durationMs":N}                              the peer stopped talking; "timestamp" is the last moment of speech
//	{"event":"mark", "name"}                                                            the audio you sent before that mark has been handed to the call
//	{"event":"error", "code", "message"}
//	{"event":"stop",  "reason"}                                                         the call ended
//
// Client to server:
//
//	{"event":"media", "payload":"<base64 pcm>"}     audio for the peer, any chunk size
//	{"event":"video", "payload":"<base64 access unit>"}   one H.264 access unit for the peer (video streams only)
//	{"event":"mark", "name":"..."}                  asks for the same mark back once the audio sent so far has
//	                                                 been played to the peer (at once when nothing is queued);
//	                                                 "clear" gives back every waiting mark immediately, and at
//	                                                 most 64 marks may wait
//	{"event":"clear"}                               drop the audio queued for the peer
//	{"event":"stop"}                                close the stream (the call is kept for a while)
//
// Audio is 16 kHz mono 16-bit PCM unless the ticket asked for another format (see
// ParseAudioFormat): 8 or 24 kHz PCM, or 8 kHz G.711 mu-law or A-law. The "start"
// message reports what the stream carries ("encoding", "sampleRate") and both directions
// use it; "frameMs" stays 60, so a frame is 480, 960 or 1440 samples. The call itself
// runs at 16 kHz and the stream converts.
//
// "timestamp" is when the frame reached the server, in milliseconds since the stream was
// opened (the "start" message). Audio does not arrive at a steady pace: a peer that is
// silent or muted sends only two or three frames a second, so a recording must place each
// frame by its timestamp and fill the gaps with silence, not append them.
//
// Speech events. A ticket with "speechEvents": true makes the stream say when the peer
// begins and stops talking, so a client that answers by voice can tell when to stop
// (send "clear") and when to reply without running a detector of its own. It is an energy
// detector that follows the noise of the peer's room: speech starts after about 120 ms
// of voice, and ends 600 ms after the last of it ("timestamp" is that last moment, in the
// same milliseconds as the media frames). It is told in the order of the audio, and a call
// that ends while the peer talks gets its speech_end before the stop. It is not a speech
// recogniser: a steady noise louder than the room was when the call began counts as speech
// until it stops.
//
// Binary frames. A ticket with "binary": true moves the audio and the video, the two
// things that flow all the time, out of base64 JSON and into binary WebSocket frames: a
// third smaller and nothing to encode or decode. Everything else stays JSON text, and the
// "start" message says "binary":true. All integers are big-endian.
//
//	server to client, audio:  0x01 | seq uint32 | timestamp uint32 | audio in the stream's format
//	server to client, video:  0x02 | flags uint8 | seq uint32 | timestamp uint32 | H.264 access unit
//	                          flags: bit 0 = keyframe, bits 1-2 = orientation (0..3, as in the JSON message)
//	client to server, audio:  0x01 | audio in the stream's format
//	client to server, video:  0x02 | H.264 access unit
//
// A stream that did not ask for binary frames answers a binary frame from the client with
// a "binary_not_enabled" error; one that did still accepts JSON "media" and "video"
// messages from the client.
//
// Video is H.264 in Annex-B framing, one access unit (one picture) per message, with
// the SPS and PPS in front of every keyframe. Send a keyframe first and again whenever
// a keyframe_request arrives. Video is only delivered on streams whose ticket asked for
// it; on the others "video" messages are ignored with an error.
type message struct {
	Event        string  `json:"event"`
	CallID       string  `json:"callId,omitempty"`
	Track        string  `json:"track,omitempty"`
	Seq          uint64  `json:"seq,omitempty"`
	Payload      string  `json:"payload,omitempty"`
	SampleRate   int     `json:"sampleRate,omitempty"`
	Channels     int     `json:"channels,omitempty"`
	Encoding     string  `json:"encoding,omitempty"`
	FrameMs      int     `json:"frameMs,omitempty"`
	Direction    string  `json:"direction,omitempty"`
	Video        *bool   `json:"video,omitempty"`
	VideoStream  *bool   `json:"videoStream,omitempty"`
	Keyframe     *bool   `json:"keyframe,omitempty"`
	Active       *bool   `json:"active,omitempty"`
	Upgrade      *bool   `json:"upgrade,omitempty"`
	Orientation  *int    `json:"orientation,omitempty"`
	State        string  `json:"state,omitempty"`
	StateCode    *int    `json:"stateCode,omitempty"`
	Reason       string  `json:"reason,omitempty"`
	Name         string  `json:"name,omitempty"`
	Timestamp    *uint32 `json:"timestamp,omitempty"`
	DurationMs   *int    `json:"durationMs,omitempty"`
	SpeechEvents *bool   `json:"speechEvents,omitempty"`
	Binary       *bool   `json:"binary,omitempty"`
	Code         string  `json:"code,omitempty"`
	Message      string  `json:"message,omitempty"`
}

type handler struct {
	engine   *call_engine.Manager
	tickets  *Tickets
	upgrader websocket.Upgrader
	origins  []string
}

// RegisterRoutes mounts GET /call/stream/:callId on the engine, outside the apikey
// middleware: it is authorised by the ticket instead (see Tickets).
func RegisterRoutes(r *gin.Engine, engine *call_engine.Manager, tickets *Tickets, cfg Config) {
	h := &handler{engine: engine, tickets: tickets, origins: cfg.AllowedOrigins}
	h.upgrader = websocket.Upgrader{
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,
		CheckOrigin:     h.checkOrigin,
	}
	r.GET("/call/stream/:callId", h.serve)
}

func (h *handler) checkOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	if strings.EqualFold(u.Host, r.Host) {
		return true
	}
	for _, allowed := range h.origins {
		if allowed == "*" || strings.EqualFold(allowed, origin) {
			return true
		}
	}
	return false
}

func (h *handler) serve(c *gin.Context) {
	callID := c.Param("callId")

	instanceID, opts, ok := h.tickets.RedeemWith(c.Query("ticket"), callID)
	if !ok {
		apierror.Fail(c, http.StatusUnauthorized, "invalid or expired ticket")
		return
	}
	if _, ok := h.engine.Get(instanceID, callID); !ok {
		apierror.Fail(c, http.StatusNotFound, call_engine.ErrCallNotFound.Error())
		return
	}

	conn, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return // Upgrade already answered
	}
	(&session{engine: h.engine, conn: conn, instanceID: instanceID, callID: callID, video: opts.Video, format: opts.Format, binary: opts.Binary, speech: opts.Speech}).run()
}

// session is one open socket attached to one call.
type session struct {
	engine     *call_engine.Manager
	conn       *websocket.Conn
	instanceID string
	callID     string
	video      bool // the ticket asked for video
	format     AudioFormat
	binary     bool       // audio and video travel as binary frames
	speech     bool       // the ticket asked for speech events
	vad        *vad       // nil unless speech
	started    time.Time  // timestamps count from here
	conv       *converter // nil for the default format: the call's audio goes through as it is

	stats  call_engine.StreamStats
	bridge *bridge
	vin    *videoIn  // nil on a stream without video
	vout   *videoOut // nil on a stream without video
	call   *call_engine.Tracked
	ctrl   chan message

	writeMu   sync.Mutex
	quit      chan struct{}
	quitOnce  sync.Once
	errorSent map[string]bool
}

func (s *session) send(m message) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_ = s.conn.SetWriteDeadline(time.Now().Add(writeWait))
	return s.conn.WriteJSON(m)
}

// sendBinary writes one binary frame.
func (s *session) sendBinary(b []byte) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_ = s.conn.SetWriteDeadline(time.Now().Add(writeWait))
	return s.conn.WriteMessage(websocket.BinaryMessage, b)
}

// stamp is the timestamp of a frame: milliseconds from the start of the stream.
func (s *session) stamp(at time.Time) uint32 {
	if ms := at.Sub(s.started).Milliseconds(); ms > 0 {
		return uint32(ms)
	}
	return 0
}

// The first byte of a binary frame.
const (
	binAudio byte = 0x01
	binVideo byte = 0x02
)

// sendError tells the client about a problem, once per kind: a client that keeps
// sending bad data must not get a message for each chunk.
func (s *session) sendError(code, text string) {
	if s.errorSent[code] {
		return
	}
	s.errorSent[code] = true
	_ = s.send(message{Event: "error", Code: code, Message: text})
}

func (s *session) stop() { s.quitOnce.Do(func() { close(s.quit) }) }

// emit queues an event for the writer. It is called from the library's goroutine, so it
// never blocks: an event the client is too slow to take is lost.
func (s *session) emit(m message) {
	select {
	case s.ctrl <- m:
	default:
	}
}

func (s *session) videoState(v call_engine.VideoState) {
	s.emit(message{Event: "video_state", Active: &v.Active, Upgrade: &v.Upgrade, Orientation: &v.Orientation, State: v.State, StateCode: &v.StateCode})
}

func (s *session) keyframeRequest() {
	if s.video {
		s.emit(message{Event: "keyframe_request"})
	}
}

func (s *session) run() {
	defer s.conn.Close()
	s.quit = make(chan struct{})
	s.errorSent = map[string]bool{}
	s.ctrl = make(chan message, controlQueue)
	s.bridge = newBridge(&s.stats)
	s.started = time.Now()
	if s.speech {
		s.vad = newVAD()
	}
	if !s.format.isDefault() {
		s.conv = newConverter(s.format)
	}

	ep := call_engine.Endpoints{
		Sink: s.bridge, Source: s.bridge,
		OnVideoState:      s.videoState,
		OnKeyframeRequest: s.keyframeRequest,
	}
	if s.video {
		s.vin = newVideoIn(&s.stats)
		ep.Video = s.vin
	}

	call, detach, err := s.engine.AttachStream(s.instanceID, s.callID, ep, &s.stats)
	if err != nil {
		code := "call_not_found"
		if errors.Is(err, call_engine.ErrStreamBusy) {
			code = "stream_busy"
		}
		_ = s.send(message{Event: "error", Code: code, Message: err.Error()})
		s.closeWith(websocket.ClosePolicyViolation, code)
		return
	}
	defer detach()
	s.call = call
	if s.video {
		s.vout = newVideoOut(func(au []byte, d time.Duration) error { return call.Call().SendVideo(au, d) }, &s.stats)
	}

	info := call.Info()
	video, videoStream := info.Video, s.video
	if err := s.send(message{
		Event: "start", CallID: s.callID,
		SampleRate: s.format.normalized().SampleRate, Channels: 1, Encoding: s.format.normalized().Encoding,
		FrameMs:   call_engine.FrameSamples * 1000 / call_engine.SampleRate,
		Direction: string(info.Direction), Video: &video, VideoStream: &videoStream, Binary: &s.binary, SpeechEvents: &s.speech,
	}); err != nil {
		return
	}

	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		s.writeLoop()
	}()
	s.readLoop()
	s.stop()
	<-writerDone
	s.bridge.Close()
}

func (s *session) closeWith(code int, text string) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_ = s.conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(code, text), time.Now().Add(writeWait))
}

func (s *session) sendFrame(seq *uint64, af audioFrame) error {
	*seq++
	payload := pcm16(af.samples)
	if s.conv != nil {
		payload = s.conv.encode(af.samples)
	}
	ts := s.stamp(af.at)

	var err error
	if s.binary {
		buf := make([]byte, 9+len(payload))
		buf[0] = binAudio
		binary.BigEndian.PutUint32(buf[1:], uint32(*seq))
		binary.BigEndian.PutUint32(buf[5:], ts)
		copy(buf[9:], payload)
		err = s.sendBinary(buf)
	} else {
		err = s.send(message{
			Event: "media", Track: "inbound", Seq: *seq, Timestamp: &ts,
			Payload: base64.StdEncoding.EncodeToString(payload),
		})
	}
	if err == nil {
		s.stats.ToClient.Add(1)
	}
	return err
}

// sendSpeech tells the client that the peer began or stopped talking; nil is nothing.
func (s *session) sendSpeech(ev *speechEvent) error {
	if ev == nil {
		return nil
	}
	ts := s.stamp(ev.at)
	if ev.start {
		return s.send(message{Event: "speech_start", Timestamp: &ts})
	}
	ms := int(ev.length.Milliseconds())
	return s.send(message{Event: "speech_end", Timestamp: &ts, DurationMs: &ms})
}

// sendMarks tells the client which of its marks have been played.
func (s *session) sendMarks() error {
	for _, name := range s.bridge.FinishedMarks() {
		if err := s.send(message{Event: "mark", Name: name}); err != nil {
			return err
		}
	}
	return nil
}

func (s *session) sendVideo(seq *uint64, f videoFrame) error {
	*seq++
	ts := s.stamp(f.at)

	var err error
	if s.binary {
		buf := make([]byte, 10+len(f.data))
		buf[0] = binVideo
		buf[1] = byte(f.orientation&3) << 1
		if f.keyframe {
			buf[1] |= 1
		}
		binary.BigEndian.PutUint32(buf[2:], uint32(*seq))
		binary.BigEndian.PutUint32(buf[6:], ts)
		copy(buf[10:], f.data)
		err = s.sendBinary(buf)
	} else {
		err = s.send(message{
			Event: "video", Track: "inbound", Seq: *seq, Timestamp: &ts,
			Keyframe: &f.keyframe, Orientation: &f.orientation,
			Payload: base64.StdEncoding.EncodeToString(f.data),
		})
	}
	if err == nil {
		s.stats.VideoToClient.Add(1)
	}
	return err
}

// writeLoop is the only writer of the socket besides sendError: the peer's audio and
// video, the events, the keepalive pings and the end of the call.
func (s *session) writeLoop() {
	ping := time.NewTicker(pingPeriod)
	defer ping.Stop()

	// a nil channel never becomes ready: audio-only streams have no video to wait for
	var video <-chan videoFrame
	if s.vin != nil {
		video = s.vin.frames
	}

	// a nil channel never becomes ready: streams without speech events have no tick
	var speechTicks <-chan time.Time
	if s.vad != nil {
		t := time.NewTicker(speechTick)
		defer t.Stop()
		speechTicks = t.C
	}

	var seq, videoSeq uint64
	var warnedLag bool
	for {
		select {
		case frame := <-s.bridge.toClient:
			if err := s.sendFrame(&seq, frame); err != nil {
				s.conn.Close()
				return
			}
			if s.vad != nil {
				if err := s.sendSpeech(s.vad.Frame(frame.at, frame.samples)); err != nil {
					s.conn.Close()
					return
				}
			}
			// Said once, when the client is reading again: the writer is the only place
			// that knows it was behind.
			if !warnedLag && s.stats.DroppedToClient.Load() > 0 {
				warnedLag = true
				_ = s.send(message{Event: "error", Code: "inbound_overflow",
					Message: "the peer's audio arrived faster than it was read; the oldest of it was dropped (the queue holds under a second)"})
			}

		case <-speechTicks:
			if err := s.sendSpeech(s.vad.Tick(time.Now())); err != nil {
				s.conn.Close()
				return
			}

		case <-s.bridge.markReady:
			if err := s.sendMarks(); err != nil {
				s.conn.Close()
				return
			}

		case f := <-video:
			if err := s.sendVideo(&videoSeq, f); err != nil {
				s.conn.Close()
				return
			}

		case m := <-s.ctrl:
			if err := s.send(m); err != nil {
				s.conn.Close()
				return
			}

		case <-ping.C:
			s.writeMu.Lock()
			err := s.conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(writeWait))
			s.writeMu.Unlock()
			if err != nil {
				s.conn.Close()
				return
			}

		case <-s.call.Done():
			// what the peer said last still reaches the client before the stop
			for drained := false; !drained; {
				select {
				case frame := <-s.bridge.toClient:
					if s.sendFrame(&seq, frame) != nil {
						drained = true
					}
				default:
					drained = true
				}
			}
			_ = s.sendMarks()
			if s.vad != nil {
				_ = s.sendSpeech(s.vad.Close())
			}
			_ = s.send(message{Event: "stop", Reason: s.call.Reason()})
			s.closeWith(websocket.CloseNormalClosure, "call ended")
			s.conn.Close() // ends readLoop
			return

		case <-s.quit:
			return
		}
	}
}

func (s *session) readLoop() {
	s.conn.SetReadLimit(maxMessageBytes)
	_ = s.conn.SetReadDeadline(time.Now().Add(pongWait))
	s.conn.SetPongHandler(func(string) error {
		return s.conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	for {
		typ, data, err := s.conn.ReadMessage()
		if err != nil {
			return
		}
		_ = s.conn.SetReadDeadline(time.Now().Add(pongWait))

		if typ == websocket.BinaryMessage {
			s.clientBinary(data)
			continue
		}
		var m message
		if err := json.Unmarshal(data, &m); err != nil {
			s.sendError("bad_message", "messages must be JSON objects")
			continue
		}

		switch m.Event {
		case "media":
			if m.Track != "" && m.Track != "outbound" {
				continue // not audio for the peer (an echo of "inbound", say)
			}
			pcm, err := base64.StdEncoding.DecodeString(m.Payload)
			if err != nil {
				s.sendError("bad_payload", "payload must be base64 of audio in the format of the \"start\" message ("+s.format.normalized().Encoding+", "+strconv.Itoa(s.format.normalized().SampleRate)+" Hz, mono)")
				continue
			}
			s.clientAudio(pcm)
		case "video":
			s.clientVideo(m)
		case "mark":
			if !s.bridge.Mark(m.Name) {
				s.sendError("too_many_marks", "too many marks are waiting for audio to be played; wait for some to come back")
			}
		case "clear":
			s.bridge.Clear()
		case "stop":
			return
		}
		// Unknown events are ignored, so the protocol can grow without breaking clients.
	}
}

// clientAudio queues audio from the client, in the stream's format, for the peer.
func (s *session) clientAudio(pcm []byte) {
	before := s.stats.DroppedFromClient.Load()
	if s.conv != nil {
		s.bridge.PushSamples(s.conv.decode(pcm))
	} else {
		s.bridge.Push(pcm)
	}
	if s.stats.DroppedFromClient.Load() != before {
		s.sendError("outbound_overflow", "too much audio is queued for the peer; it is being dropped")
	}
}

// clientBinary handles a binary frame from the client (see the layout in the protocol
// description).
func (s *session) clientBinary(data []byte) {
	if !s.binary {
		s.sendError("binary_not_enabled", "this stream uses JSON messages: ask for binary frames with {\"binary\": true} when requesting the ticket")
		return
	}
	if len(data) == 0 {
		s.sendError("bad_binary", "a binary frame starts with a type byte: 0x01 audio, 0x02 video")
		return
	}
	switch data[0] {
	case binAudio:
		s.clientAudio(data[1:])
	case binVideo:
		s.clientVideoAU(data[1:])
	default:
		s.sendError("bad_binary", "unknown binary frame type: use 0x01 for audio or 0x02 for video")
	}
}

func (s *session) clientVideo(m message) {
	if s.vout == nil {
		s.sendError("video_not_enabled", "this stream was opened without video: ask for it with {\"video\": true} when requesting the ticket")
		return
	}
	if m.Track != "" && m.Track != "outbound" {
		return
	}
	au, err := base64.StdEncoding.DecodeString(m.Payload)
	if err != nil {
		s.sendError("bad_payload", "payload must be base64 of one H.264 access unit in Annex-B framing")
		return
	}
	s.clientVideoAU(au)
}

// clientVideoAU sends one H.264 access unit from the client to the peer.
func (s *session) clientVideoAU(au []byte) {
	if s.vout == nil {
		s.sendError("video_not_enabled", "this stream was opened without video: ask for it with {\"video\": true} when requesting the ticket")
		return
	}
	switch err := s.vout.Send(au); {
	case err == nil:
	case errors.Is(err, ErrNotAnnexB), errors.Is(err, ErrAccessUnitTooLarge):
		s.sendError("bad_video", err.Error())
	default:
		// normal for a client that starts a little early; the stats count every one
		s.sendError("video_not_ready", "the call is not sending video yet ("+err.Error()+"); start it with POST /call/video or wait for the peer to accept")
	}
}
