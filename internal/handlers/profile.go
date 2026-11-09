package handlers

import (
	"context"
	"net/http"
	"strings"
	"time"

	"firebase.google.com/go/v4/auth"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/authentication"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/demos"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/i18n"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/logger"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/utils"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ProfileHandler struct {
	DB   *gorm.DB
	Auth authentication.Client
}

type ProfileSyncRequest struct {
	User ProfileSyncUser `json:"profile" binding:"required"`
}

type ProfileSyncUser struct {
	ID    string `json:"id" binding:"required"`
	Email string `json:"email" binding:"required,email"`
}

type DeleteAccountRequest struct {
	Email  string `json:"email" binding:"required,email"`
	UserID string `json:"user_id" binding:"required"`
}

func (h *ProfileHandler) SyncProfile(c *gin.Context) {
	authUserID, ok := utils.GetAuthUserID(c, h.DB)
	if !ok {
		return
	}
	authEmail, ok := utils.GetAuthEmail(c, h.DB)
	if !ok {
		return
	}

	var req ProfileSyncRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(h.DB, c, "Invalid request payload")})
		return
	}

	firebaseUID := c.GetString("firebaseUID")
	if firebaseUID == "" || req.User.ID != firebaseUID {
		c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Profile user ID does not match authenticated user")})
		return
	}

	if !strings.EqualFold(req.User.Email, authEmail) {
		c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Profile email does not match authenticated user")})
		return
	}

	profile := models.Profile{
		ID:          authUserID,
		Email:       authEmail,
		FirebaseUID: &firebaseUID,
	}

	if err := h.DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{"updated_at"}),
	}).Create(&profile).Error; err != nil {
		logger.Log.Error("Failed to sync profile", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to sync profile")})
		return
	}

	if err := demos.New(h.DB).EnsureInitialHomes(c.Request.Context(), authUserID, demoLabels(h.DB, c)); err != nil {
		logger.Log.Error("Failed to provision homes", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to provision homes")})
		return
	}

	c.JSON(http.StatusOK, profile)
}

func (h *ProfileHandler) GetProfile(c *gin.Context) {
	userID, ok := utils.GetAuthUserID(c, h.DB)
	if !ok {
		return
	}

	var profile models.Profile
	if err := h.DB.Select("web_theme", "mobile_theme", "language_id", "restock_window").Where("id = ?", userID).First(&profile).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": i18n.TranslateDB(h.DB, c, "Profile not found")})
			return
		}
		logger.Log.Error("Failed to fetch profile", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to fetch profile")})
		return
	}

	// Default to 7 if RestockWindow is nil
	restockWindow := 7
	if profile.RestockWindow != nil {
		restockWindow = *profile.RestockWindow
	}

	response := gin.H{
		"web_theme":      profile.WebTheme,
		"mobile_theme":   profile.MobileTheme,
		"language_id":    profile.LanguageID,
		"restock_window": restockWindow,
	}

	c.JSON(http.StatusOK, response)
}

func (h *ProfileHandler) UpdateProfile(c *gin.Context) {
	userID, ok := utils.GetAuthUserID(c, h.DB)
	if !ok {
		return
	}

	var payload map[string]interface{}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(h.DB, c, "Invalid request payload")})
		return
	}

	updates := make(map[string]interface{})
	if webTheme, ok := payload["web_theme"]; ok {
		updates["web_theme"] = webTheme
	}
	if mobileTheme, ok := payload["mobile_theme"]; ok {
		updates["mobile_theme"] = mobileTheme
	}
	if languageID, ok := payload["language_id"]; ok {
		updates["language_id"] = languageID
	}
	if restockWindow, ok := payload["restock_window"]; ok {
		if restockWindow == nil {
			updates["restock_window"] = nil
		} else {
			switch val := restockWindow.(type) {
			case float64:
				if val < 0 {
					c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(h.DB, c, "Restock window must be a non-negative integer")})
					return
				}
				intVal := int(val)
				updates["restock_window"] = &intVal
			case int:
				if val < 0 {
					c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(h.DB, c, "Restock window must be a non-negative integer")})
					return
				}
				updates["restock_window"] = &val
			default:
				c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(h.DB, c, "Invalid restock window")})
				return
			}
		}
	}

	if len(updates) == 0 {
		c.JSON(http.StatusOK, gin.H{"message": i18n.TranslateDB(h.DB, c, "No valid fields to update")})
		return
	}

	if err := h.DB.Model(&models.Profile{}).Where("id = ?", userID).Updates(updates).Error; err != nil {
		logger.Log.Error("Failed to update profile", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to update profile")})
		return
	}

	if _, ok := payload["language_id"]; ok {
		i18n.InvalidateUserLanguageCache(userID)
	}

	c.JSON(http.StatusOK, gin.H{"message": i18n.TranslateDB(h.DB, c, "Profile updated successfully")})
}

func (h *ProfileHandler) DeleteAccount(c *gin.Context) {
	authUserID, ok := utils.GetAuthUserID(c, h.DB)
	if !ok {
		return
	}
	authEmail, ok := utils.GetAuthEmail(c, h.DB)
	if !ok {
		return
	}

	var req DeleteAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(h.DB, c, "Invalid request payload")})
		return
	}

	userID, err := uuid.Parse(req.UserID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(h.DB, c, "Invalid user ID format")})
		return
	}

	if userID != authUserID {
		c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Unauthorized account deletion")})
		return
	}

	if !strings.EqualFold(req.Email, authEmail) {
		c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Unauthorized account deletion")})
		return
	}

	var profile models.Profile
	if err := h.DB.Where("id = ? AND email = ?", userID, authEmail).First(&profile).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": i18n.TranslateDB(h.DB, c, "Account not found")})
		return
	}

	firebaseUID := c.GetString("firebaseUID")
	if firebaseUID == "" || profile.FirebaseUID == nil || *profile.FirebaseUID != firebaseUID {
		c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Unauthorized account deletion")})
		return
	}
	if h.Auth == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Authentication is not configured")})
		return
	}

	err = h.DB.Transaction(func(tx *gorm.DB) error {
		var userHomes []models.UserHome
		if err := tx.Where("user_id = ? AND role = ?", userID, models.RoleOwner).Find(&userHomes).Error; err != nil {
			return err
		}

		var homesToDelete []uuid.UUID
		for _, uh := range userHomes {
			var otherOwnersCount int64
			if err := tx.Model(&models.UserHome{}).Where("home_id = ? AND role = ? AND user_id != ?", uh.HomeID, models.RoleOwner, userID).Count(&otherOwnersCount).Error; err != nil {
				return err
			}
			if otherOwnersCount == 0 {
				homesToDelete = append(homesToDelete, uh.HomeID)
			}
		}

		if len(homesToDelete) > 0 {
			if err := tx.Where("id IN ?", homesToDelete).Delete(&models.Home{}).Error; err != nil {
				return err
			}
		}

		if err := tx.Where("user_id = ?", userID).Delete(&models.UserHome{}).Error; err != nil {
			return err
		}

		if err := tx.Where("id = ?", userID).Delete(&models.Profile{}).Error; err != nil {
			return err
		}

		// A failed Firebase request rolls back the SQL deletion for a later retry.
		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()
		err := h.Auth.DeleteUser(ctx, *profile.FirebaseUID)
		if auth.IsUserNotFound(err) {
			return nil
		}
		return err
	})

	if err != nil {
		logger.Log.Error("Failed to delete account", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to delete account")})
		return
	}

	i18n.InvalidateUserLanguageCache(userID)

	c.JSON(http.StatusOK, gin.H{"message": i18n.TranslateDB(h.DB, c, "Account deleted successfully")})
}
