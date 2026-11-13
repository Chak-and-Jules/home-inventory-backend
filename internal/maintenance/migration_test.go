package maintenance

import (
	"testing"
	"time"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// The previous schema has neither task versions nor transaction provenance.
// Keep these separate from the new models so the test actually adds columns to
// populated tables, rather than only exercising fresh database creation.
type legacyTask struct {
	ID                    uuid.UUID  `gorm:"type:uuid;primaryKey"`
	HomeID                uuid.UUID  `gorm:"type:uuid;not null;index"`
	InventoryItemID       *uuid.UUID `gorm:"type:uuid;index"`
	Description           string     `gorm:"type:varchar(255);not null"`
	ScheduledDate         time.Time  `gorm:"type:timestamp with time zone;not null;index"`
	Frequency             string     `gorm:"type:varchar(50)"`
	CustomFrequency       *float64   `gorm:"type:numeric"`
	CustomFrequencyMetric *string    `gorm:"type:varchar(50)"`
	IsCompleted           bool       `gorm:"default:false"`
	CompletedAt           *time.Time `gorm:"type:timestamp with time zone"`
	CreatedAt             time.Time
	UpdatedAt             time.Time `gorm:"index"`
}

func (legacyTask) TableName() string { return "maintenance_tasks" }

type legacyTransaction struct {
	ID               uuid.UUID `gorm:"type:uuid;primaryKey"`
	HomeID           uuid.UUID `gorm:"type:uuid;not null;index"`
	ItemDefinitionID uuid.UUID `gorm:"type:uuid;not null;index"`
	InventoryItemID  uuid.UUID `gorm:"type:uuid;not null;index"`
	QuantityChange   float64   `gorm:"type:numeric(22,6);not null;check:transaction_quantity_range,quantity_change >= -1000000000 and quantity_change <= 1000000000"`
	CreatedAt        time.Time
}

func (legacyTransaction) TableName() string { return "inventory_transactions" }

func TestCompletionMigrationPreservesUnknownHistory(t *testing.T) {
	f := newFixture(t)
	require.NoError(t, f.DB.Migrator().DropTable(&models.MaintenanceRequest{}, &models.MaintenanceConsumption{}, &models.InventoryTransaction{}, &models.MaintenanceExecution{}, &models.TaskItemDependency{}, &models.MaintenanceTask{}))
	require.NoError(t, f.DB.AutoMigrate(&legacyTask{}, &legacyTransaction{}))
	now := f.Now()
	old := legacyTask{ID: f.task.ID, HomeID: f.home, Description: "Existing completed task", ScheduledDate: now, IsCompleted: true, CompletedAt: &now, Frequency: "Monthly", CreatedAt: now, UpdatedAt: now}
	log := legacyTransaction{ID: uuid.New(), HomeID: f.home, ItemDefinitionID: f.def.ID, InventoryItemID: f.stock.ID, QuantityChange: -100, CreatedAt: now}
	require.NoError(t, f.DB.Create(&old).Error)
	require.NoError(t, f.DB.Create(&log).Error)
	for i := 0; i < 2; i++ {
		require.NoError(t, f.DB.AutoMigrate(&models.InventoryTransaction{}, &models.MaintenanceTask{}, &models.TaskItemDependency{}, &models.MaintenanceExecution{}, &models.MaintenanceConsumption{}, &models.MaintenanceRequest{}))
		var task models.MaintenanceTask
		require.NoError(t, f.DB.First(&task, old.ID).Error)
		require.Zero(t, task.Version)
		require.True(t, task.IsCompleted)
		require.True(t, now.Equal(*task.CompletedAt))
		require.Equal(t, "Monthly", task.Frequency)
		var tx models.InventoryTransaction
		require.NoError(t, f.DB.First(&tx, log.ID).Error)
		require.Equal(t, -100.0, tx.QuantityChange)
		require.Nil(t, tx.Source)
		require.Nil(t, tx.MaintenanceExecutionID)
		f.assertStock(t, 1000)
		count(t, f.DB, &models.MaintenanceExecution{}, 0)
		count(t, f.DB, &models.MaintenanceRequest{}, 0)
	}
}
