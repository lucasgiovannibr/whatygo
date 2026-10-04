package send_handler

import (
	"io"
	"mime/multipart"
)

// readUpload reads an uploaded file into a buffer of exactly its size. io.ReadAll grows its
// buffer by doubling, so a 100 MB file took about 200 MB while it was being read.
func readUpload(file *multipart.FileHeader) ([]byte, error) {
	f, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer f.Close()

	if file.Size <= 0 {
		return io.ReadAll(f) // size unknown
	}
	data := make([]byte, file.Size)
	if _, err := io.ReadFull(f, data); err != nil {
		return nil, err
	}
	return data, nil
}
