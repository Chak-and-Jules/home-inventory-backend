package maintenance

import (
	"context"
	"testing"
	"time"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestPostgresCompetingConsumersExcludeExpiredStock(t *testing.T) {
	requirePostgres(t)
	f := newFixture(t)
	now := time.Date(2030, 5, 1, 12, 0, 0, 0, time.UTC)
	f.Now = func() time.Time { return now }
	require.NoError(t, f.DB.Model(&f.stock).Update("expiration_date", now.Add(-time.Microsecond)).Error)
	fresh := models.InventoryItem{ID: uuid.New(), HomeID: f.home, ItemDefinitionID: f.def.ID, Quantity: 150}
	require.NoError(t, f.DB.Create(&fresh).Error)
	other := f.newTask(t, 100)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	start := make(chan struct{})
	results := make(chan outcome, 2)
	for _, task := range []uuid.UUID{f.task.ID, other.ID} {
		go func(id uuid.UUID) {
			<-start
			r, e := f.Complete(ctx, f.user, id, identified())
			results <- outcome{r, e}
		}(task)
	}
	close(start)
	success, insufficient := 0, 0
	for i := 0; i < 2; i++ {
		got := <-results
		if got.err == nil {
			success++
		} else {
			require.ErrorIs(t, got.err, ErrInsufficient)
			insufficient++
		}
	}
	require.Equal(t, 1, success)
	require.Equal(t, 1, insufficient)
	f.assertStock(t, 1000)
	require.NoError(t, f.DB.First(&fresh, fresh.ID).Error)
	require.Equal(t, 50.0, fresh.Quantity)
	for _, m := range []any{&models.MaintenanceExecution{}, &models.MaintenanceConsumption{}, &models.MaintenanceRequest{}, &models.InventoryTransaction{}, &models.ShoppingListItem{}} {
		count(t, f.DB, m, 1)
	}
	var usage models.MaintenanceConsumption
	require.NoError(t, f.DB.First(&usage).Error)
	require.Equal(t, fresh.ID, usage.InventoryItemID)
	require.Equal(t, 100.0, usage.Quantity)
	var log models.InventoryTransaction
	require.NoError(t, f.DB.First(&log).Error)
	require.Equal(t, fresh.ID, log.InventoryItemID)
	require.Equal(t, -100.0, log.QuantityChange)
	var tasks []models.MaintenanceTask
	require.NoError(t, f.DB.Order("id").Find(&tasks).Error)
	require.Len(t, tasks, 2)
	completed := 0
	for _, task := range tasks {
		if task.IsCompleted {
			completed++
			require.EqualValues(t, 1, task.Version)
			require.NotNil(t, task.CompletedAt)
		} else {
			require.Zero(t, task.Version)
			require.Nil(t, task.CompletedAt)
		}
	}
	require.Equal(t, 1, completed)
	var shopping models.ShoppingListItem
	require.NoError(t, f.DB.First(&shopping).Error)
	require.Equal(t, 1450.0, shopping.Quantity)
}

// Observe the actual PostgreSQL lock wait, rather than relying on goroutine
// timing or sleeps before committing the competing stock edit.
func observeStockWait(t *testing.T, f *fixture, ctx context.Context) func() {
	t.Helper()
	backend := make(chan int, 1)
	require.NoError(t, f.DB.Callback().Query().Before("gorm:query").Register("expiry:backend", func(tx *gorm.DB) {
		lock, ok := tx.Statement.Clauses["FOR"].Expression.(clause.Locking)
		if !ok || lock.Strength != "UPDATE" || tx.Statement.Table != "inventory_items" {
			return
		}
		var pid int
		if err := tx.Session(&gorm.Session{NewDB: true}).Raw("SELECT pg_backend_pid()").Scan(&pid).Error; err != nil {
			tx.AddError(err)
		}
		backend <- pid
	}))
	t.Cleanup(func() { require.NoError(t, f.DB.Callback().Query().Remove("expiry:backend")) })
	return func() {
		t.Helper()
		var pid int
		select {
		case pid = <-backend:
		case <-ctx.Done():
			t.Fatal("stock lock was not attempted")
		}
		require.Positive(t, pid)
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			var waiting bool
			require.NoError(t, f.DB.WithContext(ctx).Raw("SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE pid = ? AND wait_event_type = 'Lock')", pid).Scan(&waiting).Error)
			if waiting {
				return
			}
			select {
			case <-ticker.C:
			case <-ctx.Done():
				t.Fatal("completion did not wait for the stock writer")
			}
		}
	}
}

func TestPostgresExpiryRecheckedAfterStockLockWait(t *testing.T) {
	requirePostgres(t)
	for _, mode := range []string{"becomes expired", "expiry moves later"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t)
			now := f.Now()
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			require.NoError(t, f.DB.Model(&f.stock).Update("expiration_date", now.Add(time.Hour)).Error)
			var earlier models.InventoryItem
			newExpiry := now
			if mode == "expiry moves later" {
				expiry := now.Add(2 * time.Hour)
				earlier = models.InventoryItem{ID: uuid.New(), HomeID: f.home, ItemDefinitionID: f.def.ID, Quantity: 100, ExpirationDate: &expiry}
				require.NoError(t, f.DB.Create(&earlier).Error)
				newExpiry = now.Add(3 * time.Hour)
			}
			changed := f.DB.WithContext(ctx).Begin()
			require.NoError(t, changed.Error)
			defer changed.Rollback()
			require.NoError(t, changed.Model(&f.stock).Update("expiration_date", newExpiry).Error)
			wait := observeStockWait(t, f, ctx)
			done := make(chan error, 1)
			go func() { _, e := f.Complete(ctx, f.user, f.task.ID, identified()); done <- e }()
			wait()
			require.NoError(t, changed.Commit().Error)
			err := <-done
			if mode == "becomes expired" {
				require.ErrorIs(t, err, ErrInsufficient)
				f.assertRolledBack(t)
			} else {
				require.NoError(t, err)
				f.assertStock(t, 1000)
				require.NoError(t, f.DB.First(&earlier, earlier.ID).Error)
				require.Zero(t, earlier.Quantity)
				var usage models.MaintenanceConsumption
				require.NoError(t, f.DB.First(&usage).Error)
				require.Equal(t, earlier.ID, usage.InventoryItemID)
			}
			var item models.InventoryItem
			require.NoError(t, f.DB.First(&item, f.stock.ID).Error)
			require.True(t, newExpiry.Equal(*item.ExpirationDate))
		})
	}
}
