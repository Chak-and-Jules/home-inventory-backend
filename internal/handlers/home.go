package handlers

import (
	"net/http"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type HomeHandler struct {
	DB *gorm.DB
}

type CreateHomeRequest struct {
	Name string `json:"name" binding:"required"`
}

type UpdateHomeRequest struct {
	Name string `json:"name" binding:"required"`
}

func (h *HomeHandler) GetHomes(c *gin.Context) {
	userID := c.MustGet("userID").(uuid.UUID)

	var userHomes []models.UserHome
	if err := h.DB.Preload("Home").Where("user_id = ?", userID).Find(&userHomes).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch homes"})
		return
	}

	c.JSON(http.StatusOK, userHomes)
}

func (h *HomeHandler) CreateHome(c *gin.Context) {
	userID := c.MustGet("userID").(uuid.UUID)
	var req CreateHomeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	home := models.Home{Name: req.Name}

	err := h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&home).Error; err != nil {
			return err
		}

		userHome := models.UserHome{
			UserID:    userID,
			HomeID:    home.ID,
			Role:      "owner",
			IsDefault: false,
		}
		if err := tx.Create(&userHome).Error; err != nil {
			return err
		}
		return nil
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create home"})
		return
	}

	c.JSON(http.StatusCreated, home)
}

func (h *HomeHandler) UpdateHome(c *gin.Context) {
	userID := c.MustGet("userID").(uuid.UUID)
	homeIDStr := c.Param("id")
	homeID, err := uuid.Parse(homeIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid home ID"})
		return
	}

	var req UpdateHomeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var userHome models.UserHome
	if err := h.DB.Where("user_id = ? AND home_id = ?", userID, homeID).First(&userHome).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Home not found or access denied"})
		return
	}
	if userHome.Role != "owner" && userHome.Role != "editor" {
		c.JSON(http.StatusForbidden, gin.H{"error": "Insufficient permissions to update home"})
		return
	}

	if err := h.DB.Model(&models.Home{}).Where("id = ?", homeID).Update("name", req.Name).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update home"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Home updated successfully"})
}

func (h *HomeHandler) DeleteHome(c *gin.Context) {
	userID := c.MustGet("userID").(uuid.UUID)
	homeIDStr := c.Param("id")
	homeID, err := uuid.Parse(homeIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid home ID"})
		return
	}

	var userHome models.UserHome
	if err := h.DB.Where("user_id = ? AND home_id = ?", userID, homeID).First(&userHome).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Home not found or access denied"})
		return
	}
	if userHome.Role != "owner" {
		c.JSON(http.StatusForbidden, gin.H{"error": "Only owners can delete homes"})
		return
	}

	if err := h.DB.Delete(&models.Home{}, "id = ?", homeID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete home"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Home deleted successfully"})
}

func (h *HomeHandler) SetDefaultHome(c *gin.Context) {
	userID := c.MustGet("userID").(uuid.UUID)
	homeIDStr := c.Param("id")
	homeID, err := uuid.Parse(homeIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid home ID"})
		return
	}

	// Verify user has access to this home
	var userHome models.UserHome
	if err := h.DB.Where("user_id = ? AND home_id = ?", userID, homeID).First(&userHome).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Home not found or access denied"})
		return
	}

	err = h.DB.Transaction(func(tx *gorm.DB) error {
		// Unset all default homes for the user
		if err := tx.Model(&models.UserHome{}).Where("user_id = ?", userID).Update("is_default", false).Error; err != nil {
			return err
		}
		// Set the selected home as default
		if err := tx.Model(&models.UserHome{}).Where("user_id = ? AND home_id = ?", userID, homeID).Update("is_default", true).Error; err != nil {
			return err
		}
		return nil
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to set default home"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Default home updated successfully"})
}
