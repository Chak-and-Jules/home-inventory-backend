package handlers

import (
	"net/http"
	"strings"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
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

func (h *ProfileHandler) SyncProfile(c *gin.Context) {
	authUserID := c.MustGet("userID").(uuid.UUID)
	authEmail := c.MustGet("email").(string)

	var req ProfileSyncRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.User.ID != authUserID {
		c.JSON(http.StatusForbidden, gin.H{"error": "Profile user ID does not match authenticated user"})
		return
	}

	if !strings.EqualFold(req.User.Email, authEmail) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Profile email does not match authenticated user"})
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to sync profile"})
		return
	}

	var exists int
	if err := h.DB.Model(&models.UserHome{}).Select("1").Where("user_id = ?", authUserID).Limit(1).Find(&exists).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check homes"})
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
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create default home"})
			return
		}
	}

	c.JSON(http.StatusOK, profile)
}
