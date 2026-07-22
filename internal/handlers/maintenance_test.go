package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/i18n"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/logger"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func setupMaintenanceTest(t *testing.T) (*MaintenanceTaskHandler, sqlmock.Sqlmock) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: db}), &gorm.Config{})
	require.NoError(t, err)
	handler := &MaintenanceTaskHandler{DB: gormDB}
	return handler, mock
}

// ⚡ Bolt: Robust expectation for i18n lookup
func expectMaintenanceI18nQuery(mock sqlmock.Sqlmock, userID uuid.UUID) {
	mock.ExpectQuery(`(?i)SELECT .* FROM "profiles" WHERE .*id.* = \$1.*`).
		WithArgs(userID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "language_id"}).AddRow(userID, nil))
}

func TestGetMaintenanceTasks(t *testing.T) {
	logger.InitLogger()
	gin.SetMode(gin.TestMode)
	userID := uuid.New()
	homeID := uuid.New()

	t.Run("success", func(t *testing.T) {
		handler, mock := setupMaintenanceTest(t)
		i18n.InvalidateUserLanguageCache(userID)
		req, _ := http.NewRequest(http.MethodGet, "/maintenance-tasks", nil)
		req.Header.Set("X-Home-Id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		// utils.VerifyHomeAccess
		mock.ExpectQuery(`(?i)SELECT \* FROM "user_homes" WHERE .*user_id.* = \$1 AND .*home_id.* = \$2.*`).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id"}).AddRow(userID, homeID))

		taskID := uuid.New()
		mock.ExpectQuery(`(?i)SELECT .* FROM "maintenance_tasks" WHERE .*home_id.* = \$1.*ORDER BY scheduled_date ASC`).
			WithArgs(homeID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "description", "scheduled_date"}).
				AddRow(taskID, homeID, "Change HVAC Filter", time.Now()))

		// Preload Dependencies
		mock.ExpectQuery(`(?i)SELECT \* FROM "task_item_dependencies" WHERE .*maintenance_task_id.* = \$1.*`).
			WithArgs(taskID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "maintenance_task_id"}))

		handler.GetMaintenanceTasks(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("success with inventory_item_id", func(t *testing.T) {
		handler, mock := setupMaintenanceTest(t)
		i18n.InvalidateUserLanguageCache(userID)
		itemID := uuid.New()
		req, _ := http.NewRequest(http.MethodGet, "/maintenance-tasks?inventory_item_id="+itemID.String(), nil)
		req.Header.Set("X-Home-Id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(`(?i)SELECT \* FROM "user_homes".*`).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id"}).AddRow(userID, homeID))

		// Verify inventory item belongs to home
		mock.ExpectQuery(`(?i)SELECT \* FROM "inventory_items" WHERE .*id.* = \$1 AND .*home_id.* = \$2.*`).
			WithArgs(itemID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id"}).AddRow(itemID, homeID))

		taskID := uuid.New()
		mock.ExpectQuery(`(?i)SELECT .* FROM "maintenance_tasks" WHERE .*home_id.* = \$1 AND .*inventory_item_id.* = \$2.*ORDER BY scheduled_date ASC`).
			WithArgs(homeID, itemID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "inventory_item_id"}).
				AddRow(taskID, homeID, itemID))

		// Preload Dependencies
		mock.ExpectQuery(`(?i)SELECT \* FROM "task_item_dependencies" WHERE .*maintenance_task_id.* = \$1.*`).
			WithArgs(taskID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "maintenance_task_id"}))

		// Preload InventoryItem
		itemDefID := uuid.New()
		mock.ExpectQuery(`(?i)SELECT \* FROM "inventory_items" WHERE .*id.* = \$1`).
			WithArgs(itemID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "item_definition_id"}).AddRow(itemID, itemDefID))

		// Preload ItemDefinition
		mock.ExpectQuery(`(?i)SELECT \* FROM "item_definitions" WHERE .*id.* = \$1`).
			WithArgs(itemDefID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(itemDefID, "Filter"))

		handler.GetMaintenanceTasks(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("inventory item not found", func(t *testing.T) {
		handler, mock := setupMaintenanceTest(t)
		i18n.InvalidateUserLanguageCache(userID)
		itemID := uuid.New()
		req, _ := http.NewRequest(http.MethodGet, "/maintenance-tasks?inventory_item_id="+itemID.String(), nil)
		req.Header.Set("X-Home-Id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(`(?i)SELECT \* FROM "user_homes".*`).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id"}).AddRow(userID, homeID))

		mock.ExpectQuery(`(?i)SELECT \* FROM "inventory_items" WHERE .*id.* = \$1 AND .*home_id.* = \$2.*`).
			WillReturnError(gorm.ErrRecordNotFound)
		expectMaintenanceI18nQuery(mock, userID)

		handler.GetMaintenanceTasks(c)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("missing home_id", func(t *testing.T) {
		handler, mock := setupMaintenanceTest(t)
		i18n.InvalidateUserLanguageCache(userID)
		req, _ := http.NewRequest(http.MethodGet, "/maintenance-tasks", nil)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		expectMaintenanceI18nQuery(mock, userID)

		handler.GetMaintenanceTasks(c)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("access denied", func(t *testing.T) {
		handler, mock := setupMaintenanceTest(t)
		i18n.InvalidateUserLanguageCache(userID)
		req, _ := http.NewRequest(http.MethodGet, "/maintenance-tasks", nil)
		req.Header.Set("X-Home-Id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(`(?i)SELECT \* FROM "user_homes".*`).
			WillReturnError(gorm.ErrRecordNotFound)
		expectMaintenanceI18nQuery(mock, userID)

		handler.GetMaintenanceTasks(c)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("invalid inventory_item_id", func(t *testing.T) {
		handler, mock := setupMaintenanceTest(t)
		i18n.InvalidateUserLanguageCache(userID)
		req, _ := http.NewRequest(http.MethodGet, "/maintenance-tasks?inventory_item_id=not-a-uuid", nil)
		req.Header.Set("X-Home-Id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(`(?i)SELECT \* FROM "user_homes".*`).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id"}).AddRow(userID, homeID))
		expectMaintenanceI18nQuery(mock, userID)

		handler.GetMaintenanceTasks(c)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("db error", func(t *testing.T) {
		handler, mock := setupMaintenanceTest(t)
		i18n.InvalidateUserLanguageCache(userID)
		req, _ := http.NewRequest(http.MethodGet, "/maintenance-tasks", nil)
		req.Header.Set("X-Home-Id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(`(?i)SELECT \* FROM "user_homes".*`).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id"}).AddRow(userID, homeID))
		mock.ExpectQuery(`(?i)SELECT .* FROM "maintenance_tasks" WHERE .*home_id.* = \$1.*`).
			WillReturnError(errors.New("db error"))
		expectMaintenanceI18nQuery(mock, userID)

		handler.GetMaintenanceTasks(c)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

func TestGetMaintenanceTask(t *testing.T) {
	logger.InitLogger()
	gin.SetMode(gin.TestMode)
	userID := uuid.New()
	homeID := uuid.New()
	taskID := uuid.New()

	t.Run("success", func(t *testing.T) {
		handler, mock := setupMaintenanceTest(t)
		i18n.InvalidateUserLanguageCache(userID)
		req, _ := http.NewRequest(http.MethodGet, "/maintenance-tasks/"+taskID.String(), nil)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: taskID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(`(?i)SELECT .* FROM "maintenance_tasks" WHERE .*id.* = \$1.*`).
			WithArgs(taskID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "description"}).AddRow(taskID, homeID, "Test Task"))

		// Preload Dependencies
		mock.ExpectQuery(`(?i)SELECT \* FROM "task_item_dependencies" WHERE .*maintenance_task_id.* = \$1.*`).
			WithArgs(taskID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "maintenance_task_id"}))

		mock.ExpectQuery(`(?i)SELECT \* FROM "user_homes" WHERE .*user_id.* = \$1 AND .*home_id.* = \$2.*`).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id"}).AddRow(userID, homeID))

		handler.GetMaintenanceTask(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		handler, mock := setupMaintenanceTest(t)
		i18n.InvalidateUserLanguageCache(userID)
		req, _ := http.NewRequest(http.MethodGet, "/maintenance-tasks/"+taskID.String(), nil)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: taskID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(`(?i)SELECT .* FROM "maintenance_tasks" WHERE .*id.* = \$1.*`).
			WillReturnError(gorm.ErrRecordNotFound)
		expectMaintenanceI18nQuery(mock, userID)

		handler.GetMaintenanceTask(c)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("access denied", func(t *testing.T) {
		handler, mock := setupMaintenanceTest(t)
		i18n.InvalidateUserLanguageCache(userID)
		req, _ := http.NewRequest(http.MethodGet, "/maintenance-tasks/"+taskID.String(), nil)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: taskID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(`(?i)SELECT .* FROM "maintenance_tasks" WHERE .*id.* = \$1.*`).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id"}).AddRow(taskID, homeID))

		// Preload Dependencies
		mock.ExpectQuery(`(?i)SELECT \* FROM "task_item_dependencies" WHERE .*maintenance_task_id.* = \$1.*`).
			WithArgs(taskID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "maintenance_task_id"}))

		mock.ExpectQuery(`(?i)SELECT \* FROM "user_homes" WHERE .*user_id.* = \$1 AND .*home_id.* = \$2.*`).
			WillReturnError(gorm.ErrRecordNotFound)
		expectMaintenanceI18nQuery(mock, userID)

		handler.GetMaintenanceTask(c)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("fetch error", func(t *testing.T) {
		handler, mock := setupMaintenanceTest(t)
		i18n.InvalidateUserLanguageCache(userID)
		req, _ := http.NewRequest(http.MethodGet, "/maintenance-tasks/"+taskID.String(), nil)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: taskID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(`(?i)SELECT .* FROM "maintenance_tasks" WHERE .*id.* = \$1.*`).
			WillReturnError(errors.New("other error"))
		expectMaintenanceI18nQuery(mock, userID)

		handler.GetMaintenanceTask(c)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

func TestCreateMaintenanceTask(t *testing.T) {
	logger.InitLogger()
	gin.SetMode(gin.TestMode)
	userID := uuid.New()
	homeID := uuid.New()
	itemID := uuid.New()

	t.Run("success", func(t *testing.T) {
		handler, mock := setupMaintenanceTest(t)
		i18n.InvalidateUserLanguageCache(userID)

		body := MaintenanceTaskRequest{
			InventoryItemID: &itemID,
			Description:     "Service Fridge",
			ScheduledDate:   time.Now(),
		}
		jsonBody, _ := json.Marshal(body)
		req, _ := http.NewRequest(http.MethodPost, "/maintenance-tasks", bytes.NewBuffer(jsonBody))
		req.Header.Set("X-Home-Id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(`(?i)SELECT \* FROM "user_homes".*`).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "owner"))

		mock.ExpectQuery(`(?i)SELECT \* FROM "inventory_items" WHERE .*id.* = \$1.*`).
			WithArgs(itemID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id"}).AddRow(itemID, homeID))

		mock.ExpectBegin()
		taskID := uuid.New()
		mock.ExpectQuery(`INSERT INTO "maintenance_tasks".*`).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(taskID))
		mock.ExpectCommit()

		mock.ExpectQuery(`(?i)SELECT \* FROM "maintenance_tasks" WHERE .*id.* = \$1.*`).
			WithArgs(taskID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id"}).AddRow(taskID, homeID))

		// Preload Dependencies
		mock.ExpectQuery(`(?i)SELECT \* FROM "task_item_dependencies" WHERE .*maintenance_task_id.* = \$1.*`).
			WithArgs(taskID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "maintenance_task_id"}))

		handler.CreateMaintenanceTask(c)

		assert.Equal(t, http.StatusCreated, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("success with dependencies", func(t *testing.T) {
		handler, mock := setupMaintenanceTest(t)
		i18n.InvalidateUserLanguageCache(userID)
		itemDefID := uuid.New()

		body := MaintenanceTaskRequest{
			Description:   "With Deps",
			ScheduledDate: time.Now(),
			Dependencies: []TaskItemDependencyRequest{
				{ItemDefinitionID: itemDefID, QuantityRequired: 2},
			},
		}
		jsonBody, _ := json.Marshal(body)
		req, _ := http.NewRequest(http.MethodPost, "/maintenance-tasks", bytes.NewBuffer(jsonBody))
		req.Header.Set("X-Home-Id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(`(?i)SELECT \* FROM "user_homes".*`).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "owner"))

		mock.ExpectBegin()
		taskID := uuid.New()
		mock.ExpectQuery(`INSERT INTO "maintenance_tasks".*`).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(taskID))

		mock.ExpectQuery(`INSERT INTO "task_item_dependencies".*`).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))
		mock.ExpectCommit()

		mock.ExpectQuery(`(?i)SELECT \* FROM "maintenance_tasks" WHERE .*id.* = \$1.*`).
			WithArgs(taskID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id"}).AddRow(taskID, homeID))

		// Preload Dependencies
		mock.ExpectQuery(`(?i)SELECT \* FROM "task_item_dependencies" WHERE .*maintenance_task_id.* = \$1.*`).
			WithArgs(taskID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "maintenance_task_id", "item_definition_id", "quantity_required"}).
				AddRow(uuid.New(), taskID, itemDefID, 2.0))

		// Preload ItemDefinition
		mock.ExpectQuery(`(?i)SELECT \* FROM "item_definitions" WHERE .*id.* = \$1.*`).
			WithArgs(itemDefID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(itemDefID, "Def"))

		handler.CreateMaintenanceTask(c)

		assert.Equal(t, http.StatusCreated, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("access denied", func(t *testing.T) {
		handler, mock := setupMaintenanceTest(t)
		i18n.InvalidateUserLanguageCache(userID)
		req, _ := http.NewRequest(http.MethodPost, "/maintenance-tasks", nil)
		req.Header.Set("X-Home-Id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(`(?i)SELECT \* FROM "user_homes".*`).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "viewer"))
		expectMaintenanceI18nQuery(mock, userID)

		handler.CreateMaintenanceTask(c)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("invalid payload", func(t *testing.T) {
		handler, mock := setupMaintenanceTest(t)
		i18n.InvalidateUserLanguageCache(userID)
		req, _ := http.NewRequest(http.MethodPost, "/maintenance-tasks", bytes.NewBufferString("invalid"))
		req.Header.Set("X-Home-Id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(`(?i)SELECT \* FROM "user_homes".*`).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "owner"))
		expectMaintenanceI18nQuery(mock, userID)

		handler.CreateMaintenanceTask(c)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("inventory item not found", func(t *testing.T) {
		handler, mock := setupMaintenanceTest(t)
		i18n.InvalidateUserLanguageCache(userID)
		body := MaintenanceTaskRequest{InventoryItemID: &itemID, Description: "desc", ScheduledDate: time.Now()}
		jsonBody, _ := json.Marshal(body)
		req, _ := http.NewRequest(http.MethodPost, "/maintenance-tasks", bytes.NewBuffer(jsonBody))
		req.Header.Set("X-Home-Id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(`(?i)SELECT \* FROM "user_homes".*`).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "owner"))
		mock.ExpectQuery(`(?i)SELECT \* FROM "inventory_items" WHERE .*id.* = \$1.*`).
			WillReturnError(gorm.ErrRecordNotFound)
		expectMaintenanceI18nQuery(mock, userID)

		handler.CreateMaintenanceTask(c)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("inventory item in different home", func(t *testing.T) {
		handler, mock := setupMaintenanceTest(t)
		i18n.InvalidateUserLanguageCache(userID)
		body := MaintenanceTaskRequest{InventoryItemID: &itemID, Description: "desc", ScheduledDate: time.Now()}
		jsonBody, _ := json.Marshal(body)
		req, _ := http.NewRequest(http.MethodPost, "/maintenance-tasks", bytes.NewBuffer(jsonBody))
		req.Header.Set("X-Home-Id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(`(?i)SELECT \* FROM "user_homes".*`).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "owner"))
		mock.ExpectQuery(`(?i)SELECT \* FROM "inventory_items" WHERE .*id.* = \$1.*`).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id"}).AddRow(itemID, uuid.New()))
		expectMaintenanceI18nQuery(mock, userID)

		handler.CreateMaintenanceTask(c)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("db error", func(t *testing.T) {
		handler, mock := setupMaintenanceTest(t)
		i18n.InvalidateUserLanguageCache(userID)
		body := MaintenanceTaskRequest{Description: "desc", ScheduledDate: time.Now()}
		jsonBody, _ := json.Marshal(body)
		req, _ := http.NewRequest(http.MethodPost, "/maintenance-tasks", bytes.NewBuffer(jsonBody))
		req.Header.Set("X-Home-Id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(`(?i)SELECT \* FROM "user_homes".*`).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "owner"))
		mock.ExpectBegin()
		mock.ExpectQuery(`INSERT INTO "maintenance_tasks".*`).
			WillReturnError(errors.New("db error"))
		mock.ExpectRollback()
		expectMaintenanceI18nQuery(mock, userID)

		handler.CreateMaintenanceTask(c)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("invalid repeat frequency format", func(t *testing.T) {
		handler, mock := setupMaintenanceTest(t)
		i18n.InvalidateUserLanguageCache(userID)
		body := MaintenanceTaskRequest{
			Description:   "Invalid Freq",
			ScheduledDate: time.Now(),
			Frequency:     "Every 0 Days", // Invalid
		}
		jsonBody, _ := json.Marshal(body)
		req, _ := http.NewRequest(http.MethodPost, "/maintenance-tasks", bytes.NewBuffer(jsonBody))
		req.Header.Set("X-Home-Id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(`(?i)SELECT \* FROM "user_homes".*`).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "owner"))
		expectMaintenanceI18nQuery(mock, userID)

		handler.CreateMaintenanceTask(c)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "Invalid repeat frequency format")
	})

	t.Run("custom frequency validation missing fields", func(t *testing.T) {
		handler, mock := setupMaintenanceTest(t)
		i18n.InvalidateUserLanguageCache(userID)
		body := MaintenanceTaskRequest{
			Description:   "Invalid Custom",
			ScheduledDate: time.Now(),
			Frequency:     "Custom",
			// custom_frequency and custom_frequency_metric omitted
		}
		jsonBody, _ := json.Marshal(body)
		req, _ := http.NewRequest(http.MethodPost, "/maintenance-tasks", bytes.NewBuffer(jsonBody))
		req.Header.Set("X-Home-Id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(`(?i)SELECT \* FROM "user_homes".*`).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "owner"))
		expectMaintenanceI18nQuery(mock, userID)

		handler.CreateMaintenanceTask(c)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "Custom frequency must be a positive number")
	})

	t.Run("custom frequency validation with non-custom having custom fields", func(t *testing.T) {
		handler, mock := setupMaintenanceTest(t)
		i18n.InvalidateUserLanguageCache(userID)
		floatVal := 12.0
		body := MaintenanceTaskRequest{
			Description:     "Invalid Custom",
			ScheduledDate:   time.Now(),
			Frequency:       "Monthly",
			CustomFrequency: &floatVal,
		}
		jsonBody, _ := json.Marshal(body)
		req, _ := http.NewRequest(http.MethodPost, "/maintenance-tasks", bytes.NewBuffer(jsonBody))
		req.Header.Set("X-Home-Id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(`(?i)SELECT \* FROM "user_homes".*`).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "owner"))
		expectMaintenanceI18nQuery(mock, userID)

		handler.CreateMaintenanceTask(c)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "Custom frequency and metric should not be provided for non-custom frequencies")
	})
}

func TestUpdateMaintenanceTask(t *testing.T) {
	logger.InitLogger()
	gin.SetMode(gin.TestMode)
	userID := uuid.New()
	homeID := uuid.New()
	taskID := uuid.New()
	itemID := uuid.New()

	t.Run("success", func(t *testing.T) {
		handler, mock := setupMaintenanceTest(t)
		i18n.InvalidateUserLanguageCache(userID)

		body := MaintenanceTaskRequest{
			Description:   "Updated",
			ScheduledDate: time.Now(),
		}
		jsonBody, _ := json.Marshal(body)
		req, _ := http.NewRequest(http.MethodPut, "/maintenance-tasks/"+taskID.String(), bytes.NewBuffer(jsonBody))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: taskID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(`(?i)SELECT .* FROM "maintenance_tasks" WHERE .*id.* = \$1.*`).
			WithArgs(taskID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "is_completed"}).AddRow(taskID, homeID, false))

		mock.ExpectQuery(`(?i)SELECT \* FROM "user_homes" WHERE .*user_id.* = \$1 AND .*home_id.* = \$2.*`).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "owner"))

		mock.ExpectBegin()
		mock.ExpectExec(`UPDATE "maintenance_tasks" SET.*`).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()

		expectMaintenanceI18nQuery(mock, userID)

		handler.UpdateMaintenanceTask(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("success ignore is_completed in update", func(t *testing.T) {
		handler, mock := setupMaintenanceTest(t)
		i18n.InvalidateUserLanguageCache(userID)

		body := MaintenanceTaskRequest{
			Description:   "Update",
			ScheduledDate: time.Now(),
			IsCompleted:   true, // Should be ignored
		}
		jsonBody, _ := json.Marshal(body)
		req, _ := http.NewRequest(http.MethodPut, "/maintenance-tasks/"+taskID.String(), bytes.NewBuffer(jsonBody))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: taskID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(`(?i)SELECT .* FROM "maintenance_tasks" WHERE .*id.* = \$1.*`).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "is_completed"}).AddRow(taskID, homeID, false))

		mock.ExpectQuery(`(?i)SELECT \* FROM "user_homes".*`).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "owner"))

		mock.ExpectBegin()
		// Ensure is_completed is NOT in the update query.
		// GORM with map updates only includes what's in the map.
		mock.ExpectExec(`(?i)UPDATE "maintenance_tasks" SET.*WHERE "id" = \$[0-9]+`).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()

		expectMaintenanceI18nQuery(mock, userID)

		handler.UpdateMaintenanceTask(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("success toggle completed", func(t *testing.T) {
		handler, mock := setupMaintenanceTest(t)
		i18n.InvalidateUserLanguageCache(userID)

		body := MaintenanceTaskRequest{
			Description:   "Updated",
			ScheduledDate: time.Now(),
			IsCompleted:   true,
		}
		jsonBody, _ := json.Marshal(body)
		req, _ := http.NewRequest(http.MethodPut, "/maintenance-tasks/"+taskID.String(), bytes.NewBuffer(jsonBody))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: taskID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(`(?i)SELECT .* FROM "maintenance_tasks" WHERE .*id.* = \$1.*`).
			WithArgs(taskID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "is_completed"}).AddRow(taskID, homeID, false))

		mock.ExpectQuery(`(?i)SELECT \* FROM "user_homes".*`).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "owner"))

		mock.ExpectBegin()
		mock.ExpectExec(`UPDATE "maintenance_tasks" SET.*`).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()

		expectMaintenanceI18nQuery(mock, userID)

		handler.UpdateMaintenanceTask(c)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("success with dependencies", func(t *testing.T) {
		handler, mock := setupMaintenanceTest(t)
		i18n.InvalidateUserLanguageCache(userID)
		itemDefID := uuid.New()

		body := MaintenanceTaskRequest{
			Description:   "Updated",
			ScheduledDate: time.Now(),
			Dependencies: []TaskItemDependencyRequest{
				{ItemDefinitionID: itemDefID, QuantityRequired: 3},
			},
		}
		jsonBody, _ := json.Marshal(body)
		req, _ := http.NewRequest(http.MethodPut, "/maintenance-tasks/"+taskID.String(), bytes.NewBuffer(jsonBody))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: taskID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(`(?i)SELECT .* FROM "maintenance_tasks" WHERE .*id.* = \$1.*`).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id"}).AddRow(taskID, homeID))

		mock.ExpectQuery(`(?i)SELECT \* FROM "user_homes".*`).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "owner"))

		mock.ExpectBegin()
		mock.ExpectExec(`UPDATE "maintenance_tasks" SET.*`).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec(`DELETE FROM "task_item_dependencies" WHERE maintenance_task_id = \$1`).
			WithArgs(taskID).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectQuery(`INSERT INTO "task_item_dependencies".*`).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))
		mock.ExpectCommit()

		expectMaintenanceI18nQuery(mock, userID)

		handler.UpdateMaintenanceTask(c)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("success partial update without dependencies", func(t *testing.T) {
		handler, mock := setupMaintenanceTest(t)
		i18n.InvalidateUserLanguageCache(userID)

		body := MaintenanceTaskRequest{
			Description:   "Partial Update",
			ScheduledDate: time.Now(),
			Dependencies:  nil, // dependencies omitted
		}
		jsonBody, _ := json.Marshal(body)
		req, _ := http.NewRequest(http.MethodPut, "/maintenance-tasks/"+taskID.String(), bytes.NewBuffer(jsonBody))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: taskID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(`(?i)SELECT .* FROM "maintenance_tasks" WHERE .*id.* = \$1.*`).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id"}).AddRow(taskID, homeID))

		mock.ExpectQuery(`(?i)SELECT \* FROM "user_homes".*`).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "owner"))

		mock.ExpectBegin()
		mock.ExpectExec(`UPDATE "maintenance_tasks" SET.*`).
			WillReturnResult(sqlmock.NewResult(0, 1))
		// DELETE should NOT be called
		mock.ExpectCommit()

		expectMaintenanceI18nQuery(mock, userID)

		handler.UpdateMaintenanceTask(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("success toggle uncompleted", func(t *testing.T) {
		handler, mock := setupMaintenanceTest(t)
		i18n.InvalidateUserLanguageCache(userID)

		body := MaintenanceTaskRequest{
			Description:   "Updated",
			ScheduledDate: time.Now(),
			IsCompleted:   false,
		}
		jsonBody, _ := json.Marshal(body)
		req, _ := http.NewRequest(http.MethodPut, "/maintenance-tasks/"+taskID.String(), bytes.NewBuffer(jsonBody))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: taskID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(`(?i)SELECT .* FROM "maintenance_tasks" WHERE .*id.* = \$1.*`).
			WithArgs(taskID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "is_completed"}).AddRow(taskID, homeID, true))

		mock.ExpectQuery(`(?i)SELECT \* FROM "user_homes".*`).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "owner"))

		mock.ExpectBegin()
		mock.ExpectExec(`UPDATE "maintenance_tasks" SET.*`).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()

		expectMaintenanceI18nQuery(mock, userID)

		handler.UpdateMaintenanceTask(c)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("success with inventory_item_id", func(t *testing.T) {
		handler, mock := setupMaintenanceTest(t)
		i18n.InvalidateUserLanguageCache(userID)

		body := MaintenanceTaskRequest{
			InventoryItemID: &itemID,
			Description:     "Updated",
			ScheduledDate:   time.Now(),
		}
		jsonBody, _ := json.Marshal(body)
		req, _ := http.NewRequest(http.MethodPut, "/maintenance-tasks/"+taskID.String(), bytes.NewBuffer(jsonBody))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: taskID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(`(?i)SELECT .* FROM "maintenance_tasks" WHERE .*id.* = \$1.*`).
			WithArgs(taskID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "is_completed"}).AddRow(taskID, homeID, false))

		mock.ExpectQuery(`(?i)SELECT \* FROM "user_homes".*`).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "owner"))

		mock.ExpectQuery(`(?i)SELECT \* FROM "inventory_items" WHERE .*id.* = \$1.*`).
			WithArgs(itemID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id"}).AddRow(itemID, homeID))

		mock.ExpectBegin()
		mock.ExpectExec(`UPDATE "maintenance_tasks" SET.*`).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()

		expectMaintenanceI18nQuery(mock, userID)

		handler.UpdateMaintenanceTask(c)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("not found", func(t *testing.T) {
		handler, mock := setupMaintenanceTest(t)
		i18n.InvalidateUserLanguageCache(userID)
		req, _ := http.NewRequest(http.MethodPut, "/maintenance-tasks/"+taskID.String(), nil)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: taskID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(`(?i)SELECT .* FROM "maintenance_tasks" WHERE .*id.* = \$1.*`).
			WillReturnError(gorm.ErrRecordNotFound)
		expectMaintenanceI18nQuery(mock, userID)

		handler.UpdateMaintenanceTask(c)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("access denied", func(t *testing.T) {
		handler, mock := setupMaintenanceTest(t)
		i18n.InvalidateUserLanguageCache(userID)
		req, _ := http.NewRequest(http.MethodPut, "/maintenance-tasks/"+taskID.String(), nil)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: taskID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(`(?i)SELECT .* FROM "maintenance_tasks" WHERE .*id.* = \$1.*`).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id"}).AddRow(taskID, homeID))
		mock.ExpectQuery(`(?i)SELECT \* FROM "user_homes" WHERE .*user_id.* = \$1 AND .*home_id.* = \$2.*`).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "viewer"))
		expectMaintenanceI18nQuery(mock, userID)

		handler.UpdateMaintenanceTask(c)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("invalid payload", func(t *testing.T) {
		handler, mock := setupMaintenanceTest(t)
		i18n.InvalidateUserLanguageCache(userID)
		req, _ := http.NewRequest(http.MethodPut, "/maintenance-tasks/"+taskID.String(), bytes.NewBufferString("invalid"))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: taskID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(`(?i)SELECT .* FROM "maintenance_tasks" WHERE .*id.* = \$1.*`).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id"}).AddRow(taskID, homeID))
		mock.ExpectQuery(`(?i)SELECT \* FROM "user_homes".*`).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "owner"))
		expectMaintenanceI18nQuery(mock, userID)

		handler.UpdateMaintenanceTask(c)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("inventory item not found", func(t *testing.T) {
		handler, mock := setupMaintenanceTest(t)
		i18n.InvalidateUserLanguageCache(userID)
		body := MaintenanceTaskRequest{InventoryItemID: &itemID, Description: "desc", ScheduledDate: time.Now()}
		jsonBody, _ := json.Marshal(body)
		req, _ := http.NewRequest(http.MethodPut, "/maintenance-tasks/"+taskID.String(), bytes.NewBuffer(jsonBody))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: taskID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(`(?i)SELECT .* FROM "maintenance_tasks" WHERE .*id.* = \$1.*`).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id"}).AddRow(taskID, homeID))
		mock.ExpectQuery(`(?i)SELECT \* FROM "user_homes".*`).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "owner"))
		mock.ExpectQuery(`(?i)SELECT \* FROM "inventory_items" WHERE .*id.* = \$1.*`).
			WillReturnError(gorm.ErrRecordNotFound)
		expectMaintenanceI18nQuery(mock, userID)

		handler.UpdateMaintenanceTask(c)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("inventory item in different home", func(t *testing.T) {
		handler, mock := setupMaintenanceTest(t)
		i18n.InvalidateUserLanguageCache(userID)
		body := MaintenanceTaskRequest{InventoryItemID: &itemID, Description: "desc", ScheduledDate: time.Now()}
		jsonBody, _ := json.Marshal(body)
		req, _ := http.NewRequest(http.MethodPut, "/maintenance-tasks/"+taskID.String(), bytes.NewBuffer(jsonBody))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: taskID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(`(?i)SELECT .* FROM "maintenance_tasks" WHERE .*id.* = \$1.*`).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id"}).AddRow(taskID, homeID))
		mock.ExpectQuery(`(?i)SELECT \* FROM "user_homes".*`).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "owner"))
		mock.ExpectQuery(`(?i)SELECT \* FROM "inventory_items" WHERE .*id.* = \$1.*`).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id"}).AddRow(itemID, uuid.New()))
		expectMaintenanceI18nQuery(mock, userID)

		handler.UpdateMaintenanceTask(c)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("db error on update", func(t *testing.T) {
		handler, mock := setupMaintenanceTest(t)
		i18n.InvalidateUserLanguageCache(userID)
		body := MaintenanceTaskRequest{Description: "desc", ScheduledDate: time.Now()}
		jsonBody, _ := json.Marshal(body)
		req, _ := http.NewRequest(http.MethodPut, "/maintenance-tasks/"+taskID.String(), bytes.NewBuffer(jsonBody))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: taskID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(`(?i)SELECT .* FROM "maintenance_tasks" WHERE .*id.* = \$1.*`).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id"}).AddRow(taskID, homeID))
		mock.ExpectQuery(`(?i)SELECT \* FROM "user_homes".*`).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "owner"))
		mock.ExpectBegin()
		mock.ExpectExec(`UPDATE "maintenance_tasks" SET.*`).
			WillReturnError(errors.New("db error"))
		mock.ExpectRollback()
		expectMaintenanceI18nQuery(mock, userID)

		handler.UpdateMaintenanceTask(c)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("invalid repeat frequency format", func(t *testing.T) {
		handler, mock := setupMaintenanceTest(t)
		i18n.InvalidateUserLanguageCache(userID)
		body := MaintenanceTaskRequest{
			Description:   "Invalid Freq",
			ScheduledDate: time.Now(),
			Frequency:     "Every 0 Days", // Invalid
		}
		jsonBody, _ := json.Marshal(body)
		req, _ := http.NewRequest(http.MethodPut, "/maintenance-tasks/"+taskID.String(), bytes.NewBuffer(jsonBody))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: taskID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(`(?i)SELECT .* FROM "maintenance_tasks" WHERE .*id.* = \$1.*`).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id"}).AddRow(taskID, homeID))
		mock.ExpectQuery(`(?i)SELECT \* FROM "user_homes".*`).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "owner"))
		expectMaintenanceI18nQuery(mock, userID)

		handler.UpdateMaintenanceTask(c)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "Invalid repeat frequency format")
	})
}

func TestCompleteMaintenanceTask(t *testing.T) {
	logger.InitLogger()
	gin.SetMode(gin.TestMode)
	userID := uuid.New()
	homeID := uuid.New()
	taskID := uuid.New()
	itemDefID := uuid.New()
	itemID := uuid.New()

	t.Run("success with dependencies", func(t *testing.T) {
		handler, mock := setupMaintenanceTest(t)
		i18n.InvalidateUserLanguageCache(userID)
		req, _ := http.NewRequest(http.MethodPost, "/maintenance-tasks/"+taskID.String()+"/complete", nil)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: taskID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(`(?i)SELECT .* FROM "maintenance_tasks" WHERE .*id.* = \$1.*`).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "is_completed"}).AddRow(taskID, homeID, false))

		mock.ExpectQuery(`(?i)SELECT \* FROM "task_item_dependencies" WHERE .*maintenance_task_id.* = \$1.*`).
			WithArgs(taskID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "maintenance_task_id", "item_definition_id", "quantity_required"}).
				AddRow(uuid.New(), taskID, itemDefID, 2.0))

		mock.ExpectQuery(`(?i)SELECT \* FROM "user_homes".*`).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "owner"))

		mock.ExpectBegin()
		mock.ExpectExec(`UPDATE "maintenance_tasks" SET.*`).
			WillReturnResult(sqlmock.NewResult(0, 1))

		// Deduction logic
		mock.ExpectQuery(`(?i)SELECT \* FROM "inventory_items" WHERE .*home_id.* = \$1 AND .*item_definition_id.* = \$2.*ORDER BY expiration_date ASC NULLS LAST`).
			WithArgs(homeID, itemDefID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "item_definition_id", "quantity"}).
				AddRow(itemID, homeID, itemDefID, 10.0))

		mock.ExpectExec(`UPDATE "inventory_items" SET .* WHERE "id" = \$3`).
			WithArgs(8.0, sqlmock.AnyArg(), itemID).
			WillReturnResult(sqlmock.NewResult(0, 1))

		mock.ExpectQuery(`INSERT INTO "inventory_transactions".*`).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))

		// Shopping list update
		mock.ExpectQuery(`(?i)SELECT \* FROM "item_definitions" WHERE .*id.* = \$1.*`).
			WithArgs(itemDefID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "name", "low_stock_threshold"}).AddRow(itemDefID, "Item", 5.0))
		mock.ExpectQuery(`(?i)SELECT \* FROM "shopping_list_items" WHERE .*home_id.* = \$1 AND .*item_definition_id.* = \$2.*`).
			WillReturnError(gorm.ErrRecordNotFound)
		mock.ExpectQuery(`(?i)SELECT COALESCE\(SUM\(quantity\), 0\) FROM "inventory_items" WHERE .*home_id.* = \$1 AND .*item_definition_id.* = \$2.*`).
			WillReturnRows(sqlmock.NewRows([]string{"sum"}).AddRow(8.0))

		mock.ExpectCommit()

		expectMaintenanceI18nQuery(mock, userID)

		handler.CompleteMaintenanceTask(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		handler, mock := setupMaintenanceTest(t)
		i18n.InvalidateUserLanguageCache(userID)
		req, _ := http.NewRequest(http.MethodPost, "/maintenance-tasks/"+taskID.String()+"/complete", nil)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: taskID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(`(?i)SELECT .* FROM "maintenance_tasks" WHERE .*id.* = \$1.*`).
			WillReturnError(gorm.ErrRecordNotFound)
		expectMaintenanceI18nQuery(mock, userID)

		handler.CompleteMaintenanceTask(c)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("already completed", func(t *testing.T) {
		handler, mock := setupMaintenanceTest(t)
		i18n.InvalidateUserLanguageCache(userID)
		req, _ := http.NewRequest(http.MethodPost, "/maintenance-tasks/"+taskID.String()+"/complete", nil)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: taskID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(`(?i)SELECT .* FROM "maintenance_tasks" WHERE .*id.* = \$1.*`).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "is_completed"}).AddRow(taskID, homeID, true))

		mock.ExpectQuery(`(?i)SELECT \* FROM "task_item_dependencies" WHERE .*maintenance_task_id.* = \$1.*`).
			WithArgs(taskID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "maintenance_task_id", "item_definition_id", "quantity_required"}).
				AddRow(uuid.New(), taskID, itemDefID, 2.0))

		expectMaintenanceI18nQuery(mock, userID)

		handler.CompleteMaintenanceTask(c)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("insufficient stock", func(t *testing.T) {
		handler, mock := setupMaintenanceTest(t)
		i18n.InvalidateUserLanguageCache(userID)
		req, _ := http.NewRequest(http.MethodPost, "/maintenance-tasks/"+taskID.String()+"/complete", nil)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: taskID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(`(?i)SELECT .* FROM "maintenance_tasks" WHERE .*id.* = \$1.*`).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "is_completed"}).AddRow(taskID, homeID, false))

		mock.ExpectQuery(`(?i)SELECT \* FROM "task_item_dependencies" WHERE .*maintenance_task_id.* = \$1.*`).
			WillReturnRows(sqlmock.NewRows([]string{"id", "maintenance_task_id", "item_definition_id", "quantity_required"}).
				AddRow(uuid.New(), taskID, itemDefID, 20.0))

		mock.ExpectQuery(`(?i)SELECT \* FROM "user_homes".*`).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "owner"))

		mock.ExpectBegin()
		mock.ExpectExec(`UPDATE "maintenance_tasks" SET.*`).
			WillReturnResult(sqlmock.NewResult(0, 1))

		mock.ExpectQuery(`(?i)SELECT \* FROM "inventory_items" WHERE .*home_id.* = \$1 AND .*item_definition_id.* = \$2.*`).
			WillReturnRows(sqlmock.NewRows([]string{"id", "quantity"}).AddRow(itemID, 10.0))

		mock.ExpectRollback()

		expectMaintenanceI18nQuery(mock, userID)

		handler.CompleteMaintenanceTask(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestDeleteMaintenanceTask(t *testing.T) {
	logger.InitLogger()
	gin.SetMode(gin.TestMode)
	userID := uuid.New()
	homeID := uuid.New()
	taskID := uuid.New()

	t.Run("success", func(t *testing.T) {
		handler, mock := setupMaintenanceTest(t)
		i18n.InvalidateUserLanguageCache(userID)
		req, _ := http.NewRequest(http.MethodDelete, "/maintenance-tasks/"+taskID.String(), nil)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: taskID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(`(?i)SELECT .* FROM "maintenance_tasks" WHERE .*id.* = \$1.*`).
			WithArgs(taskID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id"}).AddRow(taskID, homeID))

		mock.ExpectQuery(`(?i)SELECT \* FROM "user_homes" WHERE .*user_id.* = \$1 AND .*home_id.* = \$2.*`).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "owner"))

		mock.ExpectBegin()
		mock.ExpectExec(`DELETE FROM "maintenance_tasks" WHERE .*id.* = \$1.*`).
			WithArgs(taskID).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()

		expectMaintenanceI18nQuery(mock, userID)

		handler.DeleteMaintenanceTask(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		handler, mock := setupMaintenanceTest(t)
		i18n.InvalidateUserLanguageCache(userID)
		req, _ := http.NewRequest(http.MethodDelete, "/maintenance-tasks/"+taskID.String(), nil)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: taskID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(`(?i)SELECT .* FROM "maintenance_tasks" WHERE .*id.* = \$1.*`).
			WillReturnError(gorm.ErrRecordNotFound)
		expectMaintenanceI18nQuery(mock, userID)

		handler.DeleteMaintenanceTask(c)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("access denied", func(t *testing.T) {
		handler, mock := setupMaintenanceTest(t)
		i18n.InvalidateUserLanguageCache(userID)
		req, _ := http.NewRequest(http.MethodDelete, "/maintenance-tasks/"+taskID.String(), nil)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: taskID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(`(?i)SELECT .* FROM "maintenance_tasks" WHERE .*id.* = \$1.*`).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id"}).AddRow(taskID, homeID))
		mock.ExpectQuery(`(?i)SELECT \* FROM "user_homes" WHERE .*user_id.* = \$1 AND .*home_id.* = \$2.*`).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "viewer"))
		expectMaintenanceI18nQuery(mock, userID)

		handler.DeleteMaintenanceTask(c)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("fetch error", func(t *testing.T) {
		handler, mock := setupMaintenanceTest(t)
		i18n.InvalidateUserLanguageCache(userID)
		req, _ := http.NewRequest(http.MethodDelete, "/maintenance-tasks/"+taskID.String(), nil)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: taskID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(`(?i)SELECT .* FROM "maintenance_tasks" WHERE .*id.* = \$1.*`).
			WillReturnError(errors.New("other error"))
		expectMaintenanceI18nQuery(mock, userID)

		handler.DeleteMaintenanceTask(c)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("db error", func(t *testing.T) {
		handler, mock := setupMaintenanceTest(t)
		i18n.InvalidateUserLanguageCache(userID)
		req, _ := http.NewRequest(http.MethodDelete, "/maintenance-tasks/"+taskID.String(), nil)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: taskID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(`(?i)SELECT .* FROM "maintenance_tasks" WHERE .*id.* = \$1.*`).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id"}).AddRow(taskID, homeID))
		mock.ExpectQuery(`(?i)SELECT \* FROM "user_homes" WHERE .*user_id.* = \$1 AND .*home_id.* = \$2.*`).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "owner"))
		mock.ExpectBegin()
		mock.ExpectExec(`DELETE FROM "maintenance_tasks" WHERE .*id.* = \$1.*`).
			WillReturnError(errors.New("db error"))
		mock.ExpectRollback()
		expectMaintenanceI18nQuery(mock, userID)

		handler.DeleteMaintenanceTask(c)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}
