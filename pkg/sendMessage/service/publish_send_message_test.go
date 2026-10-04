package send_service

import (
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"

	"github.com/lucasgiovannibr/whatygo/pkg/config"
	instance_model "github.com/lucasgiovannibr/whatygo/pkg/instance/model"
	logger_wrapper "github.com/lucasgiovannibr/whatygo/pkg/logger"
	whatsmeow_service "github.com/lucasgiovannibr/whatygo/pkg/whatsmeow/service"
)

type eventSink struct {
	whatsmeow_service.WhatsmeowService
	wanted bool
	got    chan []byte
}

func (e *eventSink) EventWanted(*instance_model.Instance, string, string) bool { return e.wanted }
func (e *eventSink) CallWebhook(_ *instance_model.Instance, _ string, jsonData []byte) {
	e.got <- jsonData
}
func (e *eventSink) SendToGlobalQueues(string, []byte, string) {}

func newPublishService(t *testing.T, sink *eventSink, webhookFiles bool) *sendService {
	t.Helper()
	cfg := &config.Config{LogDirectory: t.TempDir(), WebhookFiles: webhookFiles}
	return &sendService{config: cfg, loggerWrapper: logger_wrapper.NewLoggerManagerForTest(t, cfg), whatsmeowService: sink}
}

func sentImage() *MessageSendStruct {
	return &MessageSendStruct{
		Info: types.MessageInfo{ID: "M1", MessageSource: types.MessageSource{Chat: types.NewJID("5511999990001", types.DefaultUserServer)}},
		Message: &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
			Mimetype: proto.String("image/jpeg"), FileLength: proto.Uint64(3),
		}},
	}
}

// The file that was just uploaded is in memory already: the event carries it without a
// second trip to WhatsApp (client is nil here, a download would panic).
func TestSendMessageEventCarriesTheBytesTheCallerHas(t *testing.T) {
	sink := &eventSink{wanted: true, got: make(chan []byte, 1)}
	s := newPublishService(t, sink, true)
	file := []byte{0xff, 0xd8, 0xff, 0x01}

	s.publishSendMessage(&instance_model.Instance{Id: "i", Name: "n", Token: "tok"}, nil, sentImage(), true, file)

	select {
	case raw := <-sink.got:
		var payload struct {
			Event string `json:"event"`
			Data  struct {
				Message map[string]interface{} `json:"Message"`
			} `json:"data"`
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatal(err)
		}
		if payload.Event != "SendMessage" {
			t.Fatalf("event = %q", payload.Event)
		}
		if payload.Data.Message["base64"] != base64.StdEncoding.EncodeToString(file) {
			t.Fatalf("the event must carry the uploaded file, got %v", payload.Data.Message["base64"])
		}
	case <-time.After(time.Second):
		t.Fatal("the event was not published")
	}
}

func TestSendMessageEventIsNotBuiltWhenNobodyWantsIt(t *testing.T) {
	sink := &eventSink{wanted: false, got: make(chan []byte, 1)}
	s := newPublishService(t, sink, true)

	s.publishSendMessage(&instance_model.Instance{Id: "i"}, nil, sentImage(), true, []byte{1})

	select {
	case <-sink.got:
		t.Fatal("an event nobody receives must not be built or delivered")
	case <-time.After(150 * time.Millisecond):
	}
}

func TestSendMessageEventWithoutFilesOmitsTheMedia(t *testing.T) {
	sink := &eventSink{wanted: true, got: make(chan []byte, 1)}
	s := newPublishService(t, sink, false) // WEBHOOK_FILES=false

	s.publishSendMessage(&instance_model.Instance{Id: "i"}, nil, sentImage(), true, []byte{1, 2, 3})

	raw := <-sink.got
	var payload struct {
		Data struct {
			Message map[string]interface{} `json:"Message"`
		} `json:"data"`
	}
	_ = json.Unmarshal(raw, &payload)
	if _, has := payload.Data.Message["base64"]; has {
		t.Fatal("WEBHOOK_FILES=false must not embed the file")
	}
}
