package models

import (
	"github.com/google/uuid"
	"time"
)

const (
	ScheduleActive      = "active"
	SchedulePaused      = "paused"
	ScheduleNeedsReview = "needs_review"
	ScheduleArchived    = "archived"
	OccurrencePending   = "pending"
	OccurrenceCompleted = "completed"
	OccurrenceSkipped   = "skipped"
	OccurrenceCancelled = "cancelled"
)

// Schedule requirements are future defaults; occurrence requirements are a
// separate snapshot. No live inventory/definition FK may erase either snapshot.
type MaintenanceSchedule struct {
	ID                 uuid.UUID  `gorm:"type:uuid;primaryKey;uniqueIndex:maintenance_schedule_home_key,priority:2" json:"id"`
	HomeID             uuid.UUID  `gorm:"type:uuid;not null;uniqueIndex:maintenance_schedule_home_key,priority:1;index:maintenance_schedule_home_state,priority:1" json:"home_id"`
	LegacyTaskID       *uuid.UUID `gorm:"type:uuid;uniqueIndex" json:"legacy_task_id"`
	State              string     `gorm:"type:varchar(20);not null;index:maintenance_schedule_home_state,priority:2;check:maintenance_schedule_state,state IN ('active','paused','needs_review','archived')" json:"state"`
	Description        string     `gorm:"not null" json:"description"`
	InventoryItemID    *uuid.UUID `gorm:"type:uuid" json:"inventory_item_id"`
	RecurrenceMode     *string    `gorm:"type:varchar(24);check:maintenance_schedule_mode,recurrence_mode IS NULL OR recurrence_mode IN ('calendar','after_completion')" json:"recurrence_mode"`
	IntervalCount      *int64     `gorm:"check:maintenance_schedule_interval,interval_count IS NULL OR interval_count > 0" json:"interval_count"`
	IntervalUnit       *string    `gorm:"type:varchar(8);check:maintenance_schedule_unit,interval_unit IS NULL OR interval_unit IN ('day','week','month','year')" json:"interval_unit"`
	TimeZone           *string    `gorm:"type:varchar(64)" json:"time_zone"`
	AnchorAt           *time.Time `gorm:"type:timestamp with time zone" json:"anchor_at"`
	AnchorLocal        *string    `json:"anchor_local"`
	RestartRequired    bool       `gorm:"not null;default:false" json:"restart_required"`
	ReviewReason       *string    `json:"review_reason"`
	RequirementsJSON   string     `gorm:"type:text;not null" json:"-"`
	LegacySnapshotJSON string     `gorm:"type:text;not null" json:"-"`
	Version            int64      `gorm:"not null;default:0;check:maintenance_schedule_version,version >= 0" json:"version"`
	CreatedAt          time.Time  `gorm:"autoCreateTime:false" json:"created_at"`
	UpdatedAt          time.Time  `gorm:"autoUpdateTime:false" json:"updated_at"`
	Home               Home       `gorm:"foreignKey:HomeID;constraint:OnDelete:CASCADE" json:"-"`
}

// IDs of mapped legacy occurrences equal their original task IDs, permanently.
// Slot occupancy is separate from lifecycle, permitting explicit supersession
// without deleting an old logical slot's history in the later lifecycle work.
type MaintenanceOccurrence struct {
	ID                 uuid.UUID           `gorm:"type:uuid;primaryKey;uniqueIndex:maintenance_occurrence_home_key,priority:2;index:maintenance_occurrence_due,priority:3;index:maintenance_occurrence_state_due,priority:4" json:"id"`
	HomeID             uuid.UUID           `gorm:"type:uuid;not null;uniqueIndex:maintenance_occurrence_home_key,priority:1;index:maintenance_occurrence_due,priority:1;index:maintenance_occurrence_state_due,priority:1" json:"home_id"`
	ScheduleID         uuid.UUID           `gorm:"type:uuid;not null;index;uniqueIndex:maintenance_live_slot,priority:1,where:occupies_slot = true;uniqueIndex:maintenance_one_pending,where:state = 'pending'" json:"schedule_id"`
	LegacyTaskID       *uuid.UUID          `gorm:"type:uuid;uniqueIndex" json:"legacy_task_id"`
	SlotKey            string              `gorm:"not null;uniqueIndex:maintenance_live_slot,priority:2,where:occupies_slot = true" json:"slot_key"`
	OccupiesSlot       bool                `gorm:"not null;default:true" json:"occupies_slot"`
	State              string              `gorm:"type:varchar(12);not null;index:maintenance_occurrence_state_due,priority:2;check:maintenance_occurrence_state,state IN ('pending','completed','skipped','cancelled')" json:"state"`
	Description        string              `gorm:"not null" json:"description"`
	InventoryItemID    *uuid.UUID          `gorm:"type:uuid" json:"inventory_item_id"`
	OriginalDueAt      *time.Time          `gorm:"type:timestamp with time zone" json:"original_due_at"`
	DueAt              *time.Time          `gorm:"type:timestamp with time zone;index:maintenance_occurrence_due,priority:2;index:maintenance_occurrence_state_due,priority:3" json:"due_at"`
	TimeZone           *string             `gorm:"type:varchar(64)" json:"time_zone"`
	RequirementsJSON   string              `gorm:"type:text;not null" json:"-"`
	SnapshotJSON       string              `gorm:"type:text;not null" json:"-"`
	SnapshotOrigin     string              `gorm:"not null" json:"snapshot_origin"`
	Version            int64               `gorm:"not null;default:0;check:maintenance_occurrence_version,version >= 0" json:"version"`
	CompletedAt        *time.Time          `gorm:"type:timestamp with time zone" json:"completed_at"`
	CancelledAt        *time.Time          `gorm:"type:timestamp with time zone" json:"cancelled_at"`
	CancelledBy        *uuid.UUID          `gorm:"type:uuid" json:"cancelled_by"`
	CancellationReason *string             `json:"cancellation_reason"`
	CreatedAt          time.Time           `gorm:"autoCreateTime:false" json:"created_at"`
	UpdatedAt          time.Time           `gorm:"autoUpdateTime:false" json:"updated_at"`
	Home               Home                `gorm:"foreignKey:HomeID;constraint:OnDelete:CASCADE" json:"-"`
	Schedule           MaintenanceSchedule `gorm:"foreignKey:HomeID,ScheduleID;references:HomeID,ID;constraint:OnDelete:NO ACTION" json:"-"`
}
