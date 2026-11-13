package handlers

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/i18n"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/maintenance"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/utils"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type MaintenanceTaskHandler struct {
	DB *gorm.DB
}

type TaskItemDependencyRequest struct {
	quantityInputs
	QuantityUnitID   *uuid.UUID `json:"quantity_unit_id"`
	ItemDefinitionID uuid.UUID  `json:"item_definition_id" binding:"required"`
	QuantityRequired float64    `json:"quantity_required" binding:"required"`
}

type MaintenanceTaskRequest struct {
	ExpectedVersion       *int64                      `json:"expected_version,omitempty"`
	InventoryItemID       *uuid.UUID                  `json:"inventory_item_id"`
	Description           string                      `json:"description" binding:"required"`
	ScheduledDate         time.Time                   `json:"scheduled_date" binding:"required"`
	Frequency             string                      `json:"frequency"`
	CustomFrequency       *float64                    `json:"custom_frequency"`
	CustomFrequencyMetric *string                     `json:"custom_frequency_metric"`
	IsCompleted           bool                        `json:"is_completed"`
	Dependencies          []TaskItemDependencyRequest `json:"dependencies"`
}

func (h *MaintenanceTaskHandler) GetMaintenanceTasks(c *gin.Context) {
	homeID, ok := utils.ParseUUIDHeader(c, h.DB, "X-Home-Id", "Invalid home_id")
	if !ok {
		return
	}

	if !utils.VerifyHomeAccess(c, h.DB, homeID) {
		c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Access denied to this home")})
		return
	}

	query := h.DB.Preload("InventoryItem.ItemDefinition").Preload("Dependencies.ItemDefinition").Where("home_id = ?", homeID)

	inventoryItemIDStr := c.Query("inventory_item_id")
	if inventoryItemIDStr != "" {
		inventoryItemID, err := uuid.Parse(inventoryItemIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(h.DB, c, "Invalid inventory item ID")})
			return
		}

		// Verify inventory item belongs to home
		var item models.InventoryItem
		if err := h.DB.Where("id = ? AND home_id = ?", inventoryItemID, homeID).First(&item).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				c.JSON(http.StatusNotFound, gin.H{"error": i18n.TranslateDB(h.DB, c, "Inventory item not found")})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to fetch inventory item")})
			}
			return
		}

		query = query.Where("inventory_item_id = ?", inventoryItemID)
	}

	var tasks []models.MaintenanceTask
	if err := query.Order("scheduled_date ASC").Find(&tasks).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to fetch maintenance tasks")})
		return
	}

	c.JSON(http.StatusOK, tasks)
}

func (h *MaintenanceTaskHandler) GetMaintenanceTask(c *gin.Context) {
	id, ok := utils.ParseUUIDParam(c, h.DB, "id", "Invalid maintenance task ID")
	if !ok {
		return
	}

	var task models.MaintenanceTask
	if err := h.DB.Preload("InventoryItem.ItemDefinition").Preload("Dependencies.ItemDefinition").First(&task, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": i18n.TranslateDB(h.DB, c, "Maintenance task not found")})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to fetch maintenance task")})
		}
		return
	}

	if !utils.VerifyHomeAccess(c, h.DB, task.HomeID) {
		c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Access denied to this home")})
		return
	}

	c.JSON(http.StatusOK, task)
}

func (h *MaintenanceTaskHandler) CreateMaintenanceTask(c *gin.Context) {
	homeID, ok := utils.ParseUUIDHeader(c, h.DB, "X-Home-Id", "Invalid home_id")
	if !ok {
		return
	}

	if !utils.VerifyHomeWriteAccess(c, h.DB, homeID) {
		c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Write access denied to this home")})
		return
	}

	var req MaintenanceTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(h.DB, c, "Invalid request payload")})
		return
	}

	if !isValidFrequency(req.Frequency) {
		c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(h.DB, c, "Invalid repeat frequency format")})
		return
	}

	if errMsg := validateMaintenanceTaskRequest(req); errMsg != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(h.DB, c, errMsg)})
		return
	}

	if req.InventoryItemID != nil {
		var item models.InventoryItem
		if err := h.DB.First(&item, req.InventoryItemID).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				c.JSON(http.StatusNotFound, gin.H{"error": i18n.TranslateDB(h.DB, c, "Inventory item not found")})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to fetch inventory item")})
			}
			return
		}
		if item.HomeID != homeID {
			c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Inventory item does not belong to this home")})
			return
		}
	}

	for index, dep := range req.Dependencies {
		value, err := quantityForDefinition(h.DB, dep.ItemDefinitionID, homeID, dep.QuantityRequired, dep.QuantityUnitID, false, dep.raw("quantity_required"))
		if err != nil {
			quantityError(c, h.DB, err)
			return
		}
		req.Dependencies[index].QuantityRequired = value
	}
	task := models.MaintenanceTask{
		HomeID:                homeID,
		InventoryItemID:       req.InventoryItemID,
		Description:           req.Description,
		ScheduledDate:         req.ScheduledDate,
		Frequency:             req.Frequency,
		CustomFrequency:       req.CustomFrequency,
		CustomFrequencyMetric: req.CustomFrequencyMetric,
		IsCompleted:           false, // ⚡ Bolt: Always create as incomplete to enforce inventory deduction via the dedicated /complete endpoint.
	}

	err := h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&task).Error; err != nil {
			return err
		}

		for _, dep := range req.Dependencies {
			dependency := models.TaskItemDependency{
				MaintenanceTaskID: task.ID,
				ItemDefinitionID:  dep.ItemDefinitionID,
				QuantityRequired:  dep.QuantityRequired,
			}
			if err := tx.Create(&dependency).Error; err != nil {
				return err
			}
		}

		return maintenance.SyncLegacyTask(tx, task.ID)
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to create maintenance task")})
		return
	}

	// Reload to include dependencies
	var result models.MaintenanceTask
	h.DB.Preload("Dependencies.ItemDefinition").First(&result, task.ID)

	c.JSON(http.StatusCreated, result)
}

func (h *MaintenanceTaskHandler) UpdateMaintenanceTask(c *gin.Context) {
	id, ok := utils.ParseUUIDParam(c, h.DB, "id", "Invalid maintenance task ID")
	if !ok {
		return
	}

	var task models.MaintenanceTask
	if err := h.DB.First(&task, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": i18n.TranslateDB(h.DB, c, "Maintenance task not found")})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to update maintenance task")})
		}
		return
	}

	if !utils.VerifyHomeWriteAccess(c, h.DB, task.HomeID) {
		c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Write access denied to this home")})
		return
	}

	var req MaintenanceTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(h.DB, c, "Invalid request payload")})
		return
	}

	if !isValidFrequency(req.Frequency) {
		c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(h.DB, c, "Invalid repeat frequency format")})
		return
	}

	if errMsg := validateMaintenanceTaskRequest(req); errMsg != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(h.DB, c, errMsg)})
		return
	}

	if req.InventoryItemID != nil {
		var item models.InventoryItem
		if err := h.DB.First(&item, req.InventoryItemID).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				c.JSON(http.StatusNotFound, gin.H{"error": i18n.TranslateDB(h.DB, c, "Inventory item not found")})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to fetch inventory item")})
			}
			return
		}
		if item.HomeID != task.HomeID {
			c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Inventory item does not belong to this home")})
			return
		}
	}

	for index, dep := range req.Dependencies {
		value, err := quantityForDefinition(h.DB, dep.ItemDefinitionID, task.HomeID, dep.QuantityRequired, dep.QuantityUnitID, false, dep.raw("quantity_required"))
		if err != nil {
			quantityError(c, h.DB, err)
			return
		}
		req.Dependencies[index].QuantityRequired = value
	}
	updates := map[string]interface{}{
		"inventory_item_id":       req.InventoryItemID,
		"description":             req.Description,
		"scheduled_date":          req.ScheduledDate,
		"frequency":               req.Frequency,
		"custom_frequency":        req.CustomFrequency,
		"custom_frequency_metric": req.CustomFrequencyMetric,
	}

	user, ok := utils.GetAuthUserID(c, h.DB)
	if !ok {
		return
	}
	if req.ExpectedVersion != nil && *req.ExpectedVersion < 0 {
		h.maintenanceFailure(c, maintenance.ErrInvalid, "Failed to update maintenance task")
		return
	}
	err := h.DB.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		locked, err := maintenance.LoadForWrite(tx, user, id)
		if err != nil {
			return err
		}
		if err := maintenance.CheckVersion(locked, req.ExpectedVersion); err != nil {
			return err
		}
		if locked.IsCompleted {
			return maintenance.ErrCompleted
		}
		if err := maintenance.CheckLegacyWrite(tx, locked); err != nil {
			return err
		}
		updates["version"] = locked.Version + 1
		changed := tx.Model(&locked).Updates(updates)
		if changed.Error != nil {
			return changed.Error
		}
		if changed.RowsAffected != 1 {
			return maintenance.ErrConflict
		}

		// ⚡ Bolt: Only update dependencies if they are provided in the request to support partial updates and avoid data loss.
		if req.Dependencies != nil {
			// Update dependencies: simple approach, delete existing and recreate
			if err := tx.Where("maintenance_task_id = ?", task.ID).Delete(&models.TaskItemDependency{}).Error; err != nil {
				return err
			}

			for _, dep := range req.Dependencies {
				dependency := models.TaskItemDependency{
					MaintenanceTaskID: task.ID,
					ItemDefinitionID:  dep.ItemDefinitionID,
					QuantityRequired:  dep.QuantityRequired,
				}
				if err := tx.Create(&dependency).Error; err != nil {
					return err
				}
			}
		}

		return maintenance.SyncLegacyTask(tx, task.ID)
	})

	if err != nil {
		h.maintenanceFailure(c, err, "Failed to update maintenance task")
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": i18n.TranslateDB(h.DB, c, "Maintenance task updated successfully")})
}

func (h *MaintenanceTaskHandler) DeleteMaintenanceTask(c *gin.Context) {
	id, ok := utils.ParseUUIDParam(c, h.DB, "id", "Invalid maintenance task ID")
	if !ok {
		return
	}

	var task models.MaintenanceTask
	if err := h.DB.First(&task, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": i18n.TranslateDB(h.DB, c, "Maintenance task not found")})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to fetch maintenance task")})
		}
		return
	}

	if !utils.VerifyHomeWriteAccess(c, h.DB, task.HomeID) {
		c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Write access denied to this home")})
		return
	}

	user, ok := utils.GetAuthUserID(c, h.DB)
	if !ok {
		return
	}
	var expected *int64
	if values, supplied := c.Request.URL.Query()["expected_version"]; supplied {
		if len(values) != 1 {
			h.maintenanceFailure(c, maintenance.ErrInvalid, "Failed to delete maintenance task")
			return
		}
		version, err := strconv.ParseInt(values[0], 10, 64)
		if err != nil || version < 0 {
			h.maintenanceFailure(c, maintenance.ErrInvalid, "Failed to delete maintenance task")
			return
		}
		expected = &version
	}
	err := h.DB.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		locked, err := maintenance.LoadForWrite(tx, user, id)
		if err != nil {
			return err
		}
		if err := maintenance.CheckVersion(locked, expected); err != nil {
			return err
		}
		if locked.IsCompleted {
			return maintenance.ErrCompleted
		}
		if err := maintenance.RetireLegacyTask(tx, locked, user, time.Now()); err != nil {
			return err
		}
		deleted := tx.Delete(&locked)
		if deleted.Error != nil {
			return deleted.Error
		}
		if deleted.RowsAffected != 1 {
			return maintenance.ErrConflict
		}
		return nil
	})
	if err != nil {
		h.maintenanceFailure(c, err, "Failed to delete maintenance task")
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": i18n.TranslateDB(h.DB, c, "Maintenance task deleted successfully")})
}

func isValidFrequency(freq string) bool {
	f := strings.TrimSpace(freq)
	if f == "" {
		return true // treated as "Once"
	}

	lf := strings.ToLower(f)
	switch lf {
	case "once", "daily", "weekly", "monthly", "every 3 months", "every 6 months", "yearly", "custom":
		return true
	}
	return false
}

func validateMaintenanceTaskRequest(req MaintenanceTaskRequest) string {
	freq := strings.ToLower(strings.TrimSpace(req.Frequency))

	if freq == "custom" {
		if req.CustomFrequency == nil {
			return "Custom frequency must be a positive number"
		}
		if *req.CustomFrequency <= 0 {
			return "Custom frequency must be a positive number"
		}
		if req.CustomFrequencyMetric == nil || strings.TrimSpace(*req.CustomFrequencyMetric) == "" {
			return "Custom frequency metric is required"
		}
		metric := strings.ToLower(strings.TrimSpace(*req.CustomFrequencyMetric))
		if metric != "day" && metric != "week" && metric != "month" && metric != "year" {
			return "Custom frequency metric must be day, week, month, or year"
		}
	} else {
		// If frequency is not custom, then custom_ fields should not be provided
		if req.CustomFrequency != nil {
			return "Custom frequency and metric should not be provided for non-custom frequencies"
		}
		if req.CustomFrequencyMetric != nil && strings.TrimSpace(*req.CustomFrequencyMetric) != "" {
			return "Custom frequency and metric should not be provided for non-custom frequencies"
		}
	}
	return ""
}

func parseFrequencyAndAdvance(current time.Time, task models.MaintenanceTask) (time.Time, bool) {
	f := strings.TrimSpace(task.Frequency)
	lf := strings.ToLower(f)

	switch lf {
	case "once", "":
		return current, false
	case "daily":
		return current.AddDate(0, 0, 1), true
	case "weekly":
		return current.AddDate(0, 0, 7), true
	case "monthly":
		return current.AddDate(0, 1, 0), true
	case "every 3 months":
		return current.AddDate(0, 3, 0), true
	case "every 6 months":
		return current.AddDate(0, 6, 0), true
	case "yearly":
		return current.AddDate(1, 0, 0), true
	case "custom":
		if task.CustomFrequency == nil || *task.CustomFrequency <= 0 || task.CustomFrequencyMetric == nil {
			return current, false
		}
		metric := strings.ToLower(strings.TrimSpace(*task.CustomFrequencyMetric))
		val := int(*task.CustomFrequency)
		if val <= 0 {
			return current, false
		}

		switch metric {
		case "day":
			return current.AddDate(0, 0, val), true
		case "week":
			return current.AddDate(0, 0, val*7), true
		case "month":
			return current.AddDate(0, val, 0), true
		case "year":
			return current.AddDate(val, 0, 0), true
		}
	}

	// Unrecognized/Invalid treats as "once" and does not repeat
	return current, false
}

func (r *TaskItemDependencyRequest) UnmarshalJSON(b []byte) error {
	type plain TaskItemDependencyRequest
	return decodeQuantityRequest(b, (*plain)(r), &r.quantityInputs)
}
