package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/logger"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
)

func authMiddleware(userID uuid.UUID) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("userID", userID)
		c.Next()
	}
}

func setupTestDB() (*gorm.DB, sqlmock.Sqlmock, error) {
	logger.InitLogger()
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

func expectItemDefinitionAccess(mock sqlmock.Sqlmock, userID, homeID uuid.UUID, role string) {
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2 ORDER BY "user_homes"."user_id" LIMIT $3`)).
		WithArgs(userID, homeID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role", "is_default", "created_at", "updated_at"}).
			AddRow(userID.String(), homeID.String(), role, false, time.Now(), time.Now()))
}

func expectItemDefinitionByID(mock sqlmock.Sqlmock, id, homeID uuid.UUID) {
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "item_definitions" WHERE "item_definitions"."id" = $1 ORDER BY "item_definitions"."id" LIMIT $2`)).
		WithArgs(id, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "name", "description", "category_id", "size_unit_id", "is_expirable", "low_stock_threshold", "target_quantity", "priority", "image_url", "barcode", "created_at", "updated_at"}).
			AddRow(id.String(), homeID.String(), "Test Item", "Test Desc", nil, nil, false, nil, nil, "medium", "http://test.com/img.jpg", nil, time.Now(), time.Now()))
}

func expectProfileLookup(mock sqlmock.Sqlmock, userID uuid.UUID) {
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT "id","language_id" FROM "profiles" WHERE id = $1 ORDER BY "profiles"."id" LIMIT $2`)).
		WithArgs(userID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "language_id"}).AddRow(userID, nil))
}

func TestGetItemDefinitions_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, err := setupTestDB()
	assert.NoError(t, err)

	sqlDB, err := db.DB()
	assert.NoError(t, err)
	defer sqlDB.Close()

	handler := &ItemDefinitionHandler{DB: db}

	userID := uuid.New()
	homeID := uuid.New()
	id := uuid.New()
	categoryID := uuid.New()
	sizeUnitID := uuid.New()

	expectItemDefinitionAccess(mock, userID, homeID, models.RoleViewer)

	// Mock the main query
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "item_definitions" WHERE home_id = $1`)).
		WithArgs(homeID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "name", "description", "category_id", "size_unit_id", "is_expirable", "low_stock_threshold", "target_quantity", "priority", "image_url", "barcode", "created_at", "updated_at"}).
			AddRow(id.String(), homeID.String(), "Test Item", "Test Desc", categoryID.String(), sizeUnitID.String(), false, nil, nil, "medium", "http://test.com/img.jpg", nil, time.Now(), time.Now()))

	// Mock the preload query for Category
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "categories" WHERE "categories"."id" = $1`)).
		WithArgs(categoryID.String()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "name", "parent_id", "created_at", "updated_at"}).
			AddRow(categoryID.String(), homeID.String(), "Test Category", nil, time.Now(), time.Now()))

	// Mock the preload query for SizeUnit
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "size_units" WHERE "size_units"."id" = $1`)).
		WithArgs(sizeUnitID.String()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "created_at", "updated_at"}).
			AddRow(sizeUnitID.String(), "kg", time.Now(), time.Now()))

	router := gin.New()
	router.Use(authMiddleware(userID))
	router.GET("/item-definitions", handler.GetItemDefinitions)

	req, _ := http.NewRequest(http.MethodGet, "/item-definitions", nil)
	req.Header.Set("X-Home-Id", homeID.String())
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

func TestGetItemDefinitions_BarcodeFilter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, err := setupTestDB()
	assert.NoError(t, err)

	sqlDB, err := db.DB()
	assert.NoError(t, err)
	defer sqlDB.Close()

	handler := &ItemDefinitionHandler{DB: db}

	userID := uuid.New()
	homeID := uuid.New()
	barcode := "123456"

	expectItemDefinitionAccess(mock, userID, homeID, models.RoleViewer)

	// Mock the main query with barcode filter
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "item_definitions" WHERE home_id = $1 AND barcode = $2`)).
		WithArgs(homeID, barcode).
		WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "barcode"}).
			AddRow(uuid.New().String(), homeID.String(), barcode))

	router := gin.New()
	router.Use(authMiddleware(userID))
	router.GET("/item-definitions", handler.GetItemDefinitions)

	req, _ := http.NewRequest(http.MethodGet, "/item-definitions?barcode="+barcode, nil)
	req.Header.Set("X-Home-Id", homeID.String())
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
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

	userID := uuid.New()
	homeID := uuid.New()

	expectItemDefinitionAccess(mock, userID, homeID, models.RoleOwner)

	// Mock error on the main query
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "item_definitions" WHERE home_id = $1`)).
		WithArgs(homeID).
		WillReturnError(gorm.ErrInvalidDB)

	expectProfileLookup(mock, userID)

	router := gin.New()
	router.Use(authMiddleware(userID))
	router.GET("/item-definitions", handler.GetItemDefinitions)

	req, _ := http.NewRequest(http.MethodGet, "/item-definitions", nil)
	req.Header.Set("X-Home-Id", homeID.String())
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	var response map[string]string
	err = json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "Failed to fetch item definitions", response["error"])

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGetItemDefinitions_InvalidHomeID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, err := setupTestDB()
	assert.NoError(t, err)

	sqlDB, err := db.DB()
	assert.NoError(t, err)
	defer sqlDB.Close()

	handler := &ItemDefinitionHandler{DB: db}
	userID := uuid.New()

	router := gin.New()
	router.Use(authMiddleware(userID))
	router.GET("/item-definitions", handler.GetItemDefinitions)

	req, _ := http.NewRequest(http.MethodGet, "/item-definitions", nil)
	req.Header.Set("X-Home-Id", "invalid-uuid")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGetItemDefinitions_Forbidden(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, err := setupTestDB()
	assert.NoError(t, err)

	sqlDB, err := db.DB()
	assert.NoError(t, err)
	defer sqlDB.Close()

	handler := &ItemDefinitionHandler{DB: db}
	userID := uuid.New()
	homeID := uuid.New()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2 ORDER BY "user_homes"."user_id" LIMIT $3`)).
		WithArgs(userID, homeID, 1).
		WillReturnError(gorm.ErrRecordNotFound)

	expectProfileLookup(mock, userID)

	router := gin.New()
	router.Use(authMiddleware(userID))
	router.GET("/item-definitions", handler.GetItemDefinitions)

	req, _ := http.NewRequest(http.MethodGet, "/item-definitions", nil)
	req.Header.Set("X-Home-Id", homeID.String())
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestCreateItemDefinition_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, err := setupTestDB()
	assert.NoError(t, err)

	sqlDB, err := db.DB()
	assert.NoError(t, err)
	defer sqlDB.Close()

	handler := &ItemDefinitionHandler{DB: db}

	userID := uuid.New()
	homeID := uuid.New()

	categoryID := uuid.New()
	sizeUnitID := uuid.New()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2 ORDER BY "user_homes"."user_id" LIMIT $3`)).
		WithArgs(userID, homeID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role", "is_default", "created_at", "updated_at"}).AddRow(userID.String(), homeID.String(), models.RoleOwner, false, time.Now(), time.Now()))
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO "item_definitions"`)).
		WithArgs(homeID, "Test Item", "Test Desc", categoryID.String(), sizeUnitID.String(), false, nil, nil, "medium", "http://test.com/img.jpg", nil, sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New().String()))

	// UpdateShoppingListForDefinition calls
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "item_definitions" WHERE "item_definitions"."id" = $1 ORDER BY "item_definitions"."id" LIMIT $2`)).
		WithArgs(sqlmock.AnyArg(), 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "name", "low_stock_threshold"}).AddRow(uuid.New(), homeID, "Test Item", nil))

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "shopping_list_items" WHERE home_id = $1 AND item_definition_id = $2 AND is_auto_generated = $3 AND is_bought = $4 ORDER BY "shopping_list_items"."id" LIMIT $5`)).
		WithArgs(homeID, sqlmock.AnyArg(), true, false, 1).
		WillReturnError(gorm.ErrRecordNotFound)

	mock.ExpectCommit()

	router := gin.New()
	router.Use(authMiddleware(userID))
	router.POST("/item-definitions", handler.CreateItemDefinition)

	reqBody := `{"name":"Test Item","description":"Test Desc","category_id":"` + categoryID.String() + `","size_unit_id":"` + sizeUnitID.String() + `","is_expirable":false,"image_url":"http://test.com/img.jpg","priority":"medium"}`
	req, _ := http.NewRequest(http.MethodPost, "/item-definitions", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Home-Id", homeID.String())
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestUpdateItemDefinition_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, err := setupTestDB()
	assert.NoError(t, err)

	sqlDB, err := db.DB()
	assert.NoError(t, err)
	defer sqlDB.Close()

	handler := &ItemDefinitionHandler{DB: db}
	userID := uuid.New()
	homeID := uuid.New()

	id := uuid.New()
	categoryID := uuid.New()
	sizeUnitID := uuid.New()

	expectItemDefinitionByID(mock, id, homeID)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2 ORDER BY "user_homes"."user_id" LIMIT $3`)).
		WithArgs(userID, homeID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role", "is_default", "created_at", "updated_at"}).AddRow(userID.String(), homeID.String(), models.RoleEditor, false, time.Now(), time.Now()))
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "item_definitions"`)).
		WithArgs(nil, categoryID.String(), "Test Desc", "http://test.com/img.jpg", false, nil, "Updated Item", "medium", sizeUnitID.String(), nil, sqlmock.AnyArg(), id.String()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	// UpdateShoppingListForDefinition calls
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "item_definitions" WHERE "item_definitions"."id" = $1 ORDER BY "item_definitions"."id" LIMIT $2`)).
		WithArgs(id, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "name", "low_stock_threshold"}).AddRow(id, homeID, "Updated Item", nil))

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "shopping_list_items" WHERE home_id = $1 AND item_definition_id = $2 AND is_auto_generated = $3 AND is_bought = $4 ORDER BY "shopping_list_items"."id" LIMIT $5`)).
		WithArgs(homeID, id, true, false, 1).
		WillReturnError(gorm.ErrRecordNotFound)

	mock.ExpectCommit()

	router := gin.New()
	router.Use(authMiddleware(userID))
	router.PUT("/item-definitions/:id", handler.UpdateItemDefinition)

	reqBody := `{"name":"Updated Item","description":"Test Desc","category_id":"` + categoryID.String() + `","size_unit_id":"` + sizeUnitID.String() + `","is_expirable":false,"image_url":"http://test.com/img.jpg","priority":"medium"}`
	req, _ := http.NewRequest(http.MethodPut, "/item-definitions/"+id.String(), strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Home-Id", homeID.String())
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestDeleteItemDefinition_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, err := setupTestDB()
	assert.NoError(t, err)

	sqlDB, err := db.DB()
	assert.NoError(t, err)
	defer sqlDB.Close()

	handler := &ItemDefinitionHandler{DB: db}

	userID := uuid.New()
	homeID := uuid.New()

	id := uuid.New()

	expectItemDefinitionByID(mock, id, homeID)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2 ORDER BY "user_homes"."user_id" LIMIT $3`)).
		WithArgs(userID, homeID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role", "is_default", "created_at", "updated_at"}).AddRow(userID.String(), homeID.String(), models.RoleOwner, false, time.Now(), time.Now()))
	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM "item_definitions" WHERE "item_definitions"."id" = \$1`).
		WithArgs(id.String()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	router := gin.New()
	router.Use(authMiddleware(userID))
	router.DELETE("/item-definitions/:id", handler.DeleteItemDefinition)

	req, _ := http.NewRequest(http.MethodDelete, "/item-definitions/"+id.String(), nil)
	req.Header.Set("X-Home-Id", homeID.String())
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestCreateItemDefinition_InvalidJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, err := setupTestDB()
	assert.NoError(t, err)

	sqlDB, err := db.DB()
	assert.NoError(t, err)
	defer sqlDB.Close()

	handler := &ItemDefinitionHandler{DB: db}
	userID := uuid.New()
	homeID := uuid.New()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2 ORDER BY "user_homes"."user_id" LIMIT $3`)).
		WithArgs(userID, homeID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role", "is_default", "created_at", "updated_at"}).AddRow(userID.String(), homeID.String(), models.RoleOwner, false, time.Now(), time.Now()))

	router := gin.New()
	router.Use(authMiddleware(userID))
	router.POST("/item-definitions", handler.CreateItemDefinition)

	req, _ := http.NewRequest(http.MethodPost, "/item-definitions", strings.NewReader("invalid-json"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Home-Id", homeID.String())
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateItemDefinition_DBError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, err := setupTestDB()
	assert.NoError(t, err)

	sqlDB, err := db.DB()
	assert.NoError(t, err)
	defer sqlDB.Close()

	handler := &ItemDefinitionHandler{DB: db}
	userID := uuid.New()
	homeID := uuid.New()

	categoryID := uuid.New()
	sizeUnitID := uuid.New()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2 ORDER BY "user_homes"."user_id" LIMIT $3`)).
		WithArgs(userID, homeID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role", "is_default", "created_at", "updated_at"}).AddRow(userID.String(), homeID.String(), models.RoleOwner, false, time.Now(), time.Now()))
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO "item_definitions"`)).
		WithArgs(homeID, "Test Item", "Test Desc", categoryID.String(), sizeUnitID.String(), false, nil, nil, "medium", "http://test.com/img.jpg", nil, sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnError(gorm.ErrInvalidDB)
	mock.ExpectRollback()

	expectProfileLookup(mock, userID)

	router := gin.New()
	router.Use(authMiddleware(userID))
	router.POST("/item-definitions", handler.CreateItemDefinition)

	reqBody := `{"name":"Test Item","description":"Test Desc","category_id":"` + categoryID.String() + `","size_unit_id":"` + sizeUnitID.String() + `","is_expirable":false,"image_url":"http://test.com/img.jpg","priority":"medium"}`
	req, _ := http.NewRequest(http.MethodPost, "/item-definitions", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Home-Id", homeID.String())
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestUpdateItemDefinition_InvalidID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, _, err := setupTestDB()
	assert.NoError(t, err)

	sqlDB, err := db.DB()
	assert.NoError(t, err)
	defer sqlDB.Close()

	handler := &ItemDefinitionHandler{DB: db}
	userID := uuid.New()
	homeID := uuid.New()

	router := gin.New()
	router.Use(authMiddleware(userID))
	router.PUT("/item-definitions/:id", handler.UpdateItemDefinition)

	req, _ := http.NewRequest(http.MethodPut, "/item-definitions/invalid-uuid", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Home-Id", homeID.String())
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestUpdateItemDefinition_InvalidJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, err := setupTestDB()
	assert.NoError(t, err)

	sqlDB, err := db.DB()
	assert.NoError(t, err)
	defer sqlDB.Close()

	handler := &ItemDefinitionHandler{DB: db}
	userID := uuid.New()
	id := uuid.New()
	homeID := uuid.New()

	expectItemDefinitionByID(mock, id, homeID)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2 ORDER BY "user_homes"."user_id" LIMIT $3`)).
		WithArgs(userID, homeID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role", "is_default", "created_at", "updated_at"}).AddRow(userID.String(), homeID.String(), models.RoleEditor, false, time.Now(), time.Now()))
	router := gin.New()
	router.Use(authMiddleware(userID))
	router.PUT("/item-definitions/:id", handler.UpdateItemDefinition)

	req, _ := http.NewRequest(http.MethodPut, "/item-definitions/"+id.String(), strings.NewReader("invalid-json"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Home-Id", homeID.String())
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestUpdateItemDefinition_DBError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, err := setupTestDB()
	assert.NoError(t, err)

	sqlDB, err := db.DB()
	assert.NoError(t, err)
	defer sqlDB.Close()

	handler := &ItemDefinitionHandler{DB: db}
	userID := uuid.New()
	homeID := uuid.New()

	id := uuid.New()
	categoryID := uuid.New()
	sizeUnitID := uuid.New()

	expectItemDefinitionByID(mock, id, homeID)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2 ORDER BY "user_homes"."user_id" LIMIT $3`)).
		WithArgs(userID, homeID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role", "is_default", "created_at", "updated_at"}).AddRow(userID.String(), homeID.String(), models.RoleOwner, false, time.Now(), time.Now()))
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "item_definitions"`)).
		WithArgs(nil, categoryID.String(), "Test Desc", "http://test.com/img.jpg", false, nil, "Updated Item", "medium", sizeUnitID.String(), nil, sqlmock.AnyArg(), id.String()).
		WillReturnError(gorm.ErrInvalidDB)
	mock.ExpectRollback()

	expectProfileLookup(mock, userID)

	router := gin.New()
	router.Use(authMiddleware(userID))
	router.PUT("/item-definitions/:id", handler.UpdateItemDefinition)

	reqBody := `{"name":"Updated Item","description":"Test Desc","category_id":"` + categoryID.String() + `","size_unit_id":"` + sizeUnitID.String() + `","is_expirable":false,"image_url":"http://test.com/img.jpg","priority":"medium"}`
	req, _ := http.NewRequest(http.MethodPut, "/item-definitions/"+id.String(), strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Home-Id", homeID.String())
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestDeleteItemDefinition_InvalidID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, _, err := setupTestDB()
	assert.NoError(t, err)

	sqlDB, err := db.DB()
	assert.NoError(t, err)
	defer sqlDB.Close()

	handler := &ItemDefinitionHandler{DB: db}
	userID := uuid.New()
	homeID := uuid.New()

	router := gin.New()
	router.Use(authMiddleware(userID))
	router.DELETE("/item-definitions/:id", handler.DeleteItemDefinition)

	req, _ := http.NewRequest(http.MethodDelete, "/item-definitions/invalid-uuid", nil)
	req.Header.Set("X-Home-Id", homeID.String())
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestDeleteItemDefinition_DBError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, err := setupTestDB()
	assert.NoError(t, err)

	sqlDB, err := db.DB()
	assert.NoError(t, err)
	defer sqlDB.Close()

	handler := &ItemDefinitionHandler{DB: db}
	userID := uuid.New()
	homeID := uuid.New()

	id := uuid.New()

	expectItemDefinitionByID(mock, id, homeID)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2 ORDER BY "user_homes"."user_id" LIMIT $3`)).
		WithArgs(userID, homeID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role", "is_default", "created_at", "updated_at"}).AddRow(userID.String(), homeID.String(), models.RoleOwner, false, time.Now(), time.Now()))
	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM "item_definitions" WHERE "item_definitions"."id" = \$1`).
		WithArgs(id.String()).
		WillReturnError(gorm.ErrInvalidDB)
	mock.ExpectRollback()

	expectProfileLookup(mock, userID)

	router := gin.New()
	router.Use(authMiddleware(userID))
	router.DELETE("/item-definitions/:id", handler.DeleteItemDefinition)

	req, _ := http.NewRequest(http.MethodDelete, "/item-definitions/"+id.String(), nil)
	req.Header.Set("X-Home-Id", homeID.String())
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestCreateItemDefinition_Forbidden(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, err := setupTestDB()
	assert.NoError(t, err)

	sqlDB, err := db.DB()
	assert.NoError(t, err)
	defer sqlDB.Close()

	handler := &ItemDefinitionHandler{DB: db}
	userID := uuid.New()
	homeID := uuid.New()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2 ORDER BY "user_homes"."user_id" LIMIT $3`)).
		WithArgs(userID, homeID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role", "is_default", "created_at", "updated_at"}).AddRow(userID.String(), homeID.String(), models.RoleViewer, false, time.Now(), time.Now()))

	router := gin.New()
	router.Use(authMiddleware(userID))
	router.POST("/item-definitions", handler.CreateItemDefinition)

	req, _ := http.NewRequest(http.MethodPost, "/item-definitions", nil)
	req.Header.Set("X-Home-Id", homeID.String())
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestUpdateItemDefinition_Forbidden(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, err := setupTestDB()
	assert.NoError(t, err)

	sqlDB, err := db.DB()
	assert.NoError(t, err)
	defer sqlDB.Close()

	handler := &ItemDefinitionHandler{DB: db}
	userID := uuid.New()
	homeID := uuid.New()
	id := uuid.New()

	expectItemDefinitionByID(mock, id, homeID)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2 ORDER BY "user_homes"."user_id" LIMIT $3`)).
		WithArgs(userID, homeID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role", "is_default", "created_at", "updated_at"}).AddRow(userID.String(), homeID.String(), models.RoleViewer, false, time.Now(), time.Now()))

	router := gin.New()
	router.Use(authMiddleware(userID))
	router.PUT("/item-definitions/:id", handler.UpdateItemDefinition)

	req, _ := http.NewRequest(http.MethodPut, "/item-definitions/"+id.String(), nil)
	req.Header.Set("X-Home-Id", homeID.String())
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestDeleteItemDefinition_Forbidden(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, err := setupTestDB()
	assert.NoError(t, err)

	sqlDB, err := db.DB()
	assert.NoError(t, err)
	defer sqlDB.Close()

	handler := &ItemDefinitionHandler{DB: db}
	userID := uuid.New()
	homeID := uuid.New()
	id := uuid.New()

	expectItemDefinitionByID(mock, id, homeID)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2 ORDER BY "user_homes"."user_id" LIMIT $3`)).
		WithArgs(userID, homeID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role", "is_default", "created_at", "updated_at"}).AddRow(userID.String(), homeID.String(), models.RoleViewer, false, time.Now(), time.Now()))

	router := gin.New()
	router.Use(authMiddleware(userID))
	router.DELETE("/item-definitions/:id", handler.DeleteItemDefinition)

	req, _ := http.NewRequest(http.MethodDelete, "/item-definitions/"+id.String(), nil)
	req.Header.Set("X-Home-Id", homeID.String())
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
}
