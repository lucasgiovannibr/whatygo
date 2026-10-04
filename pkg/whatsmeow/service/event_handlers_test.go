package whatsmeow_service

import (
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/patrickmn/go-cache"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/lucasgiovannibr/whatygo/pkg/config"
	instance_repository "github.com/lucasgiovannibr/whatygo/pkg/instance/repository"
	"github.com/lucasgiovannibr/whatygo/pkg/passkey/ceremony"
)

// The received-message and receipt cases live in their own methods; these tests drive them through
// the handler the way whatsmeow does, and pin what comes out the other side.

func privateMessage(id, text string) *events.Message {
	chat := jid("5511999999999@s.whatsapp.net")
	return &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{Chat: chat, Sender: chat},
			ID:            id,
			Timestamp:     time.Now(),
		},
		Message: &waE2E.Message{Conversation: proto.String(text)},
	}
}

func receiptClient(t *testing.T) (*MyClient, *webhookCapture) {
	t.Helper()
	mycli, capture := newHandlerClient(t, &config.Config{})
	mycli.processedMessages = cache.New(time.Minute, time.Minute)
	return mycli, capture
}

func expectNothing(t *testing.T, c *webhookCapture) {
	t.Helper()
	select {
	case p := <-c.published:
		t.Fatalf("nothing should have been published, got %s %v", p.queue, p.body["event"])
	case <-time.After(150 * time.Millisecond):
	}
}

func TestReceivedTextMessageIsPublished(t *testing.T) {
	mycli, capture := receiptClient(t)
	mycli.myEventHandler(privateMessage("M1", "hello"))

	p := waitPublished(t, capture)
	if p.queue != "inst-1.message" || p.body["event"] != "Message" || p.body["instanceId"] != "inst-1" {
		t.Fatalf("%s %v", p.queue, p.body)
	}
	if _, ok := p.body["data"].(map[string]interface{}); !ok {
		t.Fatalf("data: %T", p.body["data"])
	}
}

func TestReceivedMessageRespectsIgnoreGroups(t *testing.T) {
	mycli, capture := receiptClient(t)
	mycli.inst().IgnoreGroups = true

	evt := privateMessage("M2", "in a group")
	evt.Info.Chat = jid("120363000000000000@g.us")
	evt.Info.IsGroup = true
	mycli.myEventHandler(evt)
	expectNothing(t, capture)
}

func TestReceivedMessageNobodySubscribedIsNotPublished(t *testing.T) {
	mycli, capture := receiptClient(t)
	capture.nobodyWants = true
	mycli.myEventHandler(privateMessage("M3", "hello"))
	expectNothing(t, capture)
}

// A button answer goes out twice: as the message and as a ButtonClick event of its own.
func TestButtonAnswerEmitsMessageAndButtonClick(t *testing.T) {
	mycli, capture := receiptClient(t)
	evt := privateMessage("M4", "")
	evt.Message = &waE2E.Message{ButtonsResponseMessage: &waE2E.ButtonsResponseMessage{
		SelectedButtonID: proto.String("b1"),
		Response:         &waE2E.ButtonsResponseMessage_SelectedDisplayText{SelectedDisplayText: "Yes"},
	}}
	mycli.myEventHandler(evt)

	got := map[string]published{}
	for i := 0; i < 2; i++ {
		p := waitPublished(t, capture)
		got[p.queue] = p
	}
	if _, ok := got["inst-1.message"]; !ok {
		t.Fatalf("no Message event: %v", got)
	}
	click, ok := got["inst-1.buttonclick"]
	if !ok {
		t.Fatalf("no ButtonClick event: %v", got)
	}
	data, _ := click.body["data"].(map[string]interface{})
	if data["buttonId"] != "b1" || data["buttonText"] != "Yes" || data["type"] != "buttons_response" {
		t.Fatalf("click: %v", data)
	}
}

func receipt(kind types.ReceiptType, ids ...string) *events.Receipt {
	chat := jid("5511999999999@s.whatsapp.net")
	return &events.Receipt{
		MessageSource: types.MessageSource{Chat: chat, Sender: chat},
		MessageIDs:    ids,
		Timestamp:     time.Now(),
		Type:          kind,
	}
}

func TestReadReceiptIsPublishedWithItsState(t *testing.T) {
	mycli, capture := receiptClient(t)
	mycli.myEventHandler(receipt(types.ReceiptTypeRead, "A"))
	p := waitPublished(t, capture)
	if p.body["event"] != "Receipt" || p.body["state"] != "Read" {
		t.Fatalf("%v", p.body)
	}
}

func TestDeliveredReceiptIsDeduplicated(t *testing.T) {
	mycli, capture := receiptClient(t)
	mycli.myEventHandler(receipt(types.ReceiptTypeDelivered, "B"))
	if p := waitPublished(t, capture); p.body["state"] != "Delivered" {
		t.Fatalf("%v", p.body)
	}
	mycli.myEventHandler(receipt(types.ReceiptTypeDelivered, "B")) // the same receipt again
	expectNothing(t, capture)
}

// Receipts that are neither read nor delivered (played, sender...) are not events.
func TestOtherReceiptTypesAreNotPublished(t *testing.T) {
	mycli, capture := receiptClient(t)
	mycli.myEventHandler(receipt(types.ReceiptTypePlayed, "C"))
	expectNothing(t, capture)
}

func TestReceiptRespectsIgnoreGroups(t *testing.T) {
	mycli, capture := receiptClient(t)
	mycli.inst().IgnoreGroups = true
	r := receipt(types.ReceiptTypeRead, "D")
	r.Chat = jid("120363000000000000@g.us")
	mycli.myEventHandler(r)
	expectNothing(t, capture)
}

// Pairing records the new state in one statement and publishes PairSuccess with the status.
func TestPairSuccessMarksTheInstancePairedAndPublishes(t *testing.T) {
	mycli, capture := newHandlerClient(t, &config.Config{})
	mycli.userInfoCache = cache.New(time.Minute, time.Minute)
	mycli.passkeyCeremony = ceremony.NewStore()

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	gdb, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	mycli.instanceRepository = instance_repository.NewInstanceRepository(gdb)
	mycli.userID = testInstanceID
	mycli.inst().Id = testInstanceID

	device := jid("5511999999999:7@s.whatsapp.net")
	mycli.WAClient = whatsmeow.NewClient(&store.Device{ID: &device}, waLog.Noop)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "instances" WHERE id = $1`)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(testInstanceID))
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "instances" SET "connected"=$1,"disconnect_reason"=$2,"jid"=$3,"qrcode"=$4 WHERE id = $5`)).
		WithArgs(true, "", device.String(), "", testInstanceID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	mycli.myEventHandler(&events.PairSuccess{ID: device, BusinessName: "", Platform: "chrome"})

	p := waitPublished(t, capture)
	if p.body["event"] != "PairSuccess" {
		t.Fatalf("%v", p.body)
	}
	data, _ := p.body["data"].(map[string]interface{})
	if data["status"] != "open" || data["jid"] != device.String() {
		t.Fatalf("data: %v", data)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
