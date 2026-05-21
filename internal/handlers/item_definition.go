package handlers

import (
	"net/http"
	"sync"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/utils"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type ItemDefinitionHandler struct {
	DB         *gorm.DB
	mu         sync.RWMutex
	cache      []models.ItemDefinition
	cacheValid bool
}

type ItemDefinitionRequest struct {
	Name        string     `json:"name" binding:"required"`
	Description string     `json:"description"`
	CategoryID  *uuid.UUID `json:"category_id"`
	SizeUnitID  *uuid.UUID `json:"size_unit_id" binding:"required"`
	IsExpirable bool       `json:"is_expirable"`
	ImageURL    string     `json:"image_url"`
}

func (h *ItemDefinitionHandler) GetItemDefinitions(c *gin.Context) {
	h.mu.RLock()
	if h.cacheValid {
		defs := h.cache
		h.mu.RUnlock()
		c.JSON(http.StatusOK, defs)
		return
	}
	h.mu.RUnlock()

	var defs []models.ItemDefinition
	if err := h.DB.Preload("Category").Preload("SizeUnit").Find(&defs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch item definitions"})
		return
	}

	h.mu.Lock()
	h.cache = defs
	h.cacheValid = true
	h.mu.Unlock()

	c.JSON(http.StatusOK, defs)
}

func (h *ItemDefinitionHandler) verifyAdmin(c *gin.Context) bool {
	userID, exists := c.Get("userID")
	if !exists {
		return false
	}
	uid, ok := userID.(uuid.UUID)
	if !ok {
		return false
	}
	var profile models.Profile
	if err := h.DB.First(&profile, uid).Error; err != nil {
		return false
	}
	return profile.IsAdmin
}

func (h *ItemDefinitionHandler) CreateItemDefinition(c *gin.Context) {
	if !h.verifyAdmin(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Admin privileges required to create item definitions"})
		return
	}

	var req ItemDefinitionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	def := models.ItemDefinition{
		Name:        req.Name,
		Description: req.Description,
		CategoryID:  req.CategoryID,
		SizeUnitID:  req.SizeUnitID,
		IsExpirable: req.IsExpirable,
		ImageURL:    req.ImageURL,
	}

	if err := h.DB.Create(&def).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create item definition"})
		return
	}

	h.mu.Lock()
	h.cacheValid = false
	h.mu.Unlock()

	c.JSON(http.StatusCreated, def)
}

func (h *ItemDefinitionHandler) UpdateItemDefinition(c *gin.Context) {
	if !h.verifyAdmin(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Admin privileges required to update item definitions"})
		return
	}

	id, ok := utils.ParseUUIDParam(c, "id", "Invalid item definition ID")
	if !ok {
		return
	}

	var req ItemDefinitionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	updates := map[string]interface{}{
		"name":         req.Name,
		"description":  req.Description,
		"category_id":  req.CategoryID,
		"size_unit_id": req.SizeUnitID,
		"is_expirable": req.IsExpirable,
		"image_url":    req.ImageURL,
	}

	if err := h.DB.Model(&models.ItemDefinition{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update item definition"})
		return
	}

	h.mu.Lock()
	h.cacheValid = false
	h.mu.Unlock()

	c.JSON(http.StatusOK, gin.H{"message": "Item definition updated successfully"})
}

func (h *ItemDefinitionHandler) DeleteItemDefinition(c *gin.Context) {
	if !h.verifyAdmin(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Admin privileges required to delete item definitions"})
		return
	}

	id, ok := utils.ParseUUIDParam(c, "id", "Invalid item definition ID")
	if !ok {
		return
	}

	if err := h.DB.Delete(&models.ItemDefinition{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete item definition (it might be in use)"})
		return
	}

	h.mu.Lock()
	h.cacheValid = false
	h.mu.Unlock()

	c.JSON(http.StatusOK, gin.H{"message": "Item definition deleted successfully"})
}
