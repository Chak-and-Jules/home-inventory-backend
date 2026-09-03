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

type AddHomeUserRequest struct {
	Email string `json:"email" binding:"required,email"`
	Role  string `json:"role" binding:"required,oneof=owner editor viewer"`
}

type UpdateHomeUserRoleRequest struct {
	Role string `json:"role" binding:"required,oneof=owner editor viewer"`
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

func (h *HomeHandler) UpdateHomeUserRole(c *gin.Context) {
	userID := c.MustGet("userID").(uuid.UUID)
	homeID, ok := utils.ParseUUIDParam(c, h.DB, "id", "Invalid home ID")
	if !ok {
		return
	}
	targetUserID, ok := utils.ParseUUIDParam(c, h.DB, "userId", "Invalid user ID")
	if !ok {
		return
	}

	var req UpdateHomeUserRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(h.DB, c, "Invalid request payload")})
		return
	}

	// RBAC: Only owners and editors can update roles
	hasAccess, requesterHome := h.requireHomeRole(c, userID, homeID, models.RoleOwner, models.RoleEditor)
	if !hasAccess {
		return
	}

	if requesterHome.Role == models.RoleEditor && req.Role == models.RoleOwner {
		c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Editors cannot grant owner role")})
		return
	}

	var userHome models.UserHome
	if err := h.DB.Where("user_id = ? AND home_id = ?", targetUserID, homeID).First(&userHome).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": i18n.TranslateDB(h.DB, c, "User not found in home")})
		} else {
			logger.Log.Error("Failed to find user in home", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to find user in home")})
		}
		return
	}

	if requesterHome.Role == models.RoleEditor && userHome.Role == models.RoleOwner {
		c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Editors cannot modify owner roles")})
		return
	}

	if err := h.DB.Model(&userHome).Update("role", req.Role).Error; err != nil {
		logger.Log.Error("Failed to update user role", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to update user role")})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": i18n.TranslateDB(h.DB, c, "User role updated successfully")})
}

func (h *HomeHandler) RemoveHomeUser(c *gin.Context) {
	userID := c.MustGet("userID").(uuid.UUID)
	homeID, ok := utils.ParseUUIDParam(c, h.DB, "id", "Invalid home ID")
	if !ok {
		return
	}
	targetUserID, ok := utils.ParseUUIDParam(c, h.DB, "userId", "Invalid user ID")
	if !ok {
		return
	}

	// RBAC: Only owners and editors can remove users
	hasAccess, requesterHome := h.requireHomeRole(c, userID, homeID, models.RoleOwner, models.RoleEditor)
	if !hasAccess {
		return
	}

	var userHome models.UserHome
	if err := h.DB.Where("user_id = ? AND home_id = ?", targetUserID, homeID).First(&userHome).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": i18n.TranslateDB(h.DB, c, "User not found in home")})
		} else {
			logger.Log.Error("Failed to find user in home", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to find user in home")})
		}
		return
	}

	if requesterHome.Role == models.RoleEditor && userHome.Role == models.RoleOwner {
		c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Editors cannot remove owners")})
		return
	}

	if err := h.DB.Where("user_id = ? AND home_id = ?", targetUserID, homeID).Delete(&models.UserHome{}).Error; err != nil {
		logger.Log.Error("Failed to remove user from home", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to remove user from home")})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": i18n.TranslateDB(h.DB, c, "User removed from home successfully")})
}

func (h *HomeHandler) AddHomeUser(c *gin.Context) {
	userID := c.MustGet("userID").(uuid.UUID)
	homeID, ok := utils.ParseUUIDParam(c, h.DB, "id", "Invalid home ID")
	if !ok {
		return
	}

	var req AddHomeUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(h.DB, c, "Invalid request payload")})
		return
	}

	// RBAC: Only owners and editors can add users
	hasAccess, requesterHome := h.requireHomeRole(c, userID, homeID, models.RoleOwner, models.RoleEditor)
	if !hasAccess {
		return
	}

	if requesterHome.Role == models.RoleEditor && req.Role == models.RoleOwner {
		c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Editors cannot add owners")})
		return
	}

	var profile models.Profile
	if err := h.DB.Where("email = ?", req.Email).First(&profile).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": i18n.TranslateDB(h.DB, c, "User not found")})
		} else {
			logger.Log.Error("Failed to find user profile", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to find user profile")})
		}
		return
	}

	// Check if user is already in the home
	var count int64
	h.DB.Model(&models.UserHome{}).Where("user_id = ? AND home_id = ?", profile.ID, homeID).Count(&count)
	if count > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": i18n.TranslateDB(h.DB, c, "User is already a member of this home")})
		return
	}

	userHome := models.UserHome{
		UserID: profile.ID,
		HomeID: homeID,
		Role:   req.Role,
	}

	if err := h.DB.Create(&userHome).Error; err != nil {
		logger.Log.Error("Failed to add user to home", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to add user to home")})
		return
	}

	// Preload User for the response
	h.DB.Preload("User").First(&userHome)

	c.JSON(http.StatusCreated, userHome)
}

// requireHomeRole retrieves the user's home access record and checks if they have one of the allowed roles.
// It handles sending the appropriate 404 or 403 error response.
func (h *HomeHandler) requireHomeRole(c *gin.Context, userID, homeID uuid.UUID, allowedRoles ...string) (bool, *models.UserHome) {
	var userHome models.UserHome
	if err := h.DB.Where("user_id = ? AND home_id = ?", userID, homeID).First(&userHome).Error; err != nil {
		logger.Log.Warn("Home not found or access denied", zap.Error(err))
		c.JSON(http.StatusNotFound, gin.H{"error": i18n.TranslateDB(h.DB, c, "Home not found or access denied")})
		return false, nil
	}

	if len(allowedRoles) == 0 {
		return true, &userHome
	}

	for _, role := range allowedRoles {
		if userHome.Role == role {
			return true, &userHome
		}
	}

	// Custom error messages based on the missing role
	if len(allowedRoles) == 1 && allowedRoles[0] == models.RoleOwner {
		c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Only owners can delete homes")})
	} else {
		c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Insufficient permissions to update home")})
	}
	return false, nil
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

	hasAccess, _ := h.requireHomeRole(c, userID, homeID, models.RoleOwner, models.RoleEditor)
	if !hasAccess {
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

	// RBAC: Only owners can delete homes
	hasAccess, _ := h.requireHomeRole(c, userID, homeID, models.RoleOwner)
	if !hasAccess {
		return
	}

	var userHome models.UserHome
	if err := h.DB.Where("user_id = ? AND home_id = ?", userID, homeID).First(&userHome).Error; err != nil {
		logger.Log.Warn("User home association not found", zap.Error(err))
		c.JSON(http.StatusNotFound, gin.H{"error": i18n.TranslateDB(h.DB, c, "Home not found or access denied")})
		return
	}

	var count int64
	if err := h.DB.Model(&models.UserHome{}).Where("user_id = ?", userID).Count(&count).Error; err != nil {
		logger.Log.Error("Failed to count user homes", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to delete home")})
		return
	}

	if count <= 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(h.DB, c, "Cannot delete your only home")})
		return
	}

	approved := c.Query("approved") == "true"
	if userHome.IsDefault && !approved {
		c.JSON(http.StatusConflict, gin.H{"warning": i18n.TranslateDB(h.DB, c, "This is your default home. Please confirm deletion.")})
		return
	}

	err := h.DB.Transaction(func(tx *gorm.DB) error {
		// Delete the home - this will cascade delete UserHome records
		if err := tx.Delete(&models.Home{}, homeID).Error; err != nil {
			return err
		}

		if userHome.IsDefault {
			var oldestHome models.UserHome
			if err := tx.Where("user_id = ?", userID).Order("created_at asc").First(&oldestHome).Error; err != nil {
				return err
			}
			if err := tx.Model(&oldestHome).Update("is_default", true).Error; err != nil {
				return err
			}
		}
		return nil
	})

	if err != nil {
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
	hasAccess, _ := h.requireHomeRole(c, userID, homeID)
	if !hasAccess {
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

func (h *HomeHandler) GetHomeUsers(c *gin.Context) {
	userID := c.MustGet("userID").(uuid.UUID)
	homeID, ok := utils.ParseUUIDParam(c, h.DB, "id", "Invalid home ID")
	if !ok {
		return
	}

	// RBAC: Verify user has access to this home
	hasAccess, _ := h.requireHomeRole(c, userID, homeID)
	if !hasAccess {
		return
	}

	var userHomes []models.UserHome
	if err := h.DB.Preload("User").Where("home_id = ?", homeID).Find(&userHomes).Error; err != nil {
		logger.Log.Error("Failed to fetch home users", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to fetch home users")})
		return
	}

	c.JSON(http.StatusOK, userHomes)
}
