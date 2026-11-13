package maintenance

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func legacyRows(t *testing.T, f *fixture, id uuid.UUID) (models.MaintenanceSchedule, models.MaintenanceOccurrence) {
	t.Helper()
	var occurrence models.MaintenanceOccurrence
	require.NoError(t, f.DB.First(&occurrence, "id = ?", id).Error)
	var schedule models.MaintenanceSchedule
	require.NoError(t, f.DB.First(&schedule, occurrence.ScheduleID).Error)
	return schedule, occurrence
}
func TestLegacyInitializationMappingAndIdempotency(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	now := f.Now()
	zone := "Europe/Istanbul"
	require.NoError(t, f.DB.Model(&models.Home{}).Where("id = ?", f.home).Update("maintenance_time_zone", zone).Error)
	require.NoError(t, f.DB.Model(&f.task).Update("inventory_item_id", f.stock.ID).Error)
	old := f.newTask(t, 25)
	require.NoError(t, f.DB.Model(&old).Update("is_completed", true).Error)
	pending := f.newTask(t, 125)
	require.NoError(t, f.DB.Model(&pending).Updates(map[string]any{"frequency": " Monthly ", "inventory_item_id": f.stock.ID}).Error)
	completed := f.newTask(t, 250)
	require.NoError(t, f.DB.Model(&completed).Updates(map[string]any{"frequency": "Monthly", "is_completed": true, "completed_at": now}).Error)
	fraction := f.newTask(t, 0.125)
	metric := "week"
	require.NoError(t, f.DB.Model(&fraction).Updates(map[string]any{"frequency": "Custom", "custom_frequency": 1.5, "custom_frequency_metric": metric}).Error)
	invalid := f.newTask(t, 0)
	require.NoError(t, f.DB.Model(&invalid).Update("frequency", "Fortnightly").Error)
	noDue := f.newTask(t, 0)
	require.NoError(t, f.DB.Model(&noDue).Update("scheduled_date", time.Time{}).Error)
	// Snapshot the old tables: initialization may add only the new model, plus
	// links on known executions. It must not rewrite legacy facts or stock.
	before := persistedState(t, f.DB)
	require.NoError(t, InitializeLegacy(ctx, f.DB))
	after := persistedState(t, f.DB)
	for _, key := range []string{"*[]models.MaintenanceTask", "*[]models.TaskItemDependency", "*[]models.InventoryItem", "*[]models.InventoryTransaction", "*[]models.ShoppingListItem"} {
		require.Equal(t, before[key], after[key], key)
	}
	count(t, f.DB, &models.MaintenanceSchedule{}, 7)
	count(t, f.DB, &models.MaintenanceOccurrence{}, 7)
	count(t, f.DB, &models.MaintenanceExecution{}, 0)
	schedule, occurrence := legacyRows(t, f, f.task.ID)
	require.Equal(t, models.ScheduleActive, schedule.State)
	require.Equal(t, models.OccurrencePending, occurrence.State)
	require.Equal(t, f.task.ID, *occurrence.LegacyTaskID)
	require.Equal(t, f.stock.ID, *occurrence.InventoryItemID)
	require.True(t, now.Equal(*occurrence.DueAt))
	var requirements []Requirement
	require.NoError(t, json.Unmarshal([]byte(occurrence.RequirementsJSON), &requirements))
	require.Len(t, requirements, 1)
	require.Equal(t, 100.0, requirements[0].Quantity)
	require.Equal(t, "g", *requirements[0].BaseUnitName)
	schedule, occurrence = legacyRows(t, f, old.ID)
	require.Equal(t, models.ScheduleArchived, schedule.State)
	require.Equal(t, models.OccurrenceCompleted, occurrence.State)
	require.Nil(t, occurrence.CompletedAt)
	schedule, occurrence = legacyRows(t, f, pending.ID)
	require.Equal(t, models.ScheduleNeedsReview, schedule.State)
	require.EqualValues(t, 1, *schedule.IntervalCount)
	require.Equal(t, "month", *schedule.IntervalUnit)
	require.Nil(t, schedule.TimeZone)
	require.Nil(t, schedule.RecurrenceMode)
	require.Nil(t, schedule.AnchorLocal)
	require.False(t, schedule.RestartRequired)
	require.Equal(t, models.OccurrencePending, occurrence.State)
	schedule, occurrence = legacyRows(t, f, completed.ID)
	require.Equal(t, models.SchedulePaused, schedule.State)
	require.True(t, schedule.RestartRequired)
	require.Equal(t, models.OccurrenceCompleted, occurrence.State)
	require.True(t, now.Equal(*occurrence.CompletedAt))
	for _, id := range []uuid.UUID{fraction.ID, invalid.ID, noDue.ID} {
		schedule, occurrence = legacyRows(t, f, id)
		require.Equal(t, models.ScheduleNeedsReview, schedule.State)
		require.Nil(t, schedule.IntervalCount)
		require.Equal(t, models.OccurrencePending, occurrence.State)
	}
	schedule, _ = legacyRows(t, f, fraction.ID)
	var original models.MaintenanceTask
	require.NoError(t, json.Unmarshal([]byte(schedule.LegacySnapshotJSON), &original))
	require.Equal(t, 1.5, *original.CustomFrequency)
	_, occurrence = legacyRows(t, f, noDue.ID)
	require.Nil(t, occurrence.DueAt)
	require.NoError(t, InitializeLegacy(ctx, f.DB))
	require.Equal(t, after, persistedState(t, f.DB))
}

func TestLegacyJournalUpgradeAndHistoryRetention(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	require.NoError(t, f.DB.Model(&f.task).Update("inventory_item_id", f.stock.ID).Error)
	command := identified()
	result, err := f.Complete(ctx, f.user, f.task.ID, command)
	require.NoError(t, err)
	var original models.MaintenanceExecution
	require.NoError(t, f.DB.First(&original, result.ExecutionID).Error)
	// PostgreSQL rehearses the actual populated pre-#154 DDL. SQLite's
	// table-rebuild migrator cascades child data while adding FKs; its default
	// suite exercises the equivalent logical backfill without destructive DDL.
	if f.DB.Dialector.Name() == "postgres" {
		require.NoError(t, f.DB.Migrator().DropConstraint(&models.MaintenanceExecution{}, "Occurrence"))
		require.NoError(t, f.DB.Migrator().DropIndex(&models.MaintenanceExecution{}, "idx_maintenance_executions_occurrence_id"))
		require.NoError(t, f.DB.Migrator().DropColumn(&models.MaintenanceExecution{}, "OccurrenceID"))
	} else {
		require.NoError(t, f.DB.Model(&models.MaintenanceExecution{}).Where("id = ?", original.ID).Update("occurrence_id", nil).Error)
	}
	require.NoError(t, f.DB.Migrator().DropTable(&models.MaintenanceOccurrence{}, &models.MaintenanceSchedule{}))
	count(t, f.DB, &models.MaintenanceConsumption{}, 1)
	count(t, f.DB, &models.MaintenanceRequest{}, 1)
	require.NoError(t, f.DB.Model(&f.def).Update("name", "Renamed after the work").Error)
	for i := 0; i < 2; i++ {
		require.NoError(t, f.DB.AutoMigrate(&models.MaintenanceSchedule{}, &models.MaintenanceOccurrence{}, &models.MaintenanceExecution{}))
		require.NoError(t, InitializeLegacy(ctx, f.DB))
	}
	schedule, occurrence := legacyRows(t, f, f.task.ID)
	require.Equal(t, models.OccurrenceCompleted, occurrence.State)
	require.Equal(t, "recorded_execution", occurrence.SnapshotOrigin)
	var requirements []Requirement
	require.NoError(t, json.Unmarshal([]byte(occurrence.RequirementsJSON), &requirements))
	require.NotNil(t, requirements[0].DefinitionName)
	require.Equal(t, "Filter material", *requirements[0].DefinitionName)
	var execution models.MaintenanceExecution
	require.NoError(t, f.DB.First(&execution, original.ID).Error)
	require.Equal(t, original.ActorUserID, execution.ActorUserID)
	require.True(t, original.CompletedAt.Equal(execution.CompletedAt))
	require.Equal(t, original.SnapshotJSON, execution.SnapshotJSON)
	require.Equal(t, f.task.ID, *execution.OccurrenceID)
	count(t, f.DB, &models.MaintenanceExecution{}, 1)
	count(t, f.DB, &models.MaintenanceConsumption{}, 1)
	count(t, f.DB, &models.MaintenanceRequest{}, 1)
	f.assertStock(t, 900)
	// Future defaults and current labels may change without rewriting the
	// completed occurrence's quantities, definition labels, due or assignment.
	require.NoError(t, f.DB.Model(&schedule).Updates(map[string]any{"description": "Future description", "requirements_json": "[]", "version": 10}).Error)
	require.NoError(t, f.DB.Delete(&f.stock).Error)
	require.NoError(t, f.DB.Delete(&f.task).Error)
	require.NoError(t, InitializeLegacy(ctx, f.DB))
	var retained models.MaintenanceOccurrence
	require.NoError(t, f.DB.First(&retained, occurrence.ID).Error)
	require.Equal(t, occurrence.RequirementsJSON, retained.RequirementsJSON)
	require.Equal(t, f.stock.ID, *retained.InventoryItemID)
	replay, err := f.Complete(ctx, f.user, f.task.ID, command)
	require.NoError(t, err)
	require.True(t, replay.Replayed)
	require.Equal(t, result.ExecutionID, replay.ExecutionID)
	// Durable service history prevents ordinary schedule/occurrence deletion.
	require.Error(t, f.DB.Delete(&schedule).Error)
	require.Error(t, f.DB.Delete(&retained).Error)
	require.NoError(t, f.DB.Delete(&models.Home{}, "id = ?", f.home).Error)
	for _, m := range []any{&models.MaintenanceSchedule{}, &models.MaintenanceOccurrence{}, &models.MaintenanceExecution{}, &models.MaintenanceConsumption{}, &models.MaintenanceRequest{}} {
		count(t, f.DB, m, 0)
	}
}

func TestLegacyOrphanExecutionRecovery(t *testing.T) {
	for _, validSnapshot := range []bool{true, false} {
		t.Run(map[bool]string{true: "snapshot retained", false: "snapshot unknown"}[validSnapshot], func(t *testing.T) {
			f := newFixture(t)
			ctx := context.Background()
			task := f.task
			require.NoError(t, f.DB.Preload("Dependencies").First(&task, task.ID).Error)
			data, err := json.Marshal(task)
			require.NoError(t, err)
			if !validSnapshot {
				data = []byte("unreadable old payload")
			}
			known := models.MaintenanceExecution{ID: uuid.New(), HomeID: f.home, TaskID: task.ID, ActorUserID: f.user, TaskVersion: 1, CompletedAt: f.Now(), SnapshotJSON: string(data)}
			require.NoError(t, f.DB.Create(&known).Error)
			require.NoError(t, f.DB.Delete(&task).Error)
			require.NoError(t, InitializeLegacy(ctx, f.DB))
			schedule, occurrence := legacyRows(t, f, task.ID)
			require.Equal(t, models.OccurrenceCompleted, occurrence.State)
			require.True(t, schedule.RestartRequired)
			require.Equal(t, models.ScheduleNeedsReview, schedule.State)
			require.NoError(t, f.DB.First(&known, known.ID).Error)
			require.Equal(t, occurrence.ID, *known.OccurrenceID)
			if validSnapshot {
				require.Equal(t, task.Description, occurrence.Description)
				require.NotNil(t, occurrence.DueAt)
			} else {
				require.Equal(t, "execution_only", occurrence.SnapshotOrigin)
				require.Nil(t, occurrence.DueAt)
				require.Equal(t, "[]", occurrence.RequirementsJSON)
			}
			require.NoError(t, InitializeLegacy(ctx, f.DB))
			count(t, f.DB, &models.MaintenanceSchedule{}, 1)
			count(t, f.DB, &models.MaintenanceOccurrence{}, 1)
			count(t, f.DB, &models.MaintenanceExecution{}, 1)
		})
	}
}

func TestMaintenanceOccurrenceConstraints(t *testing.T) {
	f := newFixture(t)
	require.NoError(t, InitializeLegacy(context.Background(), f.DB))
	schedule, occurrence := legacyRows(t, f, f.task.ID)
	copy := occurrence
	copy.ID = uuid.New()
	copy.LegacyTaskID = nil
	copy.SlotKey = "next"
	require.Error(t, f.DB.Create(&copy).Error) // one unresolved occurrence
	copy.State = models.OccurrenceCompleted
	copy.SlotKey = "legacy"
	require.Error(t, f.DB.Create(&copy).Error) // live logical slot
	copy.SlotKey = "next"
	copy.State = "rubbish"
	require.Error(t, f.DB.Create(&copy).Error)
	copy.State = models.OccurrenceCompleted
	copy.Version = -1
	require.Error(t, f.DB.Create(&copy).Error)
	copy.Version = 0
	other := models.Home{ID: uuid.New(), Name: "Other"}
	require.NoError(t, f.DB.Create(&other).Error)
	copy.HomeID = other.ID
	require.Error(t, f.DB.Create(&copy).Error)
	// Explicit supersession retains old IDs while releasing only the logical slot.
	require.NoError(t, f.DB.Model(&occurrence).Updates(map[string]any{"state": models.OccurrenceCancelled, "occupies_slot": false}).Error)
	copy.HomeID = f.home
	copy.State = models.OccurrencePending
	copy.SlotKey = "legacy"
	require.NoError(t, f.DB.Create(&copy).Error)
	foreign := models.MaintenanceExecution{ID: uuid.New(), HomeID: other.ID, TaskID: uuid.New(), OccurrenceID: &copy.ID, ActorUserID: f.user, CompletedAt: f.Now(), SnapshotJSON: "{}"}
	require.Error(t, f.DB.Create(&foreign).Error)
	require.Error(t, f.DB.Delete(&schedule).Error)
}

func TestCanonicalSnapshotFailureRollsBackCompletion(t *testing.T) {
	for _, table := range []string{"maintenance_schedules", "maintenance_occurrences"} {
		t.Run(table, func(t *testing.T) {
			f := newFixture(t)
			before := persistedState(t, f.DB)
			failure := errors.New("snapshot write failed")
			require.NoError(t, f.DB.Callback().Create().After("gorm:create").Register("test:canonical", func(tx *gorm.DB) {
				if tx.Statement.Table == table {
					tx.AddError(failure)
				}
			}))
			_, err := f.Complete(context.Background(), f.user, f.task.ID, identified())
			require.ErrorIs(t, err, failure)
			require.Equal(t, before, persistedState(t, f.DB))
		})
	}
}

func TestPostgresConcurrentInitializationAndCompletion(t *testing.T) {
	requirePostgres(t)
	f := newFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	start := make(chan struct{})
	results := make(chan error, 5)
	for i := 0; i < 4; i++ {
		go func() { <-start; results <- InitializeLegacy(ctx, f.DB) }()
	}
	go func() { <-start; _, err := f.Complete(ctx, f.user, f.task.ID, identified()); results <- err }()
	close(start)
	for i := 0; i < 5; i++ {
		require.NoError(t, <-results)
	}
	_, occurrence := legacyRows(t, f, f.task.ID)
	require.Equal(t, models.OccurrenceCompleted, occurrence.State)
	require.EqualValues(t, 1, occurrence.Version)
	f.assertStock(t, 900)
	for _, m := range []any{&models.MaintenanceSchedule{}, &models.MaintenanceOccurrence{}, &models.MaintenanceExecution{}, &models.MaintenanceConsumption{}, &models.MaintenanceRequest{}} {
		count(t, f.DB, m, 1)
	}
}
