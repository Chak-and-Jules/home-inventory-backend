package main

import (
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/stretchr/testify/assert"
)

type fakeSQLDB struct {
	maxIdleConns    int
	maxOpenConns    int
	connMaxLifetime time.Duration
}

func (f *fakeSQLDB) SetMaxIdleConns(n int) {
	f.maxIdleConns = n
}

func (f *fakeSQLDB) SetMaxOpenConns(n int) {
	f.maxOpenConns = n
}

func (f *fakeSQLDB) SetConnMaxLifetime(d time.Duration) {
	f.connMaxLifetime = d
}

func TestInitAppEnvDefaultsToProduction(t *testing.T) {
	t.Setenv("APP_ENV", "")

	appEnv := initAppEnv()

	assert.Equal(t, "production", appEnv)
	assert.Equal(t, "production", getenv(t, "APP_ENV"))
}

func TestInitAppEnvKeepsExistingValue(t *testing.T) {
	t.Setenv("APP_ENV", "development")

	appEnv := initAppEnv()

	assert.Equal(t, "development", appEnv)
	assert.Equal(t, "development", getenv(t, "APP_ENV"))
}

func TestBuildPostgresDSN(t *testing.T) {
	t.Setenv("DB_HOST", "db.example.com")
	t.Setenv("DB_USER", "inventory_user")
	t.Setenv("DB_PASSWORD", "secret")
	t.Setenv("DB_NAME", "inventory")
	t.Setenv("DB_PORT", "5432")

	dsn := buildPostgresDSN()

	assert.Equal(t, "host=db.example.com user=inventory_user password=secret dbname=inventory port=5432 sslmode=require", dsn)
}

func TestConfigureConnectionPool(t *testing.T) {
	sqlDB := &fakeSQLDB{}

	configureConnectionPool(sqlDB)

	assert.Equal(t, 5, sqlDB.maxIdleConns)
	assert.Equal(t, 30, sqlDB.maxOpenConns)
	assert.Equal(t, time.Hour, sqlDB.connMaxLifetime)
}

func TestServerPortDefaultsTo8080(t *testing.T) {
	t.Setenv("PORT", "")

	assert.Equal(t, "8080", serverPort())
}

func TestServerPortUsesEnvironmentValue(t *testing.T) {
	t.Setenv("PORT", "3000")

	assert.Equal(t, "3000", serverPort())
}

func getenv(t *testing.T, key string) string {
	t.Helper()
	return os.Getenv(key)
}

func TestSetupDatabase(t *testing.T) {
	db, _ := setupDatabase("host=127.0.0.1 user=test password=test dbname=test port=0 sslmode=disable")
	assert.NotNil(t, db)
}

func TestAutoMigrateModelsIncludesShoppingListItem(t *testing.T) {
	var found bool
	for _, model := range autoMigrateModels() {
		if _, ok := model.(*models.ShoppingListItem); ok {
			found = true
			break
		}
	}

	assert.True(t, found, "ShoppingListItem must be registered for automatic migration")
}

// TestMainCrash verifies that main() crashes gracefully if database connection fails
func TestMainCrash(t *testing.T) {
	if os.Getenv("CRASH_TEST") == "1" {
		main()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestMainCrash")
	cmd.Env = append(os.Environ(), "CRASH_TEST=1", "DB_HOST=invalid_host")
	err := cmd.Run()
	if e, ok := err.(*exec.ExitError); ok && !e.Success() {
		return
	}
	t.Fatalf("process ran with err %v, want exit status 1", err)
}
