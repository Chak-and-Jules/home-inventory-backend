package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func setupPredictionsTest(t *testing.T) (*InventoryItemHandler, sqlmock.Sqlmock) {
	gin.SetMode(gin.TestMode)
	dbMock, mock, err := sqlmock.New()
	assert.NoError(t, err)

	gormDB, err := gorm.Open(postgres.New(postgres.Config{
		Conn: dbMock,
	}), &gorm.Config{})
	assert.NoError(t, err)

	handler := &InventoryItemHandler{DB: gormDB}
	return handler, mock
}

func expectProfileLookupInPredictionsTest(mock sqlmock.Sqlmock, userID uuid.UUID) {
	mock.ExpectQuery(`SELECT "id","language_id" FROM "profiles" WHERE id = \$1 ORDER BY "profiles"\."id" LIMIT \$2`).
		WithArgs(userID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "language_id"}).AddRow(userID, nil))
}

func TestGetHomePredictions(t *testing.T) {
	handler, mock := setupPredictionsTest(t)

	userID := uuid.New()
	homeID := uuid.New()
	predID := uuid.New()
	itemID := uuid.New()
	itemDefID := uuid.New()
	now := time.Now()

	t.Run("success", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/api/v1/homes/predictions", nil)
		c.Request.Header.Set("X-Home-Id", homeID.String())
		c.Set("userID", userID)

		// Mock VerifyHomeAccess
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2 ORDER BY "user_homes"."user_id" LIMIT $3`)).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, models.RoleOwner))

		// Mock query for predictions with Joins("InventoryItem")
		mock.ExpectQuery(`SELECT .* FROM "inventory_predictions" LEFT JOIN "inventory_items" "InventoryItem" ON "inventory_predictions"\."inventory_item_id" = "InventoryItem"\."id"`).
			WithArgs(homeID, models.PredictionStatusPredicted).
			WillReturnRows(sqlmock.NewRows([]string{
				"id", "inventory_item_id", "predicted_consumed_amount", "status", "created_at", "updated_at",
				"InventoryItem__id", "InventoryItem__home_id", "InventoryItem__item_definition_id", "InventoryItem__quantity",
			}).AddRow(
				predID, itemID, 2.5, models.PredictionStatusPredicted, now, now,
				itemID, homeID, itemDefID, 10.0,
			))

		// Preload ItemDefinition on InventoryItem
		mock.ExpectQuery(`SELECT .* FROM "item_definitions" WHERE "item_definitions"\."id" = \$1`).
			WithArgs(itemDefID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "name", "image_url"}).
				AddRow(itemDefID, homeID, "Milk", "http://example.com/milk.png"))

		handler.GetHomePredictions(c)

		assert.Equal(t, http.StatusOK, w.Code)
		var res []PredictionResponse
		err := json.Unmarshal(w.Body.Bytes(), &res)
		assert.NoError(t, err)
		assert.Len(t, res, 1)
		assert.Equal(t, predID, res[0].PredictionID)
		assert.Equal(t, "Milk", res[0].ItemDefinitionDetails.Name)
		assert.Equal(t, 2.5, res[0].PredictedAmount)
	})

	t.Run("missing x-home-id header", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/api/v1/homes/predictions", nil)
		c.Set("userID", userID)

		expectProfileLookupInPredictionsTest(mock, userID)

		handler.GetHomePredictions(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("access denied to home", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/api/v1/homes/predictions", nil)
		c.Request.Header.Set("X-Home-Id", homeID.String())
		c.Set("userID", userID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2 ORDER BY "user_homes"."user_id" LIMIT $3`)).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}))

		expectProfileLookupInPredictionsTest(mock, userID)

		handler.GetHomePredictions(c)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})
}

func TestIgnorePrediction(t *testing.T) {
	handler, mock := setupPredictionsTest(t)

	userID := uuid.New()
	homeID := uuid.New()
	predID := uuid.New()
	itemID := uuid.New()

	t.Run("success", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)

		body := IgnorePredictionRequest{PredictionID: predID}
		jsonBytes, _ := json.Marshal(body)
		c.Request = httptest.NewRequest("PUT", "/api/v1/homes/predictions/ignore", bytes.NewBuffer(jsonBytes))
		c.Request.Header.Set("X-Home-Id", homeID.String())
		c.Set("userID", userID)

		// Mock VerifyHomeWriteAccess
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2 ORDER BY "user_homes"."user_id" LIMIT $3`)).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, models.RoleOwner))

		// Find prediction
		mock.ExpectQuery(`SELECT .* FROM "inventory_predictions"`).
			WithArgs(predID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "inventory_item_id", "status"}).
				AddRow(predID, itemID, models.PredictionStatusPredicted))

		// Update status
		mock.ExpectBegin()
		mock.ExpectExec(`UPDATE "inventory_predictions"`).
			WithArgs(models.PredictionStatusIgnored, sqlmock.AnyArg(), predID).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		handler.IgnorePrediction(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "Prediction ignored successfully")
	})

	t.Run("not found", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)

		body := IgnorePredictionRequest{PredictionID: predID}
		jsonBytes, _ := json.Marshal(body)
		c.Request = httptest.NewRequest("PUT", "/api/v1/homes/predictions/ignore", bytes.NewBuffer(jsonBytes))
		c.Request.Header.Set("X-Home-Id", homeID.String())
		c.Set("userID", userID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2 ORDER BY "user_homes"."user_id" LIMIT $3`)).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, models.RoleOwner))

		mock.ExpectQuery(`SELECT .* FROM "inventory_predictions"`).
			WithArgs(predID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "inventory_item_id", "status"}))

		expectProfileLookupInPredictionsTest(mock, userID)

		handler.IgnorePrediction(c)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

func TestApplyPrediction(t *testing.T) {
	handler, mock := setupPredictionsTest(t)

	userID := uuid.New()
	homeID := uuid.New()
	predID := uuid.New()
	itemID := uuid.New()
	itemDefID := uuid.New()

	t.Run("success", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)

		body := ApplyPredictionRequest{
			PredictionID:  predID,
			AppliedAmount: 3.0,
		}
		jsonBytes, _ := json.Marshal(body)
		c.Request = httptest.NewRequest("PUT", "/api/v1/homes/predictions/apply", bytes.NewBuffer(jsonBytes))
		c.Request.Header.Set("X-Home-Id", homeID.String())
		c.Set("userID", userID)

		// Verify write access
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2 ORDER BY "user_homes"."user_id" LIMIT $3`)).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, models.RoleOwner))

		// Find prediction
		mock.ExpectQuery(`SELECT .* FROM "inventory_predictions"`).
			WithArgs(predID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "inventory_item_id", "status"}).
				AddRow(predID, itemID, models.PredictionStatusPredicted))

		// Transaction: Update prediction status
		mock.ExpectBegin()
		mock.ExpectExec(`UPDATE "inventory_predictions"`).
			WithArgs(models.PredictionStatusApplied, sqlmock.AnyArg(), predID).
			WillReturnResult(sqlmock.NewResult(1, 1))

		// Fetch item
		mock.ExpectQuery(`SELECT \* FROM "inventory_items" WHERE "inventory_items"\."id" = \$1 ORDER BY "inventory_items"\."id" LIMIT \$2`).
			WithArgs(itemID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "item_definition_id", "quantity"}).
				AddRow(itemID, homeID, itemDefID, 10.0))

		// Update item quantity
		mock.ExpectExec(`UPDATE "inventory_items"`).
			WithArgs(7.0, sqlmock.AnyArg(), itemID).
			WillReturnResult(sqlmock.NewResult(1, 1))

		// Insert InventoryTransaction
		mock.ExpectQuery(`INSERT INTO "inventory_transactions"`).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))

		// Update Shopping List logic queries:
		// 1. Fetch ItemDefinition first
		mock.ExpectQuery(`SELECT \* FROM "item_definitions" WHERE "item_definitions"\."id" = \$1 ORDER BY "item_definitions"\."id" LIMIT \$2`).
			WithArgs(itemDefID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "low_stock_threshold"}).AddRow(itemDefID, 2.0))

		// 2. Fetch existing shopping list item
		mock.ExpectQuery(`SELECT \* FROM "shopping_list_items" WHERE home_id = \$1 AND item_definition_id = \$2 AND is_auto_generated = \$3 AND is_bought = \$4 ORDER BY "shopping_list_items"\."id" LIMIT \$5`).
			WithArgs(homeID, itemDefID, true, false, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id"}))

		// 3. Scan total quantity
		mock.ExpectQuery(`SELECT COALESCE\(SUM\(quantity\), 0\) FROM "inventory_items" WHERE home_id = \$1 AND item_definition_id = \$2 AND \(expiration_date IS NULL OR expiration_date > NOW\(\)\)`).
			WithArgs(homeID, itemDefID).
			WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(7.0))

		mock.ExpectCommit()

		handler.ApplyPrediction(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "Prediction applied successfully")
	})

	t.Run("invalid applied amount <= 0", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)

		body := ApplyPredictionRequest{
			PredictionID:  predID,
			AppliedAmount: 0,
		}
		jsonBytes, _ := json.Marshal(body)
		c.Request = httptest.NewRequest("PUT", "/api/v1/homes/predictions/apply", bytes.NewBuffer(jsonBytes))
		c.Request.Header.Set("X-Home-Id", homeID.String())
		c.Set("userID", userID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2 ORDER BY "user_homes"."user_id" LIMIT $3`)).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, models.RoleOwner))

		expectProfileLookupInPredictionsTest(mock, userID)

		handler.ApplyPrediction(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}
