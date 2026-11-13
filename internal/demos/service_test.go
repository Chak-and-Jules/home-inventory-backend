package demos

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/guides"
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

var registerSQLite sync.Once

func serviceFixture(t *testing.T) (*Service, uuid.UUID) {
	t.Helper()
	logger.InitLogger()
	config := &gorm.Config{Logger: gormlog.Default.LogMode(gormlog.Silent)}
	var db *gorm.DB
	var err error
	if dsn := os.Getenv("DEMO_TEST_POSTGRES_DSN"); dsn != "" {
		admin, e := gorm.Open(postgres.Open(dsn), config)
		require.NoError(t, e)
		conn, e := admin.DB()
		require.NoError(t, e)
		schema := "demo_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		require.NoError(t, admin.Exec("CREATE SCHEMA "+schema).Error)
		t.Cleanup(func() {
			require.NoError(t, admin.Exec("DROP SCHEMA "+schema+" CASCADE").Error)
			require.NoError(t, conn.Close())
		})
		db, err = gorm.Open(postgres.Open(dsn+" search_path="+schema), config)
	} else {
		registerSQLite.Do(func() {
			sql.Register("sqlite3_demos", &sqlite3.SQLiteDriver{ConnectHook: func(c *sqlite3.SQLiteConn) error {
				if err := c.RegisterFunc("gen_random_uuid", uuid.NewString, false); err != nil {
					return err
				}
				return c.RegisterFunc("NOW", func() string { return time.Now().UTC().Format("2006-01-02 15:04:05") }, false)
			}})
		})
		db, err = gorm.Open(sqlite.New(sqlite.Config{DriverName: "sqlite3_demos", DSN: "file:" + uuid.NewString() + "?mode=memory&cache=shared&_foreign_keys=on"}), config)
	}
	require.NoError(t, err)
	connection, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, connection.Close()) })
	if db.Dialector.Name() == "sqlite" {
		connection.SetMaxOpenConns(1)
		require.NoError(t, db.Callback().Raw().Before("gorm:raw").Register("demo_timestamp", func(tx *gorm.DB) {
			q := tx.Statement.SQL.String()
			tx.Statement.SQL.Reset()
			tx.Statement.SQL.WriteString(strings.ReplaceAll(q, "timestamp with time zone", "datetime"))
		}))
	}
	require.NoError(t, db.AutoMigrate(&models.Profile{}, &models.Home{}, &models.UserHome{}, &models.UserDemoHome{}, &models.GuideProgress{}, &models.GuideRequest{}, &models.SizeUnit{}, &models.Category{}, &models.ItemDefinition{}, &models.InventoryItem{}, &models.InventoryTransaction{}, &models.ShoppingListItem{}, &models.MaintenanceTask{}, &models.TaskItemDependency{}, &models.MaintenanceExecution{}, &models.MaintenanceConsumption{}, &models.MaintenanceRequest{}, &models.Recipe{}, &models.RecipeIngredient{}, &models.ReceiptJob{}, &models.ReceiptJobItem{}, &models.InventoryPrediction{}))
	db, err = models.InitializeUnits(db)
	require.NoError(t, err)
	user := uuid.New()
	require.NoError(t, db.Create(&models.Profile{ID: user, Email: user.String() + "@example.com"}).Error)
	s := New(db)
	now := time.Now().UTC().Truncate(time.Second)
	s.Now = func() time.Time { return now }
	return s, user
}

func apply(t *testing.T, s *Service, user uuid.UUID, action string) View {
	t.Helper()
	v, err := s.Status(context.Background(), user)
	require.NoError(t, err)
	v, err = s.Act(context.Background(), user, Command{RequestID: uuid.New(), ExpectedVersion: &v.Version, Action: action}, nil)
	require.NoError(t, err)
	return v
}

func TestDemoServiceLifecycleAndReceipts(t *testing.T) {
	s, user := serviceFixture(t)
	ctx := context.Background()
	v, err := s.Status(ctx, user)
	require.NoError(t, err)
	require.Equal(t, "not_created", v.Status)
	require.NoError(t, s.EnsureInitialHomes(ctx, user, nil))
	require.NoError(t, s.EnsureInitialHomes(ctx, user, nil))
	v, err = s.Status(ctx, user)
	require.NoError(t, err)
	require.EqualValues(t, 1, v.Version)
	homeID := v.Home.ID
	reset := Command{RequestID: uuid.New(), ExpectedVersion: &v.Version, Action: "reset"}
	after, err := s.Act(ctx, user, reset, nil)
	require.NoError(t, err)
	require.Equal(t, homeID, after.Home.ID)
	require.EqualValues(t, 2, after.Version)
	removed := apply(t, s, user, "remove")
	require.Equal(t, "removed", removed.Status)
	require.NoError(t, s.EnsureInitialHomes(ctx, user, nil))
	replayed, err := s.Act(ctx, user, reset, nil)
	require.NoError(t, err)
	require.Equal(t, "removed", replayed.Status)
	require.Equal(t, removed.Version, replayed.Version)
	reset.Action = "remove"
	_, err = s.Act(ctx, user, reset, nil)
	require.ErrorIs(t, err, guides.ErrRequestReuse)
	newDemo := apply(t, s, user, "create")
	require.NotEqual(t, homeID, newDemo.Home.ID)
	require.NoError(t, s.RemoveByHome(ctx, user, newDemo.Home.ID))
	_, err = s.Status(ctx, user)
	require.NoError(t, err)
	_, err = s.Act(ctx, user, Command{RequestID: uuid.New(), ExpectedVersion: &newDemo.Version, Action: "create"}, nil)
	require.ErrorIs(t, err, guides.ErrConflict)
}

func TestDemoServiceErrorsIsolationAndRollback(t *testing.T) {
	s, user := serviceFixture(t)
	ctx := context.Background()
	zero := int64(0)
	_, err := s.Act(ctx, user, Command{}, nil)
	require.ErrorIs(t, err, guides.ErrInvalid)
	_, err = s.Act(ctx, user, Command{RequestID: uuid.New(), ExpectedVersion: &zero, Action: "reset"}, nil)
	require.ErrorIs(t, err, ErrUnavailable)
	require.NoError(t, s.DB.Callback().Create().Before("gorm:create").Register("fail_demo_seed", func(tx *gorm.DB) {
		if tx.Statement.Table == "inventory_items" {
			tx.AddError(errors.New("seed failed"))
		}
	}))
	_, err = s.Act(ctx, user, Command{RequestID: uuid.New(), ExpectedVersion: &zero, Action: "create"}, nil)
	require.Error(t, err)
	require.NoError(t, s.DB.Callback().Create().Remove("fail_demo_seed"))
	v, err := s.Status(ctx, user)
	require.NoError(t, err)
	require.Equal(t, "not_created", v.Status)
	v = apply(t, s, user, "create")
	_, err = s.Act(ctx, user, Command{RequestID: uuid.New(), ExpectedVersion: &v.Version, Action: "create"}, nil)
	require.ErrorIs(t, err, ErrExists)
	other := uuid.New()
	require.NoError(t, s.DB.Create(&models.Profile{ID: other, Email: other.String() + "@example.com"}).Error)
	require.ErrorIs(t, s.RemoveByHome(ctx, other, v.Home.ID), ErrAccess)
	// Ownership checks prevent accidentally treating real stock as demo content.
	require.NoError(t, s.DB.Model(&models.Home{}).Where("id = ?", v.Home.ID).Update("is_demo", false).Error)
	_, err = s.Status(ctx, user)
	require.ErrorIs(t, err, ErrAccess)
	require.NoError(t, s.DB.Model(&models.Home{}).Where("id = ?", v.Home.ID).Update("is_demo", true).Error)
	require.NoError(t, s.DB.Callback().Create().Before("gorm:create").Register("fail_receipt", func(tx *gorm.DB) {
		if tx.Statement.Table == "guide_requests" {
			tx.AddError(errors.New("receipt failed"))
		}
	}))
	_, err = s.Act(ctx, user, Command{RequestID: uuid.New(), ExpectedVersion: &v.Version, Action: "remove"}, nil)
	require.Error(t, err)
	require.NoError(t, s.DB.Callback().Create().Remove("fail_receipt"))
	saved, err := s.Status(ctx, user)
	require.NoError(t, err)
	require.Equal(t, v.Home.ID, saved.Home.ID)
	require.Equal(t, v.Version, saved.Version)
}

func TestDemoServiceExistingHomesAndRemovedTombstone(t *testing.T) {
	s, user := serviceFixture(t)
	ctx := context.Background()
	home := models.Home{ID: uuid.New(), Name: "Existing personal home"}
	require.NoError(t, s.DB.Create(&home).Error)
	require.NoError(t, s.DB.Create(&models.UserHome{UserID: user, HomeID: home.ID, Role: models.RoleOwner, IsDefault: true}).Error)
	require.NoError(t, s.EnsureInitialHomes(ctx, user, nil))
	v, err := s.Status(ctx, user)
	require.NoError(t, err)
	require.Equal(t, "not_created", v.Status)
	apply(t, s, user, "create")
	apply(t, s, user, "remove")
	// Even losing all normal memberships must not silently recreate a removed demo.
	require.NoError(t, s.DB.Where("user_id = ?", user).Delete(&models.UserHome{}).Error)
	require.NoError(t, s.EnsureInitialHomes(ctx, user, nil))
	v, err = s.Status(ctx, user)
	require.NoError(t, err)
	require.Equal(t, "removed", v.Status)
	var count int64
	require.NoError(t, s.DB.Model(&models.UserHome{}).Where("user_id = ?", user).Count(&count).Error)
	require.EqualValues(t, 1, count)
}
