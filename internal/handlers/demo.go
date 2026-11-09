package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/demos"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/guides"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/i18n"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/logger"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/utils"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type DemoHandler struct{ Service *demos.Service }

func (h *DemoHandler) Status(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	user, ok := utils.GetAuthUserID(c, h.Service.DB)
	if !ok {
		return
	}
	v, err := h.Service.Status(c.Request.Context(), user)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, v)
}

func (h *DemoHandler) Act(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	user, ok := utils.GetAuthUserID(c, h.Service.DB)
	if !ok {
		return
	}
	var command demos.Command
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&command); err != nil {
		h.fail(c, guides.ErrInvalid)
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		h.fail(c, guides.ErrInvalid)
		return
	}
	v, err := h.Service.Act(c.Request.Context(), user, command, demoLabels(h.Service.DB, c))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, v)
}

// Resolve language outside the provisioning transaction, including with a
// single-connection pool. The seeder receives a pure translation function.
func demoLabels(db *gorm.DB, c *gin.Context) demos.Labels {
	values := map[string]string{}
	for _, key := range []string{"My Home", "Sample Home", "Food", "Household", "Rice", "Milk", "Eggs", "Dish soap", "Sponges"} {
		values[key] = i18n.TranslateDB(db, c, key)
	}
	return func(key string) string {
		if value, ok := values[key]; ok {
			return value
		}
		return key
	}
}

func (h *DemoHandler) fail(c *gin.Context, err error) {
	status, code, key := 500, "demo_storage_error", "Failed to process demo home"
	switch {
	case errors.Is(err, guides.ErrInvalid):
		status, code, key = 400, "invalid_request", "Invalid demo home request"
	case errors.Is(err, guides.ErrConflict):
		status, code, key = 409, "state_conflict", "Demo home state changed. Reload and try again"
	case errors.Is(err, guides.ErrRequestReuse):
		status, code, key = 409, "request_id_reused", "Guide request ID has already been used"
	case errors.Is(err, demos.ErrExists):
		status, code, key = 409, "demo_exists", demos.ErrExists.Error()
	case errors.Is(err, demos.ErrUnavailable):
		status, code, key = 404, "demo_unavailable", demos.ErrUnavailable.Error()
	case errors.Is(err, demos.ErrAccess):
		status, code, key = 403, "demo_access_denied", demos.ErrAccess.Error()
	default:
		logger.Log.Error("Demo home operation failed", zap.Error(err))
	}
	c.JSON(status, gin.H{"code": code, "error": i18n.TranslateDB(h.Service.DB, c, key)})
}
