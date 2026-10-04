package send_service

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/lucasgiovannibr/whatygo/pkg/apierror"
	whatsmeow_service "github.com/lucasgiovannibr/whatygo/pkg/whatsmeow/service"
	"go.mau.fi/whatsmeow"
)

// isReachoutError reports whether err is WhatsApp's answer 463
// (NackCallerReachoutTimelocked). whatsmeow only carries the code in the text of
// ErrServerReturnedError ("server returned error 463").
func isReachoutError(err error) bool {
	return err != nil &&
		errors.Is(err, whatsmeow.ErrServerReturnedError) &&
		strings.HasSuffix(strings.TrimSpace(err.Error()), " 463")
}

// explainSendError turns a bare "server returned error 463" into something a person
// can act on. The original error stays wrapped (errors.Is / the code in the text
// still work). lock is the restriction WhatsApp reported for the account, if any.
func explainSendError(err error, lock *whatsmeow_service.ReachoutTimelockStatus, now time.Time) error {
	if !isReachoutError(err) {
		return err
	}

	msg := "this WhatsApp account cannot start a conversation with a contact that has not messaged it first (reachout timelock)"
	if lock.InEffect(now) {
		msg = "WhatsApp restricted this account from starting conversations with new contacts"
		if lock.EndsAt != nil {
			msg += " until " + lock.EndsAt.Format(time.RFC3339)
		}
	}
	return fmt.Errorf("%w: %s. Messages to contacts that already talked to this number still work; otherwise wait or ask the contact to message first", err, msg)
}

// interactiveRefusal is the code WhatsApp answers with (405, 473, 479) when it refuses the
// shape of an interactive message. whatsmeow only carries it in the text of the error.
func interactiveRefusal(err error) string {
	if err == nil || !errors.Is(err, whatsmeow.ErrServerReturnedError) {
		return ""
	}
	text := strings.TrimSpace(err.Error())
	for _, code := range []string{"405", "473", "479"} {
		if strings.HasSuffix(text, " "+code) {
			return code
		}
	}
	return ""
}

// explainInteractiveError turns WhatsApp's bare refusal of a list into an answer a client can
// act on. Lists (the legacy ListMessage and the native single_select) were refused for every
// wire format tried from a linked-device session, Business account included; the buttons
// and the carousel are accepted. Other message types keep their error.
func explainInteractiveError(err error, messageType string) error {
	code := interactiveRefusal(err)
	if code == "" || messageType != "ListMessage" {
		return err
	}
	return &listRefusal{&apierror.Error{
		Status:  http.StatusBadGateway,
		Code:    "whatsapp_rejected",
		Message: "WhatsApp refused the list message (server error " + code + "): list messages are not accepted from linked-device sessions, even on WhatsApp Business accounts. Use /send/button (up to 3 reply buttons) or /send/carousel instead",
	}}
}
