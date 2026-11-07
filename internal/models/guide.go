package models

import (
	"time"

	"github.com/google/uuid"
)

// GuideProgress is one versioned aggregate for both applications. Updating the
// aggregate with compare-and-swap makes shared outcomes atomic without allowing
// a stale checkpoint to overwrite a platform-specific restore.
type GuideProgress struct {
	UserID              uuid.UUID  `gorm:"type:uuid;primaryKey"`
	GuideID             string     `gorm:"type:varchar(100);primaryKey"`
	Revision            int        `gorm:"primaryKey"`
	Version             int64      `gorm:"not null;default:0"`
	WebStatus           string     `gorm:"type:varchar(20);not null;default:not_started"`
	WebStepID           string     `gorm:"type:varchar(100);not null;default:''"`
	WebHomeID           *uuid.UUID `gorm:"type:uuid"`
	WebDeferredUntil    *time.Time
	MobileStatus        string     `gorm:"type:varchar(20);not null;default:not_started"`
	MobileStepID        string     `gorm:"type:varchar(100);not null;default:''"`
	MobileHomeID        *uuid.UUID `gorm:"type:uuid"`
	MobileDeferredUntil *time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
	User                Profile `gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE"`
	WebHome             *Home   `gorm:"foreignKey:WebHomeID;constraint:OnDelete:SET NULL"`
	MobileHome          *Home   `gorm:"foreignKey:MobileHomeID;constraint:OnDelete:SET NULL"`
}

// GuideRequest records successful commands in the same transaction as progress.
// Retrying an old command acknowledges it without replaying its side effects.
type GuideRequest struct {
	UserID      uuid.UUID `gorm:"type:uuid;primaryKey"`
	RequestID   uuid.UUID `gorm:"type:uuid;primaryKey"`
	Fingerprint string    `gorm:"type:char(64);not null"`
	CreatedAt   time.Time
	User        Profile `gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE"`
}
