package utils

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"go.mau.fi/whatsmeow"
)

// Media that WhatsApp contacts send is downloaded by the server, and the contact decides how
// big it is (a document may be 2 GB). whatsmeow's Download reads the whole encrypted file into
// memory and decrypts it into a second buffer; base64 for the event and its JSON then add
// roughly four more copies (measured: about 7 times the file at the peak). A single large
// document therefore took the whole process, and every instance with it, down.
//
// DownloadMedia writes the file to a temporary file instead (whatsmeow.DownloadToFile streams,
// the memory used does not depend on the size), and the file may not grow past a limit: the
// size a message announces is only the sender's word, so the limit is enforced on what is
// actually written.

// DefaultMaxReceivedMediaMB is the limit when MAX_RECEIVED_MEDIA_MB is not set.
const DefaultMaxReceivedMediaMB = 50

// MaxReceivedMediaBytes is the largest media file taken from a received message
// (MAX_RECEIVED_MEDIA_MB, default 50).
func MaxReceivedMediaBytes() int64 {
	if mb, err := strconv.ParseInt(strings.TrimSpace(os.Getenv("MAX_RECEIVED_MEDIA_MB")), 10, 64); err == nil && mb > 0 {
		return mb << 20
	}
	return DefaultMaxReceivedMediaMB << 20
}

// cappedFile is the file whatsmeow writes the download to, refusing to grow past max. It
// deliberately has only the methods whatsmeow.File asks for: embedding *os.File would add
// ReadFrom, which io.Copy prefers and which would write around the limit.
type cappedFile struct {
	f       *os.File
	max     int64
	written int64 // bytes written since the file was last rewound
}

func (c *cappedFile) Read(p []byte) (int, error)              { return c.f.Read(p) }
func (c *cappedFile) ReadAt(p []byte, off int64) (int, error) { return c.f.ReadAt(p, off) }
func (c *cappedFile) Truncate(size int64) error               { return c.f.Truncate(size) }
func (c *cappedFile) Stat() (os.FileInfo, error)              { return c.f.Stat() }

func (c *cappedFile) Seek(offset int64, whence int) (int64, error) {
	pos, err := c.f.Seek(offset, whence)
	if err == nil && pos == 0 {
		c.written = 0 // whatsmeow rewinds to retry a download or to read the file back
	}
	return pos, err
}

func (c *cappedFile) Write(p []byte) (int, error) {
	if c.written+int64(len(p)) > c.max {
		return 0, fmt.Errorf("%w: more than %d bytes", ErrDownloadTooLarge, c.max)
	}
	n, err := c.f.Write(p)
	c.written += int64(n)
	return n, err
}

func (c *cappedFile) WriteAt(p []byte, off int64) (int, error) {
	if off+int64(len(p)) > c.max {
		return 0, fmt.Errorf("%w: more than %d bytes", ErrDownloadTooLarge, c.max)
	}
	return c.f.WriteAt(p, off)
}

// MediaFile is a downloaded media file kept on disk. Close removes it.
type MediaFile struct {
	f    *os.File
	size int64
}

// DownloadMedia runs fetch, which has to write the media to the file it is given
// (client.DownloadToFile), and returns the result. announced is the size the message says the
// file has (0 when unknown): a larger one is refused without downloading anything. In every
// case the file may not grow past limit, whatever was announced; both give an error that wraps
// ErrDownloadTooLarge.
func DownloadMedia(ctx context.Context, announced, limit int64, fetch func(context.Context, whatsmeow.File) error) (*MediaFile, error) {
	if announced > limit {
		return nil, fmt.Errorf("%w: %d bytes announced (limit %d)", ErrDownloadTooLarge, announced, limit)
	}
	tmp, err := os.CreateTemp("", "whatygo-media-*")
	if err != nil {
		return nil, fmt.Errorf("cannot create a temporary file for the media (is the temp directory writable?): %w", err)
	}
	m := &MediaFile{f: tmp}
	if err := fetch(ctx, &cappedFile{f: tmp, max: limit}); err != nil {
		m.Close()
		return nil, err
	}
	info, err := tmp.Stat()
	if err != nil {
		m.Close()
		return nil, err
	}
	m.size = info.Size()
	return m, nil
}

// Size is the length of the media in bytes.
func (m *MediaFile) Size() int64 { return m.size }

// Bytes reads the whole file into memory (one copy, sized exactly).
func (m *MediaFile) Bytes() ([]byte, error) {
	if _, err := m.f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	data := make([]byte, m.size)
	if _, err := io.ReadFull(m.f, data); err != nil {
		return nil, err
	}
	return data, nil
}

// Base64 is the standard base64 encoding of the file, built straight from the file into one
// string: no copy of the raw bytes and no intermediate encoded buffer.
func (m *MediaFile) Base64() (string, error) {
	if _, err := m.f.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	var sb strings.Builder
	sb.Grow(base64.StdEncoding.EncodedLen(int(m.size)))
	enc := base64.NewEncoder(base64.StdEncoding, &sb)
	if _, err := io.Copy(enc, io.LimitReader(m.f, m.size)); err != nil {
		return "", err
	}
	if err := enc.Close(); err != nil {
		return "", err
	}
	return sb.String(), nil
}

// Close closes the file and deletes it.
func (m *MediaFile) Close() {
	name := m.f.Name()
	_ = m.f.Close()
	_ = os.Remove(name)
}

// IsMediaTooLarge reports whether err is the refusal of a media file for its size.
func IsMediaTooLarge(err error) bool { return errors.Is(err, ErrDownloadTooLarge) }
