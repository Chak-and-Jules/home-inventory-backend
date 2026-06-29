package utils

import (
	"fmt"
	"time"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/logger"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// SendLowStockNotification logs a notification for low stock items.
// In a real application, this would send a push notification or an in-app alert.
func SendLowStockNotification(db *gorm.DB, homeID uuid.UUID, itemName string, priority string) {
	var home models.Home
	homeName := homeID.String()
	if db != nil {
		if err := db.First(&home, homeID).Error; err == nil {
			homeName = home.Name
		}
	}

	msg := fmt.Sprintf("Low stock alert for home %s: %s", homeName, itemName)
	if priority == "high" {
		msg = fmt.Sprintf("URGENT: %s", msg)
	}

	logger.Log.Info("Notification Triggered",
		zap.String("type", "low_stock"),
		zap.String("home_id", homeID.String()),
		zap.String("home_name", homeName),
		zap.String("item_name", itemName),
		zap.String("priority", priority),
		zap.String("message", msg),
	)
}

// SendExpiryNotification logs a notification for items expiring soon.
func SendExpiryNotification(db *gorm.DB, homeID uuid.UUID, itemName string, expiryDate time.Time) {
	var home models.Home
	homeName := homeID.String()
	if db != nil {
		if err := db.First(&home, homeID).Error; err == nil {
			homeName = home.Name
		}
	}

	msg := fmt.Sprintf("Item expiring soon for home %s: %s (expires on %s)", homeName, itemName, expiryDate.Format("2006-01-02"))

	logger.Log.Info("Notification Triggered",
		zap.String("type", "expiring_soon"),
		zap.String("home_id", homeID.String()),
		zap.String("home_name", homeName),
		zap.String("item_name", itemName),
		zap.String("expiry_date", expiryDate.Format("2006-01-02")),
		zap.String("message", msg),
	)
}
