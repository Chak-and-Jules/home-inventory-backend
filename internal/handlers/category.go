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

type CategoryHandler struct {
	DB         *gorm.DB
	mu         sync.RWMutex
	cache      []models.Category
	cacheValid bool
}

type CategoryRequest struct {
	Name     string     `json:"name" binding:"required"`
	ParentID *uuid.UUID `json:"parent_id"` // Optional
}

func (h *CategoryHandler) GetCategories(c *gin.Context) {
	h.mu.RLock()
	if h.cacheValid {
		categories := h.cache
		h.mu.RUnlock()
		c.JSON(http.StatusOK, categories)
		return
	}
	h.mu.RUnlock()

	var categories []models.Category
	if err := h.DB.Preload("Parent").Find(&categories).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch categories"})
		return
	}

	h.mu.Lock()
	h.cache = categories
	h.cacheValid = true
	h.mu.Unlock()

	c.JSON(http.StatusOK, categories)
}

func (h *CategoryHandler) CreateCategory(c *gin.Context) {
	var req CategoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	category := models.Category{
		Name:     req.Name,
		ParentID: req.ParentID,
	}

	if err := h.DB.Create(&category).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create category"})
		return
	}

	h.mu.Lock()
	h.cacheValid = false
	h.mu.Unlock()

	c.JSON(http.StatusCreated, category)
}

func (h *CategoryHandler) UpdateCategory(c *gin.Context) {
	id, ok := utils.ParseUUIDParam(c, "id", "Invalid category ID")
	if !ok {
		return
	}

	var req CategoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.DB.Model(&models.Category{}).Where("id = ?", id).Updates(map[string]interface{}{
		"name":      req.Name,
		"parent_id": req.ParentID,
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update category"})
		return
	}

	h.mu.Lock()
	h.cacheValid = false
	h.mu.Unlock()

	c.JSON(http.StatusOK, gin.H{"message": "Category updated successfully"})
}

func (h *CategoryHandler) DeleteCategory(c *gin.Context) {
	id, ok := utils.ParseUUIDParam(c, "id", "Invalid category ID")
	if !ok {
		return
	}

	if err := h.DB.Delete(&models.Category{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete category"})
		return
	}

	h.mu.Lock()
	h.cacheValid = false
	h.mu.Unlock()

	c.JSON(http.StatusOK, gin.H{"message": "Category deleted successfully"})
}
