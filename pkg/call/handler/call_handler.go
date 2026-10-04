package call_handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lucasgiovannibr/whatygo/pkg/apierror"
	call_engine "github.com/lucasgiovannibr/whatygo/pkg/call/engine"
	call_history "github.com/lucasgiovannibr/whatygo/pkg/call/history"
	call_service "github.com/lucasgiovannibr/whatygo/pkg/call/service"
	call_stream "github.com/lucasgiovannibr/whatygo/pkg/call/stream"
	instance_model "github.com/lucasgiovannibr/whatygo/pkg/instance/model"
)

type CallHandler interface {
	RejectCall(ctx *gin.Context)
	ActiveCalls(ctx *gin.Context)
	GetCall(ctx *gin.Context)
	AnswerCall(ctx *gin.Context)
	HangupCall(ctx *gin.Context)
	StreamTicket(ctx *gin.Context)
	DialCall(ctx *gin.Context)
	VideoCall(ctx *gin.Context)
	History(ctx *gin.Context)
	DeleteHistory(ctx *gin.Context)
}

type callHandler struct {
	callService call_service.CallService
}

// Reject call
// @Summary Reject call
// @Description Reject call
// @Tags Call
// @Accept json
// @Produce json
// @Param message body call_service.RejectCallStruct true "Call data"
// @Success 200 {object} gin.H "success"
// @Failure 500 {object} gin.H "Internal server error"
// @Router /call/reject [post]
func (g *callHandler) RejectCall(ctx *gin.Context) {
	getInstance := ctx.MustGet("instance")

	instance, ok := getInstance.(*instance_model.Instance)
	if !ok {
		apierror.Fail(ctx, http.StatusInternalServerError, "instance not found")
		return
	}

	var data *call_service.RejectCallStruct
	err := ctx.ShouldBindBodyWithJSON(&data)
	if err != nil {
		apierror.BadRequest(ctx, err)
		return
	}

	err = g.callService.RejectCall(data, instance)
	if err != nil {
		apierror.Respond(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "success"})
}

// Active calls
// @Summary Active calls
// @Description The call engine of the instance and the calls it is following (incoming and outgoing, until they end). "enabled" is false when the running client has no call engine.
// @Tags Call
// @Produce json
// @Success 200 {object} call_service.ActiveCallsResult
// @Failure 500 {object} gin.H "Internal server error"
// @Router /call/active [get]
func (g *callHandler) ActiveCalls(ctx *gin.Context) {
	instance, ok := ctx.MustGet("instance").(*instance_model.Instance)
	if !ok {
		apierror.Fail(ctx, http.StatusInternalServerError, "instance not found")
		return
	}

	ctx.JSON(http.StatusOK, g.callService.ActiveCalls(instance))
}

// callFailure answers a failed call action with the status that says what went wrong.
func callFailure(ctx *gin.Context, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, call_engine.ErrCallNotFound):
		status = http.StatusNotFound
	case errors.Is(err, call_engine.ErrWrongState), errors.Is(err, call_service.ErrCallsUnavailable), errors.Is(err, call_service.ErrHistoryDisabled):
		status = http.StatusConflict
	case errors.Is(err, call_stream.ErrTooManyTickets), errors.Is(err, call_engine.ErrTooManyCalls), errors.Is(err, call_engine.ErrDialRateLimited):
		status = http.StatusTooManyRequests
	case errors.Is(err, call_service.ErrInvalidNumber), errors.Is(err, call_engine.ErrInvalidVideoRequest), errors.Is(err, call_stream.ErrInvalidAudioFormat), errors.Is(err, call_history.ErrInvalidQuery):
		status = http.StatusBadRequest
	case errors.Is(err, call_engine.ErrDialFailed):
		status = http.StatusBadGateway
	}
	apierror.RespondWith(ctx, err, status)
}

func instanceOf(ctx *gin.Context) (*instance_model.Instance, bool) {
	instance, ok := ctx.MustGet("instance").(*instance_model.Instance)
	if !ok {
		apierror.Fail(ctx, http.StatusInternalServerError, "instance not found")
	}
	return instance, ok
}

// Get call
// @Summary Get a call
// @Description One call the instance is following: its phase, direction, whether it has video and how much audio its stream moved.
// @Tags Call
// @Produce json
// @Param callId path string true "Call id (from the CallOffer event)"
// @Success 200 {object} call_engine.Info
// @Failure 404 {object} gin.H "No such call"
// @Failure 409 {object} gin.H "Calls are not active for this instance"
// @Router /call/{callId} [get]
func (g *callHandler) GetCall(ctx *gin.Context) {
	instance, ok := instanceOf(ctx)
	if !ok {
		return
	}
	info, err := g.callService.GetCall(instance, ctx.Param("callId"))
	if err != nil {
		callFailure(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, info)
}

// Answer call
// @Summary Answer an incoming call
// @Description Answers an incoming call that is still ringing. Its audio is then available on the stream (see /call/stream-ticket); a call answered without a stream is hung up after a short grace period.
// @Tags Call
// @Accept json
// @Produce json
// @Param message body call_service.AnswerCallStruct true "Call data"
// @Success 200 {object} call_engine.Info
// @Failure 400 {object} gin.H "callId missing"
// @Failure 404 {object} gin.H "No such call"
// @Failure 409 {object} gin.H "The call is not ringing, or calls are not active for this instance"
// @Router /call/answer [post]
func (g *callHandler) AnswerCall(ctx *gin.Context) {
	instance, ok := instanceOf(ctx)
	if !ok {
		return
	}
	var data call_service.AnswerCallStruct
	if err := ctx.ShouldBindJSON(&data); err != nil || data.CallID == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "callId is required")
		return
	}
	info, err := g.callService.AnswerCall(&data, instance)
	if err != nil {
		callFailure(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, info)
}

// Hang up call
// @Summary Hang up a call
// @Description Ends a call in any phase; an incoming call that still rings is rejected. The call is over here even when telling the peer fails (that answers 500).
// @Tags Call
// @Accept json
// @Produce json
// @Param message body call_service.HangupCallStruct true "Call data"
// @Success 200 {object} gin.H "success"
// @Failure 400 {object} gin.H "callId missing"
// @Failure 404 {object} gin.H "No such call"
// @Router /call/hangup [post]
func (g *callHandler) HangupCall(ctx *gin.Context) {
	instance, ok := instanceOf(ctx)
	if !ok {
		return
	}
	var data call_service.HangupCallStruct
	if err := ctx.ShouldBindJSON(&data); err != nil || data.CallID == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "callId is required")
		return
	}
	if err := g.callService.HangupCall(&data, instance); err != nil {
		callFailure(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"message": "success"})
}

// Stream ticket
// @Summary Ticket for the audio stream of a call
// @Description Returns a one-time ticket, valid for a few seconds, for one call. Open a WebSocket to "path" on this server with it: GET /call/stream/{callId}?ticket=... . Audio is 16 kHz mono 16-bit little-endian PCM in base64 JSON messages by default (see the "start" message); "encoding" and "sampleRate" ask for 8, 16 or 24 kHz PCM, or 8 kHz G.711 mu-law or A-law, and the server converts. With "binary": true the audio and video travel as binary WebSocket frames instead of base64 in JSON (a third smaller); the control messages stay JSON. With "speechEvents": true the stream also says when the peer begins and stops talking (speech_start, speech_end). With "video": true the stream also carries the call's video as H.264 access units (Annex-B) in "video" messages, and asks for keyframes with "keyframe_request".
// @Tags Call
// @Accept json
// @Produce json
// @Param message body call_service.StreamTicketStruct true "Call data"
// @Success 200 {object} call_service.StreamTicket
// @Failure 400 {object} gin.H "callId missing"
// @Failure 404 {object} gin.H "No such call"
// @Router /call/stream-ticket [post]
func (g *callHandler) StreamTicket(ctx *gin.Context) {
	instance, ok := instanceOf(ctx)
	if !ok {
		return
	}
	var data call_service.StreamTicketStruct
	if err := ctx.ShouldBindJSON(&data); err != nil || data.CallID == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "callId is required")
		return
	}
	format, err := call_stream.ParseAudioFormat(data.Encoding, data.SampleRate)
	if err != nil {
		callFailure(ctx, err)
		return
	}
	ticket, err := g.callService.IssueStreamTicket(instance, data.CallID, call_stream.StreamOptions{Video: data.Video, Format: format, Binary: data.Binary, Speech: data.SpeechEvents})
	if err != nil {
		callFailure(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, ticket)
}

// dialTimeout bounds placing a call: resolving the number, fetching its devices,
// encrypting the call key for each and sending the offer.
const dialTimeout = 30 * time.Second

// Dial call
// @Summary Place a call
// @Description Places an outgoing call to a WhatsApp user and returns it in the "calling" phase; it rings on the other phone. With "video": true it is a video call. With "stream": true the answer also carries a ticket for the stream (with video when "video" is true), so it can be connected before the callee picks up. An instance may place a limited number of calls per minute (CALL_DIAL_LIMIT) and have a limited number at once (CALL_MAX_CONCURRENT): both answer 429.
// @Tags Call
// @Accept json
// @Produce json
// @Param message body call_service.DialCallStruct true "Who to call"
// @Success 200 {object} call_service.DialResult
// @Failure 400 {object} gin.H "Not a number a call can go to"
// @Failure 409 {object} gin.H "Calls are not active for this instance"
// @Failure 429 {object} gin.H "Too many calls"
// @Failure 502 {object} gin.H "WhatsApp or the library could not place the call"
// @Router /call/dial [post]
func (g *callHandler) DialCall(ctx *gin.Context) {
	instance, ok := instanceOf(ctx)
	if !ok {
		return
	}
	var data call_service.DialCallStruct
	if err := ctx.ShouldBindJSON(&data); err != nil || data.Number == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "number is required (one number, not a list)")
		return
	}

	dialCtx, cancel := context.WithTimeout(ctx.Request.Context(), dialTimeout)
	defer cancel()
	result, err := g.callService.DialCall(dialCtx, &data, instance)
	if err != nil {
		callFailure(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, result)
}

// Video call
// @Summary Video controls of a call
// @Description Changes the video of a call that has been answered. "start" asks the peer to turn an audio call into a video call, "accept" accepts the peer's request (see the video_state event with upgrade=true), "stop" stops sending video, "enable"/"disable" mute and unmute it ("enable" is refused on a call that never had video: turning the camera on does not turn an audio call into a video call on WhatsApp, use "start"), "orientation" tells the peer how the camera is rotated (0-3 quarter turns clockwise). The video itself travels on the stream.
// @Tags Call
// @Accept json
// @Produce json
// @Param message body call_service.VideoCallStruct true "What to do"
// @Success 200 {object} call_engine.Info
// @Failure 400 {object} gin.H "Unknown action or orientation"
// @Failure 404 {object} gin.H "No such call"
// @Failure 409 {object} gin.H "The call is not in a state that allows it"
// @Router /call/video [post]
func (g *callHandler) VideoCall(ctx *gin.Context) {
	instance, ok := instanceOf(ctx)
	if !ok {
		return
	}
	var data call_service.VideoCallStruct
	if err := ctx.ShouldBindJSON(&data); err != nil || data.CallID == "" {
		apierror.Fail(ctx, http.StatusBadRequest, "callId and action are required")
		return
	}
	info, err := g.callService.VideoCall(instance, &data)
	if err != nil {
		callFailure(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, info)
}

func NewCallHandler(
	callService call_service.CallService,
) CallHandler {
	return &callHandler{
		callService: callService,
	}
}

// Call history
// @Summary Call history
// @Description The calls the call engine followed for this instance, newest first: who, when, how long it rang and talked and how it ended (outcome: answered, missed, rejected, cancelled, unanswered, busy, failed). No audio or content is kept. Needs CALL_HISTORY=true on the server (409 otherwise); records expire after CALL_HISTORY_RETENTION_DAYS (default 90). Page with the "next" cursor of the answer.
// @Tags Call
// @Produce json
// @Param direction query string false "incoming or outgoing"
// @Param outcome query string false "answered, missed, rejected, cancelled, unanswered, busy or failed"
// @Param peer query string false "a JID or a phone number"
// @Param limit query int false "page size, 1 to 200 (default 50)"
// @Param cursor query string false "the next of the previous page"
// @Success 200 {object} call_history.Page
// @Failure 400 {object} gin.H "Invalid filter or cursor"
// @Failure 409 {object} gin.H "The server keeps no call history"
// @Router /call/history [get]
func (g *callHandler) History(ctx *gin.Context) {
	instance, ok := instanceOf(ctx)
	if !ok {
		return
	}
	q := call_history.Query{
		Direction: ctx.Query("direction"),
		Outcome:   ctx.Query("outcome"),
		Peer:      ctx.Query("peer"),
		Cursor:    ctx.Query("cursor"),
	}
	if raw := ctx.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			apierror.Fail(ctx, http.StatusBadRequest, "limit must be a number between 1 and 200")
			return
		}
		q.Limit = n
	}
	page, err := g.callService.History(instance, q)
	if err != nil {
		callFailure(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, page)
}

// Delete call history
// @Summary Erase the call history
// @Description Erases the call history of this instance: all of it, or only what started before "before" (RFC 3339). It cannot be undone. Needs CALL_HISTORY=true on the server.
// @Tags Call
// @Produce json
// @Param before query string false "erase only calls that started before this time (RFC 3339)"
// @Success 200 {object} gin.H "deleted: how many records were erased"
// @Failure 400 {object} gin.H "Invalid time"
// @Failure 409 {object} gin.H "The server keeps no call history"
// @Router /call/history [delete]
func (g *callHandler) DeleteHistory(ctx *gin.Context) {
	instance, ok := instanceOf(ctx)
	if !ok {
		return
	}
	var before time.Time
	if raw := ctx.Query("before"); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			apierror.Fail(ctx, http.StatusBadRequest, "before must be a time in RFC 3339, like 2026-10-01T00:00:00Z")
			return
		}
		before = t
	}
	deleted, err := g.callService.DeleteHistory(instance, before)
	if err != nil {
		callFailure(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"deleted": deleted})
}
