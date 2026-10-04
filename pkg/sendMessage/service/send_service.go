package send_service

import (
	"bytes"
	"context"
	crypto_rand "crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/lucasgiovannibr/whatygo/pkg/apierror"
	"github.com/lucasgiovannibr/whatygo/pkg/safemap"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/chai2010/webp"
	"github.com/gabriel-vasile/mimetype"
	config "github.com/lucasgiovannibr/whatygo/pkg/config"
	instance_model "github.com/lucasgiovannibr/whatygo/pkg/instance/model"
	logger_wrapper "github.com/lucasgiovannibr/whatygo/pkg/logger"
	"github.com/lucasgiovannibr/whatygo/pkg/utils"
	whatsmeow_service "github.com/lucasgiovannibr/whatygo/pkg/whatsmeow/service"
	"go.mau.fi/whatsmeow"
	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"golang.org/x/net/html"
	"google.golang.org/protobuf/proto"
)

type SendService interface {
	SendText(data *TextStruct, instance *instance_model.Instance) (*MessageSendStruct, error)
	SendLink(data *LinkStruct, instance *instance_model.Instance) (*MessageSendStruct, error)
	SendMediaUrl(data *MediaStruct, instance *instance_model.Instance) (*MessageSendStruct, error)
	SendMediaFile(data *MediaStruct, fileData []byte, instance *instance_model.Instance) (*MessageSendStruct, error)
	SendPoll(data *PollStruct, instance *instance_model.Instance) (*MessageSendStruct, error)
	SendPollVote(data *PollVoteStruct, instance *instance_model.Instance) (*MessageSendStruct, error)
	SendSticker(data *StickerStruct, instance *instance_model.Instance) (*MessageSendStruct, error)
	SendLocation(data *LocationStruct, instance *instance_model.Instance) (*MessageSendStruct, error)
	SendContact(data *ContactStruct, instance *instance_model.Instance) (*MessageSendStruct, error)
	SendButton(data *ButtonStruct, instance *instance_model.Instance) (*MessageSendStruct, error)
	SendList(data *ListStruct, instance *instance_model.Instance) (*MessageSendStruct, error)
	SendCarousel(data *CarouselStruct, instance *instance_model.Instance) (*MessageSendStruct, error)
	SendStatusText(data *StatusTextStruct, instance *instance_model.Instance) (*MessageSendStruct, error)
	SendStatusMediaUrl(data *StatusMediaStruct, instance *instance_model.Instance) (*MessageSendStruct, error)
	SendStatusMediaFile(data *StatusMediaStruct, fileData []byte, instance *instance_model.Instance) (*MessageSendStruct, error)
}

type sendService struct {
	clientPointer    *safemap.Map[*whatsmeow.Client]
	whatsmeowService whatsmeow_service.WhatsmeowService
	config           *config.Config
	loggerWrapper    *logger_wrapper.LoggerManager
	// existsCache remembers the "is this number on WhatsApp" check (nil = off).
	existsCache *userExistsCache
	// throttle limits the sends of each instance (see throttle.go).
	throttle *sendThrottle
}

// sendToWhatsApp is client.SendMessage behind the instance's send limit.
func (s *sendService) sendToWhatsApp(instanceID string, client *whatsmeow.Client, recipient types.JID, msg *waE2E.Message, extra whatsmeow.SendRequestExtra) (whatsmeow.SendResponse, error) {
	ctx := context.Background()
	release, err := s.throttle.acquire(ctx, instanceID)
	if err != nil {
		return whatsmeow.SendResponse{}, err
	}
	defer release()
	return client.SendMessage(ctx, recipient, msg, extra)
}

// maxSendDelay is the longest "typing" delay before a send (the same limit ChatPresence
// has).
const maxSendDelay = 60 * time.Second

// sendDelay is the typing delay of a send, and whether it had to be capped. The delay is
// an int32 of milliseconds supplied by the caller (up to ~24 days), and the request, its
// goroutine and the "typing" state were held for all of it.
func sendDelay(ms int32) (delay time.Duration, capped bool) {
	delay = time.Duration(ms) * time.Millisecond
	if delay > maxSendDelay {
		return maxSendDelay, true
	}
	return delay, false
}

type SendDataStruct struct {
	Id              string
	Number          string
	Delay           int32
	MentionAll      bool
	MentionedJID    []string
	FormatJid       *bool
	Quoted          QuotedStruct
	MediaHandle     string
	AdditionalNodes *[]waBinary.Node
	ForwardingScore *uint32
	// MediaBytes is the file that was sent, when the caller has it: the SendMessage event
	// then carries it without downloading it back from WhatsApp.
	MediaBytes []byte
}

type QuotedStruct struct {
	MessageID   string `json:"messageId"`
	Participant string `json:"participant"`
	// Text is the text of the message being replied to. Optional: without it the
	// reply's quote card is rendered empty by WhatsApp clients (issue #189).
	Text string `json:"text,omitempty"`
}

// quotedMessageFor builds ContextInfo.QuotedMessage for a reply. WhatsApp clients
// need the quoted content to render the preview card; it used to be hardcoded to
// an empty conversation, so only the link (StanzaID/Participant) worked.
func quotedMessageFor(q QuotedStruct) *waE2E.Message {
	return &waE2E.Message{Conversation: proto.String(q.Text)}
}

type TextStruct struct {
	Number          string       `json:"number"`
	Text            string       `json:"text"`
	Id              string       `json:"id"`
	Delay           int32        `json:"delay"`
	MentionedJID    []string     `json:"mentionedJid"`
	MentionAll      bool         `json:"mentionAll"`
	FormatJid       *bool        `json:"formatJid,omitempty"`
	Quoted          QuotedStruct `json:"quoted"`
	ForwardingScore *uint32      `json:"forwardingScore,omitempty"`
}

type LinkStruct struct {
	Number       string       `json:"number"`
	Text         string       `json:"text"`
	Title        string       `json:"title"`
	Url          string       `json:"url"`
	Description  string       `json:"description"`
	ImgUrl       string       `json:"imgUrl"`
	Id           string       `json:"id"`
	Delay        int32        `json:"delay"`
	MentionedJID []string     `json:"mentionedJid"`
	MentionAll   bool         `json:"mentionAll"`
	FormatJid    *bool        `json:"formatJid,omitempty"`
	Quoted       QuotedStruct `json:"quoted"`
}

type MediaStruct struct {
	Number          string       `json:"number"`
	Url             string       `json:"url"`
	Type            string       `json:"type"`
	Caption         string       `json:"caption"`
	Filename        string       `json:"filename"`
	Id              string       `json:"id"`
	Delay           int32        `json:"delay"`
	MentionedJID    []string     `json:"mentionedJid"`
	MentionAll      bool         `json:"mentionAll"`
	FormatJid       *bool        `json:"formatJid,omitempty"`
	Quoted          QuotedStruct `json:"quoted"`
	ForwardingScore *uint32      `json:"forwardingScore,omitempty"`
	// ViewOnce sends image, video, audio or video-note media that disappears
	// after the recipient opens it once. Documents do not support it.
	ViewOnce bool `json:"viewOnce,omitempty"`
}

// applyViewOnce flags the media of msg as view-once when requested. It is a
// no-op for media types that do not support it (documents) and when disabled.
func applyViewOnce(msg *waE2E.Message, viewOnce bool) {
	if !viewOnce || msg == nil {
		return
	}
	switch {
	case msg.ImageMessage != nil:
		msg.ImageMessage.ViewOnce = proto.Bool(true)
	case msg.VideoMessage != nil:
		msg.VideoMessage.ViewOnce = proto.Bool(true)
	case msg.AudioMessage != nil:
		msg.AudioMessage.ViewOnce = proto.Bool(true)
	case msg.PtvMessage != nil:
		msg.PtvMessage.ViewOnce = proto.Bool(true)
	}
}

type PollStruct struct {
	Id           string       `json:"id"`
	Number       string       `json:"number"`
	Question     string       `json:"question"`
	MaxAnswer    int          `json:"maxAnswer"`
	Options      []string     `json:"options"`
	Delay        int32        `json:"delay"`
	MentionedJID []string     `json:"mentionedJid"`
	MentionAll   bool         `json:"mentionAll"`
	FormatJid    *bool        `json:"formatJid,omitempty"`
	Quoted       QuotedStruct `json:"quoted"`
}

type StickerStruct struct {
	Number       string       `json:"number"`
	Sticker      string       `json:"sticker"`
	Id           string       `json:"id"`
	Delay        int32        `json:"delay"`
	MentionedJID []string     `json:"mentionedJid"`
	MentionAll   bool         `json:"mentionAll"`
	FormatJid    *bool        `json:"formatJid,omitempty"`
	Quoted       QuotedStruct `json:"quoted"`
}

type LocationStruct struct {
	Number       string       `json:"number"`
	Id           string       `json:"id"`
	Name         string       `json:"name"`
	Latitude     float64      `json:"latitude"`
	Longitude    float64      `json:"longitude"`
	Address      string       `json:"address"`
	Delay        int32        `json:"delay"`
	MentionedJID []string     `json:"mentionedJid"`
	MentionAll   bool         `json:"mentionAll"`
	FormatJid    *bool        `json:"formatJid,omitempty"`
	Quoted       QuotedStruct `json:"quoted"`
}

type ContactStruct struct {
	Number       string            `json:"number"`
	Id           string            `json:"id"`
	Vcard        utils.VCardStruct `json:"vcard"`
	Delay        int32             `json:"delay"`
	MentionedJID []string          `json:"mentionedJid"`
	MentionAll   bool              `json:"mentionAll"`
	FormatJid    *bool             `json:"formatJid,omitempty"`
	Quoted       QuotedStruct      `json:"quoted"`
}

// Button represents a single interactive button for /send/button.
// The `type` field drives which of the other fields are used:
//   - reply: uses `displayText` + `id`
//   - copy:  uses `displayText` + `copyCode`
//   - url:   uses `displayText` + `url`
//   - call:  uses `displayText` + `phoneNumber`
//   - pix:   uses `currency` + `name` + `keyType` + `key` (must be sent alone)
type Button struct {
	// Button kind. One of: reply, copy, url, call, pix.
	Type string `json:"type" enums:"reply,copy,url,call,pix" example:"reply"`
	// Label rendered inside the button (reply / copy / url / call). Ignored for pix.
	DisplayText string `json:"displayText" example:"Quero saber mais"`
	// Callback payload for `reply` or code-to-copy internal id for `copy`.
	Id string `json:"id" example:"btn_info"`
	// Code placed in the clipboard when type=copy.
	CopyCode string `json:"copyCode,omitempty" example:"PROMO2026"`
	// Target URL when type=url.
	URL string `json:"url,omitempty" example:"https://example.com"`
	// Destination phone number (E.164) when type=call.
	PhoneNumber string `json:"phoneNumber,omitempty" example:"+5582988898565"`
	// ISO currency code for type=pix (e.g. BRL).
	Currency string `json:"currency,omitempty" example:"BRL"`
	// Merchant display name shown on the Pix sheet.
	Name string `json:"name,omitempty" example:"Minha Loja"`
	// Pix key type. One of: phone, email, cpf, cnpj, random.
	KeyType string `json:"keyType,omitempty" enums:"phone,email,cpf,cnpj,random" example:"cpf"`
	// Pix key value matching the keyType.
	Key string `json:"key,omitempty" example:"12345678900"`
}

// ButtonStruct is the body for POST /send/button.
//
// Server-side validation:
//   - up to 3 `reply` buttons per message;
//   - `pix` must be the only button in the message.
//
// WhatsApp Web rendering quirk (NOT enforced by the server):
//   - mixing `reply` with CTA buttons (copy/url/call) shows on the phone but is invisible on WhatsApp Web;
//   - safe on both: only-reply (up to 3) OR grouped CTAs (copy + url + call).
type ButtonStruct struct {
	// Destination phone number.
	Number string `json:"number" example:"5582988898565"`
	// Header title (required).
	Title string `json:"title" example:"Oferta especial"`
	// Body description text (required).
	Description string `json:"description" example:"Confira as condicoes abaixo"`
	// Footer text (required).
	Footer string `json:"footer" example:"WhatyGo"`
	// Buttons array. See combination rules on the parent type description.
	Buttons []Button `json:"buttons"`
	// Typing delay (milliseconds) applied before sending the message.
	Delay int32 `json:"delay,omitempty" example:"1200"`
	// JIDs to mention inside the body text.
	MentionedJID []string `json:"mentionedJid,omitempty"`
	// Mention every participant (groups only).
	MentionAll bool `json:"mentionAll,omitempty"`
	// If false, skips automatic formatting/validation of `number` into a JID.
	FormatJid *bool `json:"formatJid,omitempty"`
	// Quoted (reply-to) context.
	Quoted QuotedStruct `json:"quoted,omitempty"`
	// Optional image URL used as header for reply-only buttons.
	ImageUrl string `json:"imageUrl,omitempty"`
	// Optional video URL used as header for reply-only buttons.
	VideoUrl string `json:"videoUrl,omitempty"`
}

// Row is a selectable item inside a list Section.
type Row struct {
	// Row main label.
	Title string `json:"title" example:"Plano Basico"`
	// Optional secondary line below the title.
	Description string `json:"description,omitempty" example:"R$ 29,90/mes"`
	// Callback payload returned when the user taps the row. Auto-generated if empty.
	RowId string `json:"rowId,omitempty" example:"plan_basic"`
}

// Section groups related Rows under an optional title.
type Section struct {
	// Section heading (optional; rendered as a group separator).
	Title string `json:"title,omitempty" example:"Planos"`
	// Rows inside this section.
	Rows []Row `json:"rows"`
}

// ListStruct is the body for POST /send/list.
//
// Renders as a single-select menu (legacy ListMessage format — compatible with iOS, Android and WhatsApp Web).
type ListStruct struct {
	// Destination phone number.
	Number string `json:"number" example:"5582988898565"`
	// Header title (required).
	Title string `json:"title" example:"Nossos planos"`
	// Body description text (required).
	Description string `json:"description" example:"Escolha o plano ideal para voce"`
	// Label of the button that opens the list. Defaults to "Ver Menu" when empty.
	ButtonText string `json:"buttonText" example:"Abrir cardapio"`
	// Footer text (required).
	FooterText string `json:"footerText" example:"WhatyGo"`
	// Sections with rows. At least one section with one row is required.
	Sections []Section `json:"sections"`
	// Typing delay (milliseconds) applied before sending the message.
	Delay int32 `json:"delay,omitempty" example:"1200"`
	// JIDs to mention inside the body text.
	MentionedJID []string `json:"mentionedJid,omitempty"`
	// Mention every participant (groups only).
	MentionAll bool `json:"mentionAll,omitempty"`
	// If false, skips automatic formatting/validation of `number` into a JID.
	FormatJid *bool `json:"formatJid,omitempty"`
	// Quoted (reply-to) context.
	Quoted QuotedStruct `json:"quoted,omitempty"`
	// WhatsApp refuses list messages from a linked-device session. By default, when that happens the list
	// is sent as reply buttons (up to 9 rows, 3 buttons per message); false answers 502 instead.
	FallbackButtons *bool `json:"fallbackButtons,omitempty"`
}

// CarouselButtonStruct is a button attached to a single carousel card.
//
// IMPORTANT — this struct is different from `Button` (used in /send/button):
// it has NO dedicated `url` or `phoneNumber` fields. For URL and CALL buttons
// you must put the link / phone number in the `id` field.
//
//   - REPLY (default): uses `displayText` + `id` as callback payload.
//   - URL:   uses `displayText` + `id` (put the URL here).
//   - CALL:  uses `displayText` + `id` (put the phone number here).
//   - COPY:  uses `displayText` + `copyCode`.
//
// PIX buttons are NOT supported inside carousel cards — use /send/button instead.
//
// WhatsApp Web rendering quirk (NOT enforced by the server):
// avoid mixing REPLY with CTA buttons (URL/CALL/COPY) in the same card —
// mixed sets do not render on WhatsApp Web. Prefer only-REPLY or only-CTAs per card.
type CarouselButtonStruct struct {
	// Button kind (case-insensitive). One of: REPLY (default), URL, CALL, COPY (COPY_CODE is accepted as an alias).
	Type string `json:"type" enums:"REPLY,URL,CALL,COPY,reply,url,call,copy" example:"REPLY"`
	// Label rendered inside the button.
	DisplayText string `json:"displayText" example:"Quero saber mais"`
	// Context-dependent: REPLY payload, URL target (type=URL) or phone number (type=CALL).
	Id string `json:"id" example:"card1_info"`
	// Code placed in the clipboard when type=COPY (alias: COPY_CODE).
	CopyCode string `json:"copyCode,omitempty" example:"PROMO2026"`
	// Explicit URL target for type=URL. Optional: `id` is used when empty.
	URL string `json:"url,omitempty" example:"https://example.com"`
	// Explicit phone number for type=CALL. Optional: `id` is used when empty.
	PhoneNumber string `json:"phoneNumber,omitempty" example:"5511999999999"`
}

// CarouselCardHeaderStruct is the top area of a carousel card.
// Either `imageUrl` OR `videoUrl` may be provided (image takes precedence when both are set).
type CarouselCardHeaderStruct struct {
	// Optional visible title above the media.
	Title string `json:"title,omitempty" example:"Oferta do dia"`
	// Optional subtitle rendered below the title.
	Subtitle string `json:"subtitle,omitempty" example:"Somente hoje"`
	// Public URL to an image. Downloaded, uploaded to WhatsApp servers and used as card media.
	ImageUrl string `json:"imageUrl,omitempty" example:"https://picsum.photos/seed/card1/600/400"`
	// Public URL to a video. Used only when `imageUrl` is empty.
	VideoUrl string `json:"videoUrl,omitempty"`
}

// CarouselCardBodyStruct is the text area of a carousel card.
type CarouselCardBodyStruct struct {
	// Main text of the card.
	Text string `json:"text" example:"Card 1 - Oferta especial"`
}

// CarouselCardStruct is a single card inside a carousel message.
// Each card requires at least `header` + `body`.
type CarouselCardStruct struct {
	// Card header (media + title/subtitle).
	Header CarouselCardHeaderStruct `json:"header"`
	// Card body text (required).
	Body CarouselCardBodyStruct `json:"body"`
	// Optional footer rendered under the body.
	Footer string `json:"footer,omitempty" example:"Por tempo limitado"`
	// Buttons shown on the card. See CarouselButtonStruct for combination rules.
	Buttons []CarouselButtonStruct `json:"buttons,omitempty"`
}

// CarouselStruct is the body for POST /send/carousel.
//
// Sends an interactive carousel of swipeable cards. At least one card is required.
// Each card must have `header` + `body`; button rules are described on CarouselButtonStruct.
type CarouselStruct struct {
	// Destination phone number.
	Number string `json:"number" example:"5582988898565"`
	// Optional message body shown above the cards.
	Body string `json:"body,omitempty" example:"Confira nossas novidades!"`
	// Optional message footer shown below the cards.
	Footer string `json:"footer,omitempty" example:"WhatyGo"`
	// Typing delay (milliseconds) applied before sending the message.
	Delay int32 `json:"delay,omitempty" example:"1200"`
	// If false, skips automatic formatting/validation of `number` into a JID.
	FormatJid *bool `json:"formatJid,omitempty"`
	// Quoted (reply-to) context.
	Quoted QuotedStruct `json:"quoted,omitempty"`
	// Cards displayed in order. At least one card is required.
	Cards []CarouselCardStruct `json:"cards"`
}

type StatusTextStruct struct {
	Text string `json:"text"`
	Id   string `json:"id"`
}

type StatusMediaStruct struct {
	Type    string `json:"type"`
	Url     string `json:"url"`
	Caption string `json:"caption"`
	Id      string `json:"id"`
}

type MessageSendStruct struct {
	Info               types.MessageInfo
	Message            *waE2E.Message
	MessageContextInfo *waE2E.ContextInfo
	// Fallback is set when the message went out as something else than asked ("buttons" for a
	// list WhatsApp refused); Parts is how many messages that took. Info is the first one.
	Fallback string `json:",omitempty"`
	Parts    int    `json:",omitempty"`
}

// The ways a send finds no usable connection. They are matched with errors.Is, not by
// their text: the text used to be compared, and an error wrapped with %v (which loses the
// chain) quietly stopped being retried. The texts are the ones the API has always returned.
var (
	// ErrNoActiveSession: the instance has no client, or it did not come up.
	ErrNoActiveSession = utils.ErrNoActiveSession
	// ErrClientDisconnected: the instance has a client whose socket is down.
	ErrClientDisconnected = utils.ErrClientDisconnected
)

// isDisconnectionError reports whether err means the connection was missing, which a
// retry may fix (a validation or WhatsApp error will not change by trying again).
func isDisconnectionError(err error) bool {
	return errors.Is(err, ErrNoActiveSession) || errors.Is(err, ErrClientDisconnected)
}

func (s *sendService) ensureClientConnected(instanceId string) (*whatsmeow.Client, error) {
	return utils.ClientProvider{Clients: s.clientPointer, Starter: s.whatsmeowService, Gate: true, RequirePaired: true}.Ensure(context.Background(), instanceId, s.loggerWrapper.GetLogger(instanceId))
}

// reconnectRetryStep is the pause between two attempts to get a connected client (times the
// attempt number). A variable so that the tests do not wait for it.
var reconnectRetryStep = 2 * time.Second

// ensureClientConnectedWithRetry attempts to ensure client connection with automatic reconnection and retry
func (s *sendService) ensureClientConnectedWithRetry(instanceId string, maxRetries int) (*whatsmeow.Client, error) {
	for attempt := 1; attempt <= maxRetries; attempt++ {
		s.loggerWrapper.GetLogger(instanceId).LogInfo("[%s] Connection attempt %d/%d", instanceId, attempt, maxRetries)

		client, err := s.ensureClientConnected(instanceId)
		if err == nil {
			return client, nil
		}

		// Check if it's a disconnection error that we can retry
		if isDisconnectionError(err) {
			s.loggerWrapper.GetLogger(instanceId).LogWarn("[%s] Client disconnected on attempt %d/%d, attempting reconnection...", instanceId, attempt, maxRetries)

			// Ask for the reconnection through the backoff of the automatic ones (a request
			// used to force ReconnectClient itself, which is not paced) and wait for it.
			if s.whatsmeowService.RequestReconnect(instanceId) {
				s.loggerWrapper.GetLogger(instanceId).LogInfo("[%s] Reconnection requested on attempt %d, waiting for the connection...", instanceId, attempt)
				// Wait for the connection itself instead of a fixed 3 s: when it is
				// back, retry at once; otherwise fall through to the backoff below.
				if c := utils.WaitForClient(func() *whatsmeow.Client { return s.clientPointer.Get(instanceId) }, utils.InstanceStartTimeout); c != nil && c.IsConnected() {
					continue
				}
			}

			// If this is not the last attempt, continue to retry
			if attempt < maxRetries {
				waitTime := time.Duration(attempt) * reconnectRetryStep // Progressive backoff
				s.loggerWrapper.GetLogger(instanceId).LogInfo("[%s] Waiting %v before retry attempt %d", instanceId, waitTime, attempt+1)
				time.Sleep(waitTime)
				continue
			}
		}

		// If it's the last attempt or a non-retryable error, return the error
		s.loggerWrapper.GetLogger(instanceId).LogError("[%s] Failed to ensure client connection after %d attempts: %v", instanceId, attempt, err)
		return nil, err
	}

	return nil, fmt.Errorf("failed to connect client after %d attempts", maxRetries)
}

func validateMessageFields(phone string, formatJid *bool, messageID *string, participant *string) (types.JID, error) {
	// Apply formatting if formatJid is true (default)
	shouldFormat := true // Default value
	if formatJid != nil {
		shouldFormat = *formatJid
	}

	var finalPhone string
	if shouldFormat {
		// Extract raw number if it's already a JID, then apply CreateJID formatting
		rawNumber := phone
		if strings.Contains(phone, "@s.whatsapp.net") {
			rawNumber = strings.Split(phone, "@")[0]
		}

		normalizedJID, err := utils.CreateJID(rawNumber)
		if err != nil {
			// If CreateJID fails, try with ParseJID as fallback
			recipient, ok := utils.ParseJID(phone)
			if !ok {
				return types.NewJID("", types.DefaultUserServer), fmt.Errorf("could not parse phone: %s", phone)
			}
			finalPhone = recipient.String()
		} else {
			finalPhone = normalizedJID
		}
	} else {
		// Use phone as received without formatting
		finalPhone = phone
	}

	recipient, ok := utils.ParseJID(finalPhone)
	if !ok {
		return types.NewJID("", types.DefaultUserServer), errors.New("could not parse formatted phone")
	}

	if messageID != nil {
		if participant == nil {
			return types.NewJID("", types.DefaultUserServer), apierror.Invalid("missing Participant in ContextInfo")
		}
	}

	if participant != nil {
		if messageID == nil {
			return types.NewJID("", types.DefaultUserServer), apierror.Invalid("missing StanzaId in ContextInfo")
		}
	}

	return recipient, nil
}

// validateAndCheckUserExists validates message fields and checks if the user exists on WhatsApp
// Now uses the new approach: CheckUser with formatJid=false by default, and uses remoteJID for messaging
func (s *sendService) validateAndCheckUserExists(phone string, formatJid *bool, messageID *string, participant *string, instance *instance_model.Instance) (types.JID, error) {
	// Skip WhatsApp check if disabled in config
	if !s.config.CheckUserExists {
		s.loggerWrapper.GetLogger(instance.Id).LogDebug("[%s] User existence check disabled by configuration", instance.Id)
		// Use original validation logic when check is disabled
		return validateMessageFields(phone, formatJid, messageID, participant)
	}

	// Skip WhatsApp check for group messages, broadcast, newsletter, and LID
	if strings.Contains(phone, "@g.us") || strings.Contains(phone, "@broadcast") || strings.Contains(phone, "@newsletter") || strings.Contains(phone, "@lid") {
		return validateMessageFields(phone, formatJid, messageID, participant)
	}

	// Get the client to check if user exists on WhatsApp
	client, err := s.ensureClientConnected(instance.Id)
	if err != nil {
		return types.NewJID("", types.DefaultUserServer), fmt.Errorf("failed to connect client: %w", err)
	}

	// The answer is remembered (see userExistsCache): a send used to cost one usync query,
	// two when the first said no, every single time.
	remoteJID, found, err := s.existsCache.lookup(instance.Id, phone, func() (string, bool, error) {
		// Use CheckUser approach: formatJid=false by default
		jid, ok, err := s.checkSingleUserExists(client, phone, false, instance.Id)
		if err != nil {
			return "", false, err
		}

		// If not found with formatJid=false, try with formatJid=true as fallback
		if !ok {
			s.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] User not found with formatJid=false, trying with formatJid=true", instance.Id)
			jidRetry, okRetry, errRetry := s.checkSingleUserExists(client, phone, true, instance.Id)
			if errRetry == nil && okRetry {
				return jidRetry, true, nil
			}
		}
		return jid, ok, nil
	})
	if err != nil {
		s.loggerWrapper.GetLogger(instance.Id).LogWarn("[%s] Failed to check user existence: %v", instance.Id, err)
		// Continue with sending even if check fails (network issues, etc.)
		return validateMessageFields(phone, formatJid, messageID, participant)
	}

	if !found {
		return types.NewJID("", types.DefaultUserServer), fmt.Errorf("number %s is not registered on WhatsApp", phone)
	}

	s.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] Number %s verified as valid WhatsApp user, using remoteJID: %s", instance.Id, phone, remoteJID)

	// Validate the remoteJID with formatJid=false for message sending
	formatJidFalse := false
	return validateMessageFields(remoteJID, &formatJidFalse, messageID, participant)
}

// checkSingleUserExists checks if a single user exists on WhatsApp with the specified formatJid setting
// Returns: remoteJID, found, error
func (s *sendService) checkSingleUserExists(client *whatsmeow.Client, phone string, formatJid bool, instanceId string) (string, bool, error) {
	phoneNumbers, err := utils.PrepareNumbersForWhatsAppCheck([]string{phone}, &formatJid)
	if err != nil {
		return "", false, fmt.Errorf("failed to prepare number for WhatsApp check: %v", err)
	}

	// Check if the number exists on WhatsApp
	resp, err := client.IsOnWhatsApp(context.Background(), phoneNumbers)
	if err != nil {
		return "", false, fmt.Errorf("failed to check if number %s exists on WhatsApp: %v", phoneNumbers[0], err)
	}

	// Verify if the number was found
	if len(resp) == 0 {
		return "", false, apierror.NotFound(fmt.Sprintf("number %s not found in WhatsApp response", phoneNumbers[0]))
	}

	// Check if the first result indicates the number is on WhatsApp
	if !resp[0].IsIn {
		return "", false, nil // Not an error, just not found
	}

	// Return the remoteJID from WhatsApp's response
	remoteJID := fmt.Sprintf("%v", resp[0].JID)
	return remoteJID, true, nil
}

// urlPattern matches an http(s) URL up to the next whitespace, angle bracket or quote.
var urlPattern = regexp.MustCompile(`https?://[^\s<>"']+`)

// findURL returns the first URL of a text. Punctuation that belongs to the sentence,
// not to the link ("see https://x.com/a.", "(https://x.com)"), is left out: the
// preview used to be fetched for "https://x.com/a." and fail.
func findURL(text string) string {
	m := urlPattern.FindString(text)
	return strings.TrimRight(m, ".,;:!?)]}")
}

func (s *sendService) SendText(data *TextStruct, instance *instance_model.Instance) (*MessageSendStruct, error) {
	return s.sendTextWithRetry(data, instance, 3) // 3 tentativas máximas
}

func (s *sendService) sendTextWithRetry(data *TextStruct, instance *instance_model.Instance, maxRetries int) (*MessageSendStruct, error) {
	for attempt := 1; attempt <= maxRetries; attempt++ {
		s.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] SendText attempt %d/%d", instance.Id, attempt, maxRetries)

		_, err := s.ensureClientConnectedWithRetry(instance.Id, 2)
		if err != nil {
			if attempt == maxRetries {
				return nil, err
			}
			continue
		}

		msg := &waE2E.Message{
			ExtendedTextMessage: &waE2E.ExtendedTextMessage{
				Text: &data.Text,
			},
		}

		message, err := s.SendMessage(instance, msg, "ExtendedTextMessage", &SendDataStruct{
			Id:              data.Id,
			Number:          data.Number,
			Quoted:          data.Quoted,
			Delay:           data.Delay,
			MentionAll:      data.MentionAll,
			MentionedJID:    data.MentionedJID,
			FormatJid:       data.FormatJid,
			ForwardingScore: data.ForwardingScore,
		})

		if err != nil {
			// Check if it's a client disconnection error
			if isDisconnectionError(err) {
				s.loggerWrapper.GetLogger(instance.Id).LogWarn("[%s] SendText failed due to disconnection on attempt %d/%d: %v", instance.Id, attempt, maxRetries, err)
				if attempt < maxRetries {
					waitTime := time.Duration(attempt) * time.Second
					s.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] Waiting %v before retry", instance.Id, waitTime)
					time.Sleep(waitTime)
					continue
				}
			}
			return nil, err
		}

		s.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] SendText successful on attempt %d", instance.Id, attempt)
		return message, nil
	}

	return nil, fmt.Errorf("failed to send text after %d attempts", maxRetries)
}

// fetchLinkMetadata reads the title, description and image of a page for a link
// preview. The title is og:title, else the FIRST <title> (a later inline SVG <title>
// used to replace it), and a relative og:image is resolved against the page URL (it
// used to be fetched as-is, which failed the whole send).
func fetchLinkMetadata(pageURL string) (string, string, string, error) {
	resp, err := utils.QuickClient.Get(pageURL)
	if err != nil {
		return "", "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", "", "", fmt.Errorf("link preview: HTTP status %d", resp.StatusCode)
	}

	doc, err := html.Parse(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return "", "", "", err
	}

	title, description, imgURL := parseLinkMetadata(doc)
	return title, description, resolveImageURL(pageURL, imgURL), nil
}

// parseLinkMetadata extracts the preview fields from a parsed page.
func parseLinkMetadata(doc *html.Node) (title, description, imgURL string) {
	var docTitle, ogTitle string

	var f func(*html.Node)
	f = func(n *html.Node) {
		if n.Type == html.ElementNode {
			if n.Data == "title" && n.FirstChild != nil && docTitle == "" {
				docTitle = n.FirstChild.Data
			}
			if n.Data == "meta" {
				var property, content string
				for _, attr := range n.Attr {
					if attr.Key == "property" || attr.Key == "name" {
						property = attr.Val
					}
					if attr.Key == "content" {
						content = attr.Val
					}
				}

				switch {
				case (property == "description" || property == "og:description") && content != "":
					description = content
				case property == "og:image" && content != "":
					imgURL = content
				case property == "og:title" && content != "":
					ogTitle = content
				}
			}
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(doc)

	title = ogTitle
	if title == "" {
		title = docTitle
	}
	return title, description, imgURL
}

// resolveImageURL makes a possibly relative image URL absolute.
func resolveImageURL(pageURL, img string) string {
	img = strings.TrimSpace(img)
	if img == "" {
		return ""
	}
	base, err := url.Parse(pageURL)
	if err != nil {
		return img
	}
	ref, err := url.Parse(img)
	if err != nil {
		return img
	}
	return base.ResolveReference(ref).String()
}

// linkPreview is what SendLink puts in the message.
type linkPreview struct {
	MatchedText string
	Title       string
	Description string
	Thumbnail   []byte
}

// buildLinkPreview gathers the preview of a link message. It is best effort: a page
// that cannot be fetched, or an image that cannot be downloaded, no longer fails the
// send (a site that blocks bots or has a relative image made the message impossible
// to send); the message goes out with what could be gathered. Values the caller
// supplied win over what was scraped (they used to be overwritten, even by empty ones).
func (s *sendService) buildLinkPreview(data *LinkStruct, instanceID string) linkPreview {
	p := linkPreview{Title: data.Title, Description: data.Description}
	imgURL := data.ImgUrl

	p.MatchedText = strings.TrimSpace(data.Url)
	if p.MatchedText == "" {
		p.MatchedText = findURL(data.Text)
	}

	if p.MatchedText != "" {
		title, description, image, err := fetchLinkMetadata(p.MatchedText)
		if err != nil {
			s.loggerWrapper.GetLogger(instanceID).LogWarn("[%s] Link preview: could not read %s: %v", instanceID, p.MatchedText, err)
		} else {
			if p.Title == "" {
				p.Title = title
			}
			if p.Description == "" {
				p.Description = description
			}
			if imgURL == "" {
				imgURL = image
			}
		}
	}

	if imgURL != "" {
		thumb, err := utils.DownloadBytes(imgURL, utils.MaxThumbnailDownload)
		if err != nil {
			s.loggerWrapper.GetLogger(instanceID).LogWarn("[%s] Link preview: no thumbnail: %v", instanceID, err)
		} else {
			p.Thumbnail = thumb
		}
	}
	return p
}

func (s *sendService) SendLink(data *LinkStruct, instance *instance_model.Instance) (*MessageSendStruct, error) {
	return s.sendLinkWithRetry(data, instance, 3)
}

func (s *sendService) sendLinkWithRetry(data *LinkStruct, instance *instance_model.Instance, maxRetries int) (*MessageSendStruct, error) {
	// The page is read once, not once per connection attempt.
	preview := s.buildLinkPreview(data, instance.Id)

	for attempt := 1; attempt <= maxRetries; attempt++ {
		s.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] SendLink attempt %d/%d", instance.Id, attempt, maxRetries)

		_, err := s.ensureClientConnectedWithRetry(instance.Id, 2)
		if err != nil {
			if attempt == maxRetries {
				return nil, err
			}
			continue
		}

		previewType := waE2E.ExtendedTextMessage_VIDEO
		msg := &waE2E.Message{
			ExtendedTextMessage: &waE2E.ExtendedTextMessage{
				Text:          &data.Text,
				Title:         &preview.Title,
				MatchedText:   &preview.MatchedText,
				JPEGThumbnail: preview.Thumbnail,
				Description:   &preview.Description,
				PreviewType:   &previewType,
			},
		}

		message, err := s.SendMessage(instance, msg, "ExtendedTextMessage", &SendDataStruct{
			Id:           data.Id,
			Number:       data.Number,
			Quoted:       data.Quoted,
			Delay:        data.Delay,
			MentionAll:   data.MentionAll,
			MentionedJID: data.MentionedJID,
			FormatJid:    data.FormatJid,
		})

		if err != nil {
			// Check if it's a client disconnection error
			if isDisconnectionError(err) {
				s.loggerWrapper.GetLogger(instance.Id).LogWarn("[%s] SendLink failed due to disconnection on attempt %d/%d: %v", instance.Id, attempt, maxRetries, err)
				if attempt < maxRetries {
					waitTime := time.Duration(attempt) * time.Second
					s.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] Waiting %v before retry", instance.Id, waitTime)
					time.Sleep(waitTime)
					continue
				}
			}
			return nil, err
		}

		s.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] SendLink successful on attempt %d", instance.Id, attempt)
		return message, nil
	}

	return nil, fmt.Errorf("failed to send link after %d attempts", maxRetries)
}

type ConvertAudio struct {
	Url    string `json:"url,omitempty"`
	Base64 string `json:"base64,omitempty"`
}

type ApiResponse struct {
	Duration int    `json:"duration"`
	Audio    string `json:"audio"`
}

func convertAudioWithApi(apiUrl string, apiKey string, convertData ConvertAudio) ([]byte, int, error) {
	var requestBody bytes.Buffer
	writer := multipart.NewWriter(&requestBody)

	// Adiciona o campo "url" ao form-data se a URL for fornecida
	if convertData.Url != "" {
		err := writer.WriteField("url", convertData.Url)
		if err != nil {
			return nil, 0, fmt.Errorf("erro ao adicionar a URL no form-data: %v", err)
		}
	}

	// Adiciona o campo "base64" ao form-data se a string base64 for fornecida
	if convertData.Base64 != "" {
		err := writer.WriteField("base64", convertData.Base64)
		if err != nil {
			return nil, 0, fmt.Errorf("erro ao adicionar o base64 no form-data: %v", err)
		}
	}

	// Fecha o writer multipart
	err := writer.Close()
	if err != nil {
		return nil, 0, fmt.Errorf("erro ao finalizar o form-data: %v", err)
	}

	req, err := http.NewRequest("POST", apiUrl, &requestBody)
	if err != nil {
		return nil, 0, fmt.Errorf("erro ao criar a requisição: %v", err)
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("apikey", apiKey)

	// The converter API is configured by the operator (often a container next door), not
	// taken from a request.
	client := utils.TrustedClient
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("erro ao enviar a requisição: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("erro ao ler a resposta: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("requisição falhou com status: %d, resposta: %s", resp.StatusCode, string(body))
	}

	var apiResponse ApiResponse
	err = json.Unmarshal(body, &apiResponse)
	if err != nil {
		return nil, 0, fmt.Errorf("erro ao deserializar a resposta: %v", err)
	}

	base64ToBytes, err := base64.StdEncoding.DecodeString(apiResponse.Audio)
	if err != nil {
		return nil, 0, fmt.Errorf("erro ao decodificar o áudio: %v", err)
	}

	return base64ToBytes, apiResponse.Duration, nil
}

// ffmpegTimeout bounds one audio conversion: a corrupt or endless input used to keep
// ffmpeg (and the request waiting on it) running forever.
const ffmpegTimeout = 3 * time.Minute

// maxConvertedAudio is the most an audio conversion may produce (Opus at 128 kbit/s is
// ~1 MB a minute; WhatsApp accepts far less than this).
const maxConvertedAudio int64 = 100 << 20

func convertAudioToOpusWithDuration(inputData []byte) ([]byte, int, error) {
	// A slot of the shared pool, a time limit, and a cap on what ffmpeg may write (-fs for
	// the program itself, maxConvertedAudio for the buffer): ten concurrent audio sends used
	// to be ten ffmpeg processes with no limit on their output.
	outBytes, errBytes, err := utils.RunLimited(context.Background(), ffmpegTimeout, "ffmpeg", []string{"-i", "pipe:0",
		"-f", "ogg",
		"-vn",
		"-c:a", "libopus",
		"-avoid_negative_ts", "make_zero",
		"-b:a", "128k",
		"-ar", "48000",
		"-ac", "1",
		"-write_xing", "0",
		"-compression_level", "10",
		"-application", "voip",
		"-fflags", "+bitexact",
		"-flags", "+bitexact",
		"-id3v2_version", "0",
		"-map_metadata", "-1",
		"-map_chapters", "-1",
		"-write_bext", "0",
		"-fs", strconv.FormatInt(maxConvertedAudio, 10),
		"pipe:1",
	}, inputData, maxConvertedAudio)
	if err != nil {
		if errors.Is(err, utils.ErrBusy) || errors.Is(err, utils.ErrOutputTooLarge) || strings.Contains(err.Error(), "timed out") {
			return nil, 0, fmt.Errorf("audio conversion failed: %w", err)
		}
		return nil, 0, fmt.Errorf("error during conversion: %v, details: %s", err, errBytes)
	}

	convertedData := outBytes
	outputText := string(errBytes)

	splitTime := strings.Split(outputText, "time=")

	if len(splitTime) < 2 {
		return nil, 0, errors.New("duração não encontrada")
	}

	// Use the last occurrence of time= in case there are multiple
	timeString := splitTime[len(splitTime)-1]

	re := regexp.MustCompile(`(\d+):(\d+):(\d+\.\d+)`)
	matches := re.FindStringSubmatch(timeString)
	if len(matches) != 4 {
		return nil, 0, errors.New("formato de duração não encontrado")
	}

	hours, _ := strconv.ParseFloat(matches[1], 64)
	minutes, _ := strconv.ParseFloat(matches[2], 64)
	seconds, _ := strconv.ParseFloat(matches[3], 64)
	duration := int(hours*3600 + minutes*60 + seconds)

	return convertedData, duration, nil
}

// mediaPrep is a media file checked, and converted when WhatsApp needs another format,
// ready to be uploaded.
type mediaPrep struct {
	fileData   []byte
	mimeType   string
	uploadType whatsmeow.MediaType
	duration   int // seconds, for audio
}

// prepareMediaFile validates the type of an uploaded file against what the caller said it
// is and converts audio to Opus. It does not touch the connection.
func (s *sendService) prepareMediaFile(data *MediaStruct, fileData []byte) (*mediaPrep, error) {
	mime, _ := mimetype.DetectReader(bytes.NewReader(fileData))
	mimeType := mime.String()

	var uploadType whatsmeow.MediaType
	var duration int

	switch data.Type {
	case "image":
		if mimeType != "image/jpeg" && mimeType != "image/png" && mimeType != "image/webp" {
			errMsg := fmt.Sprintf("Invalid file format: '%s'. Only 'image/jpeg', 'image/png' and 'image/webp' are accepted", mimeType)
			return nil, errors.New(errMsg)
		}
		if mimeType == "image/webp" {
			mimeType = "image/jpeg"
		}
		uploadType = whatsmeow.MediaImage
	case "video":
		if mimeType != "video/mp4" {
			errMsg := fmt.Sprintf("Invalid file format: '%s'. Only 'video/mp4' is accepted", mimeType)
			return nil, errors.New(errMsg)
		}
		uploadType = whatsmeow.MediaVideo
	case "audio":
		converterApiUrl := s.config.ApiAudioConverter
		converterApiKey := s.config.ApiAudioConverterKey
		var convertedData []byte
		var err error
		if converterApiUrl == "" {

			convertedData, duration, err = convertAudioToOpusWithDuration(fileData)
			if err != nil {
				return nil, err
			}
		} else {
			convertedData, duration, err = convertAudioWithApi(converterApiUrl, converterApiKey, ConvertAudio{Base64: base64.StdEncoding.EncodeToString(fileData)})
			if err != nil {
				return nil, err
			}
		}

		fileData = convertedData
		mimeType = "audio/ogg; codecs=opus"
		uploadType = whatsmeow.MediaAudio
	case "document":
		uploadType = whatsmeow.MediaDocument
	default:
		return nil, apierror.Invalid("invalid media type")
	}
	return &mediaPrep{fileData: fileData, mimeType: mimeType, uploadType: uploadType, duration: duration}, nil
}

// prepareMediaURL downloads the file of a URL and prepares it like prepareMediaFile.
func (s *sendService) prepareMediaURL(data *MediaStruct, instanceID string, startTime time.Time) (*mediaPrep, error) {
	instance := &instance_model.Instance{Id: instanceID}
	s.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] Iniciando download da URL: %s", instance.Id, data.Url)

	// An error page is not the file (a 404 used to be sent as a document), and the
	// size is bounded.
	fileData, err := utils.DownloadBytes(data.Url, utils.MaxMediaDownload)
	if err != nil {
		return nil, err
	}
	s.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] Download concluído em %v. Tamanho: %d bytes", instance.Id, time.Since(startTime), len(fileData))

	mime, _ := mimetype.DetectReader(bytes.NewReader(fileData))
	mimeType := mime.String()
	if strings.HasSuffix(strings.ToLower(data.Url), ".mp4") {
		mimeType = "video/mp4"
	}

	s.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] Tipo MIME detectado: %s", instance.Id, mimeType)

	var uploadType whatsmeow.MediaType
	var duration int

	processingStart := time.Now()
	switch data.Type {
	case "image":
		if mimeType != "image/jpeg" && mimeType != "image/png" && mimeType != "image/webp" {
			errMsg := fmt.Sprintf("Invalid file format: '%s'. Only 'image/jpeg', 'image/png' and 'image/webp' are accepted", mimeType)
			return nil, errors.New(errMsg)
		}
		if mimeType == "image/webp" {
			mimeType = "image/jpeg"
		}
		uploadType = whatsmeow.MediaImage

	case "video", "ptv":
		if mimeType != "video/mp4" {
			errMsg := fmt.Sprintf("Invalid file format: '%s'. Only 'video/mp4' are accepted", mimeType)
			return nil, errors.New(errMsg)
		}
		uploadType = whatsmeow.MediaVideo
	case "audio":
		s.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] Iniciando conversão de áudio...", instance.Id)
		converterApiUrl := s.config.ApiAudioConverter
		converterApiKey := s.config.ApiAudioConverterKey
		var convertedData []byte
		var err error
		if converterApiUrl == "" {
			s.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] Usando conversão local...", instance.Id)
			convertedData, duration, err = convertAudioToOpusWithDuration(fileData)
		} else {
			s.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] Usando API de conversão...", instance.Id)
			convertedData, duration, err = convertAudioWithApi(converterApiUrl, converterApiKey, ConvertAudio{Base64: base64.StdEncoding.EncodeToString(fileData)})
		}
		if err != nil {
			return nil, err
		}
		fileData = convertedData
		mimeType = "audio/ogg; codecs=opus"
		uploadType = whatsmeow.MediaAudio
		s.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] Conversão de áudio concluída em %v", instance.Id, time.Since(processingStart))
	case "document":
		uploadType = whatsmeow.MediaDocument
	default:
		return nil, apierror.Invalid("invalid media type")
	}
	return &mediaPrep{fileData: fileData, mimeType: mimeType, uploadType: uploadType, duration: duration}, nil
}

func (s *sendService) SendMediaFile(data *MediaStruct, fileData []byte, instance *instance_model.Instance) (*MessageSendStruct, error) {
	return s.sendMediaFileWithRetry(data, fileData, instance, 3)
}

func (s *sendService) sendMediaFileWithRetry(data *MediaStruct, fileData []byte, instance *instance_model.Instance, maxRetries int) (*MessageSendStruct, error) {
	// The file is checked and converted once: a retry after a disconnection used to run
	// the conversion again over the already converted audio.
	var prep *mediaPrep
	for attempt := 1; attempt <= maxRetries; attempt++ {
		s.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] SendMediaFile attempt %d/%d", instance.Id, attempt, maxRetries)

		client, err := s.ensureClientConnectedWithRetry(instance.Id, 2)
		if err != nil {
			if attempt == maxRetries {
				return nil, err
			}
			continue
		}

		if prep == nil {
			p, err := s.prepareMediaFile(data, fileData)
			if err != nil {
				return nil, err
			}
			prep = p
		}
		fileData, mimeType, uploadType, duration := prep.fileData, prep.mimeType, prep.uploadType, prep.duration

		// Detectar se é newsletter para usar upload sem criptografia
		isNewsletter := strings.Contains(data.Number, "@newsletter")

		// Validar se é documento em newsletter (não suportado)
		if isNewsletter && data.Type == "document" {
			return nil, errors.New("documentos não são suportados em canais do WhatsApp. Use imagem, vídeo, áudio ou enquete")
		}

		s.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] SendMediaFile - Upload iniciado (Newsletter: %v)...", instance.Id, isNewsletter)

		var uploaded whatsmeow.UploadResponse
		if isNewsletter {
			// Newsletter: upload SEM criptografia
			uploaded, err = client.UploadNewsletter(context.Background(), fileData, uploadType)
			s.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] Newsletter upload - Handle: %s", instance.Id, uploaded.Handle)
		} else {
			// Normal: upload COM criptografia
			uploaded, err = client.Upload(context.Background(), fileData, uploadType)
		}

		if err != nil {
			return nil, err
		}

		s.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] Media uploaded with size %d", instance.Id, uploaded.FileLength)

		var media *waE2E.Message
		var mediaType string

		switch data.Type {
		case "image":
			// Generate a JPEG preview thumbnail for better client UX (iOS in
			// particular). On failure jpegThumb is nil and the message is sent
			// without a preview rather than failing the request.
			jpegThumb := makeJPEGThumbnail(fileData, 72)
			// Width/Height let the client size the bubble before the media downloads (#104).
			imgW, imgH := imageDimensions(fileData)
			if isNewsletter {
				// Newsletter: SEM MediaKey e FileEncSHA256
				media = &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
					Caption:       proto.String(data.Caption),
					URL:           &uploaded.URL,
					DirectPath:    &uploaded.DirectPath,
					Mimetype:      proto.String(mimeType),
					FileSHA256:    uploaded.FileSHA256,
					FileLength:    &uploaded.FileLength,
					JPEGThumbnail: jpegThumb,
					Width:         imgW,
					Height:        imgH,
				}}
			} else {
				// Normal: COM MediaKey e FileEncSHA256
				media = &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
					Caption:       proto.String(data.Caption),
					URL:           proto.String(uploaded.URL),
					DirectPath:    proto.String(uploaded.DirectPath),
					MediaKey:      uploaded.MediaKey,
					Mimetype:      proto.String(mimeType),
					FileEncSHA256: uploaded.FileEncSHA256,
					FileSHA256:    uploaded.FileSHA256,
					FileLength:    proto.Uint64(uint64(len(fileData))),
					JPEGThumbnail: jpegThumb,
					Width:         imgW,
					Height:        imgH,
				}}
			}
			mediaType = "ImageMessage"
		case "video":
			if isNewsletter {
				media = &waE2E.Message{VideoMessage: &waE2E.VideoMessage{
					Caption:    proto.String(data.Caption),
					URL:        &uploaded.URL,
					DirectPath: &uploaded.DirectPath,
					Mimetype:   proto.String(mimeType),
					FileSHA256: uploaded.FileSHA256,
					FileLength: &uploaded.FileLength,
				}}
			} else {
				media = &waE2E.Message{VideoMessage: &waE2E.VideoMessage{
					Caption:       proto.String(data.Caption),
					URL:           proto.String(uploaded.URL),
					DirectPath:    proto.String(uploaded.DirectPath),
					MediaKey:      uploaded.MediaKey,
					Mimetype:      proto.String(mimeType),
					FileEncSHA256: uploaded.FileEncSHA256,
					FileSHA256:    uploaded.FileSHA256,
					FileLength:    proto.Uint64(uint64(len(fileData))),
				}}
			}
			mediaType = "VideoMessage"
		case "ptv":
			if isNewsletter {
				media = &waE2E.Message{PtvMessage: &waE2E.VideoMessage{
					URL:        &uploaded.URL,
					DirectPath: &uploaded.DirectPath,
					Mimetype:   proto.String(mimeType),
					FileSHA256: uploaded.FileSHA256,
					FileLength: &uploaded.FileLength,
				}}
			} else {
				media = &waE2E.Message{PtvMessage: &waE2E.VideoMessage{
					URL:           proto.String(uploaded.URL),
					DirectPath:    proto.String(uploaded.DirectPath),
					MediaKey:      uploaded.MediaKey,
					Mimetype:      proto.String(mimeType),
					FileEncSHA256: uploaded.FileEncSHA256,
					FileSHA256:    uploaded.FileSHA256,
					FileLength:    proto.Uint64(uint64(len(fileData))),
				}}
			}
			mediaType = "PtvMessage"
		case "audio":
			if isNewsletter {
				media = &waE2E.Message{AudioMessage: &waE2E.AudioMessage{
					URL:        &uploaded.URL,
					PTT:        proto.Bool(true),
					DirectPath: &uploaded.DirectPath,
					Mimetype:   proto.String(mimeType),
					FileSHA256: uploaded.FileSHA256,
					FileLength: &uploaded.FileLength,
					Seconds:    proto.Uint32(uint32(duration)),
				}}
			} else {
				media = &waE2E.Message{AudioMessage: &waE2E.AudioMessage{
					URL:           proto.String(uploaded.URL),
					PTT:           proto.Bool(true),
					DirectPath:    proto.String(uploaded.DirectPath),
					MediaKey:      uploaded.MediaKey,
					Mimetype:      proto.String(mimeType),
					FileEncSHA256: uploaded.FileEncSHA256,
					FileSHA256:    uploaded.FileSHA256,
					FileLength:    proto.Uint64(uploaded.FileLength),
					Seconds:       proto.Uint32(uint32(duration)),
				}}
			}
			mediaType = "AudioMessage"
		case "document":
			// For PDF documents, rasterize page 1 into a JPEG preview thumbnail.
			// A missing pdftoppm or a failure yields nil and the document is
			// sent without a preview instead of failing the request.
			var jpegThumb []byte
			if mimeType == "application/pdf" {
				jpegThumb = makePDFThumbnail(fileData, 200)
			}
			if isNewsletter {
				media = &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{
					FileName:      &data.Filename,
					Caption:       proto.String(data.Caption),
					URL:           &uploaded.URL,
					DirectPath:    &uploaded.DirectPath,
					Mimetype:      proto.String(mimeType),
					FileSHA256:    uploaded.FileSHA256,
					FileLength:    &uploaded.FileLength,
					JPEGThumbnail: jpegThumb,
				}}
			} else {
				media = &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{
					FileName:      &data.Filename,
					Caption:       proto.String(data.Caption),
					URL:           proto.String(uploaded.URL),
					DirectPath:    proto.String(uploaded.DirectPath),
					MediaKey:      uploaded.MediaKey,
					Mimetype:      proto.String(mimeType),
					FileEncSHA256: uploaded.FileEncSHA256,
					FileSHA256:    uploaded.FileSHA256,
					FileLength:    proto.Uint64(uint64(len(fileData))),
					JPEGThumbnail: jpegThumb,
				}}
			}

			if media.GetDocumentMessage().GetCaption() != "" {
				media.DocumentWithCaptionMessage = &waE2E.FutureProofMessage{
					Message: &waE2E.Message{
						DocumentMessage: media.DocumentMessage,
					},
				}
				media.DocumentMessage = nil
			}

			mediaType = "DocumentMessage"
		default:
			return nil, apierror.Invalid("invalid media type")
		}

		applyViewOnce(media, data.ViewOnce)

		message, err := s.SendMessage(instance, media, mediaType, &SendDataStruct{
			Id:              data.Id,
			Number:          data.Number,
			Quoted:          data.Quoted,
			Delay:           data.Delay,
			MentionAll:      data.MentionAll,
			MentionedJID:    data.MentionedJID,
			FormatJid:       data.FormatJid,
			MediaHandle:     uploaded.Handle,
			ForwardingScore: data.ForwardingScore,
			MediaBytes:      fileData,
		})

		if err != nil {
			// Check if it's a client disconnection error
			if isDisconnectionError(err) {
				s.loggerWrapper.GetLogger(instance.Id).LogWarn("[%s] SendMediaFile failed due to disconnection on attempt %d/%d: %v", instance.Id, attempt, maxRetries, err)
				if attempt < maxRetries {
					waitTime := time.Duration(attempt) * time.Second
					s.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] Waiting %v before retry", instance.Id, waitTime)
					time.Sleep(waitTime)
					continue
				}
			}
			return nil, err
		}

		s.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] SendMediaFile successful on attempt %d", instance.Id, attempt)
		return message, nil
	}

	return nil, fmt.Errorf("failed to send media file after %d attempts", maxRetries)
}

func (s *sendService) SendMediaUrl(data *MediaStruct, instance *instance_model.Instance) (*MessageSendStruct, error) {
	return s.sendMediaUrlWithRetry(data, instance, 3)
}

func (s *sendService) sendMediaUrlWithRetry(data *MediaStruct, instance *instance_model.Instance, maxRetries int) (*MessageSendStruct, error) {
	// The download and the conversion happen once: a retry after a disconnection used to
	// download the whole file again (up to 100 MB) and convert it again.
	var prep *mediaPrep
	for attempt := 1; attempt <= maxRetries; attempt++ {
		s.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] SendMediaUrl attempt %d/%d for URL: %s", instance.Id, attempt, maxRetries, data.Url)
		startTime := time.Now()

		client, err := s.ensureClientConnectedWithRetry(instance.Id, 2)
		if err != nil {
			if attempt == maxRetries {
				return nil, err
			}
			continue
		}

		if prep == nil {
			p, err := s.prepareMediaURL(data, instance.Id, startTime)
			if err != nil {
				return nil, err
			}
			prep = p
		}
		fileData, mimeType, uploadType, duration := prep.fileData, prep.mimeType, prep.uploadType, prep.duration

		// Detectar se é newsletter para usar upload sem criptografia
		isNewsletter := strings.Contains(data.Number, "@newsletter")

		// Validar se é documento em newsletter (não suportado)
		if isNewsletter && data.Type == "document" {
			return nil, errors.New("documentos não são suportados em canais do WhatsApp. Use imagem, vídeo, áudio ou enquete")
		}

		s.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] Iniciando upload para WhatsApp (Newsletter: %v)...", instance.Id, isNewsletter)
		uploadStart := time.Now()

		var uploaded whatsmeow.UploadResponse
		if isNewsletter {
			// Newsletter: upload sem criptografia
			uploaded, err = client.UploadNewsletter(context.Background(), fileData, uploadType)
			s.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] Newsletter upload - Handle: %s", instance.Id, uploaded.Handle)
		} else {
			// Upload normal com criptografia
			uploaded, err = client.Upload(context.Background(), fileData, uploadType)
		}

		if err != nil {
			return nil, err
		}
		s.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] Upload concluído em %v. Tamanho: %d", instance.Id, time.Since(uploadStart), uploaded.FileLength)

		var media *waE2E.Message
		var mediaType string

		switch data.Type {
		case "image":
			// Generate a JPEG preview thumbnail for better client UX (iOS in
			// particular). On failure jpegThumb is nil and the message is sent
			// without a preview rather than failing the request.
			jpegThumb := makeJPEGThumbnail(fileData, 72)
			// Width/Height let the client size the bubble before the media downloads (#104).
			imgW, imgH := imageDimensions(fileData)
			if isNewsletter {
				// Newsletter: sem criptografia (sem MediaKey e FileEncSHA256)
				media = &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
					Caption:       proto.String(data.Caption),
					URL:           &uploaded.URL,
					DirectPath:    &uploaded.DirectPath,
					Mimetype:      proto.String(mimeType),
					FileSHA256:    uploaded.FileSHA256,
					FileLength:    &uploaded.FileLength,
					JPEGThumbnail: jpegThumb,
					Width:         imgW,
					Height:        imgH,
				}}
			} else {
				// Normal: com criptografia
				media = &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
					Caption:       proto.String(data.Caption),
					URL:           proto.String(uploaded.URL),
					DirectPath:    proto.String(uploaded.DirectPath),
					MediaKey:      uploaded.MediaKey,
					Mimetype:      proto.String(mimeType),
					FileEncSHA256: uploaded.FileEncSHA256,
					FileSHA256:    uploaded.FileSHA256,
					FileLength:    proto.Uint64(uint64(len(fileData))),
					JPEGThumbnail: jpegThumb,
					Width:         imgW,
					Height:        imgH,
				}}
			}
			mediaType = "ImageMessage"
		case "video":
			if isNewsletter {
				media = &waE2E.Message{VideoMessage: &waE2E.VideoMessage{
					Caption:    proto.String(data.Caption),
					URL:        &uploaded.URL,
					DirectPath: &uploaded.DirectPath,
					Mimetype:   proto.String(mimeType),
					FileSHA256: uploaded.FileSHA256,
					FileLength: &uploaded.FileLength,
				}}
			} else {
				media = &waE2E.Message{VideoMessage: &waE2E.VideoMessage{
					Caption:       proto.String(data.Caption),
					URL:           proto.String(uploaded.URL),
					DirectPath:    proto.String(uploaded.DirectPath),
					MediaKey:      uploaded.MediaKey,
					Mimetype:      proto.String(mimeType),
					FileEncSHA256: uploaded.FileEncSHA256,
					FileSHA256:    uploaded.FileSHA256,
					FileLength:    proto.Uint64(uint64(len(fileData))),
				}}
			}
			mediaType = "VideoMessage"
		case "ptv":
			if isNewsletter {
				media = &waE2E.Message{PtvMessage: &waE2E.VideoMessage{
					URL:        &uploaded.URL,
					DirectPath: &uploaded.DirectPath,
					Mimetype:   proto.String(mimeType),
					FileSHA256: uploaded.FileSHA256,
					FileLength: &uploaded.FileLength,
				}}
			} else {
				media = &waE2E.Message{PtvMessage: &waE2E.VideoMessage{
					URL:           proto.String(uploaded.URL),
					DirectPath:    proto.String(uploaded.DirectPath),
					MediaKey:      uploaded.MediaKey,
					Mimetype:      proto.String(mimeType),
					FileEncSHA256: uploaded.FileEncSHA256,
					FileSHA256:    uploaded.FileSHA256,
					FileLength:    proto.Uint64(uint64(len(fileData))),
				}}
			}
			mediaType = "PtvMessage"
		case "audio":
			if isNewsletter {
				media = &waE2E.Message{AudioMessage: &waE2E.AudioMessage{
					URL:              &uploaded.URL,
					PTT:              proto.Bool(true),
					DirectPath:       &uploaded.DirectPath,
					Mimetype:         proto.String(mimeType),
					FileSHA256:       uploaded.FileSHA256,
					FileLength:       &uploaded.FileLength,
					StreamingSidecar: []byte(*proto.String("QpmXDsU7YLagdg==")),
					Waveform:         []byte(*proto.String("OjAnExISDgsKCAkJBwgkHAQEBBEFAwMNAxAcKCgkFzM0QUE4Jh4eKAoKChcLCwkeFgkJCQo3JiQmIiIRPz8/Ow==")),
					Seconds:          proto.Uint32(uint32(duration)),
				}}
			} else {
				media = &waE2E.Message{AudioMessage: &waE2E.AudioMessage{
					URL:              proto.String(uploaded.URL),
					PTT:              proto.Bool(true),
					DirectPath:       proto.String(uploaded.DirectPath),
					MediaKey:         uploaded.MediaKey,
					Mimetype:         proto.String(mimeType),
					FileEncSHA256:    uploaded.FileEncSHA256,
					FileSHA256:       uploaded.FileSHA256,
					FileLength:       proto.Uint64(uploaded.FileLength),
					StreamingSidecar: []byte(*proto.String("QpmXDsU7YLagdg==")),
					Waveform:         []byte(*proto.String("OjAnExISDgsKCAkJBwgkHAQEBBEFAwMNAxAcKCgkFzM0QUE4Jh4eKAoKChcLCwkeFgkJCQo3JiQmIiIRPz8/Ow==")),
					Seconds:          proto.Uint32(uint32(duration)),
				}}
			}
			mediaType = "AudioMessage"
		case "document":
			// For PDF documents, rasterize page 1 into a JPEG preview thumbnail.
			// A missing pdftoppm or a failure yields nil and the document is
			// sent without a preview instead of failing the request.
			var jpegThumb []byte
			if mimeType == "application/pdf" {
				jpegThumb = makePDFThumbnail(fileData, 200)
			}
			if isNewsletter {
				media = &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{
					URL:           &uploaded.URL,
					FileName:      &data.Filename,
					Caption:       proto.String(data.Caption),
					DirectPath:    &uploaded.DirectPath,
					Mimetype:      proto.String(mimeType),
					FileSHA256:    uploaded.FileSHA256,
					FileLength:    &uploaded.FileLength,
					JPEGThumbnail: jpegThumb,
				}}
			} else {
				media = &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{
					URL:           proto.String(uploaded.URL),
					FileName:      &data.Filename,
					Caption:       proto.String(data.Caption),
					DirectPath:    proto.String(uploaded.DirectPath),
					MediaKey:      uploaded.MediaKey,
					Mimetype:      proto.String(mimeType),
					FileEncSHA256: uploaded.FileEncSHA256,
					FileSHA256:    uploaded.FileSHA256,
					FileLength:    proto.Uint64(uint64(len(fileData))),
					JPEGThumbnail: jpegThumb,
				}}
			}

			if media.GetDocumentMessage().GetCaption() != "" {
				media.DocumentWithCaptionMessage = &waE2E.FutureProofMessage{
					Message: &waE2E.Message{
						DocumentMessage: media.DocumentMessage,
					},
				}
				media.DocumentMessage = nil
			}

			mediaType = "DocumentMessage"
		default:
			return nil, apierror.Invalid("invalid media type")
		}

		applyViewOnce(media, data.ViewOnce)

		messageStart := time.Now()
		message, err := s.SendMessage(instance, media, mediaType, &SendDataStruct{
			Id:              data.Id,
			Number:          data.Number,
			Quoted:          data.Quoted,
			Delay:           data.Delay,
			MentionAll:      data.MentionAll,
			MentionedJID:    data.MentionedJID,
			FormatJid:       data.FormatJid,
			MediaHandle:     uploaded.Handle,
			ForwardingScore: data.ForwardingScore,
			MediaBytes:      fileData,
		})

		if err != nil {
			// Check if it's a client disconnection error
			if isDisconnectionError(err) {
				s.loggerWrapper.GetLogger(instance.Id).LogWarn("[%s] SendMediaUrl failed due to disconnection on attempt %d/%d: %v", instance.Id, attempt, maxRetries, err)
				if attempt < maxRetries {
					waitTime := time.Duration(attempt) * time.Second
					s.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] Waiting %v before retry", instance.Id, waitTime)
					time.Sleep(waitTime)
					continue
				}
			}
			return nil, err
		}

		s.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] Mensagem enviada em %v", instance.Id, time.Since(messageStart))

		totalTime := time.Since(startTime)
		s.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] SendMediaUrl successful on attempt %d, processo completo em %v", instance.Id, attempt, totalTime)

		return message, nil
	}

	return nil, fmt.Errorf("failed to send media url after %d attempts", maxRetries)
}

func (s *sendService) SendPoll(data *PollStruct, instance *instance_model.Instance) (*MessageSendStruct, error) {
	return s.sendPollWithRetry(data, instance, 3)
}

func (s *sendService) sendPollWithRetry(data *PollStruct, instance *instance_model.Instance, maxRetries int) (*MessageSendStruct, error) {
	for attempt := 1; attempt <= maxRetries; attempt++ {
		s.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] SendPoll attempt %d/%d", instance.Id, attempt, maxRetries)

		client, err := s.ensureClientConnectedWithRetry(instance.Id, 2)
		if err != nil {
			if attempt == maxRetries {
				return nil, err
			}
			continue
		}

		msg := client.BuildPollCreation(data.Question, data.Options, data.MaxAnswer)

		message, err := s.SendMessage(instance, msg, "PollCreationMessage", &SendDataStruct{
			Id:           data.Id,
			Number:       data.Number,
			Quoted:       data.Quoted,
			Delay:        data.Delay,
			MentionAll:   data.MentionAll,
			MentionedJID: data.MentionedJID,
			FormatJid:    data.FormatJid,
		})

		if err != nil {
			// Check if it's a client disconnection error
			if isDisconnectionError(err) {
				s.loggerWrapper.GetLogger(instance.Id).LogWarn("[%s] SendPoll failed due to disconnection on attempt %d/%d: %v", instance.Id, attempt, maxRetries, err)
				if attempt < maxRetries {
					waitTime := time.Duration(attempt) * time.Second
					s.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] Waiting %v before retry", instance.Id, waitTime)
					time.Sleep(waitTime)
					continue
				}
			}
			return nil, err
		}

		s.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] SendPoll successful on attempt %d", instance.Id, attempt)
		return message, nil
	}

	return nil, fmt.Errorf("failed to send poll after %d attempts", maxRetries)
}

func (s *sendService) SendSticker(data *StickerStruct, instance *instance_model.Instance) (*MessageSendStruct, error) {
	client, err := s.ensureClientConnected(instance.Id)
	if err != nil {
		return nil, err
	}

	var uploaded whatsmeow.UploadResponse
	var filedata []byte

	if strings.HasPrefix(data.Sticker, "http") {
		webpData, err := stickerWebP(context.Background(), data.Sticker)
		if err != nil {
			return nil, fmt.Errorf("failed to prepare sticker payload: %v", err)
		}

		filedata = webpData

		uploaded, err = client.Upload(context.Background(), filedata, whatsmeow.MediaImage)
		if err != nil {
			return nil, fmt.Errorf("failed to upload sticker: %v", err)
		}
	} else {
		return nil, apierror.Invalid("invalid sticker URL")
	}

	msg := &waE2E.Message{StickerMessage: &waE2E.StickerMessage{
		URL:           proto.String(uploaded.URL),
		DirectPath:    proto.String(uploaded.DirectPath),
		MediaKey:      uploaded.MediaKey,
		Mimetype:      proto.String(http.DetectContentType(filedata)),
		FileEncSHA256: uploaded.FileEncSHA256,
		FileSHA256:    uploaded.FileSHA256,
		FileLength:    proto.Uint64(uint64(len(filedata))),
		IsAnimated:    proto.Bool(webpIsAnimated(filedata)),
	}}

	message, err := s.SendMessage(instance, msg, "StickerMessage", &SendDataStruct{
		Id:           data.Id,
		Number:       data.Number,
		Quoted:       data.Quoted,
		Delay:        data.Delay,
		MentionAll:   data.MentionAll,
		MentionedJID: data.MentionedJID,
		FormatJid:    data.FormatJid,
		MediaBytes:   filedata,
	})
	if err != nil {
		return nil, err
	}

	return message, nil
}

func (s *sendService) SendLocation(data *LocationStruct, instance *instance_model.Instance) (*MessageSendStruct, error) {
	_, err := s.ensureClientConnected(instance.Id)
	if err != nil {
		return nil, err
	}

	msg := &waE2E.Message{LocationMessage: &waE2E.LocationMessage{
		DegreesLatitude:  &data.Latitude,
		DegreesLongitude: &data.Longitude,
		Name:             &data.Name,
		Address:          &data.Address,
	}}

	message, err := s.SendMessage(instance, msg, "LocationMessage", &SendDataStruct{
		Id:           data.Id,
		Number:       data.Number,
		Quoted:       data.Quoted,
		Delay:        data.Delay,
		MentionAll:   data.MentionAll,
		MentionedJID: data.MentionedJID,
		FormatJid:    data.FormatJid,
	})
	if err != nil {
		return nil, err
	}

	return message, nil
}

func (s *sendService) SendContact(data *ContactStruct, instance *instance_model.Instance) (*MessageSendStruct, error) {
	_, err := s.ensureClientConnected(instance.Id)
	if err != nil {
		return nil, err
	}

	VCstring := utils.GenerateVC(utils.VCardStruct{
		FullName:     data.Vcard.FullName,
		Phone:        data.Vcard.Phone,
		Organization: data.Vcard.Organization,
	})

	msg := &waE2E.Message{ContactMessage: &waE2E.ContactMessage{
		DisplayName: &data.Vcard.FullName,
		Vcard:       &VCstring,
	}}

	messaged, err := s.SendMessage(instance, msg, "ContactMessage", &SendDataStruct{
		Id:           data.Id,
		Number:       data.Number,
		Quoted:       data.Quoted,
		Delay:        data.Delay,
		MentionAll:   data.MentionAll,
		MentionedJID: data.MentionedJID,
		FormatJid:    data.FormatJid,
	})
	if err != nil {
		return nil, err
	}

	return messaged, nil
}

func mapKeyType(keyType string) string {
	switch keyType {
	case "phone":
		return "PHONE"
	case "email":
		return "EMAIL"
	case "cpf":
		return "CPF"
	case "cnpj":
		return "CNPJ"
	case "random":
		return "EVP"
	default:
		return keyType
	}
}

func (s *sendService) SendButton(data *ButtonStruct, instance *instance_model.Instance) (*MessageSendStruct, error) {
	client, err := s.ensureClientConnected(instance.Id)
	if err != nil {
		return nil, err
	}

	hasPix := false
	replyCount := 0

	for _, v := range data.Buttons {
		switch v.Type {
		case "reply":
			replyCount++
		case "pix":
			hasPix = true
		}
	}

	if replyCount > 3 {
		return nil, apierror.Invalid("máximo de 3 botões do tipo 'reply' permitidos")
	}

	if hasPix {
		if len(data.Buttons) > 1 {
			return nil, apierror.Invalid("botão do tipo 'pix' não pode ser combinado com outros botões")
		}
	}

	buttons := []*waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton{}

	for _, v := range data.Buttons {
		var paramsJSON *string
		var name *string

		switch v.Type {
		case "reply":
			name = proto.String("quick_reply")
			jsonBytes, _ := json.Marshal(map[string]string{"display_text": v.DisplayText, "id": v.Id})
			paramsJSON = proto.String(string(jsonBytes))
		case "copy":
			name = proto.String("cta_copy")
			copyCode := v.CopyCode
			if copyCode == "" {
				copyCode = v.Id
			}
			copyId := v.Id
			if copyId == "" {
				copyId = "copy_" + strconv.FormatInt(time.Now().UnixNano(), 10)
			}
			jsonBytes, _ := json.Marshal(map[string]string{"display_text": v.DisplayText, "id": copyId, "copy_code": copyCode})
			paramsJSON = proto.String(string(jsonBytes))
		case "url":
			name = proto.String("cta_url")
			jsonBytes, _ := json.Marshal(map[string]string{"display_text": v.DisplayText, "url": v.URL, "merchant_url": v.URL})
			paramsJSON = proto.String(string(jsonBytes))
		case "call":
			name = proto.String("cta_call")
			jsonBytes, _ := json.Marshal(map[string]string{"display_text": v.DisplayText, "phone_number": v.PhoneNumber})
			paramsJSON = proto.String(string(jsonBytes))
		case "pix":
			randomId := utils.GenerateRandomString(11)
			name = proto.String("payment_info")
			paymentPayload := map[string]interface{}{
				"currency":     v.Currency,
				"total_amount": map[string]interface{}{"value": 0, "offset": 100},
				"reference_id": randomId,
				"type":         "physical-goods",
				"order": map[string]interface{}{
					"status":     "pending",
					"subtotal":   map[string]interface{}{"value": 0, "offset": 100},
					"order_type": "ORDER",
					"items": []map[string]interface{}{
						{
							"name":        "",
							"amount":      map[string]interface{}{"value": 0, "offset": 100},
							"quantity":    0,
							"sale_amount": map[string]interface{}{"value": 0, "offset": 100},
						},
					},
				},
				"payment_settings": []map[string]interface{}{
					{
						"type": "pix_static_code",
						"pix_static_code": map[string]string{
							"merchant_name": v.Name,
							"key":           v.Key,
							"key_type":      mapKeyType(v.KeyType),
						},
					},
				},
				"share_payment_status": false,
			}
			jsonBytes, _ := json.Marshal(paymentPayload)
			paramsJSON = proto.String(string(jsonBytes))
		}

		buttons = append(buttons, &waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton{
			Name:             name,
			ButtonParamsJSON: paramsJSON,
		})
	}

	var msg *waE2E.Message
	var msgType string
	var bizNodes []waBinary.Node

	if hasPix {
		// Pix: NativeFlowMessage wrapped in DocumentWithCaptionMessage, announced as payment_info.
		btnMsgSecret := make([]byte, 32)
		_, _ = crypto_rand.Read(btnMsgSecret)
		paymentMsgParams := `{"native_flow_name":"order_details","version":1}`

		var interactiveBody *waE2E.InteractiveMessage_Body
		if data.Title != "" {
			bodyText := data.Title
			interactiveBody = &waE2E.InteractiveMessage_Body{Text: &bodyText}
		}

		msg = &waE2E.Message{
			DocumentWithCaptionMessage: &waE2E.FutureProofMessage{
				Message: &waE2E.Message{
					InteractiveMessage: &waE2E.InteractiveMessage{
						Body: interactiveBody,
						InteractiveMessage: &waE2E.InteractiveMessage_NativeFlowMessage_{
							NativeFlowMessage: &waE2E.InteractiveMessage_NativeFlowMessage{
								Buttons:           buttons,
								MessageParamsJSON: &paymentMsgParams,
								MessageVersion:    proto.Int32(1),
							},
						},
					},
				},
			},
			MessageContextInfo: &waE2E.MessageContextInfo{
				MessageSecret: btnMsgSecret,
			},
		}
		msgType = "InteractiveMessage"
		bizNodes = nativeFlowBizNodes("payment_info", "1", data.Number)
	} else {
		// Reply and CTA buttons (copy/url/call): a plain InteractiveMessage with a native flow,
		// announced as <native_flow v="9" name="mixed"/>. Checked on a WhatsApp Business account,
		// on the phone and on WhatsApp Web. The legacy ButtonsMessage is refused by the server
		// (405), and the same native flow wrapped in DocumentWithCaptionMessage, with another
		// native_flow name or without v="9" is refused (405/473) or never rendered.
		body := data.Description
		if data.Title != "" {
			body = "*" + data.Title + "*\n\n" + data.Description
		}

		interactive := &waE2E.InteractiveMessage{
			Body: &waE2E.InteractiveMessage_Body{Text: proto.String(body)},
			InteractiveMessage: &waE2E.InteractiveMessage_NativeFlowMessage_{
				NativeFlowMessage: &waE2E.InteractiveMessage_NativeFlowMessage{Buttons: buttons},
			},
			// ContextInfo must exist: SendMessage fills it in when the message quotes another.
			ContextInfo: &waE2E.ContextInfo{},
		}
		if data.Footer != "" {
			interactive.Footer = &waE2E.InteractiveMessage_Footer{Text: proto.String(data.Footer)}
		}
		interactive.Header = s.buttonHeader(client, instance.Id, data)

		msg = &waE2E.Message{InteractiveMessage: interactive}
		msgType = "InteractiveMessage"
		bizNodes = nativeFlowBizNodes("mixed", "9", data.Number)
	}

	// Route through centralized SendMessage for ContextInfo, webhooks, quotes, mentions.
	message, err := s.SendMessage(instance, msg, msgType, &SendDataStruct{
		Number:          data.Number,
		Delay:           data.Delay,
		MentionAll:      data.MentionAll,
		MentionedJID:    data.MentionedJID,
		FormatJid:       data.FormatJid,
		Quoted:          data.Quoted,
		AdditionalNodes: &bizNodes,
	})
	if err != nil {
		s.loggerWrapper.GetLogger(instance.Id).LogError("[%s] Error sending button message: %v", instance.Id, err)
		return nil, err
	}

	return message, nil
}

// nativeFlowBizNodes is the <biz> node that announces a native flow message to the server, plus
// the <bot biz_bot="1"/> node that 1:1 chats need (groups do not take it).
func nativeFlowBizNodes(flowName, flowVersion, number string) []waBinary.Node {
	nodes := []waBinary.Node{{
		Tag: "biz",
		Content: []waBinary.Node{{
			Tag:   "interactive",
			Attrs: waBinary.Attrs{"type": "native_flow", "v": "1"},
			Content: []waBinary.Node{{
				Tag:   "native_flow",
				Attrs: waBinary.Attrs{"name": flowName, "v": flowVersion},
			}},
		}},
	}}
	if !strings.Contains(number, "@g.us") {
		nodes = append(nodes, waBinary.Node{Tag: "bot", Attrs: waBinary.Attrs{"biz_bot": "1"}})
	}
	return nodes
}

// buttonHeader uploads the optional image or video header of a button message. A header that
// cannot be downloaded or uploaded is skipped (logged) so the buttons still go out.
func (s *sendService) buttonHeader(client *whatsmeow.Client, instanceID string, data *ButtonStruct) *waE2E.InteractiveMessage_Header {
	switch {
	case data.ImageUrl != "":
		fileData, err := utils.DownloadBytes(data.ImageUrl, utils.MaxImageDownload)
		if err != nil {
			s.loggerWrapper.GetLogger(instanceID).LogWarn("[%s] Button header ImageUrl not attached: %v", instanceID, err)
			return nil
		}
		uploaded, err := client.Upload(context.Background(), fileData, whatsmeow.MediaImage)
		if err != nil {
			s.loggerWrapper.GetLogger(instanceID).LogWarn("[%s] Button header image upload failed: %v", instanceID, err)
			return nil
		}
		imgW, imgH := imageDimensions(fileData)
		return &waE2E.InteractiveMessage_Header{
			HasMediaAttachment: proto.Bool(true),
			Media: &waE2E.InteractiveMessage_Header_ImageMessage{
				ImageMessage: &waE2E.ImageMessage{
					URL:           proto.String(uploaded.URL),
					DirectPath:    proto.String(uploaded.DirectPath),
					MediaKey:      uploaded.MediaKey,
					Mimetype:      proto.String("image/jpeg"),
					FileEncSHA256: uploaded.FileEncSHA256,
					FileSHA256:    uploaded.FileSHA256,
					FileLength:    proto.Uint64(uint64(len(fileData))),
					JPEGThumbnail: makeJPEGThumbnail(fileData, 72),
					Width:         imgW,
					Height:        imgH,
				},
			},
		}
	case data.VideoUrl != "":
		fileData, err := utils.DownloadBytes(data.VideoUrl, utils.MaxMediaDownload)
		if err != nil {
			s.loggerWrapper.GetLogger(instanceID).LogWarn("[%s] Button header VideoUrl not attached: %v", instanceID, err)
			return nil
		}
		uploaded, err := client.Upload(context.Background(), fileData, whatsmeow.MediaVideo)
		if err != nil {
			s.loggerWrapper.GetLogger(instanceID).LogWarn("[%s] Button header video upload failed: %v", instanceID, err)
			return nil
		}
		return &waE2E.InteractiveMessage_Header{
			HasMediaAttachment: proto.Bool(true),
			Media: &waE2E.InteractiveMessage_Header_VideoMessage{
				VideoMessage: &waE2E.VideoMessage{
					URL:           proto.String(uploaded.URL),
					DirectPath:    proto.String(uploaded.DirectPath),
					MediaKey:      uploaded.MediaKey,
					Mimetype:      proto.String("video/mp4"),
					FileEncSHA256: uploaded.FileEncSHA256,
					FileSHA256:    uploaded.FileSHA256,
					FileLength:    proto.Uint64(uint64(len(fileData))),
				},
			},
		}
	}
	return nil
}

// carouselCardHeader is the header of a carousel card, without media yet. A title or subtitle
// sent as an empty string makes the iPhone drop the whole carousel (WhatsApp Web still shows
// it), so the field is left out when there is no text.
func carouselCardHeader(h CarouselCardHeaderStruct) *waE2E.InteractiveMessage_Header {
	header := &waE2E.InteractiveMessage_Header{HasMediaAttachment: proto.Bool(false)}
	if h.Title != "" {
		header.Title = proto.String(h.Title)
	}
	if h.Subtitle != "" {
		header.Subtitle = proto.String(h.Subtitle)
	}
	return header
}

// buildCarouselButton maps a carousel button to its native-flow name and
// buttonParamsJSON. The params are built with json.Marshal: they used to be
// assembled with fmt.Sprintf, so any quote or backslash in a label produced
// invalid JSON. URL/CALL values come from the explicit `url` / `phoneNumber`
// fields and fall back to `id`; COPY_CODE is accepted as an alias of COPY (#51).
func buildCarouselButton(btn CarouselButtonStruct) (name string, paramsJSON string) {
	firstNonEmpty := func(values ...string) string {
		for _, v := range values {
			if v != "" {
				return v
			}
		}
		return ""
	}

	var params map[string]string
	switch strings.ToUpper(btn.Type) {
	case "URL":
		name = "cta_url"
		params = map[string]string{"display_text": btn.DisplayText, "url": firstNonEmpty(btn.URL, btn.Id)}
	case "CALL":
		name = "cta_call"
		params = map[string]string{"display_text": btn.DisplayText, "phone_number": firstNonEmpty(btn.PhoneNumber, btn.Id)}
	case "COPY", "COPY_CODE":
		name = "cta_copy"
		params = map[string]string{"display_text": btn.DisplayText, "copy_code": firstNonEmpty(btn.CopyCode, btn.Id)}
	default: // REPLY or empty
		name = "quick_reply"
		params = map[string]string{"display_text": btn.DisplayText, "id": btn.Id}
	}

	encoded, err := json.Marshal(params)
	if err != nil { // a map[string]string cannot fail to marshal
		return name, "{}"
	}
	return name, string(encoded)
}

// imageDimensions returns the pixel width and height of an encoded image as
// proto pointers, or (nil, nil) when it cannot be decoded so the fields stay
// unset instead of advertising a 0x0 image. Cheap: it only reads the header.
func imageDimensions(fileData []byte) (*uint32, *uint32) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(fileData))
	if err != nil || cfg.Width < 1 || cfg.Height < 1 {
		return nil, nil
	}
	return proto.Uint32(uint32(cfg.Width)), proto.Uint32(uint32(cfg.Height))
}

// makeJPEGThumbnail decodes raw image bytes and produces a small JPEG
// thumbnail suitable for the JPEGThumbnail field of WhatsApp media messages.
// The thumbnail keeps the original aspect ratio and is capped at maxWidth
// pixels wide. It returns nil if the image cannot be decoded so callers can
// fall back to sending the message without a preview thumbnail.
func makeJPEGThumbnail(fileData []byte, maxWidth int) []byte {
	if maxWidth < 1 {
		maxWidth = 72
	}

	// No thumbnail rather than a decoder allocating for whatever the header declares.
	if err := utils.CheckImageDimensions(fileData, utils.MaxImagePixels()); err != nil {
		return nil
	}

	img, _, err := image.Decode(bytes.NewReader(fileData))
	if err != nil {
		return nil
	}

	bounds := img.Bounds()
	srcWidth := bounds.Dx()
	srcHeight := bounds.Dy()
	if srcWidth < 1 || srcHeight < 1 {
		return nil
	}

	thumbWidth := maxWidth
	if srcWidth < thumbWidth {
		thumbWidth = srcWidth
	}
	thumbHeight := int(float64(srcHeight) * float64(thumbWidth) / float64(srcWidth))
	if thumbHeight < 1 {
		thumbHeight = 1
	}

	thumbImg := image.NewRGBA(image.Rect(0, 0, thumbWidth, thumbHeight))
	for y := 0; y < thumbHeight; y++ {
		for x := 0; x < thumbWidth; x++ {
			srcX := x * srcWidth / thumbWidth
			srcY := y * srcHeight / thumbHeight
			thumbImg.Set(x, y, img.At(srcX+bounds.Min.X, srcY+bounds.Min.Y))
		}
	}

	var thumbBuf bytes.Buffer
	if err := jpeg.Encode(&thumbBuf, thumbImg, &jpeg.Options{Quality: 50}); err != nil {
		return nil
	}
	return thumbBuf.Bytes()
}

// makePDFThumbnail rasterizes the first page of a PDF into a JPEG thumbnail
// using the external "pdftoppm" tool (poppler-utils). It returns nil when
// pdftoppm is not installed or rasterization fails, so callers can gracefully
// send the document without a preview instead of failing the request.
// The preview of a PDF is a nicety: a file this large, or a rendering this slow, gets none.
const (
	maxPDFForThumbnail  = 20 << 20
	pdfThumbnailTimeout = 30 * time.Second
)

func makePDFThumbnail(fileData []byte, maxWidth int) []byte {
	if _, err := exec.LookPath("pdftoppm"); err != nil {
		return nil
	}

	scaleWidth := maxWidth
	if scaleWidth < 1 {
		scaleWidth = 72
	}

	// Render only the first page to a PNG on stdout, scaled to scaleWidth.
	// "-scale-to-y -1" keeps the original aspect ratio.
	// A PDF is untrusted input for poppler: limit the time (it had none), the size of the
	// file and the output, and share the conversion slots with ffmpeg.
	if len(fileData) > maxPDFForThumbnail {
		return nil
	}
	out, _, err := utils.RunLimited(context.Background(), pdfThumbnailTimeout, "pdftoppm", []string{
		"-png",
		"-f", "1",
		"-l", "1",
		"-singlefile",
		"-scale-to-x", strconv.Itoa(scaleWidth),
		"-scale-to-y", "-1",
	}, fileData, 16<<20)
	if err != nil || len(out) == 0 {
		return nil
	}

	// Re-encode the rendered PNG as a JPEG thumbnail for consistency with images.
	return makeJPEGThumbnail(out, maxWidth)
}

func (s *sendService) SendList(data *ListStruct, instance *instance_model.Instance) (*MessageSendStruct, error) {
	// Legacy ListMessage format - works on iOS, Android and Web
	// Matching PAPI Node.js default (non-modern) path exactly

	buttonText := data.ButtonText
	if buttonText == "" {
		buttonText = "Ver Menu"
	}

	// Build sections in legacy ListMessage format
	var sections []*waE2E.ListMessage_Section
	for _, sec := range data.Sections {
		sectionTitle := sec.Title
		if sectionTitle == "" {
			sectionTitle = " "
		}
		var rows []*waE2E.ListMessage_Row
		for i, r := range sec.Rows {
			rowTitle := r.Title
			if rowTitle == "" {
				rowTitle = " "
			}
			rowId := r.RowId
			if rowId == "" {
				rowId = fmt.Sprintf("row_%d_%d", i, len(rows))
			}
			rows = append(rows, &waE2E.ListMessage_Row{
				Title:       proto.String(rowTitle),
				Description: proto.String(r.Description),
				RowID:       proto.String(rowId),
			})
		}
		sections = append(sections, &waE2E.ListMessage_Section{
			Title: proto.String(sectionTitle),
			Rows:  rows,
		})
	}

	listType := waE2E.ListMessage_SINGLE_SELECT
	listMessage := &waE2E.ListMessage{
		Title:       proto.String(data.Title),
		Description: proto.String(data.Description),
		ButtonText:  proto.String(buttonText),
		FooterText:  proto.String(data.FooterText),
		ListType:    &listType,
		Sections:    sections,
	}

	// Wrap ListMessage in DocumentWithCaptionMessage (Baileys PR #36) so modern WhatsApp renders it.
	// MessageSecret (32 random bytes) is required for iOS rendering.
	listMsgSecret := make([]byte, 32)
	_, _ = crypto_rand.Read(listMsgSecret)

	msg := &waE2E.Message{
		DocumentWithCaptionMessage: &waE2E.FutureProofMessage{
			Message: &waE2E.Message{
				ListMessage: listMessage,
			},
		},
		MessageContextInfo: &waE2E.MessageContextInfo{
			MessageSecret: listMsgSecret,
		},
	}

	// Build biz <list> node — required for mobile rendering of modern lists.
	listBizNodes := []waBinary.Node{
		{
			Tag: "biz",
			Content: []waBinary.Node{{
				Tag: "list",
				Attrs: waBinary.Attrs{
					"v":    "2",
					"type": "single_select",
				},
			}},
		},
	}
	if !strings.Contains(data.Number, "@g.us") {
		listBizNodes = append(listBizNodes, waBinary.Node{
			Tag:   "bot",
			Attrs: waBinary.Attrs{"biz_bot": "1"},
		})
	}

	message, err := s.SendMessage(instance, msg, "ListMessage", &SendDataStruct{
		Number:          data.Number,
		Delay:           data.Delay,
		MentionAll:      data.MentionAll,
		MentionedJID:    data.MentionedJID,
		FormatJid:       data.FormatJid,
		Quoted:          data.Quoted,
		AdditionalNodes: &listBizNodes,
	})

	if err != nil {
		var refused *listRefusal
		if errors.As(err, &refused) && (data.FallbackButtons == nil || *data.FallbackButtons) {
			return s.sendListAsButtons(data, instance, err)
		}
		s.loggerWrapper.GetLogger(instance.Id).LogError("[%s] Error sending list: %v", instance.Id, err)
		return nil, err
	}

	s.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] List sent to %s", instance.Id, data.Number)
	return message, nil
}

func (s *sendService) SendMessage(instance *instance_model.Instance, msg *waE2E.Message, messageType string, data *SendDataStruct) (*MessageSendStruct, error) {
	s.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] SendMessage called for number: %s, type: %s", instance.Id, data.Number, messageType)

	recipient, err := s.validateAndCheckUserExists(data.Number, data.FormatJid, &data.Quoted.MessageID, &data.Quoted.MessageID, instance)
	if err != nil {
		s.loggerWrapper.GetLogger(instance.Id).LogError("[%s] Error validating message fields or user check: %v", instance.Id, err)
		return nil, err
	}

	s.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] Recipient validated: %s (Server: %s)", instance.Id, recipient.String(), recipient.Server)

	// One lookup for the whole send. The instance can be stopped or replaced while a
	// send is in flight (a delay, an upload), and the repeated lookups below used to
	// dereference the nil that leaves behind.
	client := s.clientPointer.Get(instance.Id)
	if client == nil || client.Store == nil || client.Store.ID == nil {
		return nil, ErrNoActiveSession
	}

	var message string
	if data.Id == "" {
		message = client.GenerateMessageID()
	} else {
		message = data.Id
	}

	if data.Delay > 0 {
		delay, capped := sendDelay(data.Delay)
		if capped {
			s.loggerWrapper.GetLogger(instance.Id).LogWarn("[%s] delay of %dms capped to %v", instance.Id, data.Delay, maxSendDelay)
		}
		media := ""
		if messageType == "AudioMessage" {
			media = "audio"
		}

		err := client.SendChatPresence(context.Background(), recipient, types.ChatPresence("composing"), types.ChatPresenceMedia(media))
		if err != nil {
			return nil, err
		}

		time.Sleep(delay)

		err = client.SendChatPresence(context.Background(), recipient, types.ChatPresence("paused"), types.ChatPresenceMedia(media))
		if err != nil {
			return nil, err
		}
	}

	isMedia := false

	if data.Quoted.MessageID != "" {
		switch messageType {
		case "ExtendedTextMessage":
			msg.ExtendedTextMessage.ContextInfo = &waE2E.ContextInfo{
				StanzaID:      proto.String(data.Quoted.MessageID),
				Participant:   proto.String(data.Quoted.Participant),
				QuotedMessage: quotedMessageFor(data.Quoted),
			}
		case "ImageMessage":
			msg.ImageMessage.ContextInfo = &waE2E.ContextInfo{
				StanzaID:      proto.String(data.Quoted.MessageID),
				Participant:   proto.String(data.Quoted.Participant),
				QuotedMessage: quotedMessageFor(data.Quoted),
			}
			isMedia = true
		case "VideoMessage":
			msg.VideoMessage.ContextInfo = &waE2E.ContextInfo{
				StanzaID:      proto.String(data.Quoted.MessageID),
				Participant:   proto.String(data.Quoted.Participant),
				QuotedMessage: quotedMessageFor(data.Quoted),
			}
			isMedia = true
		case "PtvMessage":
			msg.PtvMessage.ContextInfo = &waE2E.ContextInfo{
				StanzaID:      proto.String(data.Quoted.MessageID),
				Participant:   proto.String(data.Quoted.Participant),
				QuotedMessage: quotedMessageFor(data.Quoted),
			}
			isMedia = true
		case "AudioMessage":
			msg.AudioMessage.ContextInfo = &waE2E.ContextInfo{
				StanzaID:      proto.String(data.Quoted.MessageID),
				Participant:   proto.String(data.Quoted.Participant),
				QuotedMessage: quotedMessageFor(data.Quoted),
			}
			isMedia = true
		case "DocumentMessage":
			if msg.DocumentMessage != nil {
				msg.DocumentMessage.ContextInfo = &waE2E.ContextInfo{
					StanzaID:      proto.String(data.Quoted.MessageID),
					Participant:   proto.String(data.Quoted.Participant),
					QuotedMessage: quotedMessageFor(data.Quoted),
				}
			} else if msg.DocumentWithCaptionMessage != nil {
				msg.DocumentWithCaptionMessage.Message.DocumentMessage.ContextInfo = &waE2E.ContextInfo{
					StanzaID:      proto.String(data.Quoted.MessageID),
					Participant:   proto.String(data.Quoted.Participant),
					QuotedMessage: quotedMessageFor(data.Quoted),
				}
			}
			isMedia = true
		case "PollCreationMessage":
			msg.PollCreationMessage.ContextInfo = &waE2E.ContextInfo{
				StanzaID:      proto.String(data.Quoted.MessageID),
				Participant:   proto.String(data.Quoted.Participant),
				QuotedMessage: quotedMessageFor(data.Quoted),
			}
		case "StickerMessage":
			msg.StickerMessage.ContextInfo = &waE2E.ContextInfo{
				StanzaID:      proto.String(data.Quoted.MessageID),
				Participant:   proto.String(data.Quoted.Participant),
				QuotedMessage: quotedMessageFor(data.Quoted),
			}
			isMedia = true
		case "LocationMessage":
			msg.LocationMessage.ContextInfo = &waE2E.ContextInfo{
				StanzaID:      proto.String(data.Quoted.MessageID),
				Participant:   proto.String(data.Quoted.Participant),
				QuotedMessage: quotedMessageFor(data.Quoted),
			}
		case "ContactMessage":
			msg.ContactMessage.ContextInfo = &waE2E.ContextInfo{
				StanzaID:      proto.String(data.Quoted.MessageID),
				Participant:   proto.String(data.Quoted.Participant),
				QuotedMessage: quotedMessageFor(data.Quoted),
			}
		case "InteractiveMessage":
			if msg.InteractiveMessage != nil {
				msg.InteractiveMessage.ContextInfo = &waE2E.ContextInfo{
					StanzaID:      proto.String(data.Quoted.MessageID),
					Participant:   proto.String(data.Quoted.Participant),
					QuotedMessage: quotedMessageFor(data.Quoted),
				}
			} else if msg.DocumentWithCaptionMessage != nil &&
				msg.DocumentWithCaptionMessage.Message != nil &&
				msg.DocumentWithCaptionMessage.Message.InteractiveMessage != nil {
				msg.DocumentWithCaptionMessage.Message.InteractiveMessage.ContextInfo = &waE2E.ContextInfo{
					StanzaID:      proto.String(data.Quoted.MessageID),
					Participant:   proto.String(data.Quoted.Participant),
					QuotedMessage: quotedMessageFor(data.Quoted),
				}
			}
		case "ListMessage":
			if msg.ListMessage != nil {
				msg.ListMessage.ContextInfo = &waE2E.ContextInfo{
					StanzaID:      proto.String(data.Quoted.MessageID),
					Participant:   proto.String(data.Quoted.Participant),
					QuotedMessage: quotedMessageFor(data.Quoted),
				}
			} else if msg.DocumentWithCaptionMessage != nil &&
				msg.DocumentWithCaptionMessage.Message != nil &&
				msg.DocumentWithCaptionMessage.Message.ListMessage != nil {
				msg.DocumentWithCaptionMessage.Message.ListMessage.ContextInfo = &waE2E.ContextInfo{
					StanzaID:      proto.String(data.Quoted.MessageID),
					Participant:   proto.String(data.Quoted.Participant),
					QuotedMessage: quotedMessageFor(data.Quoted),
				}
			}
		case "ButtonsMessage":
			if msg.ButtonsMessage != nil {
				msg.ButtonsMessage.ContextInfo = &waE2E.ContextInfo{
					StanzaID:      proto.String(data.Quoted.MessageID),
					Participant:   proto.String(data.Quoted.Participant),
					QuotedMessage: quotedMessageFor(data.Quoted),
				}
			} else if msg.DocumentWithCaptionMessage != nil &&
				msg.DocumentWithCaptionMessage.Message != nil &&
				msg.DocumentWithCaptionMessage.Message.ButtonsMessage != nil {
				msg.DocumentWithCaptionMessage.Message.ButtonsMessage.ContextInfo = &waE2E.ContextInfo{
					StanzaID:      proto.String(data.Quoted.MessageID),
					Participant:   proto.String(data.Quoted.Participant),
					QuotedMessage: quotedMessageFor(data.Quoted),
				}
			}
		default:
			return nil, apierror.Invalid(fmt.Sprintf("invalid messageType: %s", messageType))
		}
	} else {
		switch messageType {
		case "ExtendedTextMessage":
			msg.ExtendedTextMessage.ContextInfo = &waE2E.ContextInfo{}
		case "ImageMessage":
			msg.ImageMessage.ContextInfo = &waE2E.ContextInfo{}
			isMedia = true
		case "VideoMessage":
			msg.VideoMessage.ContextInfo = &waE2E.ContextInfo{}
			isMedia = true
		case "PtvMessage":
			msg.PtvMessage.ContextInfo = &waE2E.ContextInfo{}
			isMedia = true
		case "AudioMessage":
			msg.AudioMessage.ContextInfo = &waE2E.ContextInfo{}
			isMedia = true
		case "DocumentMessage":
			if msg.DocumentMessage != nil {
				msg.DocumentMessage.ContextInfo = &waE2E.ContextInfo{}
			} else if msg.DocumentWithCaptionMessage != nil {
				msg.DocumentWithCaptionMessage.Message.DocumentMessage.ContextInfo = &waE2E.ContextInfo{}
			}
			isMedia = true
		case "PollCreationMessage":
			msg.PollCreationMessage.ContextInfo = &waE2E.ContextInfo{}
		case "PollUpdateMessage":
			// A poll vote carries no ContextInfo.
		case "StickerMessage":
			msg.StickerMessage.ContextInfo = &waE2E.ContextInfo{}
		case "LocationMessage":
			msg.LocationMessage.ContextInfo = &waE2E.ContextInfo{}
		case "ContactMessage":
			msg.ContactMessage.ContextInfo = &waE2E.ContextInfo{}
		case "InteractiveMessage":
			// ContextInfo already set in SendCarousel/SendButton/SendList
		case "ListMessage":
			// ContextInfo already set in SendList
		case "ButtonsMessage":
			// Reply-only buttons: ContextInfo already set in SendButton
		default:
			return nil, apierror.Invalid(fmt.Sprintf("invalid messageType: %s", messageType))
		}
	}

	// Apply ForwardingScore to whichever ContextInfo was set above.
	// WhatsApp renders "Encaminhada" when ContextInfo.ForwardingScore > 0.
	if data.ForwardingScore != nil && *data.ForwardingScore > 0 {
		switch messageType {
		case "ExtendedTextMessage":
			if msg.ExtendedTextMessage != nil && msg.ExtendedTextMessage.ContextInfo != nil {
				msg.ExtendedTextMessage.ContextInfo.ForwardingScore = data.ForwardingScore
				msg.ExtendedTextMessage.ContextInfo.IsForwarded = proto.Bool(true)
			}
		case "ImageMessage":
			if msg.ImageMessage != nil && msg.ImageMessage.ContextInfo != nil {
				msg.ImageMessage.ContextInfo.ForwardingScore = data.ForwardingScore
				msg.ImageMessage.ContextInfo.IsForwarded = proto.Bool(true)
			}
		case "VideoMessage":
			if msg.VideoMessage != nil && msg.VideoMessage.ContextInfo != nil {
				msg.VideoMessage.ContextInfo.ForwardingScore = data.ForwardingScore
				msg.VideoMessage.ContextInfo.IsForwarded = proto.Bool(true)
			}
		case "PtvMessage":
			if msg.PtvMessage != nil && msg.PtvMessage.ContextInfo != nil {
				msg.PtvMessage.ContextInfo.ForwardingScore = data.ForwardingScore
				msg.PtvMessage.ContextInfo.IsForwarded = proto.Bool(true)
			}
		case "AudioMessage":
			if msg.AudioMessage != nil && msg.AudioMessage.ContextInfo != nil {
				msg.AudioMessage.ContextInfo.ForwardingScore = data.ForwardingScore
				msg.AudioMessage.ContextInfo.IsForwarded = proto.Bool(true)
			}
		case "DocumentMessage":
			if msg.DocumentMessage != nil && msg.DocumentMessage.ContextInfo != nil {
				msg.DocumentMessage.ContextInfo.ForwardingScore = data.ForwardingScore
				msg.DocumentMessage.ContextInfo.IsForwarded = proto.Bool(true)
			} else if msg.DocumentWithCaptionMessage != nil && msg.DocumentWithCaptionMessage.Message != nil && msg.DocumentWithCaptionMessage.Message.DocumentMessage != nil && msg.DocumentWithCaptionMessage.Message.DocumentMessage.ContextInfo != nil {
				msg.DocumentWithCaptionMessage.Message.DocumentMessage.ContextInfo.ForwardingScore = data.ForwardingScore
				msg.DocumentWithCaptionMessage.Message.DocumentMessage.ContextInfo.IsForwarded = proto.Bool(true)
			}
		case "PollCreationMessage":
			if msg.PollCreationMessage != nil && msg.PollCreationMessage.ContextInfo != nil {
				msg.PollCreationMessage.ContextInfo.ForwardingScore = data.ForwardingScore
				msg.PollCreationMessage.ContextInfo.IsForwarded = proto.Bool(true)
			}
		case "StickerMessage":
			if msg.StickerMessage != nil && msg.StickerMessage.ContextInfo != nil {
				msg.StickerMessage.ContextInfo.ForwardingScore = data.ForwardingScore
				msg.StickerMessage.ContextInfo.IsForwarded = proto.Bool(true)
			}
		case "LocationMessage":
			if msg.LocationMessage != nil && msg.LocationMessage.ContextInfo != nil {
				msg.LocationMessage.ContextInfo.ForwardingScore = data.ForwardingScore
				msg.LocationMessage.ContextInfo.IsForwarded = proto.Bool(true)
			}
		case "ContactMessage":
			if msg.ContactMessage != nil && msg.ContactMessage.ContextInfo != nil {
				msg.ContactMessage.ContextInfo.ForwardingScore = data.ForwardingScore
				msg.ContactMessage.ContextInfo.IsForwarded = proto.Bool(true)
			}
		case "InteractiveMessage":
			if msg.InteractiveMessage != nil && msg.InteractiveMessage.ContextInfo != nil {
				msg.InteractiveMessage.ContextInfo.ForwardingScore = data.ForwardingScore
				msg.InteractiveMessage.ContextInfo.IsForwarded = proto.Bool(true)
			}
		case "ListMessage":
			if msg.ListMessage != nil && msg.ListMessage.ContextInfo != nil {
				msg.ListMessage.ContextInfo.ForwardingScore = data.ForwardingScore
				msg.ListMessage.ContextInfo.IsForwarded = proto.Bool(true)
			}
		}
	}

	isGroup := strings.Contains(data.Number, "@g.us")
	isNewsletter := strings.Contains(data.Number, "@newsletter")

	// Only try to get participants for actual groups, not newsletters
	if isGroup && !isNewsletter {
		if data.MentionAll {
			groupInfo, err := client.GetGroupInfo(context.Background(), recipient)
			if err != nil {
				return nil, err
			}

			var mentionedJIDs []string
			for _, participant := range groupInfo.Participants {
				mentionedJIDs = append(mentionedJIDs, participantMentionJID(participant))
			}
			setMessageMentionedJIDs(msg, messageType, mentionedJIDs)
		}

		if len(data.MentionedJID) > 0 {
			setMessageMentionedJIDs(msg, messageType, data.MentionedJID)
		}
	}

	recipient.User = strings.ReplaceAll(recipient.User, "+", "")

	// Chat with disappearing messages: the message has to carry the timer, or the
	// recipient is told it "will not disappear" (issue #79).
	if autoDisappearingEnabled() && disappearingApplies(recipient) {
		if seconds, known := s.whatsmeowService.ChatDisappearingSeconds(instance.Id, recipient); known && applyDisappearingExpiration(msg, seconds) {
			s.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] Chat %s has disappearing messages (%ds): timer applied", instance.Id, recipient.String(), seconds)
		}
	}

	s.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] Sending message to %s with ID %s", instance.Id, recipient.String(), message)

	// Preparar extra parameters para o envio
	sendExtra := whatsmeow.SendRequestExtra{ID: message}

	// Para newsletters/canais, adicionar o MediaHandle se houver mídia
	if recipient.Server == "newsletter" && data.MediaHandle != "" {
		sendExtra.MediaHandle = data.MediaHandle
		s.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] Newsletter detected, using MediaHandle: %s", instance.Id, data.MediaHandle)
	}

	// Injetar nodes biz/bot customizados (PIX, botões interativos, etc.) no stanza XMPP.
	if data.AdditionalNodes != nil {
		sendExtra.AdditionalNodes = data.AdditionalNodes
	}

	response, err := s.sendToWhatsApp(instance.Id, client, recipient, msg, sendExtra)
	if err != nil {
		s.loggerWrapper.GetLogger(instance.Id).LogError("[%s] Error sending message: %v", instance.Id, err)
		// A bare "server returned error 463" says nothing to a person: explain it and,
		// when WhatsApp told us about the restriction, say until when.
		return nil, explainInteractiveError(explainSendError(err, s.whatsmeowService.ReachoutTimelock(instance.Id), time.Now()), messageType)
	}

	s.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] Message sent successfully! ServerID: %d", instance.Id, response.ServerID)

	messageInfo := types.MessageInfo{
		MessageSource: types.MessageSource{
			Chat:     recipient,
			Sender:   *client.Store.ID,
			IsFromMe: true,
			IsGroup:  isGroup,
		},
		ID:        message,
		Timestamp: time.Now(),
		ServerID:  response.ServerID,
		Type:      messageType,
	}

	messageSent := &MessageSendStruct{
		Info:    messageInfo,
		Message: msg,
		MessageContextInfo: &waE2E.ContextInfo{
			StanzaID:      proto.String(data.Quoted.MessageID),
			Participant:   proto.String(data.Quoted.Participant),
			QuotedMessage: quotedMessageFor(data.Quoted),
		},
	}

	// The SendMessage event is built after the answer is on its way: it used to run first,
	// and for media it downloaded again from WhatsApp the file that had just been uploaded
	// (the caller already has it), which kept the API response waiting for all of it.
	go s.publishSendMessage(instance, client, messageSent, isMedia, data.MediaBytes)

	s.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] Message sent to %s", instance.Id, data.Number)
	return messageSent, nil
}

// publishSendMessage emits the SendMessage event of a message that was just sent. media
// is the file that was sent, when the caller still has it; without it (and with
// WEBHOOK_FILES on) the file is downloaded back from WhatsApp. When nobody would receive
// the event, nothing is built.
func (s *sendService) publishSendMessage(instance *instance_model.Instance, client *whatsmeow.Client, messageSent *MessageSendStruct, isMedia bool, media []byte) {
	defer func() {
		if r := recover(); r != nil {
			s.loggerWrapper.GetLogger(instance.Id).LogError("[%s] panic publishing the SendMessage event: %v", instance.Id, r)
		}
	}()

	msg := messageSent.Message
	if !s.whatsmeowService.EventWanted(instance, "SendMessage", messageSent.Info.Chat.String()) {
		return
	}

	postMap := make(map[string]interface{})
	postMap["event"] = "SendMessage"

	// Convertendo o MessageSendStruct para map antes de atribuir
	messageData := make(map[string]interface{})
	messageData["Info"] = messageSent.Info

	// Convertendo a mensagem para map usando json marshal/unmarshal
	msgBytes, err := json.Marshal(msg)
	if err != nil {
		s.loggerWrapper.GetLogger(instance.Id).LogError("[%s] failed to marshal message: %v", instance.Id, err)
		return
	}

	var msgMap map[string]interface{}
	if err := json.Unmarshal(msgBytes, &msgMap); err != nil {
		s.loggerWrapper.GetLogger(instance.Id).LogError("[%s] failed to unmarshal message: %v", instance.Id, err)
		return
	}

	messageData["Message"] = msgMap
	messageData["MessageContextInfo"] = messageSent.MessageContextInfo

	postMap["data"] = messageData

	if isMedia && s.config.WebhookFiles {
		data := media
		var err error

		if data == nil {
			img := msg.GetImageMessage()
			audio := msg.GetAudioMessage()
			document := msg.GetDocumentMessage()
			video := msg.GetVideoMessage()
			sticker := msg.GetStickerMessage()

			switch {
			case img != nil:
				data, err = client.Download(context.Background(), img)
			case audio != nil:
				data, err = client.Download(context.Background(), audio)
			case document != nil:
				data, err = client.Download(context.Background(), document)
			case video != nil:
				data, err = client.Download(context.Background(), video)
			case sticker != nil:
				data, err = client.Download(context.Background(), sticker)
			}
		}

		// Stickers go out as WebP; the event carries them as PNG.
		if err == nil && msg.GetStickerMessage() != nil {
			if decoded, decErr := webp.Decode(bytes.NewReader(data)); decErr == nil {
				var pngBuffer bytes.Buffer
				if png.Encode(&pngBuffer, decoded) == nil {
					data = pngBuffer.Bytes()
				}
			}
		}

		if err == nil && data != nil {
			msgMap["base64"] = base64.StdEncoding.EncodeToString(data)
		}
	}

	s.config.AddInstanceToken(postMap, instance.Token)
	postMap["instanceId"] = instance.Id
	postMap["instanceName"] = instance.Name

	queueName := strings.ToLower(fmt.Sprintf("%s.%s", instance.Id, postMap["event"]))

	values, err := json.Marshal(postMap)
	if err != nil {
		s.loggerWrapper.GetLogger(instance.Id).LogError("[%s] Failed to marshal JSON for queue", instance.Id)
		return
	}

	s.whatsmeowService.CallWebhook(instance, queueName, values)

	if s.config.AmqpGlobalEnabled || s.config.NatsGlobalEnabled {
		s.whatsmeowService.SendToGlobalQueues(postMap["event"].(string), values, instance.Id)
	}
}

func (s *sendService) SendCarousel(data *CarouselStruct, instance *instance_model.Instance) (*MessageSendStruct, error) {
	client, err := s.ensureClientConnected(instance.Id)
	if err != nil {
		return nil, err
	}

	formatJid := true
	if data.FormatJid != nil {
		formatJid = *data.FormatJid
	}

	var recipient types.JID
	var ok bool
	recipient, ok = utils.ParseJID(data.Number)
	if !ok && formatJid {
		s.loggerWrapper.GetLogger(instance.Id).LogError("[%s] Error validating message fields", instance.Id)
		return nil, apierror.Invalid("invalid phone number")
	} else if !ok && !formatJid {
		recipient = types.JID{
			User:   data.Number,
			Server: types.DefaultUserServer,
		}
	}

	// Build carousel cards
	cards := make([]*waE2E.InteractiveMessage, len(data.Cards))
	messageVersion := int32(1)

	s.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] Building carousel for %s with %d cards", instance.Id, recipient.String(), len(data.Cards))

	for i, card := range data.Cards {
		// Each card MUST have both header and body for carousel to work
		interactiveCard := &waE2E.InteractiveMessage{
			Body: &waE2E.InteractiveMessage_Body{
				Text: proto.String(card.Body.Text),
			},
			Header: carouselCardHeader(card.Header),
		}

		// Add media to header if URL provided
		if card.Header.ImageUrl != "" || card.Header.VideoUrl != "" {
			header := interactiveCard.Header

			if card.Header.ImageUrl != "" {
				// Download image
				fileData, err := utils.DownloadBytes(card.Header.ImageUrl, utils.MaxImageDownload)
				if err != nil {
					s.loggerWrapper.GetLogger(instance.Id).LogWarn("[%s] Carousel card image not attached: %v", instance.Id, err)
				} else {
					{
						uploaded, err := client.Upload(context.Background(), fileData, whatsmeow.MediaImage)
						if err == nil {
							// Generate JPEG thumbnail for iOS compatibility
							jpegThumb := makeJPEGThumbnail(fileData, 72)
							// Width/Height let the client size the bubble before the media downloads (#104).
							imgW, imgH := imageDimensions(fileData)

							header.HasMediaAttachment = proto.Bool(true)
							header.Media = &waE2E.InteractiveMessage_Header_ImageMessage{
								ImageMessage: &waE2E.ImageMessage{
									URL:           proto.String(uploaded.URL),
									DirectPath:    proto.String(uploaded.DirectPath),
									MediaKey:      uploaded.MediaKey,
									Mimetype:      proto.String("image/jpeg"),
									FileEncSHA256: uploaded.FileEncSHA256,
									FileSHA256:    uploaded.FileSHA256,
									FileLength:    proto.Uint64(uint64(len(fileData))),
									JPEGThumbnail: jpegThumb,
									Width:         imgW,
									Height:        imgH,
								},
							}
						}
					}
				}
			} else if card.Header.VideoUrl != "" {
				// Download and upload video
				fileData, err := utils.DownloadBytes(card.Header.VideoUrl, utils.MaxMediaDownload)
				if err != nil {
					s.loggerWrapper.GetLogger(instance.Id).LogWarn("[%s] Carousel card video not attached: %v", instance.Id, err)
				} else {
					{
						uploaded, err := client.Upload(context.Background(), fileData, whatsmeow.MediaVideo)
						if err == nil {
							header.HasMediaAttachment = proto.Bool(true)
							header.Media = &waE2E.InteractiveMessage_Header_VideoMessage{
								VideoMessage: &waE2E.VideoMessage{
									URL:           proto.String(uploaded.URL),
									DirectPath:    proto.String(uploaded.DirectPath),
									MediaKey:      uploaded.MediaKey,
									Mimetype:      proto.String("video/mp4"),
									FileEncSHA256: uploaded.FileEncSHA256,
									FileSHA256:    uploaded.FileSHA256,
									FileLength:    proto.Uint64(uint64(len(fileData))),
								},
							}
						}
					}
				}
			}
		}

		// Add footer if exists
		if card.Footer != "" {
			interactiveCard.Footer = &waE2E.InteractiveMessage_Footer{
				Text: proto.String(card.Footer),
			}
		}

		// Add buttons if exist
		if len(card.Buttons) > 0 {
			buttons := make([]*waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton, len(card.Buttons))
			for j, btn := range card.Buttons {
				buttonName, buttonParams := buildCarouselButton(btn)

				buttons[j] = &waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton{
					Name:             proto.String(buttonName),
					ButtonParamsJSON: proto.String(buttonParams),
				}
			}

			// Cards in carousel: do NOT set MessageParamsJSON or MessageVersion
			// (matching PAPI Node.js behavior for iOS compatibility)
			interactiveCard.InteractiveMessage = &waE2E.InteractiveMessage_NativeFlowMessage_{
				NativeFlowMessage: &waE2E.InteractiveMessage_NativeFlowMessage{
					Buttons: buttons,
				},
			}
		}

		cards[i] = interactiveCard
	}

	// Build carousel message (do NOT set CarouselCardType - matching PAPI Node.js for iOS)
	interactiveMsg := &waE2E.InteractiveMessage{
		InteractiveMessage: &waE2E.InteractiveMessage_CarouselMessage_{
			CarouselMessage: &waE2E.InteractiveMessage_CarouselMessage{
				Cards:          cards,
				MessageVersion: &messageVersion,
			},
		},
	}

	// Add body if provided (main message above carousel)
	if data.Body != "" {
		interactiveMsg.Body = &waE2E.InteractiveMessage_Body{
			Text: proto.String(data.Body),
		}
	}

	// Add footer if provided (text below carousel)
	if data.Footer != "" {
		interactiveMsg.Footer = &waE2E.InteractiveMessage_Footer{
			Text: proto.String(data.Footer),
		}
	}

	// ContextInfo is REQUIRED for iOS compatibility
	// Even if empty, iOS requires this field to display carousel
	contextInfo := &waE2E.ContextInfo{}

	// Add quoted message if exists
	if data.Quoted.MessageID != "" {
		contextInfo.StanzaID = proto.String(data.Quoted.MessageID)
		if data.Quoted.Participant != "" {
			participantJID, ok := utils.ParseJID(data.Quoted.Participant)
			if ok {
				contextInfo.Participant = proto.String(participantJID.String())
			}
		}
	}

	// Always set ContextInfo (required for iOS)
	interactiveMsg.ContextInfo = contextInfo

	// Build final message with MessageContextInfo for proper notification delivery
	msg := &waE2E.Message{
		InteractiveMessage: interactiveMsg,
		MessageContextInfo: &waE2E.MessageContextInfo{
			DeviceListMetadata: &waE2E.DeviceListMetadata{},
		},
	}

	message, err := s.SendMessage(instance, msg, "InteractiveMessage", &SendDataStruct{
		Number: data.Number,
		Delay:  data.Delay,
	})

	if err != nil {
		s.loggerWrapper.GetLogger(instance.Id).LogError("[%s] Error sending carousel: %v", instance.Id, err)
		return nil, err
	}

	s.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] Carousel sent to %s with %d cards", instance.Id, data.Number, len(data.Cards))
	return message, nil
}

func (s *sendService) SendStatusText(data *StatusTextStruct, instance *instance_model.Instance) (*MessageSendStruct, error) {
	client, err := s.ensureClientConnected(instance.Id)
	if err != nil {
		return nil, err
	}

	if data.Text == "" {
		return nil, apierror.Invalid("text is required")
	}

	msg := &waE2E.Message{
		ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text: &data.Text,
		},
	}

	messageID := data.Id
	if messageID == "" {
		messageID = client.GenerateMessageID()
	}

	recipient := types.NewJID("status", "broadcast")

	response, err := s.sendToWhatsApp(instance.Id, client, recipient, msg, whatsmeow.SendRequestExtra{ID: messageID})
	if err != nil {
		return nil, err
	}

	messageInfo := types.MessageInfo{
		MessageSource: types.MessageSource{
			Chat:     recipient,
			Sender:   *client.Store.ID,
			IsFromMe: true,
			IsGroup:  false,
		},
		ID:        messageID,
		Timestamp: time.Now(),
		ServerID:  response.ServerID,
		Type:      "StatusTextMessage",
	}

	messageSent := &MessageSendStruct{
		Info:    messageInfo,
		Message: msg,
		MessageContextInfo: &waE2E.ContextInfo{
			StanzaID:      proto.String(""),
			Participant:   proto.String(""),
			QuotedMessage: &waE2E.Message{Conversation: proto.String("")},
		},
	}

	s.sendStatusWebhook(messageSent, instance, "text")
	s.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] Status text sent successfully", instance.Id)
	return messageSent, nil
}

func (s *sendService) SendStatusMediaUrl(data *StatusMediaStruct, instance *instance_model.Instance) (*MessageSendStruct, error) {
	client, err := s.ensureClientConnected(instance.Id)
	if err != nil {
		return nil, err
	}

	if data.Url == "" {
		return nil, apierror.Invalid("url is required")
	}
	if data.Type != "image" && data.Type != "video" {
		return nil, apierror.Invalid("type must be 'image' or 'video'")
	}

	req, err := http.NewRequest("GET", data.Url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "WhatyGo/1.0")

	httpClient := utils.DownloadClient
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to download file from URL: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("failed to download file: HTTP status %d", resp.StatusCode)
	}

	fileData, err := io.ReadAll(io.LimitReader(resp.Body, utils.MaxMediaDownload+1))
	if err != nil {
		return nil, err
	}
	if int64(len(fileData)) > utils.MaxMediaDownload {
		return nil, utils.ErrDownloadTooLarge
	}

	return s.sendStatusMedia(client, data, fileData, instance)
}

func (s *sendService) SendStatusMediaFile(data *StatusMediaStruct, fileData []byte, instance *instance_model.Instance) (*MessageSendStruct, error) {
	client, err := s.ensureClientConnected(instance.Id)
	if err != nil {
		return nil, err
	}

	if data.Type != "image" && data.Type != "video" {
		return nil, apierror.Invalid("type must be 'image' or 'video'")
	}

	return s.sendStatusMedia(client, data, fileData, instance)
}

func (s *sendService) sendStatusMedia(client *whatsmeow.Client, data *StatusMediaStruct, fileData []byte, instance *instance_model.Instance) (*MessageSendStruct, error) {
	mime, _ := mimetype.DetectReader(bytes.NewReader(fileData))
	mimeType := mime.String()

	var uploadType whatsmeow.MediaType
	switch data.Type {
	case "image":
		if mimeType != "image/jpeg" && mimeType != "image/png" && mimeType != "image/webp" {
			return nil, apierror.Invalid(fmt.Sprintf("invalid file format: '%s'. Only 'image/jpeg', 'image/png' and 'image/webp' are accepted", mimeType))
		}
		if mimeType == "image/webp" {
			mimeType = "image/jpeg"
		}
		uploadType = whatsmeow.MediaImage
	case "video":
		if mimeType != "video/mp4" {
			return nil, apierror.Invalid(fmt.Sprintf("invalid file format: '%s'. Only 'video/mp4' is accepted", mimeType))
		}
		uploadType = whatsmeow.MediaVideo
	default:
		return nil, apierror.Invalid("invalid media type")
	}

	uploaded, err := client.Upload(context.Background(), fileData, uploadType)
	if err != nil {
		return nil, err
	}

	s.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] Status media uploaded, size: %d", instance.Id, uploaded.FileLength)

	var media *waE2E.Message
	var mediaType string

	switch data.Type {
	case "image":
		// Generate a JPEG preview thumbnail so status/story images render an
		// inline preview instead of the gray camera placeholder. On failure
		// jpegThumb is nil and the status is posted without a preview.
		jpegThumb := makeJPEGThumbnail(fileData, 72)
		// Width/Height let the client size the bubble before the media downloads (#104).
		imgW, imgH := imageDimensions(fileData)
		media = &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
			Caption:       proto.String(data.Caption),
			URL:           proto.String(uploaded.URL),
			DirectPath:    proto.String(uploaded.DirectPath),
			MediaKey:      uploaded.MediaKey,
			Mimetype:      proto.String(mimeType),
			FileEncSHA256: uploaded.FileEncSHA256,
			FileSHA256:    uploaded.FileSHA256,
			FileLength:    proto.Uint64(uint64(len(fileData))),
			JPEGThumbnail: jpegThumb,
			Width:         imgW,
			Height:        imgH,
		}}
		mediaType = "ImageMessage"
	case "video":
		media = &waE2E.Message{VideoMessage: &waE2E.VideoMessage{
			Caption:       proto.String(data.Caption),
			URL:           proto.String(uploaded.URL),
			DirectPath:    proto.String(uploaded.DirectPath),
			MediaKey:      uploaded.MediaKey,
			Mimetype:      proto.String(mimeType),
			FileEncSHA256: uploaded.FileEncSHA256,
			FileSHA256:    uploaded.FileSHA256,
			FileLength:    proto.Uint64(uint64(len(fileData))),
		}}
		mediaType = "VideoMessage"
	}

	messageID := data.Id
	if messageID == "" {
		messageID = client.GenerateMessageID()
	}

	recipient := types.NewJID("status", "broadcast")

	response, err := s.sendToWhatsApp(instance.Id, client, recipient, media, whatsmeow.SendRequestExtra{ID: messageID})
	if err != nil {
		return nil, err
	}

	messageInfo := types.MessageInfo{
		MessageSource: types.MessageSource{
			Chat:     recipient,
			Sender:   *client.Store.ID,
			IsFromMe: true,
			IsGroup:  false,
		},
		ID:        messageID,
		Timestamp: time.Now(),
		ServerID:  response.ServerID,
		Type:      mediaType,
	}

	messageSent := &MessageSendStruct{
		Info:    messageInfo,
		Message: media,
		MessageContextInfo: &waE2E.ContextInfo{
			StanzaID:      proto.String(""),
			Participant:   proto.String(""),
			QuotedMessage: &waE2E.Message{Conversation: proto.String("")},
		},
	}

	s.sendStatusWebhook(messageSent, instance, "media")
	return messageSent, nil
}

func (s *sendService) sendStatusWebhook(messageSent *MessageSendStruct, instance *instance_model.Instance, messageType string) {
	postMap := make(map[string]interface{})
	postMap["event"] = "SendStatus"
	messageData := make(map[string]interface{})
	messageData["Info"] = messageSent.Info
	msgBytes, err := json.Marshal(messageSent.Message)
	if err != nil {
		s.loggerWrapper.GetLogger(instance.Id).LogError("[%s] Failed to marshal status message: %v", instance.Id, err)
		return
	}
	var msgMap map[string]interface{}
	if err := json.Unmarshal(msgBytes, &msgMap); err != nil {
		s.loggerWrapper.GetLogger(instance.Id).LogError("[%s] Failed to unmarshal status message: %v", instance.Id, err)
		return
	}
	messageData["Message"] = msgMap
	messageData["MessageContextInfo"] = messageSent.MessageContextInfo
	postMap["data"] = messageData
	s.config.AddInstanceToken(postMap, instance.Token)
	postMap["instanceId"] = instance.Id
	postMap["instanceName"] = instance.Name

	values, err := json.Marshal(postMap)
	if err != nil {
		s.loggerWrapper.GetLogger(instance.Id).LogError("[%s] Failed to marshal webhook payload: %v", instance.Id, err)
		return
	}
	go s.whatsmeowService.CallWebhook(instance, "sendstatus", values)
	if s.config.AmqpGlobalEnabled || s.config.NatsGlobalEnabled {
		go s.whatsmeowService.SendToGlobalQueues("SendStatus", values, instance.Id)
	}
	s.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] Status %s sent successfully", instance.Id, messageType)
}

func NewSendService(
	clientPointer *safemap.Map[*whatsmeow.Client],
	whatsmeowService whatsmeow_service.WhatsmeowService,
	config *config.Config,
	loggerWrapper *logger_wrapper.LoggerManager,
) SendService {
	return &sendService{
		clientPointer:    clientPointer,
		whatsmeowService: whatsmeowService,
		config:           config,
		loggerWrapper:    loggerWrapper,
		existsCache:      newUserExistsCache(config.CheckUserCacheTTL),
		throttle:         newSendThrottle(throttleConfigFromEnv()),
	}
}
