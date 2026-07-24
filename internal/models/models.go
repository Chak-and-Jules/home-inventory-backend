package models

import (
	"time"

	"github.com/google/uuid"
)

// Profile represents a user profile
type Profile struct {
	ID            uuid.UUID  `gorm:"type:uuid;primaryKey"`
	Email         string     `gorm:"type:varchar(255);uniqueIndex"`
	IsAdmin       bool       `gorm:"default:false"`
	WebTheme      *string    `gorm:"type:varchar(50)"`
	MobileTheme   *string    `gorm:"type:varchar(50)"`
	LanguageID    *uuid.UUID `gorm:"type:uuid;index"`
	RestockWindow *int       `gorm:"type:integer;default:7" json:"restock_window"`
	CreatedAt     time.Time
	UpdatedAt     time.Time

	// Relations
	Language *Language `gorm:"foreignKey:LanguageID;constraint:OnDelete:SET NULL"`
}

// Language represents a language preference
type Language struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey;default:uuid_generate_v4()"`
	Name      string    `gorm:"type:varchar(255);unique;not null"`
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

const (
	RoleOwner  = "owner"
	RoleEditor = "editor"
	RoleViewer = "viewer"
)

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
	ID uuid.UUID `gorm:"type:uuid;primaryKey;default:uuid_generate_v4()"`
	// Standard unique index for when parent_id IS NOT NULL.
	// PostgreSQL treats NULL as distinct, so we need a separate partial index for the NULL case.
	HomeID   uuid.UUID  `gorm:"type:uuid;not null;uniqueIndex:idx_category_home_name_parent;uniqueIndex:idx_category_home_name_null_parent,where:parent_id IS NULL"`
	Name     string     `gorm:"type:varchar(255);not null;uniqueIndex:idx_category_home_name_parent;uniqueIndex:idx_category_home_name_null_parent,where:parent_id IS NULL"`
	ParentID *uuid.UUID `gorm:"type:uuid;uniqueIndex:idx_category_home_name_parent"`
	// Partial unique index for top-level categories where parent_id IS NULL.
	// Note: GORM doesn't natively support partial index creation via tags in all dialects,
	// but specifying it here for documentation and future-proofing.
	// We will also use application-level checks to ensure uniqueness.
	// GORM's index tag for PostgreSQL supports 'where' clause.
	CreatedAt time.Time
	UpdatedAt time.Time

	// Relations
	Home   Home      `gorm:"foreignKey:HomeID;constraint:OnDelete:CASCADE"`
	Parent *Category `gorm:"foreignKey:ParentID;constraint:OnDelete:CASCADE"`
}

// ItemDefinition represents the blueprint of an item
type ItemDefinition struct {
	ID                uuid.UUID  `gorm:"type:uuid;primaryKey;default:uuid_generate_v4()"`
	HomeID            uuid.UUID  `gorm:"type:uuid;not null;index;uniqueIndex:idx_item_def_home_barcode"`
	Name              string     `gorm:"type:varchar(255);not null"`
	Description       string     `gorm:"type:text"`
	CategoryID        *uuid.UUID `gorm:"type:uuid;index"`
	SizeUnitID        *uuid.UUID `gorm:"type:uuid;index"`
	IsExpirable       bool       `gorm:"default:false"`
	LowStockThreshold *float64   `gorm:"type:numeric"`
	TargetQuantity    *float64   `gorm:"type:numeric"`
	Priority          string     `gorm:"type:varchar(50);default:'medium'"`
	ImageURL          string     `gorm:"type:text"`
	Barcode           *string    `gorm:"type:varchar(255);index;uniqueIndex:idx_item_def_home_barcode"`
	CreatedAt         time.Time
	UpdatedAt         time.Time

	// Relations
	Home     Home      `gorm:"foreignKey:HomeID;constraint:OnDelete:CASCADE"`
	Category *Category `gorm:"foreignKey:CategoryID;constraint:OnDelete:SET NULL"`
	SizeUnit *SizeUnit `gorm:"foreignKey:SizeUnitID;constraint:OnDelete:RESTRICT"`
}

// InventoryItem represents a concrete instance of an item in a specific home
type InventoryItem struct {
	ID               uuid.UUID  `gorm:"type:uuid;primaryKey;default:uuid_generate_v4()"`
	HomeID           uuid.UUID  `gorm:"type:uuid;not null;index"`
	ItemDefinitionID uuid.UUID  `gorm:"type:uuid;not null;index"`
	Quantity         float64    `gorm:"type:numeric;not null;default:0"`
	ExpirationDate   *time.Time `gorm:"type:timestamp with time zone;column:expiration_date;index" json:"expiry_date"` // ⚡ Bolt: Added index for frequent expiration filtering and table-wide daily background scans
	CreatedAt        time.Time
	UpdatedAt        time.Time

	// Relations
	Home           Home           `gorm:"foreignKey:HomeID;constraint:OnDelete:CASCADE"`
	ItemDefinition ItemDefinition `gorm:"foreignKey:ItemDefinitionID;constraint:OnDelete:RESTRICT"`
}

// ShoppingListItem represents an item in the shopping list
type ShoppingListItem struct {
	ID               uuid.UUID  `gorm:"type:uuid;primaryKey;default:uuid_generate_v4()"`
	HomeID           uuid.UUID  `gorm:"type:uuid;not null;index"`
	ItemDefinitionID *uuid.UUID `gorm:"type:uuid;index"`
	Name             string     `gorm:"type:varchar(255);not null"`
	Quantity         float64    `gorm:"type:numeric;not null;default:1"`
	IsBought         bool       `gorm:"default:false"`
	IsAutoGenerated  bool       `gorm:"default:false"`
	IsPredictive     bool       `gorm:"default:false" json:"is_predictive"`
	IsDismissed      bool       `gorm:"default:false" json:"is_dismissed"`
	CreatedAt        time.Time
	UpdatedAt        time.Time

	// Relations
	Home           Home            `gorm:"foreignKey:HomeID;constraint:OnDelete:CASCADE"`
	ItemDefinition *ItemDefinition `gorm:"foreignKey:ItemDefinitionID;constraint:OnDelete:CASCADE"`
}

// InventoryTransaction logs changes in quantity for an inventory item
type InventoryTransaction struct {
	ID               uuid.UUID `gorm:"type:uuid;primaryKey;default:uuid_generate_v4()"`
	HomeID           uuid.UUID `gorm:"type:uuid;not null;index"`
	ItemDefinitionID uuid.UUID `gorm:"type:uuid;not null;index"`
	InventoryItemID  uuid.UUID `gorm:"type:uuid;not null;index"`
	QuantityChange   float64   `gorm:"type:numeric;not null"`
	CreatedAt        time.Time

	// Relations
	Home           Home           `gorm:"foreignKey:HomeID;constraint:OnDelete:CASCADE"`
	ItemDefinition ItemDefinition `gorm:"foreignKey:ItemDefinitionID;constraint:OnDelete:CASCADE"`
	InventoryItem  InventoryItem  `gorm:"foreignKey:InventoryItemID;constraint:OnDelete:CASCADE"`
}

// MaintenanceTask represents a recurring or one-time maintenance task
type MaintenanceTask struct {
	ID                    uuid.UUID  `gorm:"type:uuid;primaryKey;default:uuid_generate_v4()"`
	HomeID                uuid.UUID  `gorm:"type:uuid;not null;index"`
	InventoryItemID       *uuid.UUID `gorm:"type:uuid;index"`
	Description           string     `gorm:"type:varchar(255);not null"`
	ScheduledDate         time.Time  `gorm:"type:timestamp with time zone;not null;index"`
	Frequency             string     `gorm:"type:varchar(50)"` // e.g., "Once", "Daily", "Weekly", "Monthly", "Every 3 Months", "Every 6 Months", "Yearly", "Custom"
	CustomFrequency       *float64   `gorm:"type:numeric" json:"custom_frequency"`
	CustomFrequencyMetric *string    `gorm:"type:varchar(50)" json:"custom_frequency_metric"`
	IsCompleted           bool       `gorm:"default:false"`
	CompletedAt           *time.Time `gorm:"type:timestamp with time zone"`
	CreatedAt             time.Time
	UpdatedAt             time.Time `gorm:"index"`

	// Relations
	Home          Home                 `gorm:"foreignKey:HomeID;constraint:OnDelete:CASCADE"`
	InventoryItem *InventoryItem       `gorm:"foreignKey:InventoryItemID;constraint:OnDelete:SET NULL"`
	Dependencies  []TaskItemDependency `gorm:"foreignKey:MaintenanceTaskID;constraint:OnDelete:CASCADE"`
}

// TaskItemDependency links a maintenance task to required inventory items
type TaskItemDependency struct {
	ID                uuid.UUID `gorm:"type:uuid;primaryKey;default:uuid_generate_v4()"`
	MaintenanceTaskID uuid.UUID `gorm:"type:uuid;not null;index"`
	ItemDefinitionID  uuid.UUID `gorm:"type:uuid;not null;index"`
	QuantityRequired  float64   `gorm:"type:numeric;not null;default:1"`
	CreatedAt         time.Time
	UpdatedAt         time.Time

	// Relations
	MaintenanceTask *MaintenanceTask `gorm:"foreignKey:MaintenanceTaskID;constraint:OnDelete:CASCADE"`
	ItemDefinition  ItemDefinition   `gorm:"foreignKey:ItemDefinitionID;constraint:OnDelete:RESTRICT"`
}
