package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/i18n"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/logger"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/maintenance"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/utils"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

func (h *MaintenanceTaskHandler) CompleteMaintenanceTask(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	user, ok := utils.GetAuthUserID(c, h.DB)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		h.maintenanceFailure(c, maintenance.ErrInvalid, "Failed to complete maintenance task")
		return
	}
	var command maintenance.Command
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 4096))
	if err != nil {
		h.maintenanceFailure(c, maintenance.ErrInvalid, "Failed to complete maintenance task")
		return
	}
	body = bytes.TrimSpace(body)
	if len(body) > 0 {
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.DisallowUnknownFields()
		if body[0] != '{' || decoder.Decode(&command) != nil || decoder.Decode(&struct{}{}) != io.EOF {
			h.maintenanceFailure(c, maintenance.ErrInvalid, "Failed to complete maintenance task")
			return
		}
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(body, &fields)
		for _, key := range []string{"request_id", "expected_version"} {
			if raw, exists := fields[key]; exists && bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
				h.maintenanceFailure(c, maintenance.ErrInvalid, "Failed to complete maintenance task")
				return
			}
		}
	}
	result, err := maintenance.New(h.DB).Complete(c.Request.Context(), user, id, command)
	if err != nil {
		h.maintenanceFailure(c, err, "Failed to complete maintenance task")
		return
	}
	key := "Maintenance task completed successfully"
	if result.AlreadyCompleted {
		key = "Maintenance task already completed"
	}
	c.JSON(http.StatusOK, struct {
		Message string `json:"message"`
		maintenance.Result
	}{i18n.TranslateDB(h.DB, c, key), result})
}

func (h *MaintenanceTaskHandler) maintenanceFailure(c *gin.Context, err error, fallback string) {
	status, code, key := 500, "maintenance_storage_error", fallback
	switch {
	case errors.Is(err, maintenance.ErrInvalid):
		status, code, key = 400, "invalid_request", "Invalid maintenance request"
	case errors.Is(err, maintenance.ErrNotFound):
		status, code, key = 404, "task_not_found", "Maintenance task not found"
	case errors.Is(err, maintenance.ErrForbidden):
		status, code, key = 403, "write_access_denied", "Write access denied to this home"
	case errors.Is(err, maintenance.ErrConflict):
		status, code, key = 409, "state_conflict", "Maintenance task state changed. Reload and try again"
	case errors.Is(err, maintenance.ErrRequestReuse):
		status, code, key = 409, "request_id_reused", "Maintenance request ID has already been used"
	case errors.Is(err, maintenance.ErrCompleted):
		status, code, key = 409, "completed_task_immutable", "Completed maintenance tasks require an explicit correction"
	case errors.Is(err, maintenance.ErrInsufficient):
		status, code, key = 400, "insufficient_stock", "Insufficient unexpired stock for task dependencies"
	case errors.Is(err, maintenance.ErrDependency):
		status, code, key = 400, "invalid_dependency", "Invalid maintenance task dependency"
	default:
		logger.Log.Error("Maintenance command failed", zap.Error(err))
	}
	// Localization runs after the transaction has rolled back/released its pool.
	c.JSON(status, gin.H{"code": code, "error": i18n.TranslateDB(h.DB, c, key)})
}
