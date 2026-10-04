package minio_storage

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
)

type fakeStore struct {
	bucketExists bool
	policySet    string
	made         bool
	objects      map[string][]byte
	presignTTL   time.Duration
	removed      []string
}

func newFake() *fakeStore { return &fakeStore{bucketExists: true, objects: map[string][]byte{}} }

func (f *fakeStore) BucketExists(context.Context, string) (bool, error) { return f.bucketExists, nil }
func (f *fakeStore) MakeBucket(context.Context, string, minio.MakeBucketOptions) error {
	f.made = true
	return nil
}
func (f *fakeStore) SetBucketPolicy(_ context.Context, _, policy string) error {
	f.policySet = policy
	return nil
}
func (f *fakeStore) PutObject(_ context.Context, _, object string, data []byte, _ string) error {
	f.objects[object] = data
	return nil
}
func (f *fakeStore) StatObject(_ context.Context, _, object string) error {
	if _, ok := f.objects[object]; !ok {
		return errors.New("not found")
	}
	return nil
}
func (f *fakeStore) RemoveObject(_ context.Context, _, object string) error {
	delete(f.objects, object)
	f.removed = append(f.removed, object)
	return nil
}
func (f *fakeStore) ListObjects(_ context.Context, _, prefix string) ([]string, error) {
	var keys []string
	for k := range f.objects {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys, nil
}
func (f *fakeStore) PresignedGetObject(_ context.Context, bucket, object string, ttl time.Duration) (string, error) {
	f.presignTTL = ttl
	return "https://s3.example/" + bucket + "/" + object + "?sig=x", nil
}

func newTestStorage(t *testing.T, f *fakeStore, o Options) *MinioMediaStorage {
	t.Helper()
	if o.Bucket == "" {
		o.Bucket = "media"
	}
	s, err := newStorage(f, o)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// Reproduced: the bucket was made world-readable at every start, replacing its policy.
func TestTheBucketPolicyIsLeftAloneByDefault(t *testing.T) {
	f := newFake()
	newTestStorage(t, f, Options{})
	if f.policySet != "" {
		t.Fatalf("the bucket policy must not be touched unless MINIO_PUBLIC_BUCKET=true, got %s", f.policySet)
	}

	f = newFake()
	newTestStorage(t, f, Options{PublicBucket: true})
	if !strings.Contains(f.policySet, `"Principal": "*"`) || !strings.Contains(f.policySet, "arn:aws:s3:::media/*") {
		t.Fatalf("an explicit public bucket gets the public read policy, got %q", f.policySet)
	}
}

func TestAMissingBucketIsCreated(t *testing.T) {
	f := newFake()
	f.bucketExists = false
	newTestStorage(t, f, Options{})
	if !f.made {
		t.Fatal("a bucket that does not exist must be created: every store failed silently before")
	}
}

func TestEachInstanceHasItsOwnFolder(t *testing.T) {
	f := newFake()
	s := newTestStorage(t, f, Options{})

	// The same message id received by two instances (same group): two objects, not one.
	if _, err := s.Store(context.Background(), "inst-a", []byte("A"), "3EB0ABC.jpg", "image/jpeg"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store(context.Background(), "inst-b", []byte("B"), "3EB0ABC.jpg", "image/jpeg"); err != nil {
		t.Fatal(err)
	}

	if string(f.objects["whatygo-medias/inst-a/3EB0ABC.jpg"]) != "A" || string(f.objects["whatygo-medias/inst-b/3EB0ABC.jpg"]) != "B" {
		t.Fatalf("objects: %v", f.objects)
	}
}

func TestStoreReturnsAPresignedURLWithTheConfiguredTTL(t *testing.T) {
	f := newFake()
	s := newTestStorage(t, f, Options{URLTTL: 24 * time.Hour})
	u, err := s.Store(context.Background(), "inst", []byte("x"), "a.png", "image/png")
	if err != nil || !strings.Contains(u, "sig=") || !strings.Contains(u, "/whatygo-medias/inst/a.png") {
		t.Fatalf("url %q err %v", u, err)
	}
	if f.presignTTL != 24*time.Hour {
		t.Fatalf("ttl = %v", f.presignTTL)
	}

	for name, ttl := range map[string]time.Duration{"unset": 0, "negative": -time.Hour, "above the S3 maximum": 30 * 24 * time.Hour} {
		f := newFake()
		s := newTestStorage(t, f, Options{URLTTL: ttl})
		s.Store(context.Background(), "inst", []byte("x"), "a.png", "image/png")
		if f.presignTTL != maxPresignTTL {
			t.Errorf("%s: ttl = %v, want %v", name, f.presignTTL, maxPresignTTL)
		}
	}
}

func TestDeletingAnInstanceRemovesOnlyItsMedia(t *testing.T) {
	f := newFake()
	s := newTestStorage(t, f, Options{})
	ctx := context.Background()
	s.Store(ctx, "inst-a", []byte("1"), "1.jpg", "image/jpeg")
	s.Store(ctx, "inst-a", []byte("2"), "2.jpg", "image/jpeg")
	s.Store(ctx, "inst-ab", []byte("3"), "3.jpg", "image/jpeg") // a name that merely starts the same
	s.Store(ctx, "inst-b", []byte("4"), "4.jpg", "image/jpeg")

	n, err := s.DeleteInstance(ctx, "inst-a")
	if err != nil || n != 2 {
		t.Fatalf("removed %d, err %v, want 2", n, err)
	}
	if _, ok := f.objects["whatygo-medias/inst-ab/3.jpg"]; !ok {
		t.Fatal("inst-ab must not lose its files because its name starts like inst-a")
	}
	if _, ok := f.objects["whatygo-medias/inst-b/4.jpg"]; !ok {
		t.Fatal("another instance's files must stay")
	}
	if len(f.objects) != 2 {
		t.Fatalf("objects left: %v", f.objects)
	}
}

func TestKeysCannotEscapeTheInstanceFolder(t *testing.T) {
	for _, id := range []string{"", ".", "..", "../x", "a/b", `a\b`} {
		if _, err := objectKey(id, "f.jpg"); err == nil {
			t.Errorf("instance id %q must be refused", id)
		}
	}
	for in, want := range map[string]string{
		"../../etc/passwd": "passwd",
		`..\..\x.jpg`:      "x.jpg",
		"a/b/c.png":        "c.png",
		"plain.jpg":        "plain.jpg",
		"..":               "file",
		"":                 "file",
	} {
		k, err := objectKey("inst", in)
		if err != nil || k != "whatygo-medias/inst/"+want {
			t.Errorf("file name %q -> %q (%v), want .../inst/%s", in, k, err, want)
		}
	}
}

func TestGetURLNeedsTheObjectToExist(t *testing.T) {
	f := newFake()
	s := newTestStorage(t, f, Options{})
	if _, err := s.GetURL(context.Background(), "inst", "missing.jpg"); err == nil {
		t.Fatal("GetURL of a missing file must fail")
	}
	s.Store(context.Background(), "inst", []byte("x"), "there.jpg", "image/jpeg")
	if u, err := s.GetURL(context.Background(), "inst", "there.jpg"); err != nil || u == "" {
		t.Fatalf("GetURL: %q %v", u, err)
	}
}
