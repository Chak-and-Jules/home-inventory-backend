package handlers

import (
	"net/http"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/i18n"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/logger"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/utils"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
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
		logger.Log.Error("Failed to fetch homes", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to fetch homes")})
		return
	}

	c.JSON(http.StatusOK, userHomes)
}

// requireHomeRole retrieves the user's home access record and checks if they have one of the allowed roles.
// It handles sending the appropriate 404 or 403 error response.
func (h *HomeHandler) requireHomeRole(c *gin.Context, userID, homeID uuid.UUID, allowedRoles ...string) bool {
	var userHome models.UserHome
	if err := h.DB.Where("user_id = ? AND home_id = ?", userID, homeID).First(&userHome).Error; err != nil {
		logger.Log.Warn("Home not found or access denied", zap.Error(err))
		c.JSON(http.StatusNotFound, gin.H{"error": i18n.TranslateDB(h.DB, c, "Home not found or access denied")})
		return false
	}

	if len(allowedRoles) == 0 {
		return true
	}

	for _, role := range allowedRoles {
		if userHome.Role == role {
			return true
		}
	}

	// Custom error messages based on the missing role
	if len(allowedRoles) == 1 && allowedRoles[0] == models.RoleOwner {
		c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Only owners can delete homes")})
	} else {
		c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Insufficient permissions to update home")})
	}
	return false
}

func (h *HomeHandler) CreateHome(c *gin.Context) {
	userID := c.MustGet("userID").(uuid.UUID)
	var req CreateHomeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(h.DB, c, "Invalid request payload")})
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
			Role:      models.RoleOwner,
			IsDefault: false,
		}
		if err := tx.Create(&userHome).Error; err != nil {
			return err
		}
		return nil
	})

	if err != nil {
		logger.Log.Error("Failed to create home", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to create home")})
		return
	}

	c.JSON(http.StatusCreated, home)
}

func (h *HomeHandler) UpdateHome(c *gin.Context) {
	userID := c.MustGet("userID").(uuid.UUID)
	homeID, ok := utils.ParseUUIDParam(c, h.DB, "id", "Invalid home ID")
	if !ok {
		return
	}

	var req UpdateHomeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(h.DB, c, "Invalid request payload")})
		return
	}

	if !h.requireHomeRole(c, userID, homeID, models.RoleOwner, models.RoleEditor) {
		return
	}

	if err := h.DB.Model(&models.Home{}).Where("id = ?", homeID).Update("name", req.Name).Error; err != nil {
		logger.Log.Error("Failed to update home", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to update home")})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": i18n.TranslateDB(h.DB, c, "Home updated successfully")})
}

func (h *HomeHandler) DeleteHome(c *gin.Context) {
	userID := c.MustGet("userID").(uuid.UUID)
	homeID, ok := utils.ParseUUIDParam(c, h.DB, "id", "Invalid home ID")
	if !ok {
		return
	}

	if !h.requireHomeRole(c, userID, homeID, models.RoleOwner) {
		return
	}

	if err := h.DB.Delete(&models.Home{}, homeID).Error; err != nil {
		logger.Log.Error("Failed to delete home", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to delete home")})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": i18n.TranslateDB(h.DB, c, "Home deleted successfully")})
}

func (h *HomeHandler) SetDefaultHome(c *gin.Context) {
	userID := c.MustGet("userID").(uuid.UUID)
	homeID, ok := utils.ParseUUIDParam(c, h.DB, "id", "Invalid home ID")
	if !ok {
		return
	}

	// Verify user has access to this home
	if !h.requireHomeRole(c, userID, homeID) {
		return
	}

	var err error
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
		logger.Log.Error("Failed to set default home", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to set default home")})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": i18n.TranslateDB(h.DB, c, "Default home updated successfully")})
}
