package call_handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	call_engine "github.com/lucasgiovannibr/whatygo/pkg/call/engine"
	"github.com/lucasgiovannibr/whatygo/pkg/call/engine/enginetest"
	call_history "github.com/lucasgiovannibr/whatygo/pkg/call/history"
	call_service "github.com/lucasgiovannibr/whatygo/pkg/call/service"
	call_stream "github.com/lucasgiovannibr/whatygo/pkg/call/stream"
	instance_model "github.com/lucasgiovannibr/whatygo/pkg/instance/model"
	whatsmeow_service "github.com/lucasgiovannibr/whatygo/pkg/whatsmeow/service"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store"
)

// fakeWhatsmeow is the WhatsmeowService of the tests: only the call engine is real.
type fakeWhatsmeow struct {
	whatsmeow_service.WhatsmeowService
	engine *call_engine.Manager
}

func (f fakeWhatsmeow) CallEngine() *call_engine.Manager { return f.engine }

type noLog struct{}

func (noLog) LogInfo(string, ...interface{})  {}
func (noLog) LogWarn(string, ...interface{})  {}
func (noLog) LogError(string, ...interface{}) {}
func (noLog) LogDebug(string, ...interface{}) {}

type env struct {
	t       *testing.T
	engine  *call_engine.Manager
	tickets *call_stream.Tickets
	router  *gin.Engine
	dialed  []string // the targets the fake library was asked to call
	options []call_engine.DialOptions
	calls   map[string]*enginetest.Fake // the calls the fake library placed
}

// newEnv builds the call routes the way routes.go does, for the instance "inst" whose
// call engine is active; "bare" is an instance without one.
func newEnv(t *testing.T) *env { return newEnvWith(t, call_engine.Options{}) }

func newEnvWith(t *testing.T, opts call_engine.Options) *env {
	t.Helper()
	return newEnvWithHistory(t, opts, nil)
}

// newEnvWithHistory is newEnvWith for a server that keeps a call history in history (nil:
// it keeps none).
func newEnvWithHistory(t *testing.T, opts call_engine.Options, history call_history.Repository) *env {
	t.Helper()
	gin.SetMode(gin.TestMode)

	e := &env{t: t}
	opts.Dial = e.fakeDial
	engine := call_engine.NewManager(opts)
	if st := engine.Attach("inst", whatsmeow.NewClient(&store.Device{}, nil), false, noLog{}); st.State != call_engine.StateActive {
		t.Fatalf("engine state = %s (%s)", st.State, st.Error)
	}
	tickets := call_stream.NewTickets()
	svc := call_service.NewCallService(nil, fakeWhatsmeow{engine: engine}, tickets, nil, history)
	h := NewCallHandler(svc)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("instance", &instance_model.Instance{Id: c.GetHeader("X-Instance")})
	})
	g := r.Group("/call")
	g.GET("/active", h.ActiveCalls)
	g.GET("/history", h.History)
	g.DELETE("/history", h.DeleteHistory)
	g.GET("/:callId", h.GetCall)
	g.POST("/answer", h.AnswerCall)
	g.POST("/hangup", h.HangupCall)
	g.POST("/stream-ticket", h.StreamTicket)
	g.POST("/dial", h.DialCall)
	g.POST("/video", h.VideoCall)

	e.engine, e.tickets, e.router = engine, tickets, r
	return e
}

// fakeDial is the library's Call: it answers with a call in the calling phase, except
// for numbers that contain "unreachable".
func (e *env) fakeDial(_ context.Context, _ string, target string, opts call_engine.DialOptions) (call_engine.Call, error) {
	e.dialed = append(e.dialed, target)
	e.options = append(e.options, opts)
	if strings.Contains(target, "unreachable") {
		return nil, errors.New("peer has no devices")
	}
	f := enginetest.NewFake(fmt.Sprintf("OUT%d", len(e.dialed)))
	f.SetPhase(call_engine.PhaseCalling)
	f.SetVideo(opts.Video)
	if e.calls == nil {
		e.calls = map[string]*enginetest.Fake{}
	}
	e.calls[f.ID()] = f
	return f, nil
}

func (e *env) call(method, path, instance string, body interface{}) *httptest.ResponseRecorder {
	e.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("X-Instance", instance)
	w := httptest.NewRecorder()
	e.router.ServeHTTP(w, req)
	return w
}

func (e *env) ringing(id string) *enginetest.Fake {
	f := enginetest.NewFake(id)
	if _, err := e.engine.Track("inst", f, call_engine.Incoming); err != nil {
		e.t.Fatal(err)
	}
	return f
}

func decode(t *testing.T, w *httptest.ResponseRecorder, into interface{}) {
	t.Helper()
	if err := json.Unmarshal(w.Body.Bytes(), into); err != nil {
		t.Fatalf("body %q: %v", w.Body.String(), err)
	}
}

func TestActiveAndAnIdShareTheCallPrefix(t *testing.T) {
	e := newEnv(t)
	e.ringing("C1")

	w := e.call("GET", "/call/active", "inst", nil)
	var active call_service.ActiveCallsResult
	decode(t, w, &active)
	if w.Code != http.StatusOK || !active.Enabled || len(active.Calls) != 1 {
		t.Fatalf("/call/active: %d %s", w.Code, w.Body.String())
	}

	w = e.call("GET", "/call/C1", "inst", nil)
	var info call_engine.Info
	decode(t, w, &info)
	if w.Code != http.StatusOK || info.CallID != "C1" || info.Phase != call_engine.PhaseRinging || info.Direction != call_engine.Incoming {
		t.Fatalf("/call/C1: %d %s", w.Code, w.Body.String())
	}
}

func TestGetCallOfAnotherInstanceOrUnknownIs404(t *testing.T) {
	e := newEnv(t)
	e.ringing("C1")

	if w := e.call("GET", "/call/nope", "inst", nil); w.Code != http.StatusNotFound {
		t.Fatalf("unknown call: %d", w.Code)
	}
	// "bare" has no call engine at all
	if w := e.call("GET", "/call/C1", "bare", nil); w.Code != http.StatusConflict {
		t.Fatalf("instance without engine: %d", w.Code)
	}
}

func TestAnswer(t *testing.T) {
	e := newEnv(t)
	call := e.ringing("C1")

	if w := e.call("POST", "/call/answer", "inst", map[string]string{}); w.Code != http.StatusBadRequest {
		t.Fatalf("no callId: %d", w.Code)
	}
	if w := e.call("POST", "/call/answer", "inst", map[string]string{"callId": "nope"}); w.Code != http.StatusNotFound {
		t.Fatalf("unknown call: %d", w.Code)
	}
	if w := e.call("POST", "/call/answer", "bare", map[string]string{"callId": "C1"}); w.Code != http.StatusConflict {
		t.Fatalf("instance without engine: %d", w.Code)
	}

	w := e.call("POST", "/call/answer", "inst", map[string]string{"callId": "C1"})
	var info call_engine.Info
	decode(t, w, &info)
	if w.Code != http.StatusOK || info.Phase != call_engine.PhaseConnecting || call.Answered() != 1 {
		t.Fatalf("answer: %d %s (answered %d)", w.Code, w.Body.String(), call.Answered())
	}

	if w := e.call("POST", "/call/answer", "inst", map[string]string{"callId": "C1"}); w.Code != http.StatusConflict {
		t.Fatalf("second answer: %d", w.Code)
	}
	if call.Answered() != 1 {
		t.Fatalf("answered %d times", call.Answered())
	}
}

func TestHangup(t *testing.T) {
	e := newEnv(t)
	e.ringing("C1")

	if w := e.call("POST", "/call/hangup", "inst", map[string]string{}); w.Code != http.StatusBadRequest {
		t.Fatalf("no callId: %d", w.Code)
	}
	if w := e.call("POST", "/call/hangup", "inst", map[string]string{"callId": "C1"}); w.Code != http.StatusOK {
		t.Fatalf("hangup: %d %s", w.Code, w.Body.String())
	}
	if w := e.call("POST", "/call/hangup", "inst", map[string]string{"callId": "C1"}); w.Code != http.StatusNotFound {
		t.Fatalf("second hangup: %d", w.Code)
	}
	if len(e.engine.List("inst")) != 0 {
		t.Fatal("the call is still tracked")
	}
}

func TestStreamTicket(t *testing.T) {
	e := newEnv(t)
	e.ringing("C1")

	if w := e.call("POST", "/call/stream-ticket", "inst", map[string]string{"callId": "nope"}); w.Code != http.StatusNotFound {
		t.Fatalf("unknown call: %d", w.Code)
	}
	if w := e.call("POST", "/call/stream-ticket", "bare", map[string]string{"callId": "C1"}); w.Code != http.StatusConflict {
		t.Fatalf("instance without engine: %d", w.Code)
	}

	w := e.call("POST", "/call/stream-ticket", "inst", map[string]string{"callId": "C1"})
	var ticket call_service.StreamTicket
	decode(t, w, &ticket)
	if w.Code != http.StatusOK || ticket.Ticket == "" || ticket.ExpiresInSeconds != 30 ||
		ticket.Path != "/call/stream/C1?ticket="+ticket.Ticket {
		t.Fatalf("ticket: %d %s", w.Code, w.Body.String())
	}

	// it opens that call for that instance, once
	if instance, _, ok := e.tickets.Redeem(ticket.Ticket, "C1"); !ok || instance != "inst" {
		t.Fatalf("Redeem = %q, %v", instance, ok)
	}
	if _, _, ok := e.tickets.Redeem(ticket.Ticket, "C1"); ok {
		t.Fatal("the ticket worked twice")
	}
}

func TestDial(t *testing.T) {
	e := newEnv(t)

	w := e.call("POST", "/call/dial", "inst", map[string]interface{}{"number": "5511999990000@s.whatsapp.net"})
	var result call_service.DialResult
	decode(t, w, &result)
	if w.Code != http.StatusOK || result.Direction != call_engine.Outgoing || result.Phase != call_engine.PhaseCalling || result.CallID == "" {
		t.Fatalf("dial: %d %s", w.Code, w.Body.String())
	}
	if result.StreamTicket != nil {
		t.Fatal("a ticket came back that was not asked for")
	}
	if len(e.dialed) != 1 || e.dialed[0] != "5511999990000@s.whatsapp.net" {
		t.Fatalf("dialed = %v", e.dialed)
	}

	// the call is followed like any other
	if w := e.call("GET", "/call/"+result.CallID, "inst", nil); w.Code != http.StatusOK {
		t.Fatalf("GET the call: %d", w.Code)
	}
}

// CreateJID (the number-validation middleware) writes phone numbers as
// "+5511...@s.whatsapp.net"; the call goes out in a raw node, which the server drops
// when the user has that "+".
func TestDialSendsTheJIDWithoutThePlus(t *testing.T) {
	e := newEnv(t)

	e.call("POST", "/call/dial", "inst", map[string]interface{}{"number": "+5511999990000@s.whatsapp.net"})

	if len(e.dialed) != 1 || e.dialed[0] != "5511999990000@s.whatsapp.net" {
		t.Fatalf("dialed = %v", e.dialed)
	}
}

func TestDialTurnsAPhoneNumberIntoAJID(t *testing.T) {
	e := newEnv(t)

	e.call("POST", "/call/dial", "inst", map[string]interface{}{"number": "5511999990000"})

	if len(e.dialed) != 1 || e.dialed[0] != "5511999990000@s.whatsapp.net" {
		t.Fatalf("dialed = %v", e.dialed)
	}
}

func TestDialKeepsALIDAndDropsTheDevice(t *testing.T) {
	e := newEnv(t)

	e.call("POST", "/call/dial", "inst", map[string]interface{}{"number": "123456789012345:7@lid"})

	if len(e.dialed) != 1 || e.dialed[0] != "123456789012345@lid" {
		t.Fatalf("dialed = %v", e.dialed)
	}
}

func TestDialRefusesWhatIsNotAUser(t *testing.T) {
	e := newEnv(t)
	for _, number := range []interface{}{
		"120363012345678901@g.us", // a group
		"status@broadcast",        // a broadcast list
		"",                        // nothing
		"not a number",            // garbage
		[]string{"5511999990000", "5511999990001"}, // a list: one call, one number
	} {
		w := e.call("POST", "/call/dial", "inst", map[string]interface{}{"number": number})
		if w.Code != http.StatusBadRequest {
			t.Errorf("number %v: status %d, want 400 (%s)", number, w.Code, w.Body.String())
		}
	}
	if len(e.dialed) != 0 {
		t.Fatalf("placed calls to %v", e.dialed)
	}
}

func TestDialCanReturnTheStreamTicketWithTheCall(t *testing.T) {
	e := newEnv(t)

	w := e.call("POST", "/call/dial", "inst", map[string]interface{}{"number": "5511999990000", "stream": true})

	var result call_service.DialResult
	decode(t, w, &result)
	if w.Code != http.StatusOK || result.StreamTicket == nil {
		t.Fatalf("dial: %d %s", w.Code, w.Body.String())
	}
	if instance, _, ok := e.tickets.Redeem(result.StreamTicket.Ticket, result.CallID); !ok || instance != "inst" {
		t.Fatalf("the ticket does not open the new call: %q %v", instance, ok)
	}
}

func TestDialOfAnInstanceWithoutAnEngineIs409(t *testing.T) {
	e := newEnv(t)

	if w := e.call("POST", "/call/dial", "bare", map[string]interface{}{"number": "5511999990000"}); w.Code != http.StatusConflict {
		t.Fatalf("status %d", w.Code)
	}
	if len(e.dialed) != 0 {
		t.Fatal("placed a call through an instance without an engine")
	}
}

func TestDialFailureIs502AndTheLimitsAre429(t *testing.T) {
	e := newEnvWith(t, call_engine.Options{DialsPerMinute: 3, MaxConcurrent: 20})

	if w := e.call("POST", "/call/dial", "inst", map[string]interface{}{"number": "unreachable@lid"}); w.Code != http.StatusBadGateway {
		t.Fatalf("unreachable peer: status %d %s", w.Code, w.Body.String())
	}
	// that attempt used one of the three; two more go through, the fourth does not
	for i := 0; i < 2; i++ {
		if w := e.call("POST", "/call/dial", "inst", map[string]interface{}{"number": "5511999990000"}); w.Code != http.StatusOK {
			t.Fatalf("dial %d: status %d", i, w.Code)
		}
	}
	if w := e.call("POST", "/call/dial", "inst", map[string]interface{}{"number": "5511999990000"}); w.Code != http.StatusTooManyRequests {
		t.Fatalf("over the rate: status %d", w.Code)
	}
}

func TestDialStopsAtTheConcurrentCallLimit(t *testing.T) {
	e := newEnvWith(t, call_engine.Options{MaxConcurrent: 1})
	e.ringing("BUSY")

	w := e.call("POST", "/call/dial", "inst", map[string]interface{}{"number": "5511999990000"})

	if w.Code != http.StatusTooManyRequests || len(e.dialed) != 0 {
		t.Fatalf("status %d, dialed %v", w.Code, e.dialed)
	}
}

func TestDialCanPlaceAVideoCallAndStreamItsVideo(t *testing.T) {
	e := newEnv(t)

	w := e.call("POST", "/call/dial", "inst", map[string]interface{}{"number": "5511999990000", "video": true, "stream": true})

	var result call_service.DialResult
	decode(t, w, &result)
	if w.Code != http.StatusOK || !result.Video {
		t.Fatalf("dial: %d %s", w.Code, w.Body.String())
	}
	if len(e.options) != 1 || !e.options[0].Video {
		t.Fatalf("options = %+v: the library was not asked for a video call", e.options)
	}
	if _, video, ok := e.tickets.Redeem(result.StreamTicket.Ticket, result.CallID); !ok || !video {
		t.Fatalf("the ticket of a video call must ask for video: video=%v ok=%v", video, ok)
	}
}

func TestAnAudioDialIsNotAVideoCall(t *testing.T) {
	e := newEnv(t)

	w := e.call("POST", "/call/dial", "inst", map[string]interface{}{"number": "5511999990000", "stream": true})

	var result call_service.DialResult
	decode(t, w, &result)
	if len(e.options) != 1 || e.options[0].Video || result.Video {
		t.Fatalf("options = %+v video = %v", e.options, result.Video)
	}
	if _, video, _ := e.tickets.Redeem(result.StreamTicket.Ticket, result.CallID); video {
		t.Fatal("the ticket of an audio call asks for video")
	}
}

func TestStreamTicketCanAskForVideo(t *testing.T) {
	e := newEnv(t)
	e.ringing("C1")

	w := e.call("POST", "/call/stream-ticket", "inst", map[string]interface{}{"callId": "C1", "video": true})

	var ticket call_service.StreamTicket
	decode(t, w, &ticket)
	if _, video, ok := e.tickets.Redeem(ticket.Ticket, "C1"); w.Code != http.StatusOK || !ok || !video {
		t.Fatalf("status %d video=%v ok=%v", w.Code, video, ok)
	}
}

func TestVideoControls(t *testing.T) {
	e := newEnv(t)
	call := e.ringing("C1")
	call.SetPhase(call_engine.PhaseActive)

	for _, body := range []map[string]interface{}{
		{"callId": "C1", "action": "start"},
		{"callId": "C1", "action": "accept"},
		{"callId": "C1", "action": "stop"},
		{"callId": "C1", "action": "enable"},
		{"callId": "C1", "action": "disable"},
		{"callId": "C1", "action": "orientation", "orientation": 2},
	} {
		if w := e.call("POST", "/call/video", "inst", body); w.Code != http.StatusOK {
			t.Fatalf("%v: status %d %s", body, w.Code, w.Body.String())
		}
	}
	want := []string{"start", "accept", "stop", "enable", "disable", "orientation:2"}
	got := call.VideoActions()
	if len(got) != len(want) {
		t.Fatalf("actions = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("actions = %v, want %v", got, want)
		}
	}
}

func TestVideoControlsRefuseWhatIsWrong(t *testing.T) {
	e := newEnv(t)
	call := e.ringing("C1")

	cases := []struct {
		name string
		inst string
		body map[string]interface{}
		want int
	}{
		{"no call id", "inst", map[string]interface{}{"action": "start"}, http.StatusBadRequest},
		{"unknown action", "inst", map[string]interface{}{"callId": "C1", "action": "explode"}, http.StatusBadRequest},
		{"no action", "inst", map[string]interface{}{"callId": "C1"}, http.StatusBadRequest},
		{"orientation out of range", "inst", map[string]interface{}{"callId": "C1", "action": "orientation", "orientation": 9}, http.StatusBadRequest},
		{"unknown call", "inst", map[string]interface{}{"callId": "nope", "action": "start"}, http.StatusNotFound},
		{"call still ringing", "inst", map[string]interface{}{"callId": "C1", "action": "start"}, http.StatusConflict},
		{"instance without engine", "bare", map[string]interface{}{"callId": "C1", "action": "start"}, http.StatusConflict},
	}
	for _, c := range cases {
		if w := e.call("POST", "/call/video", c.inst, c.body); w.Code != c.want {
			t.Errorf("%s: status %d, want %d (%s)", c.name, w.Code, c.want, w.Body.String())
		}
	}
	if got := call.VideoActions(); len(got) != 0 {
		t.Fatalf("the call was touched: %v", got)
	}
}

func TestAVideoControlTheLibraryRefusesIs409WithItsReason(t *testing.T) {
	e := newEnv(t)
	call := e.ringing("C1")
	call.SetPhase(call_engine.PhaseActive)
	call.FailVideoActions(errors.New("no pending peer video upgrade"))

	w := e.call("POST", "/call/video", "inst", map[string]interface{}{"callId": "C1", "action": "accept"})

	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "no pending peer video upgrade") {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
}

func TestStreamTicketAsksForAnAudioFormat(t *testing.T) {
	e := newEnv(t)
	e.ringing("C1")

	w := e.call("POST", "/call/stream-ticket", "inst", map[string]interface{}{"callId": "C1", "encoding": "mulaw"})
	var ticket call_service.StreamTicket
	decode(t, w, &ticket)
	if w.Code != http.StatusOK || ticket.Encoding != call_stream.EncodingMulaw || ticket.SampleRate != 8000 {
		t.Fatalf("ticket: %d %s", w.Code, w.Body.String())
	}
	if _, o, ok := e.tickets.RedeemWith(ticket.Ticket, "C1"); !ok || o.Format != (call_stream.AudioFormat{Encoding: call_stream.EncodingMulaw, SampleRate: 8000}) {
		t.Fatalf("the ticket carries %+v (%v)", o, ok)
	}

	// without a format the ticket says what the default is
	w = e.call("POST", "/call/stream-ticket", "inst", map[string]interface{}{"callId": "C1"})
	decode(t, w, &ticket)
	if ticket.Encoding != call_stream.EncodingPCM || ticket.SampleRate != 16000 {
		t.Fatalf("default ticket: %s", w.Body.String())
	}
}

func TestAnUnsupportedAudioFormatIsA400(t *testing.T) {
	e := newEnv(t)
	e.ringing("C1")

	for _, body := range []map[string]interface{}{
		{"callId": "C1", "encoding": "opus"},
		{"callId": "C1", "sampleRate": 44100},
		{"callId": "C1", "encoding": "alaw", "sampleRate": 16000},
	} {
		if w := e.call("POST", "/call/stream-ticket", "inst", body); w.Code != http.StatusBadRequest {
			t.Errorf("%v: status %d, want 400 (%s)", body, w.Code, w.Body.String())
		}
	}
}

// A bad format must be refused before the phone rings, not after.
func TestDialWithABadStreamFormatPlacesNoCall(t *testing.T) {
	e := newEnv(t)

	w := e.call("POST", "/call/dial", "inst", map[string]interface{}{"number": "5511999990000", "stream": true, "encoding": "opus"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400 (%s)", w.Code, w.Body.String())
	}
	if len(e.dialed) != 0 {
		t.Fatalf("placed calls to %v", e.dialed)
	}

	// the format is only read when a stream is asked for
	if w := e.call("POST", "/call/dial", "inst", map[string]interface{}{"number": "5511999990000", "encoding": "opus"}); w.Code != http.StatusOK {
		t.Fatalf("dial without a stream: %d %s", w.Code, w.Body.String())
	}
}

func TestDialCanAskForTheAudioFormatOfItsStream(t *testing.T) {
	e := newEnv(t)

	w := e.call("POST", "/call/dial", "inst", map[string]interface{}{"number": "5511999990000", "stream": true, "encoding": "alaw"})
	var result call_service.DialResult
	decode(t, w, &result)
	if w.Code != http.StatusOK || result.StreamTicket == nil || result.StreamTicket.Encoding != call_stream.EncodingAlaw {
		t.Fatalf("dial: %d %s", w.Code, w.Body.String())
	}
}

func TestStreamTicketAsksForBinaryFrames(t *testing.T) {
	e := newEnv(t)
	e.ringing("C1")

	w := e.call("POST", "/call/stream-ticket", "inst", map[string]interface{}{"callId": "C1", "binary": true})
	var ticket call_service.StreamTicket
	decode(t, w, &ticket)
	if w.Code != http.StatusOK || !ticket.Binary {
		t.Fatalf("ticket: %d %s", w.Code, w.Body.String())
	}
	if _, o, ok := e.tickets.RedeemWith(ticket.Ticket, "C1"); !ok || !o.Binary {
		t.Fatalf("the ticket carries %+v (%v)", o, ok)
	}

	w = e.call("POST", "/call/stream-ticket", "inst", map[string]interface{}{"callId": "C1"})
	decode(t, w, &ticket)
	if ticket.Binary {
		t.Fatalf("a ticket that did not ask for binary frames says it uses them: %s", w.Body.String())
	}

	// dial hands the option on to its ticket
	w = e.call("POST", "/call/dial", "inst", map[string]interface{}{"number": "5511999990000", "stream": true, "binary": true})
	var result call_service.DialResult
	decode(t, w, &result)
	if w.Code != http.StatusOK || result.StreamTicket == nil || !result.StreamTicket.Binary {
		t.Fatalf("dial: %d %s", w.Code, w.Body.String())
	}
}
