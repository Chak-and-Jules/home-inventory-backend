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

type ItemDefinitionHandler struct {
	DB *gorm.DB
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
	// ⚡ Bolt: Pass Canonical MIME header key (X-Home-Id instead of x-home-id) to avoid runtime string allocations during normalization
	homeID, ok := utils.ParseUUIDHeader(c, h.DB, "X-Home-Id", "Invalid home_id")
	if !ok {
		return
	}

	if !utils.VerifyHomeAccess(c, h.DB, homeID) {
		c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Access denied to this home")})
		return
	}

	var defs []models.ItemDefinition
	if err := h.DB.Preload("Category").Preload("SizeUnit").Where("home_id = ?", homeID).Find(&defs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to fetch item definitions")})
		return
	}

	c.JSON(http.StatusOK, defs)
}

func (h *ItemDefinitionHandler) CreateItemDefinition(c *gin.Context) {
	// ⚡ Bolt: Pass Canonical MIME header key (X-Home-Id instead of x-home-id) to avoid runtime string allocations during normalization
	homeID, ok := utils.ParseUUIDHeader(c, h.DB, "X-Home-Id", "Invalid home_id")
	if !ok {
		return
	}

	if !utils.VerifyHomeWriteAccess(c, h.DB, homeID) {
		c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Write access denied to this home")})
		return
	}

	var req ItemDefinitionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(h.DB, c, "Invalid request payload")})
		return
	}

	def := models.ItemDefinition{
		HomeID:      homeID,
		Name:        req.Name,
		Description: req.Description,
		CategoryID:  req.CategoryID,
		SizeUnitID:  req.SizeUnitID,
		IsExpirable: req.IsExpirable,
		ImageURL:    req.ImageURL,
	}

	if err := h.DB.Create(&def).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to create item definition")})
		return
	}

	c.JSON(http.StatusCreated, def)
}

func (h *ItemDefinitionHandler) UpdateItemDefinition(c *gin.Context) {
	id, ok := utils.ParseUUIDParam(c, h.DB, "id", "Invalid item definition ID")
	if !ok {
		return
	}

	var itemDef models.ItemDefinition
	if err := h.DB.First(&itemDef, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": i18n.TranslateDB(h.DB, c, "Item definition not found")})
		return
	}

	if !utils.VerifyHomeWriteAccess(c, h.DB, itemDef.HomeID) {
		c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Write access denied to this home")})
		return
	}

	var req ItemDefinitionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(h.DB, c, "Invalid request payload")})
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

	if err := h.DB.Model(&itemDef).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to update item definition")})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": i18n.TranslateDB(h.DB, c, "Item definition updated successfully")})
}

func (h *ItemDefinitionHandler) DeleteItemDefinition(c *gin.Context) {
	id, ok := utils.ParseUUIDParam(c, h.DB, "id", "Invalid item definition ID")
	if !ok {
		return
	}

	var itemDef models.ItemDefinition
	if err := h.DB.First(&itemDef, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": i18n.TranslateDB(h.DB, c, "Item definition not found")})
		return
	}

	if !utils.VerifyHomeWriteAccess(c, h.DB, itemDef.HomeID) {
		c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Write access denied to this home")})
		return
	}

	if err := h.DB.Delete(&models.ItemDefinition{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to delete item definition (it might be in use)")})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": i18n.TranslateDB(h.DB, c, "Item definition deleted successfully")})
}
