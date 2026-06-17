package handlers

import (
	"net/http"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/i18n"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/utils"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type CategoryHandler struct {
	DB *gorm.DB
}

type CategoryRequest struct {
	Name     string     `json:"name" binding:"required"`
	ParentID *uuid.UUID `json:"parent_id"` // Optional
}

func (h *CategoryHandler) GetCategories(c *gin.Context) {
	homeID, ok := utils.ParseUUIDHeader(c, h.DB, "x-home-id", "Invalid home_id")
	if !ok {
		return
	}

	if !utils.VerifyHomeAccess(c, h.DB, homeID) {
		c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Access denied to this home")})
		return
	}

	var categories []models.Category
	if err := h.DB.Preload("Parent").Where("home_id = ?", homeID).Find(&categories).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to fetch categories")})
		return
	}

	c.JSON(http.StatusOK, categories)
}

func (h *CategoryHandler) CreateCategory(c *gin.Context) {
	homeID, ok := utils.ParseUUIDHeader(c, h.DB, "x-home-id", "Invalid home_id")
	if !ok {
		return
	}

	if !utils.VerifyHomeWriteAccess(c, h.DB, homeID) {
		c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Write access denied to this home")})
		return
	}

	var req CategoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(h.DB, c, "Invalid request payload")})
		return
	}

	// Check for unique name in the same hierarchy level
	var count int64
	query := h.DB.Model(&models.Category{}).Where("home_id = ? AND name = ?", homeID, req.Name)
	if req.ParentID == nil {
		query = query.Where("parent_id IS NULL")
	} else {
		query = query.Where("parent_id = ?", req.ParentID)
	}
	if err := query.Count(&count).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to validate category uniqueness")})
		return
	}
	if count > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": i18n.TranslateDB(h.DB, c, "A category with this name already exists at this level")})
		return
	}

	category := models.Category{
		HomeID:   homeID,
		Name:     req.Name,
		ParentID: req.ParentID,
	}

	if err := h.DB.Create(&category).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to create category")})
		return
	}

	c.JSON(http.StatusCreated, category)
}

func (h *CategoryHandler) UpdateCategory(c *gin.Context) {
	id, ok := utils.ParseUUIDParam(c, h.DB, "id", "Invalid category ID")
	if !ok {
		return
	}

	var category models.Category
	if err := h.DB.First(&category, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": i18n.TranslateDB(h.DB, c, "Category not found")})
		return
	}

	if !utils.VerifyHomeWriteAccess(c, h.DB, category.HomeID) {
		c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Write access denied to this home")})
		return
	}

	var req CategoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(h.DB, c, "Invalid request payload")})
		return
	}

	// Check for unique name in the same hierarchy level
	var count int64
	query := h.DB.Model(&models.Category{}).Where("home_id = ? AND name = ? AND id != ?", category.HomeID, req.Name, id)
	if req.ParentID == nil {
		query = query.Where("parent_id IS NULL")
	} else {
		query = query.Where("parent_id = ?", req.ParentID)
	}
	if err := query.Count(&count).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to validate category uniqueness")})
		return
	}
	if count > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": i18n.TranslateDB(h.DB, c, "A category with this name already exists at this level")})
		return
	}

	if err := h.DB.Model(&category).Updates(map[string]interface{}{
		"name":      req.Name,
		"parent_id": req.ParentID,
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to update category")})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": i18n.TranslateDB(h.DB, c, "Category updated successfully")})
}

func (h *CategoryHandler) DeleteCategory(c *gin.Context) {
	id, ok := utils.ParseUUIDParam(c, h.DB, "id", "Invalid category ID")
	if !ok {
		return
	}

	var category models.Category
	if err := h.DB.First(&category, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": i18n.TranslateDB(h.DB, c, "Category not found")})
		return
	}

	if !utils.VerifyHomeWriteAccess(c, h.DB, category.HomeID) {
		c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Write access denied to this home")})
		return
	}

	if err := h.DB.Delete(&models.Category{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to delete category")})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": i18n.TranslateDB(h.DB, c, "Category deleted successfully")})
}
