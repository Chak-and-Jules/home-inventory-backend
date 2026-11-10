package maintenance

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type outcome struct {
	result Result
	err    error
}

func requirePostgres(t *testing.T) {
	t.Helper()
	if os.Getenv("UNIT_TEST_POSTGRES_DSN") == "" {
		t.Skip("requires disposable PostgreSQL via UNIT_TEST_POSTGRES_DSN")
	}
}

func TestPostgresConcurrentCompletion(t *testing.T) {
	requirePostgres(t)
	for _, mode := range []string{"legacy", "same command", "distinct versioned commands"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			start := make(chan struct{})
			results := make(chan outcome, 8)
			shared := identified()
			for i := 0; i < 8; i++ {
				command := Command{}
				if mode == "same command" {
					command = shared
				}
				if mode == "distinct versioned commands" {
					command = identified()
				}
				go func(c Command) { <-start; r, err := f.Complete(ctx, f.user, f.task.ID, c); results <- outcome{r, err} }(command)
			}
			close(start)
			success := 0
			for i := 0; i < 8; i++ {
				got := <-results
				if mode == "distinct versioned commands" && got.err != nil {
					require.ErrorIs(t, got.err, ErrConflict)
				} else {
					require.NoError(t, got.err)
					require.NotNil(t, got.result.ExecutionID)
					success++
				}
			}
			if mode == "distinct versioned commands" {
				require.Equal(t, 1, success)
			} else {
				require.Equal(t, 8, success)
			}
			f.assertStock(t, 900)
			count(t, f.DB, &models.MaintenanceExecution{}, 1)
			count(t, f.DB, &models.MaintenanceConsumption{}, 1)
			count(t, f.DB, &models.InventoryTransaction{}, 1)
			count(t, f.DB, &models.ShoppingListItem{}, 1)
			receipts := int64(1)
			if mode == "legacy" {
				receipts = 0
			}
			count(t, f.DB, &models.MaintenanceRequest{}, receipts)
		})
	}
}

func TestPostgresSameRequestAcrossTasks(t *testing.T) {
	requirePostgres(t)
	f := newFixture(t)
	second := f.newTask(t, 100)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	start := make(chan struct{})
	results := make(chan outcome, 2)
	command := identified()
	for _, id := range []uuid.UUID{f.task.ID, second.ID} {
		go func(task uuid.UUID) {
			<-start
			r, e := f.Complete(ctx, f.user, task, command)
			results <- outcome{r, e}
		}(id)
	}
	close(start)
	success, conflicts := 0, 0
	for i := 0; i < 2; i++ {
		got := <-results
		if got.err == nil {
			success++
		} else {
			require.ErrorIs(t, got.err, ErrRequestReuse)
			conflicts++
		}
	}
	require.Equal(t, 1, success)
	require.Equal(t, 1, conflicts)
	f.assertStock(t, 900)
	for _, m := range []any{&models.MaintenanceExecution{}, &models.MaintenanceConsumption{}, &models.InventoryTransaction{}, &models.MaintenanceRequest{}, &models.ShoppingListItem{}} {
		count(t, f.DB, m, 1)
	}
	var tasks []models.MaintenanceTask
	require.NoError(t, f.DB.Where("is_completed = ?", true).Find(&tasks).Error)
	require.Len(t, tasks, 1)
}

func TestPostgresCompletionReadsStateAfterWaiting(t *testing.T) {
	requirePostgres(t)
	for _, mode := range []string{"edited legacy", "edited versioned", "deleted"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			tx := f.DB.WithContext(ctx).Begin()
			require.NoError(t, tx.Error)
			defer tx.Rollback()
			_, err := LoadForWrite(tx, f.user, f.task.ID)
			require.NoError(t, err)
			attempted := make(chan struct{})
			var once sync.Once
			require.NoError(t, f.DB.Callback().Query().Before("gorm:query").Register("test:task_wait", func(q *gorm.DB) {
				lock, ok := q.Statement.Clauses["FOR"].Expression.(clause.Locking)
				if ok && lock.Strength == "UPDATE" && q.Statement.Table == "maintenance_tasks" {
					once.Do(func() { close(attempted) })
				}
			}))
			results := make(chan outcome, 1)
			command := Command{}
			if mode == "edited versioned" {
				command = identified()
			}
			go func() { r, e := f.Complete(ctx, f.user, f.task.ID, command); results <- outcome{r, e} }()
			select {
			case <-attempted:
			case <-ctx.Done():
				t.Fatal("completion did not attempt authoritative task lock")
			}
			if mode == "deleted" {
				require.NoError(t, tx.Delete(&f.task).Error)
			} else {
				require.NoError(t, tx.Model(&models.TaskItemDependency{}).Where("maintenance_task_id = ?", f.task.ID).Update("quantity_required", 200).Error)
				require.NoError(t, tx.Model(&f.task).Updates(map[string]any{"description": "Edited while completion waited", "version": 1}).Error)
			}
			require.NoError(t, tx.Commit().Error)
			got := <-results
			require.NoError(t, f.DB.Callback().Query().Remove("test:task_wait"))
			if mode == "edited legacy" {
				require.NoError(t, got.err)
				f.assertStock(t, 800)
				var execution models.MaintenanceExecution
				require.NoError(t, f.DB.First(&execution).Error)
				require.Contains(t, execution.SnapshotJSON, "Edited while completion waited")
			} else {
				if mode == "deleted" {
					require.ErrorIs(t, got.err, ErrNotFound)
				} else {
					require.ErrorIs(t, got.err, ErrConflict)
				}
				f.assertStock(t, 1000)
				count(t, f.DB, &models.MaintenanceExecution{}, 0)
				count(t, f.DB, &models.MaintenanceConsumption{}, 0)
				count(t, f.DB, &models.MaintenanceRequest{}, 0)
			}
		})
	}
}

func TestPostgresDifferentTasksCannotOverspendStock(t *testing.T) {
	requirePostgres(t)
	f := newFixture(t)
	require.NoError(t, f.DB.Model(&f.stock).Update("quantity", 150).Error)
	other := f.newTask(t, 100)
	start := make(chan struct{})
	results := make(chan error, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for _, id := range []uuid.UUID{f.task.ID, other.ID} {
		go func(task uuid.UUID) { <-start; _, e := f.Complete(ctx, f.user, task, identified()); results <- e }(id)
	}
	close(start)
	success := 0
	for i := 0; i < 2; i++ {
		e := <-results
		if e == nil {
			success++
		} else {
			require.True(t, errors.Is(e, ErrInsufficient), e)
		}
	}
	require.Equal(t, 1, success)
	f.assertStock(t, 50)
	count(t, f.DB, &models.MaintenanceExecution{}, 1)
	count(t, f.DB, &models.MaintenanceRequest{}, 1)
}
