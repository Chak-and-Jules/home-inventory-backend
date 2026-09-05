package handlers

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/i18n"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/logger"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ProfileHandler struct {
	DB *gorm.DB
}

type ProfileSyncRequest struct {
	User ProfileSyncUser `json:"profile" binding:"required"`
}

type ProfileSyncUser struct {
	ID    uuid.UUID `json:"id" binding:"required"`
	Email string    `json:"email" binding:"required,email"`
}

type DeleteAccountRequest struct {
	Email  string `json:"email" binding:"required,email"`
	UserID string `json:"user_id" binding:"required"`
}

func (h *ProfileHandler) SyncProfile(c *gin.Context) {
	authUserID := c.MustGet("userID").(uuid.UUID)
	authEmail := c.MustGet("email").(string)

	var req ProfileSyncRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(h.DB, c, "Invalid request payload")})
		return
	}

	if req.User.ID != authUserID {
		c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Profile user ID does not match authenticated user")})
		return
	}

	if !strings.EqualFold(req.User.Email, authEmail) {
		c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Profile email does not match authenticated user")})
		return
	}

	profile := models.Profile{
		ID:    req.User.ID,
		Email: req.User.Email,
	}

	if err := h.DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{"updated_at"}),
	}).Create(&profile).Error; err != nil {
		logger.Log.Error("Failed to sync profile", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to sync profile")})
		return
	}

	var exists int
	if err := h.DB.Model(&models.UserHome{}).Select("1").Where("user_id = ?", authUserID).Limit(1).Find(&exists).Error; err != nil {
		logger.Log.Error("Failed to check homes", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to check homes")})
		return
	}

	if exists == 0 {
		home := models.Home{Name: "My Home"}

		err := h.DB.Transaction(func(tx *gorm.DB) error {
			if err := tx.Create(&home).Error; err != nil {
				return err
			}

			userHome := models.UserHome{
				UserID:    authUserID,
				HomeID:    home.ID,
				Role:      models.RoleOwner,
				IsDefault: true,
			}
			return tx.Create(&userHome).Error
		})

		if err != nil {
			logger.Log.Error("Failed to create default home", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to create default home")})
			return
		}
	}

	c.JSON(http.StatusOK, profile)
}

func (h *ProfileHandler) GetProfile(c *gin.Context) {
	userID := c.MustGet("userID").(uuid.UUID)

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
	userID := c.MustGet("userID").(uuid.UUID)

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
	userIDVal, exists := c.Get("userID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}
	authUserID := userIDVal.(uuid.UUID)

	emailVal, exists := c.Get("email")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}
	authEmail := emailVal.(string)

	var req DeleteAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload"})
		return
	}

	userID, err := uuid.Parse(req.UserID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user ID format"})
		return
	}

	if userID != authUserID {
		c.JSON(http.StatusForbidden, gin.H{"error": "Unauthorized account deletion"})
		return
	}

	if !strings.EqualFold(req.Email, authEmail) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Unauthorized account deletion"})
		return
	}

	var profile models.Profile
	if err := h.DB.Where("id = ? AND email = ?", userID, req.Email).First(&profile).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Account not found"})
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

		return nil
	})

	if err != nil {
		logger.Log.Error("Failed to delete account from database", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete account"})
		return
	}

	supabaseURL := os.Getenv("SUPABASE_URL")
	serviceKey := os.Getenv("SUPABASE_SERVICE_ROLE_KEY")

	if supabaseURL != "" && serviceKey != "" {
		reqURL := fmt.Sprintf("%s/auth/v1/admin/users/%s", supabaseURL, userID.String())
		httpReq, err := http.NewRequest(http.MethodDelete, reqURL, nil)
		if err == nil {
			httpReq.Header.Set("Authorization", "Bearer "+serviceKey)
			httpReq.Header.Set("apikey", serviceKey)

			client := &http.Client{Timeout: 10 * time.Second}
			resp, err := client.Do(httpReq)
			if err != nil {
				logger.Log.Error("Failed to delete user from Supabase auth", zap.Error(err))
			} else {
				defer resp.Body.Close()
				if resp.StatusCode >= 400 {
					logger.Log.Error("Supabase auth deletion returned error status", zap.Int("status", resp.StatusCode))
				}
			}
		} else {
			logger.Log.Error("Failed to create request for Supabase auth deletion", zap.Error(err))
		}
	}

	i18n.InvalidateUserLanguageCache(userID)

	c.JSON(http.StatusOK, gin.H{"message": "Account deleted successfully"})
}
