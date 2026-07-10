package handlers

import (
	"net/http"
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

type MaintenanceTaskRequest struct {
	InventoryItemID *uuid.UUID `json:"inventory_item_id"`
	Description     string     `json:"description" binding:"required"`
	ScheduledDate   time.Time  `json:"scheduled_date" binding:"required"`
	Frequency       string     `json:"frequency"`
	IsCompleted     bool       `json:"is_completed"`
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

	query := h.DB.Preload("InventoryItem.ItemDefinition").Where("home_id = ?", homeID)

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
	if err := h.DB.Preload("InventoryItem.ItemDefinition").Where("id = ?", id).Take(&task).Error; err != nil {
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

	if req.InventoryItemID != nil {
		var item models.InventoryItem
		if err := h.DB.Where("id = ?", req.InventoryItemID).Take(&item).Error; err != nil {
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
		IsCompleted:     req.IsCompleted,
	}

	if task.IsCompleted {
		now := time.Now()
		task.CompletedAt = &now
	}

	if err := h.DB.Create(&task).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to create maintenance task")})
		return
	}

	c.JSON(http.StatusCreated, task)
}

func (h *MaintenanceTaskHandler) UpdateMaintenanceTask(c *gin.Context) {
	id, ok := utils.ParseUUIDParam(c, h.DB, "id", "Invalid maintenance task ID")
	if !ok {
		return
	}

	var task models.MaintenanceTask
	if err := h.DB.Where("id = ?", id).Take(&task).Error; err != nil {
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

	if req.InventoryItemID != nil {
		var item models.InventoryItem
		if err := h.DB.Where("id = ?", req.InventoryItemID).Take(&item).Error; err != nil {
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

	if req.IsCompleted != task.IsCompleted {
		updates["is_completed"] = req.IsCompleted
		if req.IsCompleted {
			now := time.Now()
			updates["completed_at"] = &now
		} else {
			updates["completed_at"] = nil
		}
	}

	if err := h.DB.Model(&task).Updates(updates).Error; err != nil {
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
	if err := h.DB.Where("id = ?", id).Take(&task).Error; err != nil {
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

	if err := h.DB.Where("id = ?", id).Delete(&models.MaintenanceTask{}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to delete maintenance task")})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": i18n.TranslateDB(h.DB, c, "Maintenance task deleted successfully")})
}
