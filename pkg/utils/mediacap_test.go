package utils

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"testing"

	"go.mau.fi/whatsmeow"
)

// fetchOf returns a fetch function that copies data into the file the way whatsmeow does
// (io.Copy, which would use a ReadFrom method if the file had one).
func fetchOf(data []byte) func(context.Context, whatsmeow.File) error {
	return func(_ context.Context, f whatsmeow.File) error {
		_, err := io.Copy(f, bytes.NewReader(data))
		return err
	}
}

func TestMaxReceivedMediaBytes(t *testing.T) {
	t.Setenv("MAX_RECEIVED_MEDIA_MB", "")
	if got := MaxReceivedMediaBytes(); got != 50<<20 {
		t.Fatalf("default: %d", got)
	}
	t.Setenv("MAX_RECEIVED_MEDIA_MB", "8")
	if got := MaxReceivedMediaBytes(); got != 8<<20 {
		t.Fatalf("set: %d", got)
	}
	for _, bad := range []string{"0", "-3", "abc"} {
		t.Setenv("MAX_RECEIVED_MEDIA_MB", bad)
		if got := MaxReceivedMediaBytes(); got != 50<<20 {
			t.Fatalf("%q must give the default, got %d", bad, got)
		}
	}
}

func TestDownloadMediaKeepsWhatWasWritten(t *testing.T) {
	data := bytes.Repeat([]byte("whatygo"), 1000)
	m, err := DownloadMedia(context.Background(), int64(len(data)), 1<<20, fetchOf(data))
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if m.Size() != int64(len(data)) {
		t.Fatalf("size %d", m.Size())
	}
	got, err := m.Bytes()
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("bytes differ (%v)", err)
	}
	enc, err := m.Base64()
	if err != nil || enc != base64.StdEncoding.EncodeToString(data) {
		t.Fatalf("base64 differs (%v)", err)
	}
	// reading twice works: the file is rewound each time
	if again, _ := m.Bytes(); !bytes.Equal(again, data) {
		t.Fatal("second read differs")
	}
}

func TestDownloadMediaRefusesAnAnnouncedSizeWithoutDownloading(t *testing.T) {
	called := false
	_, err := DownloadMedia(context.Background(), 11, 10, func(context.Context, whatsmeow.File) error {
		called = true
		return nil
	})
	if !IsMediaTooLarge(err) || !errors.Is(err, ErrDownloadTooLarge) {
		t.Fatalf("want too large, got %v", err)
	}
	if called {
		t.Fatal("nothing may be fetched when the announced size is over the limit")
	}
}

// The announced size is the sender's word: the limit has to hold for what is written.
func TestDownloadMediaCapsWhatIsActuallyWritten(t *testing.T) {
	const limit = 1000
	var path string
	m, err := DownloadMedia(context.Background(), 10 /* a lie */, limit, func(_ context.Context, f whatsmeow.File) error {
		path = f.(*cappedFile).f.Name()
		_, err := io.Copy(f, io.LimitReader(zeroReader{}, 5*limit))
		return err
	})
	if m != nil || !IsMediaTooLarge(err) {
		t.Fatalf("want too large, got %v %v", m, err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("the temporary file must be removed after a refusal (%v)", statErr)
	}
}

func TestDownloadMediaLimitIsPerAttemptNotPerByteEverWritten(t *testing.T) {
	const limit = 100
	data := bytes.Repeat([]byte{7}, 80)
	// whatsmeow rewinds the file to retry a download: 80 bytes twice is fine, 160 at once is not
	m, err := DownloadMedia(context.Background(), 0, limit, func(_ context.Context, f whatsmeow.File) error {
		if _, err := f.Write(data); err != nil {
			return err
		}
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return err
		}
		_, err := f.Write(data)
		return err
	})
	if err != nil {
		t.Fatalf("a retry that rewinds must not count the first attempt: %v", err)
	}
	m.Close()

	_, err = DownloadMedia(context.Background(), 0, limit, func(_ context.Context, f whatsmeow.File) error {
		if _, err := f.Write(data); err != nil {
			return err
		}
		_, err := f.Write(data)
		return err
	})
	if !IsMediaTooLarge(err) {
		t.Fatalf("160 bytes in one attempt must be refused, got %v", err)
	}
}

func TestDownloadMediaPropagatesFetchErrorAndCleansUp(t *testing.T) {
	boom := errors.New("boom")
	var path string
	_, err := DownloadMedia(context.Background(), 0, 100, func(_ context.Context, f whatsmeow.File) error {
		path = f.(*cappedFile).f.Name()
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("got %v", err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatal("temporary file left behind")
	}
}

func TestMediaFileCloseDeletesTheFile(t *testing.T) {
	var path string
	m, err := DownloadMedia(context.Background(), 0, 100, func(_ context.Context, f whatsmeow.File) error {
		path = f.(*cappedFile).f.Name()
		_, err := f.Write([]byte("x"))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	m.Close()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("Close must delete the file")
	}
}

// cappedFile must not offer ReadFrom: io.Copy would use it and write around the limit.
func TestCappedFileHasNoReadFrom(t *testing.T) {
	var f interface{} = &cappedFile{}
	if _, ok := f.(io.ReaderFrom); ok {
		t.Fatal("cappedFile must not implement io.ReaderFrom")
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}
