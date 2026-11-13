package maintenance

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/logger"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/google/uuid"
	"github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlog "gorm.io/gorm/logger"
)

var sqliteOnce sync.Once

type fixture struct {
	*Service
	user, home uuid.UUID
	def        models.ItemDefinition
	stock      models.InventoryItem
	task       models.MaintenanceTask
}

func testDB(t *testing.T) *gorm.DB {
	t.Helper()
	config := &gorm.Config{Logger: gormlog.Default.LogMode(gormlog.Silent)}
	var db *gorm.DB
	var err error
	if dsn := os.Getenv("UNIT_TEST_POSTGRES_DSN"); dsn != "" {
		admin, e := gorm.Open(postgres.New(postgres.Config{DSN: dsn, PreferSimpleProtocol: true}), config)
		require.NoError(t, e)
		schema := "maintenance_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		require.NoError(t, admin.Exec("CREATE SCHEMA "+schema).Error)
		db, err = gorm.Open(postgres.New(postgres.Config{DSN: dsn + " search_path=" + schema, PreferSimpleProtocol: true}), config)
		t.Cleanup(func() {
			c, _ := db.DB()
			c.Close()
			require.NoError(t, admin.Exec("DROP SCHEMA "+schema+" CASCADE").Error)
			a, _ := admin.DB()
			a.Close()
		})
	} else {
		sqliteOnce.Do(func() {
			sql.Register("sqlite3_maintenance", &sqlite3.SQLiteDriver{ConnectHook: func(c *sqlite3.SQLiteConn) error {
				if err := c.RegisterFunc("gen_random_uuid", uuid.NewString, false); err != nil {
					return err
				}
				return c.RegisterFunc("NOW", func() string { return time.Now().UTC().Format("2006-01-02 15:04:05") }, false)
			}})
		})
		db, err = gorm.Open(sqlite.New(sqlite.Config{DriverName: "sqlite3_maintenance", DSN: "file:" + uuid.NewString() + "?mode=memory&cache=shared&_foreign_keys=on"}), config)
		require.NoError(t, err)
		c, e := db.DB()
		require.NoError(t, e)
		c.SetMaxOpenConns(1)
		t.Cleanup(func() { c.Close() })
		require.NoError(t, db.Callback().Raw().Before("gorm:raw").Register("test:timestamps", func(tx *gorm.DB) {
			q := tx.Statement.SQL.String()
			tx.Statement.SQL.Reset()
			tx.Statement.SQL.WriteString(strings.ReplaceAll(q, "timestamp with time zone", "datetime"))
		}))
	}
	require.NoError(t, err)
	return db
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	logger.InitLogger()
	db := testDB(t)
	require.NoError(t, db.AutoMigrate(&models.Profile{}, &models.Home{}, &models.UserHome{}, &models.SizeUnit{}, &models.ItemDefinition{}, &models.InventoryItem{}, &models.InventoryTransaction{}, &models.ShoppingListItem{}, &models.MaintenanceTask{}, &models.MaintenanceSchedule{}, &models.MaintenanceOccurrence{}, &models.TaskItemDependency{}, &models.MaintenanceExecution{}, &models.MaintenanceConsumption{}, &models.MaintenanceRequest{}))
	db, err := models.InitializeUnits(db)
	require.NoError(t, err)
	f := &fixture{Service: New(db), user: uuid.New(), home: uuid.New()}
	now := time.Now().UTC().Truncate(time.Second)
	f.Now = func() time.Time { return now }
	require.NoError(t, db.Create(&models.Profile{ID: f.user, Email: f.user.String() + "@example.com"}).Error)
	require.NoError(t, db.Create(&models.Home{ID: f.home, Name: "Home"}).Error)
	require.NoError(t, db.Create(&models.UserHome{UserID: f.user, HomeID: f.home, Role: models.RoleOwner}).Error)
	var unit models.SizeUnit
	require.NoError(t, db.First(&unit, "name = ?", "kg").Error)
	threshold, target := 950.0, 1500.0
	f.def = models.ItemDefinition{ID: uuid.New(), HomeID: f.home, Name: "Filter material", SizeUnitID: &unit.ID, LowStockThreshold: &threshold, TargetQuantity: &target}
	require.NoError(t, db.Create(&f.def).Error)
	expiry := now.AddDate(0, 1, 0)
	f.stock = models.InventoryItem{ID: uuid.New(), HomeID: f.home, ItemDefinitionID: f.def.ID, Quantity: 1000, ExpirationDate: &expiry}
	require.NoError(t, db.Create(&f.stock).Error)
	f.task = f.newTask(t, 100)
	return f
}

func (f *fixture) newTask(t *testing.T, amount float64) models.MaintenanceTask {
	t.Helper()
	task := models.MaintenanceTask{ID: uuid.New(), HomeID: f.home, Description: "Replace filter", ScheduledDate: f.Now(), Frequency: "Once"}
	require.NoError(t, f.DB.Create(&task).Error)
	if amount > 0 {
		require.NoError(t, f.DB.Create(&models.TaskItemDependency{ID: uuid.New(), MaintenanceTaskID: task.ID, ItemDefinitionID: f.def.ID, QuantityRequired: amount}).Error)
	}
	return task
}
func identified() Command {
	id := uuid.New()
	version := int64(0)
	return Command{RequestID: &id, ExpectedVersion: &version}
}
func count(t *testing.T, db *gorm.DB, model any, want int64) {
	t.Helper()
	var n int64
	require.NoError(t, db.Model(model).Count(&n).Error)
	require.Equal(t, want, n, fmt.Sprintf("%T", model))
}
func (f *fixture) assertStock(t *testing.T, want float64) {
	t.Helper()
	var stock models.InventoryItem
	require.NoError(t, f.DB.First(&stock, f.stock.ID).Error)
	require.Equal(t, want, stock.Quantity)
}
func (f *fixture) assertRolledBack(t *testing.T) {
	t.Helper()
	f.assertStock(t, 1000)
	var task models.MaintenanceTask
	require.NoError(t, f.DB.First(&task, f.task.ID).Error)
	require.False(t, task.IsCompleted)
	require.Nil(t, task.CompletedAt)
	require.Zero(t, task.Version)
	for _, m := range []any{&models.InventoryTransaction{}, &models.MaintenanceExecution{}, &models.MaintenanceConsumption{}, &models.MaintenanceRequest{}, &models.ShoppingListItem{}} {
		count(t, f.DB, m, 0)
	}
}

func TestCompleteJournalAndReplay(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	command := identified()
	result, err := f.Complete(ctx, f.user, f.task.ID, command)
	require.NoError(t, err)
	require.NotNil(t, result.ExecutionID)
	require.EqualValues(t, 1, result.Version)
	require.False(t, result.AlreadyCompleted)
	require.False(t, result.Replayed)
	require.True(t, f.Now().Equal(*result.CompletedAt))
	f.assertStock(t, 900)
	var execution models.MaintenanceExecution
	require.NoError(t, f.DB.First(&execution, result.ExecutionID).Error)
	require.Equal(t, f.user, execution.ActorUserID)
	require.Equal(t, command.RequestID, execution.RequestID)
	require.Equal(t, f.home, execution.HomeID)
	require.Equal(t, f.task.ID, execution.TaskID)
	var snapshot models.MaintenanceTask
	require.NoError(t, json.Unmarshal([]byte(execution.SnapshotJSON), &snapshot))
	require.Equal(t, f.task.Description, snapshot.Description)
	require.Len(t, snapshot.Dependencies, 1)
	require.Equal(t, 100.0, snapshot.Dependencies[0].QuantityRequired)
	var usage models.MaintenanceConsumption
	require.NoError(t, f.DB.First(&usage).Error)
	require.Equal(t, *result.ExecutionID, usage.ExecutionID)
	require.Equal(t, f.def.Name, usage.DefinitionName)
	require.Equal(t, "g", usage.BaseUnitName)
	require.Equal(t, "mass", usage.UnitFamily)
	require.Equal(t, 100.0, usage.Quantity)
	require.Equal(t, f.stock.ID, usage.InventoryItemID)
	require.True(t, f.stock.ExpirationDate.Equal(*usage.ExpirationDate))
	var log models.InventoryTransaction
	require.NoError(t, f.DB.First(&log, usage.InventoryTransactionID).Error)
	require.Equal(t, -100.0, log.QuantityChange)
	require.Equal(t, models.InventorySourceMaintenance, *log.Source)
	require.Equal(t, result.ExecutionID, log.MaintenanceExecutionID)
	var shopping models.ShoppingListItem
	require.NoError(t, f.DB.First(&shopping).Error)
	require.Equal(t, 600.0, shopping.Quantity)
	retry, err := f.Complete(ctx, f.user, f.task.ID, command)
	require.NoError(t, err)
	require.True(t, retry.Replayed)
	require.Equal(t, result.ExecutionID, retry.ExecutionID)
	require.Equal(t, result.CompletedAt, retry.CompletedAt)
	require.Equal(t, result.AlreadyCompleted, retry.AlreadyCompleted)
	// An unrelated command with the old version must conflict, even on a completed task.
	_, err = f.Complete(ctx, f.user, f.task.ID, identified())
	require.ErrorIs(t, err, ErrConflict)
	changed := int64(1)
	command.ExpectedVersion = &changed
	_, err = f.Complete(ctx, f.user, f.task.ID, command)
	require.ErrorIs(t, err, ErrRequestReuse)
	other := f.newTask(t, 100)
	_, err = f.Complete(ctx, f.user, other.ID, command)
	require.ErrorIs(t, err, ErrRequestReuse)
	legacy, err := f.Complete(ctx, f.user, f.task.ID, Command{})
	require.NoError(t, err)
	require.True(t, legacy.AlreadyCompleted)
	require.Equal(t, result.ExecutionID, legacy.ExecutionID)
	f.assertStock(t, 900)
	for _, m := range []any{&models.MaintenanceExecution{}, &models.MaintenanceConsumption{}, &models.MaintenanceRequest{}, &models.InventoryTransaction{}, &models.ShoppingListItem{}} {
		count(t, f.DB, m, 1)
	}
}

func TestCompleteRollbackAtEveryWrite(t *testing.T) {
	for _, stage := range []string{"maintenance_executions", "inventory_items", "inventory_transactions", "maintenance_consumptions", "maintenance_tasks", "shopping_list_items", "maintenance_requests"} {
		t.Run(stage, func(t *testing.T) {
			f := newFixture(t)
			failure := errors.New("injected storage failure")
			fail := func(tx *gorm.DB) {
				if tx.Statement.Table == stage {
					tx.AddError(failure)
				}
			}
			require.NoError(t, f.DB.Callback().Create().After("gorm:create").Register("test:fail", fail))
			require.NoError(t, f.DB.Callback().Update().After("gorm:update").Register("test:fail", fail))
			command := identified()
			_, err := f.Complete(context.Background(), f.user, f.task.ID, command)
			require.ErrorIs(t, err, failure)
			f.assertRolledBack(t)
			require.NoError(t, f.DB.Callback().Create().Remove("test:fail"))
			require.NoError(t, f.DB.Callback().Update().Remove("test:fail"))
			_, err = f.Complete(context.Background(), f.user, f.task.ID, command)
			require.NoError(t, err)
			f.assertStock(t, 900)
		})
	}
	t.Run("shopping read failure without threshold", func(t *testing.T) {
		f := newFixture(t)
		require.NoError(t, f.DB.Model(&f.def).Update("low_stock_threshold", nil).Error)
		require.NoError(t, f.DB.Callback().Query().After("gorm:query").Register("test:fail_read", func(tx *gorm.DB) {
			if tx.Statement.Table == "shopping_list_items" {
				tx.AddError(errors.New("shopping unavailable"))
			}
		}))
		_, err := f.Complete(context.Background(), f.user, f.task.ID, identified())
		require.Error(t, err)
		require.NoError(t, f.DB.Callback().Query().Remove("test:fail_read"))
		f.assertRolledBack(t)
	})
}

func TestCompleteInsufficientSecondSupplyAndExactBatches(t *testing.T) {
	t.Run("second supply rolls back first", func(t *testing.T) {
		f := newFixture(t)
		// Fixed UUIDs force the well-stocked definition to be consumed first.
		second := f.def
		second.ID = uuid.MustParse("ffffffff-ffff-ffff-ffff-ffffffffffff")
		second.Name = "Unavailable"
		require.NoError(t, f.DB.Create(&second).Error)
		require.NoError(t, f.DB.Create(&models.TaskItemDependency{ID: uuid.New(), MaintenanceTaskID: f.task.ID, ItemDefinitionID: second.ID, QuantityRequired: 1}).Error)
		_, err := f.Complete(context.Background(), f.user, f.task.ID, identified())
		require.ErrorIs(t, err, ErrInsufficient)
		f.assertRolledBack(t)
	})
	t.Run("duplicate requirements split over ordered batches", func(t *testing.T) {
		f := newFixture(t)
		require.NoError(t, f.DB.Model(&f.stock).Update("quantity", 0.1).Error)
		later := f.stock
		later.ID = uuid.New()
		later.Quantity = 0.2
		later.ExpirationDate = nil
		require.NoError(t, f.DB.Create(&later).Error)
		require.NoError(t, f.DB.Model(&models.TaskItemDependency{}).Where("maintenance_task_id = ?", f.task.ID).Update("quantity_required", 0.1).Error)
		require.NoError(t, f.DB.Create(&models.TaskItemDependency{ID: uuid.New(), MaintenanceTaskID: f.task.ID, ItemDefinitionID: f.def.ID, QuantityRequired: 0.2}).Error)
		_, err := f.Complete(context.Background(), f.user, f.task.ID, identified())
		require.NoError(t, err)
		f.assertStock(t, 0)
		require.NoError(t, f.DB.First(&later, later.ID).Error)
		require.Zero(t, later.Quantity)
		var usage []models.MaintenanceConsumption
		require.NoError(t, f.DB.Order("quantity").Find(&usage).Error)
		require.Len(t, usage, 2)
		require.Equal(t, 0.1, usage[0].Quantity)
		require.Equal(t, 0.2, usage[1].Quantity)
		require.Equal(t, f.stock.ID, usage[0].InventoryItemID)
	})
}

func TestCompletePermissionsLegacyAndRetainedFacts(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	command := identified()
	require.NoError(t, f.DB.Model(&models.UserHome{}).Where("user_id = ? AND home_id = ?", f.user, f.home).Update("role", models.RoleViewer).Error)
	_, err := f.Complete(ctx, f.user, f.task.ID, command)
	require.ErrorIs(t, err, ErrForbidden)
	f.assertRolledBack(t)
	_, err = f.Complete(ctx, uuid.New(), f.task.ID, command)
	require.ErrorIs(t, err, ErrForbidden)
	require.NoError(t, f.DB.Model(&models.UserHome{}).Where("user_id = ? AND home_id = ?", f.user, f.home).Update("role", models.RoleEditor).Error)
	result, err := f.Complete(ctx, f.user, f.task.ID, command)
	require.NoError(t, err)
	require.NoError(t, f.DB.Where("user_id = ? AND home_id = ?", f.user, f.home).Delete(&models.UserHome{}).Error)
	_, err = f.Complete(ctx, f.user, f.task.ID, command)
	require.ErrorIs(t, err, ErrForbidden)
	_, err = f.Complete(ctx, f.user, f.task.ID, Command{})
	require.ErrorIs(t, err, ErrForbidden)
	require.NoError(t, f.DB.Create(&models.UserHome{UserID: f.user, HomeID: f.home, Role: models.RoleOwner}).Error)
	// Old completed data has no invented actor, execution or timestamp.
	old := f.newTask(t, 0)
	require.NoError(t, f.DB.Model(&old).Update("is_completed", true).Error)
	oldResult, err := f.Complete(ctx, f.user, old.ID, identified())
	require.NoError(t, err)
	require.True(t, oldResult.AlreadyCompleted)
	require.Nil(t, oldResult.ExecutionID)
	require.Nil(t, oldResult.CompletedAt)
	// A raw reopen must not cause a second stock deduction.
	require.NoError(t, f.DB.Model(&f.task).Update("is_completed", false).Error)
	_, err = f.Complete(ctx, f.user, f.task.ID, Command{})
	require.ErrorIs(t, err, ErrCompleted)
	require.NoError(t, f.DB.Delete(&f.task).Error)
	require.NoError(t, f.DB.Delete(&f.stock).Error)
	count(t, f.DB, &models.InventoryTransaction{}, 0)
	count(t, f.DB, &models.MaintenanceConsumption{}, 1)
	replayed, err := f.Complete(ctx, f.user, f.task.ID, command)
	require.NoError(t, err)
	require.Equal(t, result.ExecutionID, replayed.ExecutionID)
	require.NoError(t, f.DB.Delete(&models.Home{}, "id = ?", f.home).Error)
	for _, m := range []any{&models.MaintenanceExecution{}, &models.MaintenanceConsumption{}, &models.MaintenanceRequest{}} {
		count(t, f.DB, m, 0)
	}
	_, err = f.Complete(ctx, f.user, f.task.ID, command)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestCompleteValidationAndMissingData(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	zero := uuid.Nil
	negative := int64(-1)
	version := int64(0)
	for _, c := range []Command{{RequestID: &zero}, {ExpectedVersion: &version}, {RequestID: identified().RequestID, ExpectedVersion: &negative}} {
		_, err := f.Complete(ctx, f.user, f.task.ID, c)
		require.ErrorIs(t, err, ErrInvalid)
	}
	_, err := f.Complete(ctx, uuid.Nil, f.task.ID, Command{})
	require.ErrorIs(t, err, ErrInvalid)
	_, err = f.Complete(ctx, f.user, uuid.New(), Command{})
	require.ErrorIs(t, err, ErrNotFound)
	foreignHome := models.Home{ID: uuid.New(), Name: "Other"}
	require.NoError(t, f.DB.Create(&foreignHome).Error)
	foreign := f.def
	foreign.ID = uuid.New()
	foreign.HomeID = foreignHome.ID
	require.NoError(t, f.DB.Create(&foreign).Error)
	require.NoError(t, f.DB.Model(&models.TaskItemDependency{}).Where("maintenance_task_id = ?", f.task.ID).Update("item_definition_id", foreign.ID).Error)
	_, err = f.Complete(ctx, f.user, f.task.ID, Command{})
	require.ErrorIs(t, err, ErrDependency)
	f.assertRolledBack(t)
	// A supply-free task can still be journaled.
	free := f.newTask(t, 0)
	_, err = f.Complete(ctx, f.user, free.ID, Command{})
	require.NoError(t, err)
	count(t, f.DB, &models.MaintenanceExecution{}, 1)
}
