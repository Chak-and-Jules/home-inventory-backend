package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
)

func setupTestDB() (*gorm.DB, sqlmock.Sqlmock, error) {
	db, mock, err := sqlmock.New()
	if err != nil {
		return nil, nil, err
	}

	dialector := postgres.New(postgres.Config{
		Conn:       db,
		DriverName: "postgres",
	})
	gormDB, err := gorm.Open(dialector, &gorm.Config{})
	if err != nil {
		return nil, nil, err
	}

	return gormDB, mock, nil
}

func TestGetItemDefinitions_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, err := setupTestDB()
	assert.NoError(t, err)

	sqlDB, err := db.DB()
	assert.NoError(t, err)
	defer sqlDB.Close()

	handler := &ItemDefinitionHandler{DB: db}

	id := uuid.New()
	categoryID := uuid.New()
	sizeUnitID := uuid.New()

	// Mock the main query
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "item_definitions"`)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "description", "category_id", "size_unit_id", "is_expirable", "image_url", "created_at", "updated_at"}).
			AddRow(id.String(), "Test Item", "Test Desc", categoryID.String(), sizeUnitID.String(), false, "http://test.com/img.jpg", time.Now(), time.Now()))

	// Mock the preload query for Category
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "categories" WHERE "categories"."id" = $1`)).
		WithArgs(categoryID.String()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "parent_id", "created_at", "updated_at"}).
			AddRow(categoryID.String(), "Test Category", nil, time.Now(), time.Now()))

	// Mock the preload query for SizeUnit
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "size_units" WHERE "size_units"."id" = $1`)).
		WithArgs(sizeUnitID.String()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "created_at", "updated_at"}).
			AddRow(sizeUnitID.String(), "kg", time.Now(), time.Now()))

	router := gin.New()
	router.GET("/item-definitions", handler.GetItemDefinitions)

	req, _ := http.NewRequest(http.MethodGet, "/item-definitions", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response []models.ItemDefinition
	err = json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Len(t, response, 1)
	assert.Equal(t, "Test Item", response[0].Name)
	assert.Equal(t, "Test Category", response[0].Category.Name)
	assert.Equal(t, "kg", response[0].SizeUnit.Name)

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGetItemDefinitions_Error(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, err := setupTestDB()
	assert.NoError(t, err)

	sqlDB, err := db.DB()
	assert.NoError(t, err)
	defer sqlDB.Close()

	handler := &ItemDefinitionHandler{DB: db}

	// Mock error on the main query
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "item_definitions"`)).
		WillReturnError(gorm.ErrInvalidDB)

	router := gin.New()
	router.GET("/item-definitions", handler.GetItemDefinitions)

	req, _ := http.NewRequest(http.MethodGet, "/item-definitions", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	var response map[string]string
	err = json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "Failed to fetch item definitions", response["error"])

	assert.NoError(t, mock.ExpectationsWereMet())
}
