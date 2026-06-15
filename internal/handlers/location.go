package handlers

import (
	"net/http"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/utils"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type LocationHandler struct {
	DB *gorm.DB
}

type LocationRequest struct {
	Name string `json:"name" binding:"required"`
}

func (h *LocationHandler) GetLocations(c *gin.Context) {
	homeID, ok := utils.ParseUUIDHeader(c, "x-home-id", "Invalid home_id")
	if !ok {
		return
	}

	if !utils.VerifyHomeAccess(c, h.DB, homeID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied to this home"})
		return
	}

	var locations []models.Location
	if err := h.DB.Where("home_id = ?", homeID).Find(&locations).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch locations"})
		return
	}

	c.JSON(http.StatusOK, locations)
}

func (h *LocationHandler) CreateLocation(c *gin.Context) {
	homeID, ok := utils.ParseUUIDHeader(c, "x-home-id", "Invalid home_id")
	if !ok {
		return
	}

	if !utils.VerifyHomeWriteAccess(c, h.DB, homeID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Write access denied to this home"})
		return
	}

	var req LocationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	location := models.Location{
		HomeID: homeID,
		Name:   req.Name,
	}

	if err := h.DB.Create(&location).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create location"})
		return
	}

	c.JSON(http.StatusCreated, location)
}

func (h *LocationHandler) UpdateLocation(c *gin.Context) {
	id, ok := utils.ParseUUIDParam(c, "id", "Invalid location ID")
	if !ok {
		return
	}

	var location models.Location
	if err := h.DB.First(&location, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Location not found"})
		return
	}

	if !utils.VerifyHomeWriteAccess(c, h.DB, location.HomeID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Write access denied to this home"})
		return
	}

	var req LocationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.DB.Model(&location).Updates(map[string]interface{}{
		"name": req.Name,
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update location"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Location updated successfully"})
}

func (h *LocationHandler) DeleteLocation(c *gin.Context) {
	id, ok := utils.ParseUUIDParam(c, "id", "Invalid location ID")
	if !ok {
		return
	}

	var location models.Location
	if err := h.DB.First(&location, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Location not found"})
		return
	}

	if !utils.VerifyHomeWriteAccess(c, h.DB, location.HomeID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Write access denied to this home"})
		return
	}

	if err := h.DB.Delete(&models.Location{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete location"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Location deleted successfully"})
}
