package send_service

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lucasgiovannibr/whatygo/pkg/apierror"
	instance_model "github.com/lucasgiovannibr/whatygo/pkg/instance/model"
)

// WhatsApp refuses list messages from a linked-device session (405/479, Business accounts
// included), but it shows reply buttons. When the list is refused it is sent as reply buttons:
// at most buttonsPerMessage in a message, in at most maxListFallbackRows/buttonsPerMessage
// messages, so a list never turns into a flood.
const (
	buttonsPerMessage     = 3
	maxListFallbackRows   = 9
	maxReplyButtonLabel   = 20
	pauseBetweenListParts = 600 * time.Millisecond
)

// listRefusal is the error SendMessage returns when WhatsApp refused a list message. It unwraps
// to the explained *apierror.Error, so the API answer is the same whether or not it is caught.
type listRefusal struct{ api *apierror.Error }

func (e *listRefusal) Error() string { return e.api.Error() }
func (e *listRefusal) Unwrap() error { return e.api }

type listRow struct {
	section     string
	title       string
	description string
	id          string
}

func flattenList(data *ListStruct) []listRow {
	var rows []listRow
	for _, sec := range data.Sections {
		for _, r := range sec.Rows {
			id := r.RowId
			if id == "" {
				id = fmt.Sprintf("row_%d", len(rows))
			}
			rows = append(rows, listRow{
				section:     strings.TrimSpace(sec.Title),
				title:       strings.TrimSpace(r.Title),
				description: strings.TrimSpace(r.Description),
				id:          id,
			})
		}
	}
	return rows
}

// replyLabel fits a row title in the label of a reply button.
func replyLabel(title string) string {
	runes := []rune(strings.TrimSpace(title))
	if len(runes) == 0 {
		return "Opção"
	}
	if len(runes) <= maxReplyButtonLabel {
		return string(runes)
	}
	return string(runes[:maxReplyButtonLabel-1]) + "…"
}

// listRowsText describes rows in the body of a message: the buttons only have room for a short
// label, so the full title and the description of each row go in the text.
func listRowsText(rows []listRow) string {
	var b strings.Builder
	last := ""
	for i, r := range rows {
		if r.section != "" && (i == 0 || r.section != last) {
			if i > 0 {
				b.WriteString("\n")
			}
			b.WriteString("*" + r.section + "*\n")
		}
		last = r.section
		b.WriteString("• " + r.title)
		if r.description != "" {
			b.WriteString(" — " + r.description)
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// listToButtons turns a list into reply-button messages: the rows in order, three to a message.
// The id of a row is the id of its button, so the answer reaches the webhook as it would from
// a list. Only the first message carries the quote, the mentions and the typing delay.
func listToButtons(data *ListStruct) ([]ButtonStruct, error) {
	rows := flattenList(data)
	if len(rows) == 0 {
		return nil, errors.New("the list has no rows")
	}
	if len(rows) > maxListFallbackRows {
		return nil, fmt.Errorf("the list has %d rows and the buttons alternative takes at most %d", len(rows), maxListFallbackRows)
	}

	total := (len(rows) + buttonsPerMessage - 1) / buttonsPerMessage
	parts := make([]ButtonStruct, 0, total)
	for n := 0; n < total; n++ {
		chunk := rows[n*buttonsPerMessage : min((n+1)*buttonsPerMessage, len(rows))]

		part := ButtonStruct{
			Number:    data.Number,
			Title:     data.Title,
			Footer:    data.FooterText,
			FormatJid: data.FormatJid,
		}
		text := listRowsText(chunk)
		if n == 0 {
			part.Description = data.Description + "\n\n" + text
			part.Delay = data.Delay
			part.Quoted = data.Quoted
			part.MentionAll = data.MentionAll
			part.MentionedJID = data.MentionedJID
		} else {
			part.Description = text
		}
		if total > 1 {
			part.Title = fmt.Sprintf("%s (%d/%d)", data.Title, n+1, total)
		}
		for _, r := range chunk {
			part.Buttons = append(part.Buttons, Button{Type: "reply", DisplayText: replyLabel(r.title), Id: r.id})
		}
		parts = append(parts, part)
	}
	return parts, nil
}

// sendListAsButtons is the answer to a refused list: the same list as reply buttons. It returns
// the first message sent; Fallback and Parts say what happened.
func (s *sendService) sendListAsButtons(data *ListStruct, instance *instance_model.Instance, refusal error) (*MessageSendStruct, error) {
	parts, err := listToButtons(data)
	if err != nil {
		return nil, fmt.Errorf("%w. Sending it as buttons was not possible: %v", refusal, err)
	}

	s.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] WhatsApp refused the list: sending it as %d message(s) of reply buttons", instance.Id, len(parts))

	var first *MessageSendStruct
	for i := range parts {
		if i > 0 {
			time.Sleep(pauseBetweenListParts)
		}
		sent, err := s.SendButton(&parts[i], instance)
		if err != nil {
			if first != nil {
				return nil, fmt.Errorf("sent %d of %d messages of the list before failing: %w", i, len(parts), err)
			}
			return nil, err
		}
		if first == nil {
			first = sent
		}
	}
	first.Fallback = "buttons"
	first.Parts = len(parts)
	return first, nil
}
