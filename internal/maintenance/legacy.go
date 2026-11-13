package maintenance

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Requirement is a durable fact, not a live association. Unknown legacy labels
// or units remain null; quantities are the original stored base quantities.
type Requirement struct {
	DependencyID     uuid.UUID  `json:"dependency_id"`
	ItemDefinitionID uuid.UUID  `json:"item_definition_id"`
	Quantity         float64    `json:"quantity"`
	DefinitionName   *string    `json:"definition_name"`
	BaseSizeUnitID   *uuid.UUID `json:"base_size_unit_id"`
	BaseUnitName     *string    `json:"base_unit_name"`
	UnitFamily       *string    `json:"unit_family"`
}

func dueTime(value time.Time) *time.Time {
	if value.IsZero() || value.Year() < 1 || value.Year() > 9999 {
		return nil
	}
	return utcTime(&value)
}
func textPointer(value string) *string { return &value }

func recurrence(task models.MaintenanceTask) (state string, count *int64, unit *string, restart bool, reason *string) {
	frequency := strings.ToLower(strings.TrimSpace(task.Frequency))
	if frequency != "custom" && (task.CustomFrequency != nil || (task.CustomFrequencyMetric != nil && strings.TrimSpace(*task.CustomFrequencyMetric) != "")) {
		return models.ScheduleNeedsReview, nil, nil, task.IsCompleted, textPointer("legacy_interval_invalid")
	}
	if frequency == "" || frequency == "once" {
		state = models.ScheduleActive
		if task.IsCompleted {
			state = models.ScheduleArchived
		}
		if task.CustomFrequency != nil || (task.CustomFrequencyMetric != nil && strings.TrimSpace(*task.CustomFrequencyMetric) != "") {
			return models.ScheduleNeedsReview, nil, nil, false, textPointer("legacy_interval_invalid")
		}
		if dueTime(task.ScheduledDate) == nil {
			return models.ScheduleNeedsReview, nil, nil, false, textPointer("legacy_due_time_unknown")
		}
		return
	}
	var n int64
	var metric string
	switch frequency {
	case "daily":
		n, metric = 1, "day"
	case "weekly":
		n, metric = 1, "week"
	case "monthly":
		n, metric = 1, "month"
	case "every 3 months":
		n, metric = 3, "month"
	case "every 6 months":
		n, metric = 6, "month"
	case "yearly":
		n, metric = 1, "year"
	case "custom":
		if task.CustomFrequency != nil && task.CustomFrequencyMetric != nil {
			value := *task.CustomFrequency
			if !math.IsNaN(value) && !math.IsInf(value, 0) && value > 0 && value < 1e8 && math.Trunc(value) == value {
				n = int64(value)
				metric = strings.ToLower(strings.TrimSpace(*task.CustomFrequencyMetric))
			}
		}
	}
	valid := n > 0
	if valid {
		date := task.ScheduledDate
		switch metric {
		case "day":
			date = date.AddDate(0, 0, int(n))
		case "week":
			date = date.AddDate(0, 0, int(n)*7)
		case "month":
			date = date.AddDate(0, int(n), 0)
		case "year":
			date = date.AddDate(int(n), 0, 0)
		default:
			valid = false
		}
		valid = valid && dueTime(date) != nil && dueTime(task.ScheduledDate) != nil
	}
	restart = task.IsCompleted
	state = models.ScheduleNeedsReview
	reason = textPointer("legacy_mode_and_zone_unconfirmed")
	if !valid {
		reason = textPointer("legacy_interval_invalid")
		return
	}
	count, unit = &n, &metric
	if task.IsCompleted {
		state = models.SchedulePaused
		reason = textPointer("legacy_restart_required")
	}
	return
}

func requirementSnapshot(tx *gorm.DB, task models.MaintenanceTask, execution *models.MaintenanceExecution) (string, error) {
	catalog, err := models.Catalog(tx)
	if err != nil {
		return "", err
	}
	ids := make([]uuid.UUID, 0, len(task.Dependencies))
	for _, dep := range task.Dependencies {
		ids = append(ids, dep.ItemDefinitionID)
	}
	defs := map[uuid.UUID]models.ItemDefinition{}
	if len(ids) > 0 {
		var rows []models.ItemDefinition
		if err := tx.Where("home_id = ? AND id IN ?", task.HomeID, ids).Find(&rows).Error; err != nil {
			return "", err
		}
		for _, def := range rows {
			defs[def.ID] = def
		}
	}
	used := map[uuid.UUID]models.MaintenanceConsumption{}
	if execution != nil {
		var rows []models.MaintenanceConsumption
		if err := tx.Where("execution_id = ?", execution.ID).Order("id").Find(&rows).Error; err != nil {
			return "", err
		}
		for _, row := range rows {
			if _, exists := used[row.ItemDefinitionID]; !exists {
				used[row.ItemDefinitionID] = row
			}
		}
	}
	result := make([]Requirement, 0, len(task.Dependencies))
	for _, dep := range task.Dependencies {
		req := Requirement{DependencyID: dep.ID, ItemDefinitionID: dep.ItemDefinitionID, Quantity: dep.QuantityRequired}
		if usage, ok := used[dep.ItemDefinitionID]; ok {
			req.DefinitionName = &usage.DefinitionName
			req.BaseSizeUnitID = &usage.BaseSizeUnitID
			req.BaseUnitName = &usage.BaseUnitName
			req.UnitFamily = &usage.UnitFamily
		} else if def, ok := defs[dep.ItemDefinitionID]; ok && execution == nil {
			req.DefinitionName = &def.Name
			req.BaseSizeUnitID = catalog.BaseID(def.SizeUnitID)
			if req.BaseSizeUnitID != nil {
				base := catalog.Units[*req.BaseSizeUnitID]
				req.BaseUnitName = &base.Name
				req.UnitFamily = &base.Family
			}
		}
		result = append(result, req)
	}
	data, err := json.Marshal(result)
	return string(data), err
}

// SyncLegacyTask runs inside the caller's home/task coordinated transaction.
// Only an advancing pending legacy revision may refresh snapshots. Terminal
// facts and future schedule edits are never rebuilt on subsequent startup.
func SyncLegacyTask(tx *gorm.DB, id uuid.UUID) error {
	var task models.MaintenanceTask
	if err := tx.Preload("Dependencies", func(q *gorm.DB) *gorm.DB { return q.Order("id") }).First(&task, "id = ?", id).Error; err != nil {
		return err
	}
	return syncLegacy(tx, task, "legacy_current_record")
}

// CheckLegacyWrite protects canonical decisions that an older client cannot
// represent. Lock order is home, legacy task, schedule, occurrence, then stock.
// Callers already hold the first two locks (new creates have no mapping yet).
func CheckLegacyWrite(tx *gorm.DB, task models.MaintenanceTask) error {
	var route models.MaintenanceOccurrence
	if err := tx.First(&route, "id = ? AND home_id = ?", task.ID, task.HomeID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	var schedule models.MaintenanceSchedule
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&schedule, "id = ? AND home_id = ?", route.ScheduleID, task.HomeID).Error; err != nil {
		return err
	}
	var occurrence models.MaintenanceOccurrence
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&occurrence, "id = ? AND home_id = ?", task.ID, task.HomeID).Error; err != nil {
		return err
	}
	if occurrence.ScheduleID != schedule.ID || occurrence.State != models.OccurrencePending || occurrence.Version != task.Version || schedule.Version != task.Version || schedule.RecurrenceMode != nil || schedule.TimeZone != nil {
		return ErrConflict
	}
	return nil
}

func syncLegacy(tx *gorm.DB, task models.MaintenanceTask, origin string) error {
	var execution models.MaintenanceExecution
	err := tx.Where("home_id = ? AND task_id = ?", task.HomeID, task.ID).Order("completed_at, id").First(&execution).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	var known *models.MaintenanceExecution
	if err == nil {
		known = &execution
	}
	requirements, err := requirementSnapshot(tx, task, nil)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(task)
	if err != nil {
		return err
	}
	state, count, unit, restart, reason := recurrence(task)
	if origin == "execution_only" {
		state = models.ScheduleNeedsReview
		restart = true
		reason = textPointer("legacy_task_missing")
	}
	scheduleID := uuid.NewSHA1(uuid.NameSpaceOID, []byte("maintenance:schedule:"+task.ID.String()))
	schedule := models.MaintenanceSchedule{ID: scheduleID, HomeID: task.HomeID, LegacyTaskID: &task.ID, State: state,
		Description: task.Description, InventoryItemID: task.InventoryItemID, IntervalCount: count, IntervalUnit: unit,
		AnchorAt: dueTime(task.ScheduledDate), RestartRequired: restart, ReviewReason: reason, RequirementsJSON: requirements,
		LegacySnapshotJSON: string(raw), Version: task.Version, CreatedAt: task.CreatedAt, UpdatedAt: task.UpdatedAt}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&schedule).Error; err != nil {
		return err
	}
	var existing models.MaintenanceSchedule
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&existing, "id = ? AND home_id = ?", scheduleID, task.HomeID).Error; err != nil {
		return err
	}
	facts := task
	if task.IsCompleted && known != nil {
		var recorded models.MaintenanceTask
		if json.Unmarshal([]byte(known.SnapshotJSON), &recorded) == nil && recorded.ID == task.ID && recorded.HomeID == task.HomeID {
			facts = recorded
			origin = "recorded_execution"
		}
	}
	snapshot, err := json.Marshal(facts)
	if err != nil {
		return err
	}
	occurrenceRequirements, err := requirementSnapshot(tx, facts, known)
	if err != nil {
		return err
	}
	occurrenceState := models.OccurrencePending
	if task.IsCompleted {
		occurrenceState = models.OccurrenceCompleted
	}
	occurrence := models.MaintenanceOccurrence{ID: task.ID, HomeID: task.HomeID, ScheduleID: existing.ID, LegacyTaskID: &task.ID,
		SlotKey: "legacy", OccupiesSlot: true, State: occurrenceState, Description: facts.Description, InventoryItemID: facts.InventoryItemID,
		OriginalDueAt: dueTime(facts.ScheduledDate), DueAt: dueTime(facts.ScheduledDate), RequirementsJSON: occurrenceRequirements,
		SnapshotJSON: string(snapshot), SnapshotOrigin: origin, Version: task.Version, CompletedAt: utcTime(task.CompletedAt), CreatedAt: task.CreatedAt, UpdatedAt: task.UpdatedAt}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&occurrence).Error; err != nil {
		return err
	}
	var previous models.MaintenanceOccurrence
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&previous, "id = ? AND home_id = ?", task.ID, task.HomeID).Error; err != nil {
		return err
	}
	if previous.ScheduleID != existing.ID || previous.LegacyTaskID == nil || *previous.LegacyTaskID != task.ID {
		return ErrConflict
	}
	if previous.State == models.OccurrencePending && previous.Version < task.Version {
		if existing.RecurrenceMode != nil || existing.TimeZone != nil || existing.Version != previous.Version {
			return ErrConflict
		}
		// Updating a pending legacy task changes this pending occurrence and its
		// future defaults only. Preserve its original due and initial migration data.
		occurrence.OriginalDueAt = previous.OriginalDueAt
		if err := tx.Model(&previous).Select("state", "description", "inventory_item_id", "due_at", "requirements_json", "snapshot_json", "snapshot_origin", "version", "completed_at", "updated_at").Updates(&occurrence).Error; err != nil {
			return err
		}
		if err := tx.Model(&existing).Select("state", "description", "inventory_item_id", "interval_count", "interval_unit", "anchor_at", "restart_required", "review_reason", "requirements_json", "version", "updated_at").Updates(&schedule).Error; err != nil {
			return err
		}
	}
	return tx.Model(&models.MaintenanceExecution{}).Where("home_id = ? AND task_id = ? AND occurrence_id IS NULL", task.HomeID, task.ID).Update("occurrence_id", task.ID).Error
}

// RetireLegacyTask preserves an explicit cancelled snapshot before removing the
// legacy pending row. No execution or supply usage is invented by deletion.
func RetireLegacyTask(tx *gorm.DB, task models.MaintenanceTask, user uuid.UUID, at time.Time) error {
	if err := CheckLegacyWrite(tx, task); err != nil {
		return err
	}
	if err := SyncLegacyTask(tx, task.ID); err != nil {
		return err
	}
	var occurrence models.MaintenanceOccurrence
	if err := tx.First(&occurrence, "id = ? AND home_id = ?", task.ID, task.HomeID).Error; err != nil {
		return err
	}
	if occurrence.State != models.OccurrencePending {
		return ErrConflict
	}
	if err := tx.Model(&occurrence).Updates(map[string]any{"state": models.OccurrenceCancelled, "cancelled_at": at.UTC(), "cancelled_by": user, "cancellation_reason": "legacy_delete", "version": task.Version + 1, "updated_at": at.UTC()}).Error; err != nil {
		return err
	}
	return tx.Model(&models.MaintenanceSchedule{}).Where("id = ? AND home_id = ?", occurrence.ScheduleID, task.HomeID).Updates(map[string]any{"state": models.ScheduleArchived, "version": task.Version + 1, "updated_at": at.UTC()}).Error
}

// InitializeLegacy is resumable and idempotent. A row's task/home locks use the
// same order as runtime writes. It never consumes stock or fabricates events.
func InitializeLegacy(ctx context.Context, db *gorm.DB) error {
	db = db.WithContext(ctx)
	var tasks []models.MaintenanceTask
	if err := db.Select("id", "home_id").Order("id").FindInBatches(&tasks, 100, func(_ *gorm.DB, _ int) error {
		for _, route := range tasks {
			if err := db.Transaction(func(tx *gorm.DB) error {
				var home models.Home
				if err := tx.Clauses(clause.Locking{Strength: "KEY SHARE"}).Select("id").First(&home, "id = ?", route.HomeID).Error; err != nil {
					if errors.Is(err, gorm.ErrRecordNotFound) {
						return nil
					}
					return err
				}
				var current models.MaintenanceTask
				if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").First(&current, "id = ? AND home_id = ?", route.ID, route.HomeID).Error; err != nil {
					if errors.Is(err, gorm.ErrRecordNotFound) {
						return nil
					}
					return err
				}
				return SyncLegacyTask(tx, route.ID)
			}); err != nil {
				return err
			}
		}
		return nil
	}).Error; err != nil {
		return err
	}
	// #152 deliberately retained executions after legacy task deletion. Recover
	// those into the same occurrence identity, even if only execution facts remain.
	var executions []models.MaintenanceExecution
	return db.Where("occurrence_id IS NULL").Order("id").FindInBatches(&executions, 100, func(_ *gorm.DB, _ int) error {
		for _, execution := range executions {
			if err := db.Transaction(func(tx *gorm.DB) error {
				var home models.Home
				if err := tx.Clauses(clause.Locking{Strength: "KEY SHARE"}).Select("id").First(&home, "id = ?", execution.HomeID).Error; err != nil {
					if errors.Is(err, gorm.ErrRecordNotFound) {
						return nil
					}
					return err
				}
				task := models.MaintenanceTask{ID: execution.TaskID, HomeID: execution.HomeID}
				var recorded models.MaintenanceTask
				if json.Unmarshal([]byte(execution.SnapshotJSON), &recorded) == nil && recorded.ID == execution.TaskID && recorded.HomeID == execution.HomeID {
					task = recorded
				}
				task.IsCompleted = true
				task.CompletedAt = &execution.CompletedAt
				task.Version = execution.TaskVersion
				return syncLegacy(tx, task, "execution_only")
			}); err != nil {
				return err
			}
		}
		return nil
	}).Error
}
