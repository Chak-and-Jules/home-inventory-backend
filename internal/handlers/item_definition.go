package handlers

import (
	"net/http"

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
	var defs []models.ItemDefinition
	if err := h.DB.Preload("Category").Preload("SizeUnit").Find(&defs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch item definitions"})
		return
	}
	c.JSON(http.StatusOK, defs)
}

func (h *ItemDefinitionHandler) CreateItemDefinition(c *gin.Context) {
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

	c.JSON(http.StatusCreated, def)
}

func (h *ItemDefinitionHandler) UpdateItemDefinition(c *gin.Context) {
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

	c.JSON(http.StatusOK, gin.H{"message": "Item definition updated successfully"})
}

func (h *ItemDefinitionHandler) DeleteItemDefinition(c *gin.Context) {
	id, ok := utils.ParseUUIDParam(c, "id", "Invalid item definition ID")
	if !ok {
		return
	}

	if err := h.DB.Delete(&models.ItemDefinition{}, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete item definition (it might be in use)"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Item definition deleted successfully"})
}
