package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/authentication"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/guides"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/imagestore"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/logger"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/maintenance"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/routes"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/utils"
	"github.com/joho/godotenv"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func main() {
	bootLog("application boot starting")
	done := bootStep("logger initialization")
	logger.InitLogger()
	done("completed")
	defer logger.Sync()

	// Load environment variables from .env file
	done = bootStep("environment loading")
	if err := godotenv.Load(); err != nil {
		logger.Log.Info("No .env file found. Using OS environment variables.")
	}
	done("completed")

	done = bootStep("application environment initialization")
	appEnv := initAppEnv()
	logger.Log.Sugar().Infof("Starting application in %s environment", appEnv)
	done("completed")

	// Database Connection String
	done = bootStep("database DSN construction")
	dsn := buildPostgresDSN()
	done("completed")

	// Connect to PostgreSQL via GORM
	done = bootStep("database connection")
	db, err := setupDatabase(dsn)
	if err != nil {
		done("failed")
		logger.Log.Sugar().Fatalf("Failed to connect to database: %v", err)
	}
	done("completed")

	// Get generic database object sql.DB to configure connection pool
	done = bootStep("database pool handle retrieval")
	sqlDB, err := db.DB()
	if err != nil {
		done("failed")
		logger.Log.Sugar().Fatalf("Failed to get generic database object: %v", err)
	}
	done("completed")

	// Configure connection pool
	done = bootStep("database connection pool configuration")
	configureConnectionPool(sqlDB)
	done("completed")

	// Auto-migrate models (creates/updates tables based on struct definitions)

	logger.Log.Info("Running AutoMigrate...")
	done = bootStep("database auto migration")
	err = db.AutoMigrate(autoMigrateModels()...)
	if err != nil {
		logger.Log.Sugar().Fatalf("Database schema initialization failed: %v", err)
	}
	done("completed")

	db, err = models.InitializeUnits(db)
	if err != nil {
		logger.Log.Sugar().Fatalf("Unit catalog initialization failed: %v", err)
	}

	done = bootStep("maintenance legacy initialization")
	if err := maintenance.InitializeLegacy(context.Background(), db); err != nil {
		done("failed")
		logger.Log.Sugar().Fatalf("Maintenance initialization failed: %v", err)
	}
	done("completed")

	// Setup Gin Router
	if err := guides.ValidateCatalog(guides.Catalog()); err != nil {
		logger.Log.Sugar().Fatalf("Guide catalog validation failed: %v", err)
	}
	done = bootStep("router setup")
	authClient, err := authentication.NewClient(context.Background())
	if err != nil {
		logger.Log.Sugar().Fatalf("Firebase authentication initialization failed: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	imageStore, closeImages, err := imagestore.Open(ctx)
	cancel()
	if err != nil {
		logger.Log.Sugar().Fatalf("Image storage initialization failed: %v", err)
	}
	defer closeImages()
	r := routes.SetupRouter(db, authClient, imageStore)
	done("completed")

	// Start background tasks
	done = bootStep("background task startup")
	startDailyRefresh(db)
	done("completed")

	port := serverPort()

	logger.Log.Sugar().Infof("Starting server on port %s...", port)
	bootLog("server listen starting port=%s", port)
	if err := r.Run(":" + port); err != nil {
		logger.Log.Sugar().Fatalf("Failed to start server: %v", err)
	}
}

func bootLog(format string, args ...interface{}) {
	fmt.Fprintf(os.Stdout, "boot: %s %s\n", time.Now().Format(time.RFC3339Nano), fmt.Sprintf(format, args...))
}

func bootStep(name string) func(string) {
	start := time.Now()
	bootLog("%s starting", name)
	return func(status string) {
		bootLog("%s %s duration=%s", name, status, time.Since(start))
	}
}

func autoMigrateModels() []interface{} {
	return []interface{}{
		&models.Profile{},
		&models.Language{},
		&models.Home{},
		&models.UserHome{},
		&models.UserDemoHome{},
		&models.GuideProgress{}, &models.GuideRequest{},
		&models.SizeUnit{},
		&models.Category{},
		&models.ItemDefinition{},
		&models.InventoryItem{},
		&models.ShoppingListItem{},
		&models.InventoryTransaction{},
		&models.MaintenanceTask{},
		&models.MaintenanceSchedule{}, &models.MaintenanceOccurrence{},
		&models.TaskItemDependency{}, &models.MaintenanceExecution{}, &models.MaintenanceConsumption{}, &models.MaintenanceRequest{},
		&models.ReceiptJob{}, &models.ReceiptJobItem{},
		&models.Recipe{}, &models.RecipeIngredient{}, &models.InventoryPrediction{},
	}
}

func initAppEnv() string {
	appEnv := os.Getenv("APP_ENV")
	if appEnv == "" {
		appEnv = "production"
		os.Setenv("APP_ENV", appEnv)
	}
	return appEnv
}

func buildPostgresDSN() string {
	return fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=require",
		os.Getenv("DB_HOST"),
		os.Getenv("DB_USER"),
		os.Getenv("DB_PASSWORD"),
		os.Getenv("DB_NAME"),
		os.Getenv("DB_PORT"),
	)
}

func configureConnectionPool(sqlDB interface {
	SetMaxIdleConns(int)
	SetMaxOpenConns(int)
	SetConnMaxLifetime(time.Duration)
}) {
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetMaxOpenConns(30)
	sqlDB.SetConnMaxLifetime(time.Hour)
}

func serverPort() string {
	port := os.Getenv("PORT")
	if port == "" {
		return "8080"
	}
	return port
}

func startDailyRefresh(db *gorm.DB) {
	go func() {
		for {
			utils.RefreshAllShoppingLists(db)
			// Sleep until next day at 3 AM
			now := time.Now()
			nextRun := time.Date(now.Year(), now.Month(), now.Day()+1, 3, 0, 0, 0, now.Location())
			time.Sleep(time.Until(nextRun))
		}
	}()
}

func setupDatabase(dsn string) (*gorm.DB, error) {
	return gorm.Open(postgres.New(postgres.Config{
		DSN:                  dsn,
		PreferSimpleProtocol: true, // disables implicit prepared statement usage
	}), &gorm.Config{
		NamingStrategy: schema.NamingStrategy{
			TablePrefix:   "",
			SingularTable: false,
		},
	})
}
