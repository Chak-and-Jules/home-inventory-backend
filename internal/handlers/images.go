package handlers

import (
	"bytes"
	"context"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/i18n"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/imagestore"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/logger"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/utils"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

const (
	MaxImageBytes       = 5 << 20
	MaxImagePixels      = 20_000_000
	MaxSignedImagePaths = 100
	ImageURLLifetime    = 15 * time.Minute
)

// Bound decoded-image memory on small Cloud Run instances. The slot spans the
// request so a slow storage write cannot retain unlimited decoded images.
var imageUploadSlots = make(chan struct{}, 2)

type ImageHandler struct {
	DB    *gorm.DB
	Store imagestore.Store
}

type SignedImage struct {
	Path      string    `json:"path"`
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (h *ImageHandler) fail(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{"error": i18n.TranslateDB(h.DB, c, message)})
}

func (h *ImageHandler) authorize(c *gin.Context, write bool) (uuid.UUID, bool) {
	if _, ok := utils.GetAuthUserID(c, h.DB); !ok {
		return uuid.Nil, false
	}
	homeID, ok := utils.ParseUUIDHeader(c, h.DB, "X-Home-Id", "Invalid home_id")
	if !ok {
		return uuid.Nil, false
	}
	membership, err := utils.GetUserHome(c, h.DB.WithContext(c.Request.Context()), homeID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		h.fail(c, http.StatusForbidden, "Access denied to this home")
		return uuid.Nil, false
	}
	if err != nil {
		logger.Log.Error("Failed to verify image access", zap.Error(err))
		h.fail(c, http.StatusInternalServerError, "Failed to verify image access")
		return uuid.Nil, false
	}
	if write && membership.Role != models.RoleOwner && membership.Role != models.RoleEditor {
		h.fail(c, http.StatusForbidden, "Write access denied to this home")
		return uuid.Nil, false
	}
	if h.Store == nil {
		h.fail(c, http.StatusServiceUnavailable, "Image storage is not configured")
		return uuid.Nil, false
	}
	return homeID, true
}

func (h *ImageHandler) Upload(c *gin.Context) {
	homeID, ok := h.authorize(c, true)
	if !ok {
		return
	}
	select {
	case imageUploadSlots <- struct{}{}:
		defer func() { <-imageUploadSlots }()
	default:
		c.Header("Retry-After", "1")
		h.fail(c, http.StatusServiceUnavailable, "Image uploads are busy; please retry")
		return
	}
	// Bound the entire multipart envelope as well as the file itself. Read it
	// directly instead of writing multipart temporary files to the container.
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, MaxImageBytes+(64<<10))
	reader, err := c.Request.MultipartReader()
	if err != nil {
		h.fail(c, http.StatusBadRequest, "Expected one image file")
		return
	}
	var data []byte
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			h.uploadError(c, err)
			return
		}
		if part.FormName() != "file" || part.FileName() == "" || data != nil {
			h.fail(c, http.StatusBadRequest, "Expected one image file")
			return
		}
		data, err = io.ReadAll(io.LimitReader(part, MaxImageBytes+1))
		if err != nil {
			h.uploadError(c, err)
			return
		}
		if len(data) > MaxImageBytes {
			h.fail(c, http.StatusRequestEntityTooLarge, "Image exceeds the 5 MiB limit")
			return
		}
	}
	if len(data) == 0 {
		h.fail(c, http.StatusBadRequest, "Expected one image file")
		return
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "jpeg" && format != "png") {
		h.fail(c, http.StatusUnsupportedMediaType, "Only valid JPEG and PNG images are supported")
		return
	}
	if config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > MaxImagePixels {
		h.fail(c, http.StatusBadRequest, "Image exceeds the 20 megapixel limit")
		return
	}
	// Decode the bounded image once to reject truncated/corrupt pixel data too.
	if _, _, err := image.Decode(bytes.NewReader(data)); err != nil {
		h.fail(c, http.StatusUnsupportedMediaType, "Only valid JPEG and PNG images are supported")
		return
	}
	extension, contentType := ".jpg", "image/jpeg"
	if format == "png" {
		extension, contentType = ".png", "image/png"
	}
	path := homeID.String() + "/" + uuid.NewString() + extension
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	if err := h.Store.Upload(ctx, path, contentType, data); err != nil {
		logger.Log.Error("Failed to upload image", zap.Error(err))
		h.fail(c, http.StatusBadGateway, "Failed to upload image")
		return
	}
	c.JSON(http.StatusCreated, gin.H{"path": path})
}

func (h *ImageHandler) uploadError(c *gin.Context, err error) {
	var limit *http.MaxBytesError
	if errors.As(err, &limit) {
		h.fail(c, http.StatusRequestEntityTooLarge, "Image exceeds the 5 MiB limit")
		return
	}
	h.fail(c, http.StatusBadRequest, "Expected one image file")
}

func validImagePath(homeID uuid.UUID, path string) bool {
	parts := strings.Split(path, "/")
	if len(parts) != 2 || parts[0] != homeID.String() {
		return false
	}
	extension := ".jpg"
	if strings.HasSuffix(parts[1], ".png") {
		extension = ".png"
	}
	if !strings.HasSuffix(parts[1], extension) {
		return false
	}
	id := strings.TrimSuffix(parts[1], extension)
	parsed, err := uuid.Parse(id)
	return err == nil && parsed != uuid.Nil && parsed.String() == id
}

func (h *ImageHandler) Delete(c *gin.Context) {
	homeID, ok := h.authorize(c, true)
	if !ok {
		return
	}
	path := c.Query("path")
	if !validImagePath(homeID, path) {
		h.fail(c, http.StatusBadRequest, "Invalid image path for this home")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	if err := h.Store.Delete(ctx, path); err != nil {
		logger.Log.Error("Failed to delete image", zap.Error(err))
		h.fail(c, http.StatusBadGateway, "Failed to delete image")
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *ImageHandler) SignedURLs(c *gin.Context) {
	homeID, ok := h.authorize(c, false)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32<<10)
	var request struct {
		Paths []string `json:"paths"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		h.fail(c, http.StatusBadRequest, "Invalid request payload")
		return
	}
	if len(request.Paths) == 0 || len(request.Paths) > MaxSignedImagePaths {
		h.fail(c, http.StatusBadRequest, "Provide between 1 and 100 image paths")
		return
	}
	// Validate every key before any signing call, including mixed-home batches.
	for _, path := range request.Paths {
		if !validImagePath(homeID, path) {
			h.fail(c, http.StatusBadRequest, "Invalid image path for this home")
			return
		}
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	expires := time.Now().UTC().Add(ImageURLLifetime).Truncate(time.Second)
	images := make([]SignedImage, 0, len(request.Paths))
	seen := make(map[string]bool)
	for _, path := range request.Paths {
		if seen[path] {
			continue
		}
		seen[path] = true
		url, err := h.Store.SignedURL(ctx, path, expires)
		if errors.Is(err, imagestore.ErrNotFound) {
			h.fail(c, http.StatusNotFound, "Image not found")
			return
		}
		if err != nil {
			logger.Log.Error("Failed to sign image URLs", zap.Error(err))
			h.fail(c, http.StatusBadGateway, "Failed to sign image URLs")
			return
		}
		images = append(images, SignedImage{Path: path, URL: url, ExpiresAt: expires})
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"images": images})
}
