package handlers

import (
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/i18n"
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
	ItemDefinitionID uuid.UUID `json:"item_definition_id" binding:"required"`
	QuantityRequired float64   `json:"quantity_required" binding:"required"`
}

type MaintenanceTaskRequest struct {
	InventoryItemID *uuid.UUID                  `json:"inventory_item_id"`
	Description     string                      `json:"description" binding:"required"`
	ScheduledDate   time.Time                   `json:"scheduled_date" binding:"required"`
	Frequency       string                      `json:"frequency"`
	IsCompleted     bool                        `json:"is_completed"`
	Dependencies    []TaskItemDependencyRequest `json:"dependencies"`
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

	task := models.MaintenanceTask{
		HomeID:          homeID,
		InventoryItemID: req.InventoryItemID,
		Description:     req.Description,
		ScheduledDate:   req.ScheduledDate,
		Frequency:       req.Frequency,
		IsCompleted:     false, // ⚡ Bolt: Always create as incomplete to enforce inventory deduction via the dedicated /complete endpoint.
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

		return nil
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

	updates := map[string]interface{}{
		"inventory_item_id": req.InventoryItemID,
		"description":       req.Description,
		"scheduled_date":    req.ScheduledDate,
		"frequency":         req.Frequency,
	}

	err := h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&task).Updates(updates).Error; err != nil {
			return err
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

		return nil
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to update maintenance task")})
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

	if err := h.DB.Delete(&models.MaintenanceTask{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to delete maintenance task")})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": i18n.TranslateDB(h.DB, c, "Maintenance task deleted successfully")})
}

func (h *MaintenanceTaskHandler) CompleteMaintenanceTask(c *gin.Context) {
	id, ok := utils.ParseUUIDParam(c, h.DB, "id", "Invalid maintenance task ID")
	if !ok {
		return
	}

	var task models.MaintenanceTask
	if err := h.DB.Preload("Dependencies").First(&task, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": i18n.TranslateDB(h.DB, c, "Maintenance task not found")})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to fetch maintenance task")})
		}
		return
	}

	if task.IsCompleted {
		c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(h.DB, c, "Maintenance task already completed")})
		return
	}

	if !utils.VerifyHomeWriteAccess(c, h.DB, task.HomeID) {
		c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Write access denied to this home")})
		return
	}

	err := h.DB.Transaction(func(tx *gorm.DB) error {
		now := time.Now()
		updates := map[string]interface{}{
			"is_completed": true,
			"completed_at": &now,
		}

		if err := tx.Model(&models.MaintenanceTask{}).Where("id = ?", task.ID).Updates(updates).Error; err != nil {
			return err
		}

		for _, dep := range task.Dependencies {
			remainingToDeduct := dep.QuantityRequired

			var items []models.InventoryItem
			if err := tx.Where("home_id = ? AND item_definition_id = ?", task.HomeID, dep.ItemDefinitionID).
				Order("expiration_date ASC NULLS LAST").
				Find(&items).Error; err != nil {
				return err
			}

			var totalAvailable float64
			for _, item := range items {
				totalAvailable += item.Quantity
			}

			if totalAvailable < dep.QuantityRequired {
				return fmt.Errorf("insufficient stock for item %s", dep.ItemDefinitionID)
			}

			for _, item := range items {
				if remainingToDeduct <= 0 {
					break
				}

				deduction := item.Quantity
				if deduction > remainingToDeduct {
					deduction = remainingToDeduct
				}

				if err := tx.Model(&item).Update("quantity", item.Quantity-deduction).Error; err != nil {
					return err
				}

				// Log transaction
				txLog := models.InventoryTransaction{
					HomeID:           task.HomeID,
					ItemDefinitionID: dep.ItemDefinitionID,
					InventoryItemID:  item.ID,
					QuantityChange:   -deduction,
				}
				if err := tx.Create(&txLog).Error; err != nil {
					return err
				}

				remainingToDeduct -= deduction
			}

			if err := utils.UpdateShoppingListForDefinition(tx, task.HomeID, dep.ItemDefinitionID); err != nil {
				return err
			}
		}

		return nil
	})

	if err != nil {
		if strings.HasPrefix(err.Error(), "insufficient stock") {
			c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(h.DB, c, "Insufficient stock for task dependencies")})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to complete maintenance task")})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": i18n.TranslateDB(h.DB, c, "Maintenance task completed successfully")})
}

var (
	daysRegex  = regexp.MustCompile(`^(?i)every\s+(\d+)\s+days?$`)
	weeksRegex = regexp.MustCompile(`^(?i)every\s+(\d+)\s+weeks?$`)
)

func isValidFrequency(freq string) bool {
	f := strings.TrimSpace(freq)
	if f == "" {
		return true // treated as "Once"
	}

	lf := strings.ToLower(f)
	switch lf {
	case "once", "daily", "weekly", "monthly", "every 3 months", "every 6 months", "yearly":
		return true
	}

	if matches := daysRegex.FindStringSubmatch(f); matches != nil {
		val, err := strconv.Atoi(matches[1])
		return err == nil && val > 0
	}

	if matches := weeksRegex.FindStringSubmatch(f); matches != nil {
		val, err := strconv.Atoi(matches[1])
		return err == nil && val > 0
	}

	return false
}

func parseFrequencyAndAdvance(current time.Time, freq string) (time.Time, bool) {
	f := strings.TrimSpace(freq)
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
	}

	if matches := daysRegex.FindStringSubmatch(f); matches != nil {
		if days, err := strconv.Atoi(matches[1]); err == nil && days > 0 {
			return current.AddDate(0, 0, days), true
		}
	}

	if matches := weeksRegex.FindStringSubmatch(f); matches != nil {
		if weeks, err := strconv.Atoi(matches[1]); err == nil && weeks > 0 {
			return current.AddDate(0, 0, weeks*7), true
		}
	}

	// Unrecognized/Invalid treats as "once" and does not repeat
	return current, false
}
