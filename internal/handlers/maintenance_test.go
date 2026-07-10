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

// ⚡ Bolt: Robust expectation for i18n lookup to handle GORM's implicit clauses
func expectMaintenanceI18nQuery(mock sqlmock.Sqlmock, userID uuid.UUID) {
	mock.ExpectQuery(`SELECT "id","language_id" FROM "profiles" WHERE id = \$1.*`).
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

		mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND home_id = \$2.*`).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id"}).AddRow(userID, homeID))

		taskID := uuid.New()
		mock.ExpectQuery(`SELECT \* FROM "maintenance_tasks" WHERE home_id = \$1.*`).
			WithArgs(homeID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "description", "scheduled_date"}).
				AddRow(taskID, homeID, "Change HVAC Filter", time.Now()))

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

		mock.ExpectQuery(`SELECT \* FROM "user_homes".*`).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id"}).AddRow(userID, homeID))

		// ⚡ Bolt: Expect validation query for inventory item
		mock.ExpectQuery(`SELECT \* FROM "inventory_items" WHERE id = \$1 AND home_id = \$2.*`).
			WithArgs(itemID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id"}).AddRow(itemID, homeID))

		mock.ExpectQuery(`SELECT \* FROM "maintenance_tasks" WHERE home_id = \$1 AND inventory_item_id = \$2.*`).
			WithArgs(homeID, itemID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "inventory_item_id"}).
				AddRow(uuid.New(), homeID, itemID))

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

		mock.ExpectQuery(`SELECT \* FROM "user_homes".*`).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id"}).AddRow(userID, homeID))

		mock.ExpectQuery(`SELECT \* FROM "inventory_items" WHERE id = \$1 AND home_id = \$2.*`).
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

		mock.ExpectQuery(`SELECT \* FROM "user_homes".*`).
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

		mock.ExpectQuery(`SELECT \* FROM "user_homes".*`).
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

		mock.ExpectQuery(`SELECT \* FROM "user_homes".*`).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id"}).AddRow(userID, homeID))
		mock.ExpectQuery(`SELECT \* FROM "maintenance_tasks".*`).
			WillReturnError(errors.New("db error"))
		expectMaintenanceI18nQuery(mock, userID)

		handler.GetMaintenanceTasks(c)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("inventory fetch error", func(t *testing.T) {
		handler, mock := setupMaintenanceTest(t)
		i18n.InvalidateUserLanguageCache(userID)
		itemID := uuid.New()
		req, _ := http.NewRequest(http.MethodGet, "/maintenance-tasks?inventory_item_id="+itemID.String(), nil)
		req.Header.Set("X-Home-Id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "user_homes".*`).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id"}).AddRow(userID, homeID))

		mock.ExpectQuery(`SELECT \* FROM "inventory_items" WHERE id = \$1 AND home_id = \$2.*`).
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

		mock.ExpectQuery(`SELECT \* FROM "maintenance_tasks" WHERE id = \$1.*`).
			WithArgs(taskID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "description"}).AddRow(taskID, homeID, "Test Task"))

		mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND home_id = \$2.*`).
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

		mock.ExpectQuery(`SELECT \* FROM "maintenance_tasks" WHERE id = \$1.*`).
			WillReturnError(gorm.ErrRecordNotFound)
		expectMaintenanceI18nQuery(mock, userID)

		handler.GetMaintenanceTask(c)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("db error", func(t *testing.T) {
		handler, mock := setupMaintenanceTest(t)
		i18n.InvalidateUserLanguageCache(userID)
		req, _ := http.NewRequest(http.MethodGet, "/maintenance-tasks/"+taskID.String(), nil)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: taskID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "maintenance_tasks" WHERE id = \$1.*`).
			WillReturnError(errors.New("db error"))
		expectMaintenanceI18nQuery(mock, userID)

		handler.GetMaintenanceTask(c)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
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

		mock.ExpectQuery(`SELECT \* FROM "maintenance_tasks" WHERE id = \$1.*`).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id"}).AddRow(taskID, homeID))
		mock.ExpectQuery(`SELECT \* FROM "user_homes".*`).
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

		mock.ExpectQuery(`SELECT \* FROM "maintenance_tasks" WHERE id = \$1.*`).
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

		mock.ExpectQuery(`SELECT \* FROM "user_homes".*`).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "owner"))

		mock.ExpectQuery(`SELECT \* FROM "inventory_items" WHERE id = \$1.*`).
			WithArgs(itemID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id"}).AddRow(itemID, homeID))

		mock.ExpectBegin()
		mock.ExpectQuery(`INSERT INTO "maintenance_tasks".*`).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))
		mock.ExpectCommit()

		handler.CreateMaintenanceTask(c)

		assert.Equal(t, http.StatusCreated, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("success completed", func(t *testing.T) {
		handler, mock := setupMaintenanceTest(t)
		i18n.InvalidateUserLanguageCache(userID)

		body := MaintenanceTaskRequest{
			Description:   "Completed Task",
			ScheduledDate: time.Now(),
			IsCompleted:   true,
		}
		jsonBody, _ := json.Marshal(body)
		req, _ := http.NewRequest(http.MethodPost, "/maintenance-tasks", bytes.NewBuffer(jsonBody))
		req.Header.Set("X-Home-Id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "user_homes".*`).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "owner"))

		mock.ExpectBegin()
		mock.ExpectQuery(`INSERT INTO "maintenance_tasks".*`).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))
		mock.ExpectCommit()

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

		mock.ExpectQuery(`SELECT \* FROM "user_homes".*`).
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

		mock.ExpectQuery(`SELECT \* FROM "user_homes".*`).
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

		mock.ExpectQuery(`SELECT \* FROM "user_homes".*`).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "owner"))
		mock.ExpectQuery(`SELECT \* FROM "inventory_items" WHERE id = \$1.*`).
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

		mock.ExpectQuery(`SELECT \* FROM "user_homes".*`).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "owner"))
		mock.ExpectQuery(`SELECT \* FROM "inventory_items" WHERE id = \$1.*`).
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

		mock.ExpectQuery(`SELECT \* FROM "user_homes".*`).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "owner"))
		mock.ExpectBegin()
		mock.ExpectQuery(`INSERT INTO "maintenance_tasks".*`).
			WillReturnError(errors.New("db error"))
		mock.ExpectRollback()
		expectMaintenanceI18nQuery(mock, userID)

		handler.CreateMaintenanceTask(c)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
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

		mock.ExpectQuery(`SELECT \* FROM "maintenance_tasks" WHERE id = \$1.*`).
			WithArgs(taskID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "is_completed"}).AddRow(taskID, homeID, false))

		mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND home_id = \$2.*`).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "owner"))

		mock.ExpectBegin()
		mock.ExpectExec(`UPDATE "maintenance_tasks" SET.*`).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()

		// Success response message is translated
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

		mock.ExpectQuery(`SELECT \* FROM "maintenance_tasks" WHERE id = \$1.*`).
			WithArgs(taskID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "is_completed"}).AddRow(taskID, homeID, false))

		mock.ExpectQuery(`SELECT \* FROM "user_homes".*`).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "owner"))

		mock.ExpectBegin()
		mock.ExpectExec(`UPDATE "maintenance_tasks" SET.*`).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()

		expectMaintenanceI18nQuery(mock, userID)

		handler.UpdateMaintenanceTask(c)

		assert.Equal(t, http.StatusOK, w.Code)
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

		mock.ExpectQuery(`SELECT \* FROM "maintenance_tasks" WHERE id = \$1.*`).
			WithArgs(taskID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "is_completed"}).AddRow(taskID, homeID, true))

		mock.ExpectQuery(`SELECT \* FROM "user_homes".*`).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "owner"))

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

		mock.ExpectQuery(`SELECT \* FROM "maintenance_tasks" WHERE id = \$1.*`).
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

		mock.ExpectQuery(`SELECT \* FROM "maintenance_tasks" WHERE id = \$1.*`).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id"}).AddRow(taskID, homeID))
		mock.ExpectQuery(`SELECT \* FROM "user_homes".*`).
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

		mock.ExpectQuery(`SELECT \* FROM "maintenance_tasks" WHERE id = \$1.*`).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id"}).AddRow(taskID, homeID))
		mock.ExpectQuery(`SELECT \* FROM "user_homes".*`).
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

		mock.ExpectQuery(`SELECT \* FROM "maintenance_tasks" WHERE id = \$1.*`).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id"}).AddRow(taskID, homeID))
		mock.ExpectQuery(`SELECT \* FROM "user_homes".*`).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "owner"))
		mock.ExpectQuery(`SELECT \* FROM "inventory_items" WHERE id = \$1.*`).
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

		mock.ExpectQuery(`SELECT \* FROM "maintenance_tasks" WHERE id = \$1.*`).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id"}).AddRow(taskID, homeID))
		mock.ExpectQuery(`SELECT \* FROM "user_homes".*`).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "owner"))
		mock.ExpectQuery(`SELECT \* FROM "inventory_items" WHERE id = \$1.*`).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id"}).AddRow(itemID, uuid.New()))
		expectMaintenanceI18nQuery(mock, userID)

		handler.UpdateMaintenanceTask(c)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("fetch error", func(t *testing.T) {
		handler, mock := setupMaintenanceTest(t)
		i18n.InvalidateUserLanguageCache(userID)
		req, _ := http.NewRequest(http.MethodPut, "/maintenance-tasks/"+taskID.String(), nil)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: taskID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "maintenance_tasks" WHERE id = \$1.*`).
			WillReturnError(errors.New("other error"))
		expectMaintenanceI18nQuery(mock, userID)

		handler.UpdateMaintenanceTask(c)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
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

		mock.ExpectQuery(`SELECT \* FROM "maintenance_tasks" WHERE id = \$1.*`).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id"}).AddRow(taskID, homeID))
		mock.ExpectQuery(`SELECT \* FROM "user_homes".*`).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "owner"))
		mock.ExpectBegin()
		mock.ExpectExec(`UPDATE "maintenance_tasks" SET.*`).
			WillReturnError(errors.New("db error"))
		mock.ExpectRollback()
		expectMaintenanceI18nQuery(mock, userID)

		handler.UpdateMaintenanceTask(c)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
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

		mock.ExpectQuery(`SELECT \* FROM "maintenance_tasks" WHERE id = \$1.*`).
			WithArgs(taskID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id"}).AddRow(taskID, homeID))

		mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND home_id = \$2.*`).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "owner"))

		mock.ExpectBegin()
		mock.ExpectExec(`DELETE FROM "maintenance_tasks" WHERE id = \$1.*`).
			WithArgs(taskID).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()

		// Success response message is translated
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

		mock.ExpectQuery(`SELECT \* FROM "maintenance_tasks" WHERE id = \$1.*`).
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

		mock.ExpectQuery(`SELECT \* FROM "maintenance_tasks" WHERE id = \$1.*`).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id"}).AddRow(taskID, homeID))
		mock.ExpectQuery(`SELECT \* FROM "user_homes".*`).
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

		mock.ExpectQuery(`SELECT \* FROM "maintenance_tasks" WHERE id = \$1.*`).
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

		mock.ExpectQuery(`SELECT \* FROM "maintenance_tasks" WHERE id = \$1.*`).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id"}).AddRow(taskID, homeID))
		mock.ExpectQuery(`SELECT \* FROM "user_homes".*`).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "owner"))
		mock.ExpectBegin()
		mock.ExpectExec(`DELETE FROM "maintenance_tasks" WHERE id = \$1.*`).
			WillReturnError(errors.New("db error"))
		mock.ExpectRollback()
		expectMaintenanceI18nQuery(mock, userID)

		handler.DeleteMaintenanceTask(c)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}
