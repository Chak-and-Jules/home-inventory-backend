package utils

import (
	"errors"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// GetUserHome retrieves the user's home access record
func GetUserHome(c *gin.Context, db *gorm.DB, homeID uuid.UUID) (*models.UserHome, error) {
	userID, exists := c.Get("userID")
	if !exists {
		return nil, errors.New("missing user id")
	}
	uid, ok := userID.(uuid.UUID)
	if !ok {
		return nil, errors.New("invalid user id type")
	}

	var userHome models.UserHome
	if err := db.Where("user_id = ? AND home_id = ?", uid, homeID).First(&userHome).Error; err != nil {
		return nil, err
	}
	return &userHome, nil
}

// VerifyHomeAccess checks if the user has any access to the home
func VerifyHomeAccess(c *gin.Context, db *gorm.DB, homeID uuid.UUID) bool {
	_, err := GetUserHome(c, db, homeID)
	return err == nil
}

// VerifyHomeWriteAccess checks if the user has owner or editor access to the home
func VerifyHomeWriteAccess(c *gin.Context, db *gorm.DB, homeID uuid.UUID) bool {
	userHome, err := GetUserHome(c, db, homeID)
	if err != nil {
		return false
	}
	return userHome.Role == models.RoleOwner || userHome.Role == models.RoleEditor
}
