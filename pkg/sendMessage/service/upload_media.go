package send_service

import (
	"bytes"
	"context"
	"io"
	"os"

	"go.mau.fi/whatsmeow"
)

// streamUploadThreshold is the size from which a file is uploaded through a temporary file.
// whatsmeow's Upload keeps the plaintext, the ciphertext and a copy of it with the MAC appended
// in memory at once (about three times the file on top of the file itself); UploadReader
// encrypts as it streams into a file, so its memory does not depend on the size.
const streamUploadThreshold = 8 << 20

// mediaUploader is the part of *whatsmeow.Client uploadMedia uses.
type mediaUploader interface {
	Upload(ctx context.Context, plaintext []byte, appInfo whatsmeow.MediaType) (whatsmeow.UploadResponse, error)
	UploadReader(ctx context.Context, plaintext io.Reader, tempFile io.ReadWriteSeeker, appInfo whatsmeow.MediaType) (whatsmeow.UploadResponse, error)
}

// uploadMedia uploads (encrypted) a media file. Small files go through Upload; large ones
// through a temporary file. Without a writable temporary directory it falls back to Upload.
func uploadMedia(ctx context.Context, client mediaUploader, data []byte, kind whatsmeow.MediaType) (whatsmeow.UploadResponse, error) {
	if len(data) < streamUploadThreshold {
		return client.Upload(ctx, data, kind)
	}
	tmp, err := os.CreateTemp("", "whatygo-upload-*")
	if err != nil {
		return client.Upload(ctx, data, kind)
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()
	return client.UploadReader(ctx, bytes.NewReader(data), tmp, kind)
}
