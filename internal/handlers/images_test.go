package handlers

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"hash/crc32"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/imagestore"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/logger"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type imageStoreStub struct {
	mu                            sync.Mutex
	objects                       map[string][]byte
	types                         map[string]string
	signed                        []string
	uploadErr, deleteErr, signErr error
	started, release              chan struct{}
}

func (s *imageStoreStub) Upload(ctx context.Context, path, kind string, data []byte) error {
	if s.started != nil {
		s.started <- struct{}{}
		select {
		case <-s.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.uploadErr != nil {
		return s.uploadErr
	}
	s.objects[path] = bytes.Clone(data)
	s.types[path] = kind
	return nil
}
func (s *imageStoreStub) Delete(_ context.Context, path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.deleteErr != nil {
		return s.deleteErr
	}
	delete(s.objects, path)
	return nil
}
func (s *imageStoreStub) SignedURL(_ context.Context, path string, expires time.Time) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.signed = append(s.signed, path)
	if s.signErr != nil {
		return "", s.signErr
	}
	if _, ok := s.objects[path]; !ok {
		return "", imagestore.ErrNotFound
	}
	return "https://storage.example/" + path + "?temporary=signature", nil
}

type imageFixture struct {
	db            *gorm.DB
	handler       *ImageHandler
	store         *imageStoreStub
	router        *gin.Engine
	home, user    uuid.UUID
	authenticated bool
}

func newImageFixture(t *testing.T) *imageFixture {
	t.Helper()
	logger.InitLogger()
	gin.SetMode(gin.TestMode)
	db := openUnitTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.Profile{}, &models.Home{}, &models.UserHome{}))
	f := &imageFixture{db: db, home: uuid.New(), user: uuid.New(), authenticated: true,
		store: &imageStoreStub{objects: map[string][]byte{}, types: map[string]string{}}}
	require.NoError(t, db.Create(&models.Profile{ID: f.user, Email: uuid.NewString() + "@example.com"}).Error)
	require.NoError(t, db.Create(&models.Home{ID: f.home, Name: "Image home"}).Error)
	require.NoError(t, db.Create(&models.UserHome{UserID: f.user, HomeID: f.home, Role: models.RoleOwner}).Error)
	f.handler = &ImageHandler{DB: db, Store: f.store}
	f.router = gin.New()
	f.router.Use(func(c *gin.Context) {
		if f.authenticated {
			c.Set("userID", f.user)
		}
	})
	f.router.POST("/images", f.handler.Upload)
	f.router.DELETE("/images", f.handler.Delete)
	f.router.POST("/images/signed-urls", f.handler.SignedURLs)
	return f
}

func (f *imageFixture) request(method, path, contentType string, data []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewReader(data))
	req.Header.Set("X-Home-Id", f.home.String())
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	return w
}
func multipartImage(t *testing.T, name string, data []byte, extra bool) (string, []byte) {
	t.Helper()
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	part, err := w.CreateFormFile("file", name)
	require.NoError(t, err)
	_, err = part.Write(data)
	require.NoError(t, err)
	if extra {
		require.NoError(t, w.WriteField("unexpected", "field"))
	}
	require.NoError(t, w.Close())
	return w.FormDataContentType(), b.Bytes()
}
func sampleImage(t *testing.T, format string) []byte {
	t.Helper()
	var b bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	if format == "png" {
		require.NoError(t, png.Encode(&b, img))
	} else {
		require.NoError(t, jpeg.Encode(&b, img, nil))
	}
	return b.Bytes()
}
func signedBody(paths ...string) []byte { b, _ := json.Marshal(gin.H{"paths": paths}); return b }

func TestImageLifecycle(t *testing.T) {
	f := newImageFixture(t)
	for _, format := range []string{"jpeg", "png"} {
		t.Run(format, func(t *testing.T) {
			data := sampleImage(t, format)
			ct, body := multipartImage(t, "untrusted-filename.svg", data, false)
			w := f.request("POST", "/images", ct, body)
			require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
			var upload struct {
				Path string `json:"path"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &upload))
			require.True(t, validImagePath(f.home, upload.Path))
			require.NotContains(t, upload.Path, "untrusted")
			require.Equal(t, data, f.store.objects[upload.Path])
			require.Equal(t, "image/"+format, f.store.types[upload.Path])
			// A viewer can read but the same paths are signed once per batch.
			require.NoError(t, f.db.Model(&models.UserHome{}).Where("user_id = ?", f.user).Update("role", models.RoleViewer).Error)
			before := time.Now().UTC()
			w = f.request("POST", "/images/signed-urls", "application/json", signedBody(upload.Path, upload.Path))
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
			var signed struct {
				Images []SignedImage `json:"images"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &signed))
			require.Len(t, signed.Images, 1)
			require.Equal(t, upload.Path, signed.Images[0].Path)
			require.WithinDuration(t, before.Add(ImageURLLifetime), signed.Images[0].ExpiresAt, time.Second)
			require.NoError(t, f.db.Model(&models.UserHome{}).Where("user_id = ?", f.user).Update("role", models.RoleEditor).Error)
			for range 2 {
				w = f.request("DELETE", "/images?path="+url.QueryEscape(upload.Path), "", nil)
				require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
			}
			require.NotContains(t, f.store.objects, upload.Path)
		})
	}
}

func TestImageAuthorization(t *testing.T) {
	for _, operation := range []struct{ method, path string }{{"POST", "/images"}, {"DELETE", "/images"}, {"POST", "/images/signed-urls"}} {
		for _, scenario := range []string{"unauthenticated", "missing home", "invalid home", "foreign home", "viewer", "unconfigured", "database failure"} {
			t.Run(operation.method+operation.path+scenario, func(t *testing.T) {
				f := newImageFixture(t)
				expected := http.StatusForbidden
				switch scenario {
				case "unauthenticated":
					f.authenticated = false
					expected = http.StatusUnauthorized
				case "foreign home":
					f.home = uuid.New()
				case "viewer":
					require.NoError(t, f.db.Model(&models.UserHome{}).Where("user_id = ?", f.user).Update("role", models.RoleViewer).Error)
					if operation.path == "/images/signed-urls" {
						return
					} // reader success is covered by the lifecycle
				case "unconfigured":
					f.handler.Store = nil
					expected = http.StatusServiceUnavailable
				case "database failure":
					conn, err := f.db.DB()
					require.NoError(t, err)
					require.NoError(t, conn.Close())
					expected = http.StatusInternalServerError
				case "missing home", "invalid home":
					expected = http.StatusBadRequest
				}
				req := httptest.NewRequest(operation.method, operation.path, nil)
				if scenario != "missing home" {
					req.Header.Set("X-Home-Id", f.home.String())
				}
				if scenario == "invalid home" {
					req.Header.Set("X-Home-Id", "not-a-uuid")
				}
				w := httptest.NewRecorder()
				f.router.ServeHTTP(w, req)
				require.Equal(t, expected, w.Code, w.Body.String())
				require.Empty(t, f.store.objects)
				require.Empty(t, f.store.signed)
			})
		}
	}
}

func TestImageUploadValidation(t *testing.T) {
	valid := sampleImage(t, "png")
	largeDimensions := bytes.Clone(valid)
	binary.BigEndian.PutUint32(largeDimensions[16:20], 5000)
	binary.BigEndian.PutUint32(largeDimensions[20:24], 5000)
	binary.BigEndian.PutUint32(largeDimensions[29:33], crc32.ChecksumIEEE(largeDimensions[12:29]))
	for _, tc := range []struct {
		name, fileName string
		data           []byte
		extra          bool
		status         int
	}{
		{"empty", "empty.jpg", nil, false, 400},
		{"svg", "picture.png", []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`), false, 415},
		{"corrupt pixels", "picture.png", valid[:33], false, 415},
		{"too many pixels", "picture.png", largeDimensions, false, 400},
		{"too large", "picture.jpg", bytes.Repeat([]byte{0}, MaxImageBytes+1), false, 413},
		{"envelope too large", strings.Repeat("x", 100<<10), bytes.Repeat([]byte{0}, MaxImageBytes), false, 413},
		{"extra field", "picture.png", valid, true, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newImageFixture(t)
			ct, body := multipartImage(t, tc.fileName, tc.data, tc.extra)
			w := f.request("POST", "/images", ct, body)
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Empty(t, f.store.objects)
		})
	}
	for _, tc := range []struct {
		contentType string
		data        []byte
	}{
		{"application/json", []byte(`{}`)},
		{"multipart/form-data; boundary=broken", []byte("not multipart")},
		{"multipart/form-data; boundary=broken", []byte("--broken\r\nContent-Disposition: form-data; name=\"file\"; filename=\"a.png\"\r\n\r\npartial")},
		{"multipart/form-data; boundary=broken", []byte("--broken--\r\n")},
	} {
		f := newImageFixture(t)
		w := f.request("POST", "/images", tc.contentType, tc.data)
		require.Equal(t, 400, w.Code, w.Body.String())
		require.Empty(t, f.store.objects)
	}
}

func TestImagePathValidation(t *testing.T) {
	f := newImageFixture(t)
	valid := f.home.String() + "/" + uuid.NewString() + ".jpg"
	invalid := []string{"", "https://other.example/image.jpg", "gs://bucket/" + valid, uuid.NewString() + "/" + uuid.NewString() + ".jpg",
		f.home.String() + "/../file.jpg", f.home.String() + "/nested/" + uuid.NewString() + ".jpg", f.home.String() + "/" + uuid.Nil.String() + ".jpg",
		strings.TrimSuffix(valid, ".jpg") + ".svg", valid + "?query=1", strings.ToUpper(valid), f.home.String() + "/not-a-uuid.png"}
	for _, path := range invalid {
		w := f.request("DELETE", "/images?path="+url.QueryEscape(path), "", nil)
		require.Equal(t, 400, w.Code, path)
		w = f.request("POST", "/images/signed-urls", "application/json", signedBody(valid, path))
		require.Equal(t, 400, w.Code, path)
		require.Empty(t, f.store.signed)
	}
	for _, body := range [][]byte{[]byte(`{`), signedBody(), signedBody(make([]string, 101)...), bytes.Repeat([]byte(" "), (32<<10)+1)} {
		w := f.request("POST", "/images/signed-urls", "application/json", body)
		require.Equal(t, 400, w.Code, w.Body.String())
	}
}

func TestImageStorageFailures(t *testing.T) {
	f := newImageFixture(t)
	f.store.uploadErr = errors.New("private provider error")
	ct, body := multipartImage(t, "image.jpg", sampleImage(t, "jpeg"), false)
	w := f.request("POST", "/images", ct, body)
	require.Equal(t, 502, w.Code)
	require.NotContains(t, w.Body.String(), "private")
	path := f.home.String() + "/" + uuid.NewString() + ".jpg"
	f.store.deleteErr = errors.New("provider error")
	w = f.request("DELETE", "/images?path="+path, "", nil)
	require.Equal(t, 502, w.Code)
	w = f.request("POST", "/images/signed-urls", "application/json", signedBody(path))
	require.Equal(t, 404, w.Code)
	f.store.signErr = errors.New("signBlob denied")
	w = f.request("POST", "/images/signed-urls", "application/json", signedBody(path))
	require.Equal(t, 502, w.Code)
	require.NotContains(t, w.Body.String(), "signBlob")
}

func TestImageConcurrentUploadsAreBounded(t *testing.T) {
	f := newImageFixture(t)
	f.store.started = make(chan struct{}, 2)
	f.store.release = make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(f.store.release) }) }
	defer release()
	ct, body := multipartImage(t, "image.png", sampleImage(t, "png"), false)
	results := make(chan *httptest.ResponseRecorder, 2)
	for range 2 {
		go func() { results <- f.request("POST", "/images", ct, body) }()
	}
	for range 2 {
		select {
		case <-f.store.started:
		case <-time.After(5 * time.Second):
			t.Fatal("upload did not reach storage")
		}
	}
	w := f.request("POST", "/images", ct, body)
	require.Equal(t, 503, w.Code)
	require.Equal(t, "1", w.Header().Get("Retry-After"))
	release()
	for range 2 {
		require.Equal(t, 201, (<-results).Code)
	}
}

// Ensure unexpected stream failures are rejected without a storage write.
type failedImageReader struct{}

func (failedImageReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func TestImageUploadBrokenStream(t *testing.T) {
	f := newImageFixture(t)
	req := httptest.NewRequest("POST", "/images", failedImageReader{})
	req.Header.Set("Content-Type", "multipart/form-data; boundary=broken")
	req.Header.Set("X-Home-Id", f.home.String())
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	require.Equal(t, 400, w.Code)
	require.Empty(t, f.store.objects)
}
