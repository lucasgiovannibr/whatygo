package minio_storage

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"path"
	"strings"
	"time"

	storage_interfaces "github.com/lucasgiovannibr/whatygo/pkg/storage/interfaces"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// mediaFolder is the prefix of every object this service writes. Below it each instance has
// its own folder: message ids are not unique across accounts (two instances in the same
// group receive the same message), so a flat <messageID>.<ext> let one instance overwrite
// another's object, and made it impossible to remove one instance's files.
const mediaFolder = "whatygo-medias"

// maxPresignTTL is the longest an S3 presigned URL can live (7 days).
const maxPresignTTL = 7 * 24 * time.Hour

// Options configure the storage.
type Options struct {
	Endpoint, AccessKey, SecretKey, Bucket, Region string
	UseSSL                                         bool
	// PublicBucket makes every object of the bucket world-readable (MINIO_PUBLIC_BUCKET=true).
	// It used to be done unconditionally: SetBucketPolicy replaces the policy the bucket
	// already had, and exposed everything in it, media of every customer included.
	PublicBucket bool
	// URLTTL is how long the presigned URLs of stored media work (default and maximum 7
	// days).
	URLTTL time.Duration
}

// objectStore is the part of the MinIO client this package uses, so it can be tested.
type objectStore interface {
	BucketExists(ctx context.Context, bucket string) (bool, error)
	MakeBucket(ctx context.Context, bucket string, opts minio.MakeBucketOptions) error
	SetBucketPolicy(ctx context.Context, bucket, policy string) error
	PutObject(ctx context.Context, bucket, object string, data []byte, contentType string) error
	StatObject(ctx context.Context, bucket, object string) error
	RemoveObject(ctx context.Context, bucket, object string) error
	ListObjects(ctx context.Context, bucket, prefix string) ([]string, error)
	PresignedGetObject(ctx context.Context, bucket, object string, expiry time.Duration) (string, error)
}

type MinioMediaStorage struct {
	store      objectStore
	bucketName string
	urlTTL     time.Duration
}

func publicReadPolicy(bucketName string) string {
	return `{
		"Version": "2012-10-17",
		"Statement": [
			{
				"Effect": "Allow",
				"Principal": "*",
				"Action": ["s3:GetObject"],
				"Resource": ["arn:aws:s3:::` + bucketName + `/*"]
			}
		]
	}`
}

// safeName reduces a file name to its last element: it comes from a message id and a
// mime-type extension, but it ends up in an object key.
func safeName(fileName string) string {
	fileName = strings.ReplaceAll(fileName, `\`, "/")
	fileName = path.Base(fileName)
	if fileName == "." || fileName == ".." || fileName == "/" {
		return "file"
	}
	return fileName
}

// objectKey is where a media file of an instance lives. instanceID is a path element: it
// must be one (the id of an instance is a UUID).
func objectKey(instanceID, fileName string) (string, error) {
	if instanceID == "" || instanceID != path.Base(instanceID) || instanceID == "." || instanceID == ".." || strings.ContainsAny(instanceID, `\`) {
		return "", fmt.Errorf("invalid instance id %q for a storage key", instanceID)
	}
	return mediaFolder + "/" + instanceID + "/" + safeName(fileName), nil
}

// NewMinioMediaStorage connects to the S3-compatible storage.
func NewMinioMediaStorage(o Options) (storage_interfaces.MediaStorage, error) {
	client, err := minio.New(o.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(o.AccessKey, o.SecretKey, ""),
		Secure: o.UseSSL,
		Region: o.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create MinIO client: %w", err)
	}
	return newStorage(&minioStore{client}, o)
}

func newStorage(store objectStore, o Options) (*MinioMediaStorage, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	exists, err := store.BucketExists(ctx, o.Bucket)
	if err != nil {
		return nil, fmt.Errorf("cannot reach bucket %q: %w", o.Bucket, err)
	}
	if !exists {
		if err := store.MakeBucket(ctx, o.Bucket, minio.MakeBucketOptions{Region: o.Region}); err != nil {
			return nil, fmt.Errorf("bucket %q does not exist and could not be created: %w", o.Bucket, err)
		}
	}

	// The bucket keeps the policy it has unless the operator asks for a public one.
	if o.PublicBucket {
		if err := store.SetBucketPolicy(ctx, o.Bucket, publicReadPolicy(o.Bucket)); err != nil {
			// Some providers (like Backblaze B2) do not support it; the URLs are presigned anyway.
			fmt.Printf("Warning: MINIO_PUBLIC_BUCKET is set but the bucket policy could not be applied: %v\n", err)
		}
	}

	ttl := o.URLTTL
	if ttl <= 0 || ttl > maxPresignTTL {
		ttl = maxPresignTTL
	}
	return &MinioMediaStorage{store: store, bucketName: o.Bucket, urlTTL: ttl}, nil
}

// Store saves a media file of an instance and returns a presigned URL for it.
func (m *MinioMediaStorage) Store(ctx context.Context, instanceID string, data []byte, fileName string, contentType string) (string, error) {
	key, err := objectKey(instanceID, fileName)
	if err != nil {
		return "", err
	}
	if err := m.store.PutObject(ctx, m.bucketName, key, data, contentType); err != nil {
		return "", fmt.Errorf("failed to store object: %w", err)
	}
	return m.presign(ctx, key)
}

func (m *MinioMediaStorage) presign(ctx context.Context, key string) (string, error) {
	u, err := m.store.PresignedGetObject(ctx, m.bucketName, key, m.urlTTL)
	if err != nil {
		return "", fmt.Errorf("failed to generate presigned URL: %w", err)
	}
	return u, nil
}

// Delete removes one media file of an instance.
func (m *MinioMediaStorage) Delete(ctx context.Context, instanceID string, fileName string) error {
	key, err := objectKey(instanceID, fileName)
	if err != nil {
		return err
	}
	if err := m.store.RemoveObject(ctx, m.bucketName, key); err != nil {
		return fmt.Errorf("failed to delete object: %w", err)
	}
	return nil
}

// GetURL returns a fresh presigned URL for a stored file of an instance.
func (m *MinioMediaStorage) GetURL(ctx context.Context, instanceID string, fileName string) (string, error) {
	key, err := objectKey(instanceID, fileName)
	if err != nil {
		return "", err
	}
	if err := m.store.StatObject(ctx, m.bucketName, key); err != nil {
		return "", fmt.Errorf("failed to get object stats: %w", err)
	}
	return m.presign(ctx, key)
}

// DeleteInstance removes every media file of an instance (it was deleted), and returns how
// many were removed. Files used to stay in the bucket for good.
func (m *MinioMediaStorage) DeleteInstance(ctx context.Context, instanceID string) (int, error) {
	prefix, err := objectKey(instanceID, "x")
	if err != nil {
		return 0, err
	}
	prefix = strings.TrimSuffix(prefix, "x") // ".../<instanceID>/"

	keys, err := m.store.ListObjects(ctx, m.bucketName, prefix)
	if err != nil {
		return 0, fmt.Errorf("failed to list the media of the instance: %w", err)
	}
	removed := 0
	for _, key := range keys {
		if !strings.HasPrefix(key, prefix) { // never remove outside the instance's folder
			continue
		}
		if err := m.store.RemoveObject(ctx, m.bucketName, key); err != nil {
			return removed, fmt.Errorf("failed to delete %s: %w", key, err)
		}
		removed++
	}
	return removed, nil
}

// minioStore adapts the MinIO client to objectStore.
type minioStore struct{ c *minio.Client }

func (s *minioStore) BucketExists(ctx context.Context, bucket string) (bool, error) {
	return s.c.BucketExists(ctx, bucket)
}

func (s *minioStore) MakeBucket(ctx context.Context, bucket string, opts minio.MakeBucketOptions) error {
	return s.c.MakeBucket(ctx, bucket, opts)
}

func (s *minioStore) SetBucketPolicy(ctx context.Context, bucket, policy string) error {
	return s.c.SetBucketPolicy(ctx, bucket, policy)
}

func (s *minioStore) PutObject(ctx context.Context, bucket, object string, data []byte, contentType string) error {
	_, err := s.c.PutObject(ctx, bucket, object, bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{ContentType: contentType})
	return err
}

func (s *minioStore) StatObject(ctx context.Context, bucket, object string) error {
	_, err := s.c.StatObject(ctx, bucket, object, minio.StatObjectOptions{})
	return err
}

func (s *minioStore) RemoveObject(ctx context.Context, bucket, object string) error {
	return s.c.RemoveObject(ctx, bucket, object, minio.RemoveObjectOptions{})
}

func (s *minioStore) ListObjects(ctx context.Context, bucket, prefix string) ([]string, error) {
	var keys []string
	for obj := range s.c.ListObjects(ctx, bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true}) {
		if obj.Err != nil {
			return keys, obj.Err
		}
		keys = append(keys, obj.Key)
	}
	return keys, nil
}

func (s *minioStore) PresignedGetObject(ctx context.Context, bucket, object string, expiry time.Duration) (string, error) {
	u, err := s.c.PresignedGetObject(ctx, bucket, object, expiry, url.Values{})
	if err != nil {
		return "", err
	}
	return u.String(), nil
}
