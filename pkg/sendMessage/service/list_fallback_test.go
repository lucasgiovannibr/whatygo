package send_service

import (
	"errors"
	"strings"
	"testing"

	"github.com/lucasgiovannibr/whatygo/pkg/apierror"
)

func sampleList(rows ...Row) *ListStruct {
	return &ListStruct{Number: "5531999990000", Title: "Planos", Description: "Escolha", FooterText: "Evolution", Sections: []Section{{Title: "Todos", Rows: rows}}}
}

func TestListToButtonsSplitsThreeToAMessage(t *testing.T) {
	list := sampleList(
		Row{Title: "A", RowId: "a"}, Row{Title: "B", RowId: "b"}, Row{Title: "C", RowId: "c"},
		Row{Title: "D", RowId: "d"},
	)
	parts, err := listToButtons(list)
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 2 || len(parts[0].Buttons) != 3 || len(parts[1].Buttons) != 1 {
		t.Fatalf("4 rows must be 3 + 1 buttons, got %+v", parts)
	}
	if parts[0].Title != "Planos (1/2)" || parts[1].Title != "Planos (2/2)" {
		t.Fatalf("titles must number the messages: %q / %q", parts[0].Title, parts[1].Title)
	}
	if b := parts[1].Buttons[0]; b.Type != "reply" || b.Id != "d" || b.DisplayText != "D" {
		t.Fatalf("a row becomes a reply button with its id: %+v", b)
	}
}

func TestListToButtonsOneMessageKeepsTheTitle(t *testing.T) {
	parts, err := listToButtons(sampleList(Row{Title: "A", Description: "primeira", RowId: "a"}))
	if err != nil || len(parts) != 1 {
		t.Fatalf("%v %v", parts, err)
	}
	if parts[0].Title != "Planos" {
		t.Fatalf("a single message is not numbered: %q", parts[0].Title)
	}
	body := parts[0].Description
	if !strings.Contains(body, "Escolha") || !strings.Contains(body, "• A — primeira") || !strings.Contains(body, "*Todos*") {
		t.Fatalf("the body carries the description, the section and the row text: %q", body)
	}
}

func TestListToButtonsOnlyTheFirstMessageQuotes(t *testing.T) {
	list := sampleList(Row{Title: "A"}, Row{Title: "B"}, Row{Title: "C"}, Row{Title: "D"})
	list.Quoted = QuotedStruct{MessageID: "q1"}
	list.Delay = 500
	parts, _ := listToButtons(list)
	if parts[0].Quoted.MessageID != "q1" || parts[0].Delay != 500 {
		t.Fatalf("the first message keeps the quote and the typing delay: %+v", parts[0])
	}
	if parts[1].Quoted.MessageID != "" || parts[1].Delay != 0 {
		t.Fatalf("the others do not: %+v", parts[1])
	}
}

func TestListToButtonsGeneratesIdsAndFitsLabels(t *testing.T) {
	parts, _ := listToButtons(sampleList(Row{Title: "Um título bem comprido demais"}, Row{Title: ""}))
	b := parts[0].Buttons
	if b[0].Id != "row_0" || b[1].Id != "row_1" {
		t.Fatalf("missing row ids are generated: %q %q", b[0].Id, b[1].Id)
	}
	if got := []rune(b[0].DisplayText); len(got) != maxReplyButtonLabel || got[len(got)-1] != '…' {
		t.Fatalf("a long title is cut to the label size: %q", b[0].DisplayText)
	}
	if b[1].DisplayText == "" {
		t.Fatal("an empty title still needs a label")
	}
}

func TestListToButtonsLimits(t *testing.T) {
	if _, err := listToButtons(sampleList()); err == nil {
		t.Fatal("a list without rows cannot be sent as buttons")
	}
	many := make([]Row, maxListFallbackRows+1)
	if _, err := listToButtons(sampleList(many...)); err == nil || !strings.Contains(err.Error(), "at most 9") {
		t.Fatalf("too many rows must be refused with the limit: %v", err)
	}
	if parts, err := listToButtons(sampleList(many[:maxListFallbackRows]...)); err != nil || len(parts) != 3 {
		t.Fatalf("nine rows are three messages: %v %v", len(parts), err)
	}
}

func TestListRefusalIsCaughtAndStillAnApiError(t *testing.T) {
	err := explainInteractiveError(serverError(405), "ListMessage")
	var refused *listRefusal
	if !errors.As(err, &refused) {
		t.Fatal("the refusal of a list must be recognisable to fall back")
	}
	var apiErr *apierror.Error
	if !errors.As(err, &apiErr) || apiErr.Status != 502 || apiErr.Code != "whatsapp_rejected" {
		t.Fatalf("and it must still answer 502 whatsapp_rejected: %v", err)
	}
	if status, code := apierror.Classify(err); status != 502 || code != "whatsapp_rejected" {
		t.Fatalf("Classify: %d %s", status, code)
	}
}
