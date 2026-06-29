package handlers

import (
	"bytes"
	"errors"
	"net/http"
	"regexp"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/logger"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func setupInventoryTest(t *testing.T) (*InventoryItemHandler, sqlmock.Sqlmock) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: db}), &gorm.Config{})
	require.NoError(t, err)
	handler := &InventoryItemHandler{DB: gormDB}
	return handler, mock
}

func TestGetInventoryItems(t *testing.T) {
	logger.InitLogger()
	gin.SetMode(gin.TestMode)
	userID := uuid.New()
	homeID := uuid.New()

	t.Run("success", func(t *testing.T) {
		handler, mock := setupInventoryTest(t)
		req, err := http.NewRequest(http.MethodGet, "/inventory", nil)
		require.NoError(t, err)
		req.Header.Set("X-Home-Id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND home_id = \$2 ORDER BY "user_homes"\."user_id" LIMIT \$3`).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id"}).AddRow(userID, homeID))

		itemDefID := uuid.New()
		catID := uuid.New()
		unitID := uuid.New()

		mock.ExpectQuery(`SELECT \* FROM "inventory_items" WHERE home_id = \$1`).
			WithArgs(homeID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "item_definition_id", "quantity", "expiration_date"}).AddRow(uuid.New(), homeID, itemDefID, 5, nil))

		mock.ExpectQuery(`SELECT \* FROM "item_definitions" WHERE "item_definitions"\."id" = \$1`).
			WithArgs(itemDefID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "name", "category_id", "size_unit_id"}).AddRow(itemDefID, "Test Item", catID, unitID))

		mock.ExpectQuery(`SELECT \* FROM "categories" WHERE "categories"\."id" = \$1`).
			WithArgs(catID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(catID, "Test Category"))

		mock.ExpectQuery(`SELECT \* FROM "size_units" WHERE "size_units"\."id" = \$1`).
			WithArgs(unitID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(unitID, "Test Unit"))

		handler.GetInventoryItems(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("missing home_id", func(t *testing.T) {
		handler, _ := setupInventoryTest(t)
		req, err := http.NewRequest(http.MethodGet, "/inventory", nil)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		handler.GetInventoryItems(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("access denied", func(t *testing.T) {
		handler, mock := setupInventoryTest(t)
		req, err := http.NewRequest(http.MethodGet, "/inventory", nil)
		require.NoError(t, err)
		req.Header.Set("X-Home-Id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND home_id = \$2 ORDER BY "user_homes"\."user_id" LIMIT \$3`).
			WithArgs(userID, homeID, 1).
			WillReturnError(errors.New("not found"))

		handler.GetInventoryItems(c)

		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("GetInventoryItems DB Error", func(t *testing.T) {
		handler, mock := setupInventoryTest(t)
		req, err := http.NewRequest(http.MethodGet, "/inventory", nil)
		require.NoError(t, err)
		req.Header.Set("X-Home-Id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND home_id = \$2 ORDER BY "user_homes"\."user_id" LIMIT \$3`).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id"}).AddRow(userID, homeID))

		mock.ExpectQuery(`SELECT \* FROM "inventory_items" WHERE home_id = \$1`).
			WithArgs(homeID).
			WillReturnError(errors.New("db error"))

		handler.GetInventoryItems(c)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "Failed to fetch inventory items")
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestCreateInventoryItem(t *testing.T) {
	logger.InitLogger()
	gin.SetMode(gin.TestMode)
	userID := uuid.New()
	homeID := uuid.New()
	itemDefID := uuid.New()

	t.Run("success", func(t *testing.T) {
		handler, mock := setupInventoryTest(t)
		body := `{"item_definition_id": "` + itemDefID.String() + `", "quantity": 5, "expiry_date": "2026-12-31T23:59:59Z"}`
		req, err := http.NewRequest(http.MethodPost, "/inventory", bytes.NewBufferString(body))
		require.NoError(t, err)
		req.Header.Set("X-Home-Id", homeID.String())
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND home_id = \$2 ORDER BY "user_homes"\."user_id" LIMIT \$3`).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "owner"))

		mock.ExpectBegin()
		mock.ExpectQuery(`INSERT INTO "inventory_items"`).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))

		mock.ExpectQuery(`INSERT INTO "inventory_transactions"`).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))

		// Mock UpdateShoppingListForDefinition inside transaction
		mock.ExpectQuery(`SELECT \* FROM "item_definitions" WHERE "item_definitions"\."id" = \$1 ORDER BY "item_definitions"\."id" LIMIT \$2`).
			WithArgs(itemDefID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "name", "low_stock_threshold"}).AddRow(itemDefID, homeID, "Test Item", nil))

		mock.ExpectQuery(`SELECT \* FROM "shopping_list_items" WHERE home_id = \$1 AND item_definition_id = \$2 AND is_auto_generated = \$3 AND is_bought = \$4 ORDER BY "shopping_list_items"\."id" LIMIT \$5`).
			WithArgs(homeID, itemDefID, true, false, 1).
			WillReturnError(gorm.ErrRecordNotFound)

		mock.ExpectCommit()

		handler.CreateInventoryItem(c)

		assert.Equal(t, http.StatusCreated, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("access denied", func(t *testing.T) {
		handler, mock := setupInventoryTest(t)
		body := `{"item_definition_id": "` + itemDefID.String() + `", "quantity": 5}`
		req, _ := http.NewRequest(http.MethodPost, "/inventory", bytes.NewBufferString(body))
		req.Header.Set("X-Home-Id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "user_homes"`).
			WillReturnError(errors.New("forbidden"))

		handler.CreateInventoryItem(c)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("invalid payload", func(t *testing.T) {
		handler, mock := setupInventoryTest(t)
		body := `{"quantity": -1}`
		req, _ := http.NewRequest(http.MethodPost, "/inventory", bytes.NewBufferString(body))
		req.Header.Set("X-Home-Id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND home_id = \$2 ORDER BY "user_homes"\."user_id" LIMIT \$3`).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "owner"))

		handler.CreateInventoryItem(c)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("transaction failure", func(t *testing.T) {
		handler, mock := setupInventoryTest(t)
		body := `{"item_definition_id": "` + itemDefID.String() + `", "quantity": 5}`
		req, _ := http.NewRequest(http.MethodPost, "/inventory", bytes.NewBufferString(body))
		req.Header.Set("X-Home-Id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "user_homes"`).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "owner"))

		mock.ExpectBegin()
		mock.ExpectQuery(`INSERT INTO "inventory_items"`).
			WillReturnError(errors.New("db error"))
		mock.ExpectRollback()

		handler.CreateInventoryItem(c)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

func TestUpdateInventoryItem(t *testing.T) {
	logger.InitLogger()
	gin.SetMode(gin.TestMode)
	userID := uuid.New()
	homeID := uuid.New()
	itemID := uuid.New()
	itemDefID := uuid.New()

	t.Run("success", func(t *testing.T) {
		handler, mock := setupInventoryTest(t)
		body := `{"quantity": 10, "expiry_date": "2027-12-31T23:59:59Z"}`
		req, err := http.NewRequest(http.MethodPut, "/inventory/"+itemID.String(), bytes.NewBufferString(body))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: itemID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "inventory_items" WHERE "inventory_items"\."id" = \$1 ORDER BY "inventory_items"\."id" LIMIT \$2`).
			WithArgs(itemID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "item_definition_id", "quantity"}).AddRow(itemID, homeID, itemDefID, 5))

		mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND home_id = \$2 ORDER BY "user_homes"\."user_id" LIMIT \$3`).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "owner"))

		mock.ExpectBegin()
		mock.ExpectExec(`UPDATE "inventory_items" SET "expiration_date"=\$1,"quantity"=\$2,"updated_at"=\$3 WHERE "id" = \$4`).
			WithArgs(sqlmock.AnyArg(), 10.0, sqlmock.AnyArg(), itemID).
			WillReturnResult(sqlmock.NewResult(1, 1))

		mock.ExpectQuery(`INSERT INTO "inventory_transactions"`).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))

		// Mock UpdateShoppingListForDefinition
		mock.ExpectQuery(`SELECT \* FROM "item_definitions" WHERE "item_definitions"\."id" = \$1 ORDER BY "item_definitions"\."id" LIMIT \$2`).
			WithArgs(itemDefID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "name", "low_stock_threshold"}).AddRow(itemDefID, homeID, "Test Item", nil))

		mock.ExpectQuery(`SELECT \* FROM "shopping_list_items" WHERE home_id = \$1 AND item_definition_id = \$2 AND is_auto_generated = \$3 AND is_bought = \$4 ORDER BY "shopping_list_items"\."id" LIMIT \$5`).
			WithArgs(homeID, itemDefID, true, false, 1).
			WillReturnError(gorm.ErrRecordNotFound)

		mock.ExpectCommit()

		handler.UpdateInventoryItem(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		handler, mock := setupInventoryTest(t)
		body := `{"quantity": 10}`
		req, _ := http.NewRequest(http.MethodPut, "/inventory/"+itemID.String(), bytes.NewBufferString(body))
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: itemID.String()}}

		mock.ExpectQuery(`SELECT \* FROM "inventory_items"`).WillReturnError(gorm.ErrRecordNotFound)

		handler.UpdateInventoryItem(c)
		assert.Equal(t, http.StatusNotFound, c.Writer.Status())
	})

	t.Run("access denied", func(t *testing.T) {
		handler, mock := setupInventoryTest(t)
		body := `{"quantity": 10}`
		req, _ := http.NewRequest(http.MethodPut, "/inventory/"+itemID.String(), bytes.NewBufferString(body))
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: itemID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "inventory_items"`).WillReturnRows(sqlmock.NewRows([]string{"id", "home_id"}).AddRow(itemID, homeID))
		mock.ExpectQuery(`SELECT \* FROM "user_homes"`).WillReturnError(errors.New("denied"))

		handler.UpdateInventoryItem(c)
		assert.Equal(t, http.StatusForbidden, c.Writer.Status())
	})
}

func TestUpdateInventoryItemQuantity(t *testing.T) {
	logger.InitLogger()
	gin.SetMode(gin.TestMode)
	userID := uuid.New()
	homeID := uuid.New()
	itemID := uuid.New()
	itemDefID := uuid.New()

	t.Run("success", func(t *testing.T) {
		handler, mock := setupInventoryTest(t)
		body := `{"quantity": 10}`
		req, err := http.NewRequest(http.MethodPatch, "/inventory/"+itemID.String()+"/quantity", bytes.NewBufferString(body))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: itemID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "inventory_items" WHERE "inventory_items"\."id" = \$1 ORDER BY "inventory_items"\."id" LIMIT \$2`).
			WithArgs(itemID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "item_definition_id", "quantity"}).AddRow(itemID, homeID, itemDefID, 5))

		mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND home_id = \$2 ORDER BY "user_homes"\."user_id" LIMIT \$3`).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "owner"))

		mock.ExpectBegin()
		mock.ExpectExec(`UPDATE "inventory_items" SET "quantity"=\$1,"updated_at"=\$2 WHERE "id" = \$3`).
			WithArgs(10.0, sqlmock.AnyArg(), itemID).
			WillReturnResult(sqlmock.NewResult(1, 1))

		mock.ExpectQuery(`INSERT INTO "inventory_transactions"`).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))

		// Mock UpdateShoppingListForDefinition
		mock.ExpectQuery(`SELECT \* FROM "item_definitions" WHERE "item_definitions"\."id" = \$1 ORDER BY "item_definitions"\."id" LIMIT \$2`).
			WithArgs(itemDefID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "name", "low_stock_threshold"}).AddRow(itemDefID, homeID, "Test Item", nil))

		mock.ExpectQuery(`SELECT \* FROM "shopping_list_items" WHERE home_id = \$1 AND item_definition_id = \$2 AND is_auto_generated = \$3 AND is_bought = \$4 ORDER BY "shopping_list_items"\."id" LIMIT \$5`).
			WithArgs(homeID, itemDefID, true, false, 1).
			WillReturnError(gorm.ErrRecordNotFound)

		mock.ExpectCommit()

		handler.UpdateInventoryItemQuantity(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		handler, mock := setupInventoryTest(t)
		body := `{"quantity": 10}`
		req, _ := http.NewRequest(http.MethodPatch, "/inventory/"+itemID.String()+"/quantity", bytes.NewBufferString(body))
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: itemID.String()}}

		mock.ExpectQuery(`SELECT \* FROM "inventory_items"`).WillReturnError(gorm.ErrRecordNotFound)

		handler.UpdateInventoryItemQuantity(c)
		assert.Equal(t, http.StatusNotFound, c.Writer.Status())
	})
}

func TestDeleteInventoryItem(t *testing.T) {
	logger.InitLogger()
	gin.SetMode(gin.TestMode)
	userID := uuid.New()
	homeID := uuid.New()
	itemID := uuid.New()
	itemDefID := uuid.New()

	t.Run("success", func(t *testing.T) {
		handler, mock := setupInventoryTest(t)
		req, err := http.NewRequest(http.MethodDelete, "/inventory/"+itemID.String(), nil)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: itemID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "inventory_items" WHERE "inventory_items"\."id" = \$1 ORDER BY "inventory_items"\."id" LIMIT \$2`).
			WithArgs(itemID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "item_definition_id", "quantity"}).AddRow(itemID, homeID, itemDefID, 5))

		mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND home_id = \$2 ORDER BY "user_homes"\."user_id" LIMIT \$3`).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "owner"))

		mock.ExpectBegin()
		mock.ExpectQuery(`INSERT INTO "inventory_transactions"`).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))

		mock.ExpectExec(`DELETE FROM "inventory_items" WHERE "inventory_items"\."id" = \$1`).
			WithArgs(itemID).
			WillReturnResult(sqlmock.NewResult(1, 1))

		// Mock UpdateShoppingListForDefinition
		mock.ExpectQuery(`SELECT \* FROM "item_definitions" WHERE "item_definitions"\."id" = \$1 ORDER BY "item_definitions"\."id" LIMIT \$2`).
			WithArgs(itemDefID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "name", "low_stock_threshold"}).AddRow(itemDefID, homeID, "Test Item", nil))

		mock.ExpectQuery(`SELECT \* FROM "shopping_list_items" WHERE home_id = \$1 AND item_definition_id = \$2 AND is_auto_generated = \$3 AND is_bought = \$4 ORDER BY "shopping_list_items"\."id" LIMIT \$5`).
			WithArgs(homeID, itemDefID, true, false, 1).
			WillReturnError(gorm.ErrRecordNotFound)

		mock.ExpectCommit()

		handler.DeleteInventoryItem(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		handler, mock := setupInventoryTest(t)
		req, _ := http.NewRequest(http.MethodDelete, "/inventory/"+itemID.String(), nil)
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: itemID.String()}}

		mock.ExpectQuery(`SELECT \* FROM "inventory_items"`).WillReturnError(gorm.ErrRecordNotFound)

		handler.DeleteInventoryItem(c)
		assert.Equal(t, http.StatusNotFound, c.Writer.Status())
	})
}

// TODO: Add full test cases for GetAlmostFinishedItems here to ensure good coverage.

func TestGetAlmostFinishedItems(t *testing.T) {
	logger.InitLogger()
	gin.SetMode(gin.TestMode)
	userID := uuid.New()
	homeID := uuid.New()
	itemDefID := uuid.New()
	catID := uuid.New()
	unitID := uuid.New()

	t.Run("success", func(t *testing.T) {
		handler, mock := setupInventoryTest(t)
		req, err := http.NewRequest(http.MethodGet, "/inventory/almost-finished", nil)
		require.NoError(t, err)
		req.Header.Set("X-Home-Id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND home_id = \$2 ORDER BY "user_homes"\."user_id" LIMIT \$3`).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id"}).AddRow(userID, homeID))

		mock.ExpectQuery(`SELECT \* FROM "item_definitions" WHERE home_id = \$1`).
			WithArgs(homeID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "name", "category_id", "size_unit_id"}).AddRow(itemDefID, homeID, "Test Item", catID, unitID))

		mock.ExpectQuery(`SELECT \* FROM "categories" WHERE "categories"\."id" = \$1`).
			WithArgs(catID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(catID, "Test Category"))

		mock.ExpectQuery(`SELECT \* FROM "size_units" WHERE "size_units"\."id" = \$1`).
			WithArgs(unitID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(unitID, "Test Unit"))

		mock.ExpectQuery(`SELECT \* FROM "inventory_items" WHERE home_id = \$1`).
			WithArgs(homeID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "item_definition_id", "quantity", "expiration_date"}).AddRow(uuid.New(), homeID, itemDefID, 5, nil))

		mock.ExpectQuery(`SELECT item_definition_id, SUM\(-quantity_change\) as total_consumed, MIN\(created_at\) as first_tx_time, MAX\(created_at\) as last_tx_time FROM "inventory_transactions" WHERE home_id = \$1 AND quantity_change < 0 AND created_at >= \$2 GROUP BY "item_definition_id"`).
			WithArgs(homeID, sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"item_definition_id", "total_consumed", "first_tx_time", "last_tx_time"}).AddRow(itemDefID, 10.0, time.Now().Add(-time.Hour), time.Now().Add(-time.Hour)))

		handler.GetAlmostFinishedItems(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("missing header", func(t *testing.T) {
		handler, _ := setupInventoryTest(t)
		req, err := http.NewRequest(http.MethodGet, "/inventory/almost-finished", nil)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		handler.GetAlmostFinishedItems(c)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("access denied", func(t *testing.T) {
		handler, mock := setupInventoryTest(t)
		req, err := http.NewRequest(http.MethodGet, "/inventory/almost-finished", nil)
		require.NoError(t, err)
		req.Header.Set("X-Home-Id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND home_id = \$2 ORDER BY "user_homes"\."user_id" LIMIT \$3`).
			WithArgs(userID, homeID, 1).
			WillReturnError(errors.New("not found"))

		handler.GetAlmostFinishedItems(c)
		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error fetching item definitions", func(t *testing.T) {
		handler, mock := setupInventoryTest(t)
		req, err := http.NewRequest(http.MethodGet, "/inventory/almost-finished", nil)
		require.NoError(t, err)
		req.Header.Set("X-Home-Id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND home_id = \$2 ORDER BY "user_homes"\."user_id" LIMIT \$3`).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id"}).AddRow(userID, homeID))

		mock.ExpectQuery(`SELECT \* FROM "item_definitions" WHERE home_id = \$1`).
			WithArgs(homeID).
			WillReturnError(errors.New("db error"))

		handler.GetAlmostFinishedItems(c)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error fetching inventory items", func(t *testing.T) {
		handler, mock := setupInventoryTest(t)
		req, err := http.NewRequest(http.MethodGet, "/inventory/almost-finished", nil)
		require.NoError(t, err)
		req.Header.Set("X-Home-Id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND home_id = \$2 ORDER BY "user_homes"\."user_id" LIMIT \$3`).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id"}).AddRow(userID, homeID))

		mock.ExpectQuery(`SELECT \* FROM "item_definitions" WHERE home_id = \$1`).
			WithArgs(homeID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "name", "category_id", "size_unit_id"}).AddRow(itemDefID, homeID, "Test Item", catID, unitID))

		mock.ExpectQuery(`SELECT \* FROM "categories" WHERE "categories"\."id" = \$1`).
			WithArgs(catID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(catID, "Test Category"))

		mock.ExpectQuery(`SELECT \* FROM "size_units" WHERE "size_units"\."id" = \$1`).
			WithArgs(unitID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(unitID, "Test Unit"))

		mock.ExpectQuery(`SELECT \* FROM "inventory_items" WHERE home_id = \$1`).
			WithArgs(homeID).
			WillReturnError(errors.New("db error"))

		handler.GetAlmostFinishedItems(c)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("expired and expiring soon items", func(t *testing.T) {
		handler, mock := setupInventoryTest(t)
		req, err := http.NewRequest(http.MethodGet, "/inventory/almost-finished", nil)
		require.NoError(t, err)
		req.Header.Set("X-Home-Id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND home_id = \$2 ORDER BY "user_homes"\."user_id" LIMIT \$3`).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id"}).AddRow(userID, homeID))

		expiredItemDefID := uuid.New()
		expiringSoonItemDefID := uuid.New()

		mock.ExpectQuery(`SELECT \* FROM "item_definitions" WHERE home_id = \$1`).
			WithArgs(homeID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "name"}).
				AddRow(expiredItemDefID, homeID, "Expired Item").
				AddRow(expiringSoonItemDefID, homeID, "Expiring Soon Item"))

		now := time.Now()
		expiredDate := now.Add(-time.Hour)
		expiringSoonDate := now.Add(24 * time.Hour)

		mock.ExpectQuery(`SELECT \* FROM "inventory_items" WHERE home_id = \$1`).
			WithArgs(homeID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "item_definition_id", "quantity", "expiration_date"}).
				AddRow(uuid.New(), homeID, expiredItemDefID, 10, expiredDate).
				AddRow(uuid.New(), homeID, expiringSoonItemDefID, 5, expiringSoonDate))

		mock.ExpectQuery(`SELECT item_definition_id, SUM\(-quantity_change\) as total_consumed, MIN\(created_at\) as first_tx_time, MAX\(created_at\) as last_tx_time FROM "inventory_transactions" WHERE home_id = \$1 AND quantity_change < 0 AND created_at >= \$2 GROUP BY "item_definition_id"`).
			WithArgs(homeID, sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"item_definition_id", "total_consumed", "first_tx_time", "last_tx_time"}))

		handler.GetAlmostFinishedItems(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "expiring_soon")
		assert.Contains(t, w.Body.String(), "Expiring Soon Item")
		// The expired item has 0 usable quantity, but if it has no threshold it might not show up unless we specifically handle 0 quantity items.
		// In our current logic, it only shows up if it has a threshold or is expiring soon.
		// Since it's ALREADY expired, it's not "expiring soon" (which is now < expiry_date < now+3d).
		// And since it's expired, its totalQuantity is 0. If it has no threshold, it won't show up.
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error fetching inventory transactions", func(t *testing.T) {
		handler, mock := setupInventoryTest(t)
		req, err := http.NewRequest(http.MethodGet, "/inventory/almost-finished", nil)
		require.NoError(t, err)
		req.Header.Set("X-Home-Id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND home_id = \$2 ORDER BY "user_homes"\."user_id" LIMIT \$3`).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id"}).AddRow(userID, homeID))

		mock.ExpectQuery(`SELECT \* FROM "item_definitions" WHERE home_id = \$1`).
			WithArgs(homeID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "name", "category_id", "size_unit_id"}).AddRow(itemDefID, homeID, "Test Item", catID, unitID))

		mock.ExpectQuery(`SELECT \* FROM "categories" WHERE "categories"\."id" = \$1`).
			WithArgs(catID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(catID, "Test Category"))

		mock.ExpectQuery(`SELECT \* FROM "size_units" WHERE "size_units"\."id" = \$1`).
			WithArgs(unitID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(unitID, "Test Unit"))

		mock.ExpectQuery(`SELECT \* FROM "inventory_items" WHERE home_id = \$1`).
			WithArgs(homeID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "item_definition_id", "quantity", "expiration_date"}).AddRow(uuid.New(), homeID, itemDefID, 5, nil))

		mock.ExpectQuery(`SELECT item_definition_id, SUM\(-quantity_change\) as total_consumed, MIN\(created_at\) as first_tx_time, MAX\(created_at\) as last_tx_time FROM "inventory_transactions" WHERE home_id = \$1 AND quantity_change < 0 AND created_at >= \$2 GROUP BY "item_definition_id"`).
			WithArgs(homeID, sqlmock.AnyArg()).
			WillReturnError(errors.New("db error"))

		handler.GetAlmostFinishedItems(c)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

}

func TestGetInventoryItems_FiltersAndSorts(t *testing.T) {
	logger.InitLogger()
	gin.SetMode(gin.TestMode)
	userID := uuid.New()
	homeID := uuid.New()

	t.Run("filter and sort", func(t *testing.T) {
		handler, mock := setupInventoryTest(t)
		req, _ := http.NewRequest(http.MethodGet, "/inventory?filter=expiring_soon&sort=expiry", nil)
		req.Header.Set("X-Home-Id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "user_homes"`).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id"}).AddRow(userID, homeID))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "inventory_items" WHERE home_id = $1 AND (expiration_date > NOW() AND expiration_date <= $2) ORDER BY expiration_date ASC`)).
			WithArgs(homeID, sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "quantity"}).AddRow(uuid.New(), homeID, 5.0))

		handler.GetInventoryItems(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}
