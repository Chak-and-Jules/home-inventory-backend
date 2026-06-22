package utils

import (
	"fmt"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/logger"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// SendLowStockNotification logs a notification for low stock items.
// In a real application, this would send a push notification or an in-app alert.
func SendLowStockNotification(homeID uuid.UUID, itemName string, priority string) {
	msg := fmt.Sprintf("Low stock alert for home %s: %s", homeID, itemName)
	if priority == "high" {
		msg = fmt.Sprintf("URGENT: %s", msg)
	}

	logger.Log.Info("Notification Triggered",
		zap.String("type", "low_stock"),
		zap.String("home_id", homeID.String()),
		zap.String("item_name", itemName),
		zap.String("priority", priority),
		zap.String("message", msg),
	)
}
