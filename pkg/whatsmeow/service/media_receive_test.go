package whatsmeow_service

import (
	"context"
	"encoding/base64"
	"runtime"
	"testing"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	"github.com/lucasgiovannibr/whatygo/pkg/config"
	"github.com/lucasgiovannibr/whatygo/pkg/utils"
)

func TestPickMediaDescribesEachKind(t *testing.T) {
	cases := []struct {
		name      string
		msg       *waE2E.Message
		kind, ext string
		mime      string
		size      int64
		sticker   bool
	}{
		{"image", &waE2E.Message{ImageMessage: &waE2E.ImageMessage{FileLength: proto.Uint64(10)}}, "image", ".jpg", "image/jpeg", 10, false},
		{"audio", &waE2E.Message{AudioMessage: &waE2E.AudioMessage{FileLength: proto.Uint64(20)}}, "audio", ".ogg", "audio/ogg", 20, false},
		{"video", &waE2E.Message{VideoMessage: &waE2E.VideoMessage{FileLength: proto.Uint64(30)}}, "video", ".mp4", "video/mp4", 30, false},
		{"sticker", &waE2E.Message{StickerMessage: &waE2E.StickerMessage{FileLength: proto.Uint64(40)}}, "sticker", ".png", "image/png", 40, true},
		{"document", &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{Mimetype: proto.String("application/pdf"), FileLength: proto.Uint64(50)}}, "document", getExtensionFromMimeType("application/pdf"), "application/pdf", 50, false},
		{"unknown size", &waE2E.Message{ImageMessage: &waE2E.ImageMessage{}}, "image", ".jpg", "image/jpeg", 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, ok := pickMedia(c.msg)
			if !ok {
				t.Fatal("media not found")
			}
			if m.kind != c.kind || m.extension != c.ext || m.mimeType != c.mime || m.size != c.size || m.sticker != c.sticker || m.child || m.target == nil {
				t.Fatalf("%+v", m)
			}
		})
	}
}

// The message itself wins over the child it carries, and among the kinds the order is the
// one the handler always had: image, audio, document, video, sticker.
func TestPickMediaPrecedence(t *testing.T) {
	both := &waE2E.Message{
		VideoMessage:           &waE2E.VideoMessage{},
		StickerMessage:         &waE2E.StickerMessage{},
		AssociatedChildMessage: &waE2E.FutureProofMessage{Message: &waE2E.Message{ImageMessage: &waE2E.ImageMessage{}}},
	}
	m, _ := pickMedia(both)
	if m.kind != "video" || m.child {
		t.Fatalf("got %+v", m)
	}
	m, _ = pickMedia(&waE2E.Message{AudioMessage: &waE2E.AudioMessage{}, ImageMessage: &waE2E.ImageMessage{}})
	if m.kind != "image" {
		t.Fatalf("image is looked at first, got %s", m.kind)
	}
}

func TestPickMediaInAChildMessage(t *testing.T) {
	m, ok := pickMedia(&waE2E.Message{AssociatedChildMessage: &waE2E.FutureProofMessage{Message: &waE2E.Message{StickerMessage: &waE2E.StickerMessage{}}}})
	if !ok || !m.child || !m.sticker {
		t.Fatalf("%+v %v", m, ok)
	}
}

func TestPickMediaNothing(t *testing.T) {
	for _, m := range []*waE2E.Message{nil, {}, {Conversation: proto.String("hi")}, {AssociatedChildMessage: &waE2E.FutureProofMessage{}}} {
		if _, ok := pickMedia(m); ok {
			t.Fatalf("%+v has no media", m)
		}
	}
}

// A file announced above MAX_RECEIVED_MEDIA_MB is not downloaded (the WhatsApp client is not even
// touched: it is nil here), and the event says why the file is missing.
func TestAttachMediaSkipsAFileOverTheLimit(t *testing.T) {
	t.Setenv("MAX_RECEIVED_MEDIA_MB", "1")
	mycli, _ := newHandlerClient(t, &config.Config{})
	evt := privateMessage("BIG", "")
	evt.Message = &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{
		Mimetype:   proto.String("application/pdf"),
		FileLength: proto.Uint64(2 << 30), // the 2 GB a document may have
	}}

	data := map[string]interface{}{}
	mycli.attachMedia(evt, data)

	msg, ok := data["Message"].(map[string]interface{})
	if !ok {
		t.Fatalf("the event must carry a Message section: %#v", data)
	}
	if msg["mediaSkipped"] != "too_large" || msg["mediaSize"] != int64(2<<30) || msg["mediaLimit"] != int64(1<<20) {
		t.Fatalf("unexpected: %#v", msg)
	}
	if _, has := msg["base64"]; has {
		t.Fatal("no file may be attached")
	}
}

// Encoding the downloaded file allocates the encoded string and little else: the old path made
// a copy of the raw bytes, an encoded buffer and the string.
func TestBase64OfAMediaFileAllocatesOnlyTheString(t *testing.T) {
	const size = 24 << 20
	raw := make([]byte, size)
	for i := range raw {
		raw[i] = byte(i)
	}
	file, err := utils.DownloadMedia(context.Background(), size, 2*size, func(_ context.Context, f whatsmeow.File) error {
		_, err := f.Write(raw)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	enc, err := file.Base64()
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(enc), base64.StdEncoding.EncodedLen(size); got != want {
		t.Fatalf("encoded length %d, want %d", got, want)
	}
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > uint64(len(enc))*12/10 {
		t.Fatalf("allocated %d bytes to encode a %d byte string: more than the string itself", allocated, len(enc))
	}
}
