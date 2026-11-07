package imagestore

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"hash/crc32"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"cloud.google.com/go/storage"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/iamcredentials/v1"
	"google.golang.org/api/option"
)

type storageFixture struct {
	mu                                                       sync.Mutex
	objects                                                  map[string]map[string]interface{}
	data                                                     map[string][]byte
	uploadDenied, deleteDenied, signDenied, invalidSignature bool
	lastPayload                                              []byte
	key                                                      *rsa.PrivateKey
	store                                                    *GCS
}

// Exercise real Storage and IAM SDK requests against local HTTP fixtures.
func newStorageFixture(t *testing.T) *storageFixture {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	f := &storageFixture{objects: map[string]map[string]interface{}{}, data: map[string][]byte{}, key: key}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		reject := func(code int) {
			w.WriteHeader(code)
			json.NewEncoder(w).Encode(map[string]interface{}{"error": map[string]interface{}{"code": code, "message": "fixture failure"}})
		}
		switch {
		case r.Method == "POST" && strings.Contains(r.URL.Path, "/upload/"):
			if f.uploadDenied {
				reject(403)
				return
			}
			_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil {
				t.Errorf("upload content type: %v", err)
				reject(400)
				return
			}
			reader := multipart.NewReader(r.Body, params["boundary"])
			metadata, err := reader.NextPart()
			if err != nil {
				t.Errorf("metadata: %v", err)
				reject(400)
				return
			}
			var object map[string]interface{}
			if err := json.NewDecoder(metadata).Decode(&object); err != nil {
				t.Errorf("metadata JSON: %v", err)
				reject(400)
				return
			}
			part, err := reader.NextPart()
			if err != nil {
				t.Errorf("image body: %v", err)
				reject(400)
				return
			}
			data, err := io.ReadAll(part)
			if err != nil {
				t.Errorf("image data: %v", err)
				reject(400)
				return
			}
			path, _ := object["name"].(string)
			if r.URL.Query().Get("ifGenerationMatch") != "0" {
				t.Error("upload can overwrite an existing image")
			}
			if _, exists := f.objects[path]; exists {
				reject(412)
				return
			}
			object["bucket"], object["generation"] = "test-images", "1"
			var checksum [4]byte
			binary.BigEndian.PutUint32(checksum[:], crc32.Checksum(data, crc32.MakeTable(crc32.Castagnoli)))
			object["crc32c"] = base64.StdEncoding.EncodeToString(checksum[:])
			f.objects[path], f.data[path] = object, data
			json.NewEncoder(w).Encode(object)
		case strings.HasSuffix(r.URL.Path, ":signBlob"):
			if f.signDenied {
				reject(403)
				return
			}
			if r.URL.Path != "/v1/projects/-/serviceAccounts/runtime@test-project.iam.gserviceaccount.com:signBlob" {
				t.Errorf("unexpected signer: %s", r.URL.Path)
			}
			var request struct {
				Payload string `json:"payload"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Errorf("signing request: %v", err)
				reject(400)
				return
			}
			payload, err := base64.StdEncoding.DecodeString(request.Payload)
			if err != nil {
				t.Errorf("signing payload: %v", err)
				reject(400)
				return
			}
			f.lastPayload = payload
			digest := sha256.Sum256(payload)
			signature, err := rsa.SignPKCS1v15(rand.Reader, f.key, crypto.SHA256, digest[:])
			if err != nil {
				t.Errorf("fixture signing: %v", err)
				reject(400)
				return
			}
			encoded := base64.StdEncoding.EncodeToString(signature)
			if f.invalidSignature {
				encoded = "not-base64!"
			}
			json.NewEncoder(w).Encode(map[string]string{"signedBlob": encoded})
		case strings.Contains(r.URL.Path, "/b/test-images/o/"):
			_, path, _ := strings.Cut(r.URL.Path, "/b/test-images/o/")
			object, ok := f.objects[path]
			if r.Method == "DELETE" && f.deleteDenied {
				reject(403)
				return
			}
			if !ok {
				reject(404)
				return
			}
			if r.Method == "DELETE" {
				delete(f.objects, path)
				delete(f.data, path)
				w.WriteHeader(204)
				return
			}
			json.NewEncoder(w).Encode(object)
		default:
			t.Errorf("unexpected storage request: %s %s", r.Method, r.URL)
			reject(400)
		}
	}))
	t.Cleanup(server.Close)
	client, err := storage.NewClient(context.Background(), option.WithEndpoint(server.URL+"/storage/v1/"), option.WithoutAuthentication())
	require.NoError(t, err)
	signer, err := iamcredentials.NewService(context.Background(), option.WithEndpoint(server.URL+"/"), option.WithHTTPClient(server.Client()))
	require.NoError(t, err)
	email := "runtime@test-project.iam.gserviceaccount.com"
	f.store = &GCS{client: client, bucket: "test-images", email: email, sign: iamSigner(signer, email)}
	t.Cleanup(func() { require.NoError(t, f.store.Close()) })
	return f
}

func TestGCSUploadSignDelete(t *testing.T) {
	f := newStorageFixture(t)
	ctx := context.Background()
	path, data := "home/image.jpg", []byte("image fixture bytes")
	require.NoError(t, f.store.Upload(ctx, path, "image/jpeg", data))
	f.mu.Lock()
	require.Equal(t, data, f.data[path])
	require.Equal(t, "image/jpeg", f.objects[path]["contentType"])
	require.Equal(t, "inline", f.objects[path]["contentDisposition"])
	require.Equal(t, "private, max-age=300", f.objects[path]["cacheControl"])
	f.mu.Unlock()
	// Even the adapter enforces immutable object names on retry/collision.
	require.Error(t, f.store.Upload(ctx, path, "image/jpeg", []byte("replacement")))
	expires := time.Now().UTC().Add(time.Hour)
	signed, err := f.store.SignedURL(ctx, path, expires)
	require.NoError(t, err)
	u, err := url.Parse(signed)
	require.NoError(t, err)
	require.Equal(t, "https", u.Scheme)
	require.Equal(t, "storage.googleapis.com", u.Host)
	require.Equal(t, "/test-images/"+path, u.Path)
	require.Equal(t, "GOOG4-RSA-SHA256", u.Query().Get("X-Goog-Algorithm"))
	// Verify the effective URL expiry, accounting for V4 whole-second rounding
	// and time spent looking up the object before signing.
	seconds, err := strconv.Atoi(u.Query().Get("X-Goog-Expires"))
	require.NoError(t, err)
	require.Greater(t, seconds, 0)
	require.LessOrEqual(t, seconds, 3600)
	signedAt, err := time.Parse("20060102T150405Z", u.Query().Get("X-Goog-Date"))
	require.NoError(t, err)
	require.WithinDuration(t, expires, signedAt.Add(time.Duration(seconds)*time.Second), 2*time.Second)
	require.True(t, strings.HasPrefix(u.Query().Get("X-Goog-Credential"), f.store.email+"/"))
	signature, err := hex.DecodeString(u.Query().Get("X-Goog-Signature"))
	require.NoError(t, err)
	f.mu.Lock()
	digest := sha256.Sum256(f.lastPayload)
	f.mu.Unlock()
	require.NoError(t, rsa.VerifyPKCS1v15(&f.key.PublicKey, crypto.SHA256, digest[:], signature))
	require.NoError(t, f.store.Delete(ctx, path))
	require.NoError(t, f.store.Delete(ctx, path))
	_, err = f.store.SignedURL(ctx, path, expires)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestGCSProviderFailures(t *testing.T) {
	f := newStorageFixture(t)
	ctx := context.Background()
	path := "home/image.jpg"
	f.mu.Lock()
	f.uploadDenied = true
	f.mu.Unlock()
	require.Error(t, f.store.Upload(ctx, path, "image/jpeg", bytes.Repeat([]byte("x"), 1<<20)))
	f.mu.Lock()
	f.uploadDenied = false
	f.mu.Unlock()
	require.NoError(t, f.store.Upload(ctx, path, "image/jpeg", []byte("image")))
	f.mu.Lock()
	f.deleteDenied = true
	f.mu.Unlock()
	require.Error(t, f.store.Delete(ctx, path))
	f.mu.Lock()
	f.signDenied = true
	f.mu.Unlock()
	_, err := f.store.SignedURL(ctx, path, time.Now().Add(time.Minute))
	require.Error(t, err)
	f.mu.Lock()
	f.signDenied = false
	f.invalidSignature = true
	f.mu.Unlock()
	_, err = f.store.SignedURL(ctx, path, time.Now().Add(time.Minute))
	require.Error(t, err)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	require.Error(t, f.store.Upload(canceled, path, "image/jpeg", []byte("image")))
	_, err = f.store.SignedURL(canceled, path, time.Now().Add(time.Minute))
	require.Error(t, err)
	_, err = f.store.sign(canceled, []byte("sign"))
	require.Error(t, err)
}

func TestGCSConfiguration(t *testing.T) {
	t.Setenv("GCS_BUCKET_NAME", "")
	store, cleanup, err := Open(context.Background())
	require.NoError(t, err)
	require.Nil(t, store)
	require.NoError(t, cleanup())
	t.Setenv("GCS_BUCKET_NAME", "gs://invalid-bucket")
	_, _, err = Open(context.Background())
	require.Error(t, err)
	for _, bucket := range []string{"", "gs://bucket", "bucket/name", "bucket name", "bucket\n"} {
		_, err := New(context.Background(), bucket)
		require.Error(t, err)
	}
	t.Setenv("GCS_SIGNER_EMAIL", "not-an-email")
	_, err = New(context.Background(), "test-images")
	require.ErrorContains(t, err, "email")
	t.Setenv("GCS_SIGNER_EMAIL", "runtime@test-project.iam.gserviceaccount.com")
	file := filepath.Join(t.TempDir(), "adc.json")
	require.NoError(t, os.WriteFile(file, []byte(`{"type":"authorized_user","client_id":"fixture","client_secret":"fixture","refresh_token":"fixture"}`), 0600))
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", file)
	client, err := New(context.Background(), "test-images")
	require.NoError(t, err)
	require.Equal(t, "test-images", client.bucket)
	require.NoError(t, client.Close())
	t.Setenv("GCS_BUCKET_NAME", "test-images")
	store, cleanup, err = Open(context.Background())
	require.NoError(t, err)
	require.NotNil(t, store)
	require.NoError(t, cleanup())
	// Cloud Run metadata discovers the same identity without an extra env var.
	t.Setenv("GCS_SIGNER_EMAIL", "")
	metadata := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Metadata-Flavor", "Google")
		io.WriteString(w, "runtime@test-project.iam.gserviceaccount.com")
	}))
	defer metadata.Close()
	t.Setenv("GCE_METADATA_HOST", strings.TrimPrefix(metadata.URL, "http://"))
	client, err = New(context.Background(), "test-images")
	require.NoError(t, err)
	require.Equal(t, "runtime@test-project.iam.gserviceaccount.com", client.email)
	require.NoError(t, client.Close())
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = New(canceled, "test-images")
	require.Error(t, err)
	t.Setenv("GCS_SIGNER_EMAIL", "runtime@test-project.iam.gserviceaccount.com")
	require.NoError(t, os.WriteFile(file, []byte(`not JSON`), 0600))
	_, err = New(context.Background(), "test-images")
	require.Error(t, err)
	require.False(t, errors.Is(err, ErrNotFound))
}
