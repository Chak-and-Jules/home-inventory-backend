package models

import (
	"time"

	"github.com/google/uuid"
)

const InventorySourceMaintenance = "maintenance"

// MaintenanceExecution is the initial, immutable execution of a legacy task.
// Task/user identifiers are snapshots, not cascading foreign keys: task, actor
// or batch deletion must not erase the home's service/consumption history.
// The occurrence work will reuse these execution IDs and their command journal.
type MaintenanceExecution struct {
	ID           uuid.UUID  `gorm:"type:uuid;primaryKey"`
	HomeID       uuid.UUID  `gorm:"type:uuid;not null;index"`
	TaskID       uuid.UUID  `gorm:"type:uuid;not null;uniqueIndex"`
	ActorUserID  uuid.UUID  `gorm:"type:uuid;not null"`
	RequestID    *uuid.UUID `gorm:"type:uuid"`
	TaskVersion  int64      `gorm:"not null"`
	CompletedAt  time.Time  `gorm:"not null"`
	SnapshotJSON string     `gorm:"type:text;not null"`
	CreatedAt    time.Time
	Home         Home `gorm:"foreignKey:HomeID;constraint:OnDelete:CASCADE"`
}

// MaintenanceConsumption keeps the batch/unit/definition facts even if the live
// stock transaction is later deleted by its existing batch cascade.
type MaintenanceConsumption struct {
	ID                     uuid.UUID `gorm:"type:uuid;primaryKey"`
	ExecutionID            uuid.UUID `gorm:"type:uuid;not null;index"`
	InventoryTransactionID uuid.UUID `gorm:"type:uuid;not null;uniqueIndex"`
	ItemDefinitionID       uuid.UUID `gorm:"type:uuid;not null"`
	InventoryItemID        uuid.UUID `gorm:"type:uuid;not null"`
	DefinitionName         string    `gorm:"not null"`
	BaseSizeUnitID         uuid.UUID `gorm:"type:uuid;not null"`
	BaseUnitName           string    `gorm:"not null"`
	UnitFamily             string    `gorm:"not null"`
	Quantity               float64   `gorm:"type:numeric(22,6);not null;check:maintenance_consumption_range,quantity > 0 and quantity <= 1000000000"`
	ExpirationDate         *time.Time
	CreatedAt              time.Time
	Execution              MaintenanceExecution `gorm:"foreignKey:ExecutionID;constraint:OnDelete:CASCADE"`
}

// MaintenanceRequest records only successful commands, including acknowledged
// no-ops on legacy completed tasks. IDs share one per-user maintenance namespace.
// Home deletion removes receipts; task deletion cannot replay an old effect.
type MaintenanceRequest struct {
	UserID           uuid.UUID  `gorm:"type:uuid;primaryKey"`
	RequestID        uuid.UUID  `gorm:"type:uuid;primaryKey"`
	HomeID           uuid.UUID  `gorm:"type:uuid;not null;index"`
	TaskID           uuid.UUID  `gorm:"type:uuid;not null"`
	Fingerprint      string     `gorm:"type:char(64);not null"`
	ExecutionID      *uuid.UUID `gorm:"type:uuid"`
	TaskVersion      int64
	CompletedAt      *time.Time
	AlreadyCompleted bool
	CreatedAt        time.Time
	Home             Home                  `gorm:"foreignKey:HomeID;constraint:OnDelete:CASCADE"`
	Execution        *MaintenanceExecution `gorm:"foreignKey:ExecutionID;constraint:OnDelete:CASCADE"`
}
