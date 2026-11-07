package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/guides"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/i18n"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/logger"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/utils"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type GuideHandler struct{ Service *guides.Service }

func (h *GuideHandler) List(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	user, ok := utils.GetAuthUserID(c, h.Service.DB)
	if !ok {
		return
	}
	platform := guides.Platform(c.Query("platform"))
	client, err := strconv.Atoi(c.Query("client_version"))
	if err != nil || !platform.Valid() || client < 1 {
		h.fail(c, guides.ErrInvalid)
		return
	}
	var home *uuid.UUID
	if value := c.GetHeader("X-Home-Id"); value != "" {
		id, err := uuid.Parse(value)
		if err != nil || id == uuid.Nil {
			h.fail(c, guides.ErrInvalid)
			return
		}
		home = &id
	}
	result, err := h.Service.List(c.Request.Context(), user, platform, client, home)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *GuideHandler) Act(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	user, ok := utils.GetAuthUserID(c, h.Service.DB)
	if !ok {
		return
	}
	var command guides.Command
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
	result, err := h.Service.Act(c.Request.Context(), user, c.Param("id"), command)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *GuideHandler) fail(c *gin.Context, err error) {
	status, code, key := http.StatusInternalServerError, "guide_storage_error", "Failed to process guide state"
	switch {
	case errors.Is(err, guides.ErrInvalid):
		status, code, key = http.StatusBadRequest, "invalid_request", "Invalid guide request"
	case errors.Is(err, guides.ErrNotFound):
		status, code, key = http.StatusNotFound, "guide_not_found", "Guide not available for this client"
	case errors.Is(err, guides.ErrConflict):
		status, code, key = http.StatusConflict, "state_conflict", "Guide state changed. Reload and try again"
	case errors.Is(err, guides.ErrRequestReuse):
		status, code, key = http.StatusConflict, "request_id_reused", "Guide request ID has already been used"
	case errors.Is(err, guides.ErrTransition):
		status, code, key = http.StatusConflict, "invalid_transition", "Guide action is not allowed in its current state"
	case errors.Is(err, guides.ErrUnavailable):
		status, code, key = http.StatusForbidden, "guide_unavailable", "Guide prerequisites are not satisfied"
	case errors.Is(err, guides.ErrHome):
		status, code, key = http.StatusForbidden, "home_access_denied", "Access denied to this home"
	default:
		logger.Log.Error("Guide state operation failed", zap.Error(err))
	}
	c.JSON(status, gin.H{"code": code, "error": i18n.TranslateDB(h.Service.DB, c, key)})
}
