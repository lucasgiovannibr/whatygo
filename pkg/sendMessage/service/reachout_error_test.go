package send_service

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/lucasgiovannibr/whatygo/pkg/apierror"
	whatsmeow_service "github.com/lucasgiovannibr/whatygo/pkg/whatsmeow/service"
	"go.mau.fi/whatsmeow"
)

func serverError(code int) error {
	// exactly how whatsmeow builds it (send.go: fmt.Errorf("%w %d", ErrServerReturnedError, code))
	return fmt.Errorf("%w %d", whatsmeow.ErrServerReturnedError, code)
}

func TestIsReachoutError(t *testing.T) {
	if !isReachoutError(serverError(463)) {
		t.Fatal("error 463 must be recognised")
	}
	if isReachoutError(serverError(405)) || isReachoutError(serverError(4630)) {
		t.Fatal("other codes must not match")
	}
	if isReachoutError(errors.New("something 463")) || isReachoutError(nil) {
		t.Fatal("only whatsmeow's server error counts")
	}
}

func TestExplainSendErrorWithoutKnownRestriction(t *testing.T) {
	orig := serverError(463)
	got := explainSendError(orig, nil, time.Now())
	if !errors.Is(got, whatsmeow.ErrServerReturnedError) || !strings.Contains(got.Error(), "463") {
		t.Fatalf("the original error must stay wrapped and visible: %v", got)
	}
	if !strings.Contains(got.Error(), "messaged it first") {
		t.Fatalf("must explain what happened: %v", got)
	}
}

func TestExplainSendErrorWithActiveRestriction(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	end := now.Add(48 * time.Hour)
	lock := &whatsmeow_service.ReachoutTimelockStatus{Active: true, EnforcementType: "X", EndsAt: &end}

	got := explainSendError(serverError(463), lock, now)
	if !strings.Contains(got.Error(), end.Format(time.RFC3339)) || !strings.Contains(got.Error(), "restricted") {
		t.Fatalf("must mention the restriction and when it ends: %v", got)
	}

	// a restriction that already ended is not reported as active
	past := now.Add(-time.Hour)
	ended := &whatsmeow_service.ReachoutTimelockStatus{Active: true, EndsAt: &past}
	if got := explainSendError(serverError(463), ended, now); strings.Contains(got.Error(), past.Format(time.RFC3339)) {
		t.Fatalf("an ended restriction must not be quoted: %v", got)
	}
}

func TestExplainSendErrorLeavesOtherErrorsAlone(t *testing.T) {
	orig := serverError(500)
	if got := explainSendError(orig, nil, time.Now()); got != orig {
		t.Fatalf("other errors must pass through untouched: %v", got)
	}
}

func TestExplainInteractiveErrorOnlyForLists(t *testing.T) {
	for _, code := range []int{405, 473, 479} {
		got := explainInteractiveError(serverError(code), "ListMessage")
		var apiErr *apierror.Error
		if !errors.As(got, &apiErr) || apiErr.Code != "whatsapp_rejected" || !strings.Contains(apiErr.Message, fmt.Sprint(code)) {
			t.Fatalf("code %d on a list must become an explained api error: %v", code, got)
		}
	}
	orig := serverError(405)
	if got := explainInteractiveError(orig, "InteractiveMessage"); got != orig {
		t.Fatalf("a button or carousel keeps its error: %v", got)
	}
	other := serverError(500)
	if got := explainInteractiveError(other, "ListMessage"); got != other {
		t.Fatalf("an unrelated server error is left alone: %v", got)
	}
	if got := explainInteractiveError(nil, "ListMessage"); got != nil {
		t.Fatalf("nil stays nil: %v", got)
	}
}
