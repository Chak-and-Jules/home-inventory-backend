package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/i18n"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/logger"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func setupPredictiveRestockTest(t *testing.T) (*InventoryItemHandler, sqlmock.Sqlmock) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: db}), &gorm.Config{})
	require.NoError(t, err)
	handler := &InventoryItemHandler{DB: gormDB}
	return handler, mock
}

func expectProfileLookupPR(mock sqlmock.Sqlmock, userID uuid.UUID) {
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT "id","language_id" FROM "profiles" WHERE id = $1 ORDER BY "profiles"."id" LIMIT $2`)).
		WithArgs(userID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "language_id"}).AddRow(userID, nil))
}

func expectHomeReadAccessPR(mock sqlmock.Sqlmock, userID, homeID uuid.UUID, allowed bool) {
	query := mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2 ORDER BY "user_homes"."user_id" LIMIT $3`))
	if allowed {
		query.WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, models.RoleOwner))
	} else {
		query.WithArgs(userID, homeID, 1).
			WillReturnError(gorm.ErrRecordNotFound)
	}
}

func TestGetPredictiveRestockInsights(t *testing.T) {
	logger.InitLogger()
	gin.SetMode(gin.TestMode)

	t.Run("success returning insights for consumable items", func(t *testing.T) {
		handler, mock := setupPredictiveRestockTest(t)
		userID := uuid.New()
		homeID := uuid.New()
		itemDefID := uuid.New()
		i18n.InvalidateUserLanguageCache(userID)

		req, _ := http.NewRequest(http.MethodGet, "/inventory/insights/restock", nil)
		req.Header.Set("X-Home-Id", homeID.String())
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		expectHomeReadAccessPR(mock, userID, homeID, true)

		// 1. Mock InventoryTransaction select (to calculate ADC)
		// Set firstTxTime slightly less than 10 days ago so ADC is slightly more than 1.0 (e.g. 1.0001)
		// This guarantees that stock of 5.0 will run out exactly on Day 5!
		firstTxTime := time.Now().Add(-240*time.Hour + 1*time.Minute)
		lastTxTime := time.Now()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT item_definition_id, SUM(-quantity_change) as total_consumed, MIN(created_at) as first_tx_time, MAX(created_at) as last_tx_time FROM "inventory_transactions"`)).
			WithArgs(homeID, sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"item_definition_id", "total_consumed", "first_tx_time", "last_tx_time"}).
				AddRow(itemDefID, 10.0, firstTxTime, lastTxTime))

		// 2. Mock ItemDefinition select
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "item_definitions" WHERE home_id = $1`)).
			WithArgs(homeID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "name", "low_stock_threshold"}).
				AddRow(itemDefID, homeID, "Dishwasher Tablet", 2.0))

		// 3. Mock InventoryItems select (Current Stock = 5)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "inventory_items" WHERE home_id = $1`)).
			WithArgs(homeID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "item_definition_id", "quantity"}).
				AddRow(uuid.New(), homeID, itemDefID, 5.0))

		// 4. Mock MaintenanceTask select (none)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "maintenance_tasks" WHERE home_id = $1 AND is_completed = $2`)).
			WithArgs(homeID, false).
			WillReturnRows(sqlmock.NewRows([]string{"id"}))

		// Expect profile language lookup due to TranslateDB calls
		expectProfileLookupPR(mock, userID)

		handler.GetPredictiveRestockInsights(c)

		assert.Equal(t, http.StatusOK, w.Code)

		var res []RestockInsightResponse
		err := json.Unmarshal(w.Body.Bytes(), &res)
		require.NoError(t, err)

		require.Len(t, res, 1)
		assert.Equal(t, "Dishwasher Tablet", res[0].ItemDefinition.Name)
		assert.Equal(t, 5.0, res[0].CurrentStock)
		assert.InDelta(t, 1.0, res[0].AverageDailyConsumption, 0.01)
		assert.NotNil(t, res[0].PredictedDepletionDate)
		assert.Equal(t, 5, *res[0].DaysLeft)
		assert.Contains(t, res[0].Reason, "Dishwasher Tablet")

		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("success handling maintenance task dependencies", func(t *testing.T) {
		handler, mock := setupPredictiveRestockTest(t)
		userID := uuid.New()
		homeID := uuid.New()
		itemDefID := uuid.New()
		taskID := uuid.New()
		i18n.InvalidateUserLanguageCache(userID)

		req, _ := http.NewRequest(http.MethodGet, "/inventory/insights/restock", nil)
		req.Header.Set("X-Home-Id", homeID.String())
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		expectHomeReadAccessPR(mock, userID, homeID, true)

		// 1. Mock InventoryTransaction select (ADC = 0)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT item_definition_id, SUM(-quantity_change) as total_consumed, MIN(created_at) as first_tx_time, MAX(created_at) as last_tx_time FROM "inventory_transactions"`)).
			WithArgs(homeID, sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"item_definition_id", "total_consumed", "first_tx_time", "last_tx_time"}))

		// 2. Mock ItemDefinition select
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "item_definitions" WHERE home_id = $1`)).
			WithArgs(homeID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "name"}).
				AddRow(itemDefID, homeID, "HVAC Filter"))

		// 3. Mock InventoryItems select (Current Stock = 1)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "inventory_items" WHERE home_id = $1`)).
			WithArgs(homeID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "item_definition_id", "quantity"}).
				AddRow(uuid.New(), homeID, itemDefID, 1.0))

		// 4. Mock MaintenanceTask select with dependency requiring 2 HVAC Filters in 3 days
		scheduledDate := time.Now().AddDate(0, 0, 3)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "maintenance_tasks" WHERE home_id = $1 AND is_completed = $2`)).
			WithArgs(homeID, false).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "description", "scheduled_date", "frequency", "is_completed"}).
				AddRow(taskID, homeID, "Change HVAC Filter", scheduledDate, "once", false))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "task_item_dependencies" WHERE "task_item_dependencies"."maintenance_task_id" = $1`)).
			WithArgs(taskID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "maintenance_task_id", "item_definition_id", "quantity_required"}).
				AddRow(uuid.New(), taskID, itemDefID, 2.0))

		// Expect profile language lookup due to TranslateDB calls
		expectProfileLookupPR(mock, userID)

		handler.GetPredictiveRestockInsights(c)

		assert.Equal(t, http.StatusOK, w.Code)

		var res []RestockInsightResponse
		err := json.Unmarshal(w.Body.Bytes(), &res)
		require.NoError(t, err)

		require.Len(t, res, 1)
		assert.Equal(t, "HVAC Filter", res[0].ItemDefinition.Name)
		assert.Equal(t, 1.0, res[0].CurrentStock)
		assert.Equal(t, 0.0, res[0].AverageDailyConsumption)
		assert.NotNil(t, res[0].PredictedDepletionDate)
		assert.Equal(t, 3, *res[0].DaysLeft)
		assert.Contains(t, res[0].Reason, "Change HVAC Filter")

		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("access denied", func(t *testing.T) {
		handler, mock := setupPredictiveRestockTest(t)
		userID := uuid.New()
		homeID := uuid.New()
		i18n.InvalidateUserLanguageCache(userID)

		req, _ := http.NewRequest(http.MethodGet, "/inventory/insights/restock", nil)
		req.Header.Set("X-Home-Id", homeID.String())
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		expectHomeReadAccessPR(mock, userID, homeID, false)
		expectProfileLookupPR(mock, userID)

		handler.GetPredictiveRestockInsights(c)

		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}
