package models

import (
	"github.com/google/uuid"
	"time"
)

// GuidePlatformState checkpoints use the variant's own named steps, never indices.
type GuidePlatformState struct {
	Revision      int        `json:"revision"`
	Status        string     `json:"status"`
	Step          string     `json:"step"`
	Restored      bool       `json:"restored"`
	DeferredUntil *time.Time `json:"deferred_until"`
	HomeID        *uuid.UUID `json:"home_id"`
	DefinitionID  *uuid.UUID `json:"definition_id"`
	InventoryID   *uuid.UUID `json:"inventory_id"`
}

type GuideProgress struct {
	UserID    uuid.UUID          `gorm:"type:uuid;primaryKey"`
	GuideID   string             `gorm:"type:varchar(100);primaryKey"`
	Revision  int                `gorm:"not null"`
	Version   uint64             `gorm:"not null"`
	Outcome   string             `gorm:"type:varchar(20);not null"`
	Web       GuidePlatformState `gorm:"serializer:json;type:text;not null"`
	Mobile    GuidePlatformState `gorm:"serializer:json;type:text;not null"`
	CreatedAt time.Time
	UpdatedAt time.Time
	User      Profile `gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE" json:"-"`
}

// A receipt survives subsequent actions, so retrying a lost response never
// repeats a stock write or rolls back a newer disposition/restore.
type GuideMutation struct {
	UserID      uuid.UUID `gorm:"type:uuid;primaryKey"`
	GuideID     string    `gorm:"type:varchar(100);primaryKey"`
	RequestID   uuid.UUID `gorm:"type:uuid;primaryKey"`
	PayloadHash string    `gorm:"type:varchar(64);not null"`
	CreatedAt   time.Time
	User        Profile `gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE" json:"-"`
}
