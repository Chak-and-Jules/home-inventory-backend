// Package imagestore stores private home images and signs short-lived read URLs.
package imagestore

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/mail"
	"os"
	"strings"
	"time"

	"cloud.google.com/go/compute/metadata"
	"cloud.google.com/go/storage"
	"google.golang.org/api/iamcredentials/v1"
)

var ErrNotFound = errors.New("image not found")

type Store interface {
	Upload(context.Context, string, string, []byte) error
	Delete(context.Context, string) error
	SignedURL(context.Context, string, time.Time) (string, error)
}

type GCS struct {
	client *storage.Client
	bucket string
	email  string
	sign   func(context.Context, []byte) ([]byte, error)
}

// Open leaves image endpoints disabled when no bucket is configured, while
// returning a cleanup function that callers can always defer after success.
func Open(ctx context.Context) (Store, func() error, error) {
	if bucket := os.Getenv("GCS_BUCKET_NAME"); bucket != "" {
		store, err := New(ctx, bucket)
		if err != nil {
			return nil, nil, err
		}
		return store, store.Close, nil
	}
	return nil, func() error { return nil }, nil
}

// New uses ADC for both storage operations and IAM signing. Cloud Run discovers
// the signing identity from its metadata service. Local ADC users can specify
// GCS_SIGNER_EMAIL and must have signBlob permission on that service account.
func New(ctx context.Context, bucket string) (*GCS, error) {
	if bucket == "" || strings.ContainsAny(bucket, "/\\ \t\r\n") {
		return nil, fmt.Errorf("GCS_BUCKET_NAME must be a bucket name")
	}
	email := os.Getenv("GCS_SIGNER_EMAIL")
	if email == "" {
		var err error
		email, err = metadata.EmailWithContext(ctx, "default")
		if err != nil {
			return nil, fmt.Errorf("discover image signing identity (set GCS_SIGNER_EMAIL for local development): %w", err)
		}
	}
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email || strings.ContainsAny(email, "/\\") {
		return nil, fmt.Errorf("invalid image signing service account email")
	}
	client, err := storage.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("initialize image storage: %w", err)
	}
	signer, err := iamcredentials.NewService(ctx)
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("initialize image signing: %w", err)
	}
	return &GCS{client: client, bucket: bucket, email: email, sign: iamSigner(signer, email)}, nil
}

func iamSigner(signer *iamcredentials.Service, email string) func(context.Context, []byte) ([]byte, error) {
	return func(ctx context.Context, data []byte) ([]byte, error) {
		response, err := signer.Projects.ServiceAccounts.SignBlob("projects/-/serviceAccounts/"+email,
			&iamcredentials.SignBlobRequest{Payload: base64.StdEncoding.EncodeToString(data)}).Context(ctx).Do()
		if err != nil {
			return nil, err
		}
		return base64.StdEncoding.DecodeString(response.SignedBlob)
	}
}

func (s *GCS) Close() error { return s.client.Close() }

func (s *GCS) Upload(ctx context.Context, path, contentType string, data []byte) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	w := s.client.Bucket(s.bucket).Object(path).If(storage.Conditions{DoesNotExist: true}).NewWriter(ctx)
	// Images are bounded by the API. A single upload avoids the SDK's default
	// 16 MiB resumable-upload buffer per concurrent request.
	w.ChunkSize = 0
	w.ContentType = contentType
	w.ContentDisposition = "inline"
	w.CacheControl = "private, max-age=300"
	if _, err := io.Copy(w, bytes.NewReader(data)); err != nil {
		cancel()
		_ = w.Close()
		return err
	}
	return w.Close()
}

func (s *GCS) Delete(ctx context.Context, path string) error {
	err := s.client.Bucket(s.bucket).Object(path).Delete(ctx)
	if errors.Is(err, storage.ErrObjectNotExist) {
		return nil
	}
	return err
}

func (s *GCS) SignedURL(ctx context.Context, path string, expires time.Time) (string, error) {
	if _, err := s.client.Bucket(s.bucket).Object(path).Attrs(ctx); err != nil {
		if errors.Is(err, storage.ErrObjectNotExist) {
			return "", ErrNotFound
		}
		return "", err
	}
	return storage.SignedURL(s.bucket, path, &storage.SignedURLOptions{
		Scheme:         storage.SigningSchemeV4,
		Method:         "GET",
		Expires:        expires,
		GoogleAccessID: s.email,
		SignBytes:      func(data []byte) ([]byte, error) { return s.sign(ctx, data) },
	})
}
