package models

import (
	"time"

	"github.com/google/uuid"
)

// UserDemoHome owns the private demo and persists deliberate removal. A NULL
// HomeID is a tombstone, not a request to provision another house on login.
type UserDemoHome struct {
	UserID    uuid.UUID  `gorm:"type:uuid;primaryKey"`
	HomeID    *uuid.UUID `gorm:"type:uuid;uniqueIndex"`
	Version   int64      `gorm:"not null;default:0"`
	CreatedAt time.Time
	UpdatedAt time.Time
	User      Profile `gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE"`
	Home      *Home   `gorm:"foreignKey:HomeID;constraint:OnDelete:SET NULL"`
}
