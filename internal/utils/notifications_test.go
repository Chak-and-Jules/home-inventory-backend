package utils

import (
	"errors"
	"testing"
	"time"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/logger"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestNotificationLogging(t *testing.T) {
	homeID := uuid.New()
	expiry := time.Date(2026, 9, 25, 23, 30, 0, 0, time.FixedZone("local", 3*60*60))
	for _, lookup := range []string{"no database", "found", "missing", "database error"} {
		for _, notification := range []string{"normal", "urgent", "expiry"} {
			t.Run(lookup+"/"+notification, func(t *testing.T) {
				core, logs := observer.New(zap.InfoLevel)
				oldLogger := logger.Log
				logger.Log = zap.New(core)
				t.Cleanup(func() { logger.Log = oldLogger })
				var db *gorm.DB
				homeName := homeID.String()
				if lookup != "no database" {
					sqlDB, mock, err := sqlmock.New()
					require.NoError(t, err)
					t.Cleanup(func() { _ = sqlDB.Close() })
					db, err = gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{})
					require.NoError(t, err)
					query := mock.ExpectQuery(`SELECT \* FROM "homes" WHERE "homes"\."id" = \$1 ORDER BY "homes"\."id" LIMIT \$2`).WithArgs(homeID, 1)
					switch lookup {
					case "found":
						homeName = "Kitchen"
						query.WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(homeID, homeName))
					case "missing":
						query.WillReturnRows(sqlmock.NewRows([]string{"id", "name"}))
					case "database error":
						query.WillReturnError(errors.New("database offline"))
					}
					t.Cleanup(func() { require.NoError(t, mock.ExpectationsWereMet()) })
				}
				fields := map[string]interface{}{"home_id": homeID.String(), "home_name": homeName, "item_name": "Milk"}
				if notification == "expiry" {
					SendExpiryNotification(db, homeID, "Milk", expiry)
					fields["type"] = "expiring_soon"
					fields["expiry_date"] = "2026-09-25"
					fields["message"] = "Item expiring soon for home " + homeName + ": Milk (expires on 2026-09-25)"
				} else {
					priority := "medium"
					message := "Low stock alert for home " + homeName + ": Milk"
					if notification == "urgent" {
						priority = "high"
						message = "URGENT: " + message
					}
					SendLowStockNotification(db, homeID, "Milk", priority)
					fields["type"], fields["priority"], fields["message"] = "low_stock", priority, message
				}
				require.Equal(t, 1, logs.Len())
				entry := logs.All()[0]
				require.Equal(t, "Notification Triggered", entry.Message)
				require.Equal(t, fields, entry.ContextMap())
			})
		}
	}
}
