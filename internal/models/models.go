package models

import (
	"time"

	"github.com/google/uuid"
)

// Profile represents a user profile
type Profile struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey"`
	IsAdmin   bool      `gorm:"default:false"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Home represents a physical location where items are stored
type Home struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey;default:uuid_generate_v4()"`
	Name      string    `gorm:"type:varchar(255);not null"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

// UserHome defines the many-to-many relationship and roles for users and homes
type UserHome struct {
	UserID    uuid.UUID `gorm:"type:uuid;primaryKey"`
	HomeID    uuid.UUID `gorm:"type:uuid;primaryKey"`
	Role      string    `gorm:"type:varchar(50);not null;default:'viewer'"`
	IsDefault bool      `gorm:"default:false"`
	CreatedAt time.Time
	UpdatedAt time.Time

	// Relations
	User Profile `gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE"`
	Home Home    `gorm:"foreignKey:HomeID;constraint:OnDelete:CASCADE"`
}

// SizeUnit represents standard units like kg, g, lt, etc.
type SizeUnit struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey;default:uuid_generate_v4()"`
	Name      string    `gorm:"type:varchar(50);unique;not null"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Category represents an optional 2-level category hierarchy
type Category struct {
	ID        uuid.UUID  `gorm:"type:uuid;primaryKey;default:uuid_generate_v4()"`
	Name      string     `gorm:"type:varchar(255);not null"`
	ParentID  *uuid.UUID `gorm:"type:uuid;index"`
	CreatedAt time.Time
	UpdatedAt time.Time

	// Relations
	Parent *Category `gorm:"foreignKey:ParentID;constraint:OnDelete:CASCADE"`
}

// ItemDefinition represents the blueprint of an item
type ItemDefinition struct {
	ID          uuid.UUID  `gorm:"type:uuid;primaryKey;default:uuid_generate_v4()"`
	Name        string     `gorm:"type:varchar(255);not null"`
	Description string     `gorm:"type:text"`
	CategoryID  *uuid.UUID `gorm:"type:uuid;index"`
	SizeUnitID  *uuid.UUID `gorm:"type:uuid;index"`
	IsExpirable bool       `gorm:"default:false"`
	ImageURL    string     `gorm:"type:text"`
	CreatedAt   time.Time
	UpdatedAt   time.Time

	// Relations
	Category *Category `gorm:"foreignKey:CategoryID;constraint:OnDelete:SET NULL"`
	SizeUnit *SizeUnit `gorm:"foreignKey:SizeUnitID;constraint:OnDelete:RESTRICT"`
}

// InventoryItem represents a concrete instance of an item in a specific home
type InventoryItem struct {
	ID               uuid.UUID  `gorm:"type:uuid;primaryKey;default:uuid_generate_v4()"`
	HomeID           uuid.UUID  `gorm:"type:uuid;not null;index"`
	ItemDefinitionID uuid.UUID  `gorm:"type:uuid;not null;index"`
	Quantity         float64    `gorm:"type:numeric;not null;default:0"`
	ExpirationDate   *time.Time `gorm:"type:timestamp with time zone"`
	CreatedAt        time.Time
	UpdatedAt        time.Time

	// Relations
	Home           Home           `gorm:"foreignKey:HomeID;constraint:OnDelete:CASCADE"`
	ItemDefinition ItemDefinition `gorm:"foreignKey:ItemDefinitionID;constraint:OnDelete:RESTRICT"`
}
