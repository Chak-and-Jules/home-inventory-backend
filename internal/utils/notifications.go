package utils

import (
	"fmt"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/logger"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// SendLowStockNotification logs a notification for low stock items.
// In a real application, this would send a push notification or an in-app alert.
func SendLowStockNotification(homeID uuid.UUID, homeName string, itemName string, priority string) {
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
func SendExpiryNotification(homeID uuid.UUID, homeName string, itemName string, expiryDate string) {
	msg := fmt.Sprintf("Item expiring soon for home %s: %s (expires on %s)", homeName, itemName, expiryDate)

	logger.Log.Info("Notification Triggered",
		zap.String("type", "expiring_soon"),
		zap.String("home_id", homeID.String()),
		zap.String("home_name", homeName),
		zap.String("item_name", itemName),
		zap.String("expiry_date", expiryDate),
		zap.String("message", msg),
	)
}
