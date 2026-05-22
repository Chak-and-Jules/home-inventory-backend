package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/routes"
	"github.com/joho/godotenv"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func main() {
	// Load environment variables from .env file
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found. Using OS environment variables.")
	}

	appEnv := initAppEnv()
	log.Printf("Starting application in %s environment\n", appEnv)

	// Database Connection String
	dsn := buildPostgresDSN()

	// Connect to PostgreSQL via GORM
	db, err := setupDatabase(dsn)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	// Get generic database object sql.DB to configure connection pool
	sqlDB, err := db.DB()
	if err != nil {
		log.Fatalf("Failed to get generic database object: %v", err)
	}

	// Configure connection pool
	configureConnectionPool(sqlDB)

	// Auto-migrate models (creates/updates tables based on struct definitions)

	log.Println("Running AutoMigrate...")
	err = db.AutoMigrate(
		&models.Profile{},
		&models.Home{},
		&models.UserHome{},
		&models.SizeUnit{},
		&models.Category{},
		&models.ItemDefinition{},
		&models.InventoryItem{},
	)
	if err != nil {
		log.Println("AutoMigrate warning:", err)
	}

	// Setup Gin Router
	r := routes.SetupRouter(db)

	port := serverPort()

	log.Printf("Starting server on port %s...\n", port)
	if err := r.Run(":" + port); err != nil {
		log.Fatalf("Failed to start server: %v", err)
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
