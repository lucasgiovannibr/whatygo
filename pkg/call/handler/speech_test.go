package call_handler

import (
	"net/http"
	"testing"

	call_service "github.com/lucasgiovannibr/whatygo/pkg/call/service"
)

func TestStreamTicketAsksForSpeechEvents(t *testing.T) {
	e := newEnv(t)
	e.ringing("C1")

	w := e.call("POST", "/call/stream-ticket", "inst", map[string]interface{}{"callId": "C1", "speechEvents": true})
	var ticket call_service.StreamTicket
	decode(t, w, &ticket)
	if w.Code != http.StatusOK || !ticket.SpeechEvents {
		t.Fatalf("ticket: %d %s", w.Code, w.Body.String())
	}
	if _, o, ok := e.tickets.RedeemWith(ticket.Ticket, "C1"); !ok || !o.Speech {
		t.Fatalf("the ticket carries %+v (%v)", o, ok)
	}

	w = e.call("POST", "/call/stream-ticket", "inst", map[string]interface{}{"callId": "C1"})
	decode(t, w, &ticket)
	if ticket.SpeechEvents {
		t.Fatalf("a ticket that did not ask for speech events says it reports them: %s", w.Body.String())
	}
}

func TestDialHandsSpeechEventsOnToItsTicket(t *testing.T) {
	e := newEnv(t)

	w := e.call("POST", "/call/dial", "inst", map[string]interface{}{"number": "5511999990000", "stream": true, "speechEvents": true})
	var result call_service.DialResult
	decode(t, w, &result)
	if w.Code != http.StatusOK || result.StreamTicket == nil || !result.StreamTicket.SpeechEvents {
		t.Fatalf("dial: %d %s", w.Code, w.Body.String())
	}
}
