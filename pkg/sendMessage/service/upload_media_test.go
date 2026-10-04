package send_service

import (
	"bytes"
	"context"
	"io"
	"os"
	"testing"

	"go.mau.fi/whatsmeow"
)

type fakeUploader struct {
	uploaded     int
	streamed     int
	streamedData []byte
	tempWasFile  bool
}

func (f *fakeUploader) Upload(_ context.Context, _ []byte, _ whatsmeow.MediaType) (whatsmeow.UploadResponse, error) {
	f.uploaded++
	return whatsmeow.UploadResponse{URL: "plain"}, nil
}

func (f *fakeUploader) UploadReader(_ context.Context, r io.Reader, tmp io.ReadWriteSeeker, _ whatsmeow.MediaType) (whatsmeow.UploadResponse, error) {
	f.streamed++
	_, f.tempWasFile = tmp.(*os.File)
	f.streamedData, _ = io.ReadAll(r)
	return whatsmeow.UploadResponse{URL: "stream"}, nil
}

func TestSmallFilesUseUploadAndLargeOnesStream(t *testing.T) {
	f := &fakeUploader{}
	resp, err := uploadMedia(context.Background(), f, make([]byte, 1<<20), whatsmeow.MediaImage)
	if err != nil || resp.URL != "plain" || f.uploaded != 1 || f.streamed != 0 {
		t.Fatalf("a small file goes through Upload: %v %+v %+v", err, resp, f)
	}

	big := bytes.Repeat([]byte{7}, streamUploadThreshold)
	f = &fakeUploader{}
	resp, err = uploadMedia(context.Background(), f, big, whatsmeow.MediaVideo)
	if err != nil || resp.URL != "stream" || f.streamed != 1 || f.uploaded != 0 {
		t.Fatalf("a large file is streamed: %v %+v %+v", err, resp, f)
	}
	if !f.tempWasFile || !bytes.Equal(f.streamedData, big) {
		t.Fatal("the plaintext must reach the uploader through a temporary file")
	}
}

func TestStreamedUploadRemovesItsTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	t.Setenv("TEMP", dir)
	t.Setenv("TMP", dir)
	f := &fakeUploader{}
	if _, err := uploadMedia(context.Background(), f, make([]byte, streamUploadThreshold), whatsmeow.MediaDocument); err != nil {
		t.Fatal(err)
	}
	left, _ := os.ReadDir(dir)
	if len(left) != 0 {
		t.Fatalf("the temporary file must be removed, left %d", len(left))
	}
}

func TestWithoutATemporaryDirectoryItFallsBackToUpload(t *testing.T) {
	t.Setenv("TMPDIR", "/nonexistent-dir-for-test")
	t.Setenv("TEMP", "/nonexistent-dir-for-test")
	t.Setenv("TMP", "/nonexistent-dir-for-test")
	f := &fakeUploader{}
	resp, err := uploadMedia(context.Background(), f, make([]byte, streamUploadThreshold), whatsmeow.MediaDocument)
	if err != nil || resp.URL != "plain" || f.uploaded != 1 {
		t.Fatalf("must fall back to Upload: %v %+v %+v", err, resp, f)
	}
}
