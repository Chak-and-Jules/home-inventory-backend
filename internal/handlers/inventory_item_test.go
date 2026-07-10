package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
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

func setupInventoryTest(t *testing.T) (*InventoryItemHandler, sqlmock.Sqlmock) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: db}), &gorm.Config{})
	require.NoError(t, err)
	handler := &InventoryItemHandler{DB: gormDB}
	return handler, mock
}

func expectI18n(mock sqlmock.Sqlmock, userID uuid.UUID) {
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT "id","language_id" FROM "profiles" WHERE id = $1`)).
		WithArgs(userID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "language_id"}).AddRow(userID, nil))
}

func TestGetInventoryItems(t *testing.T) {
	logger.InitLogger()
	gin.SetMode(gin.TestMode)
	userID := uuid.New()
	homeID := uuid.New()

	t.Run("success", func(t *testing.T) {
		handler, mock := setupInventoryTest(t)
		i18n.InvalidateUserLanguageCache(userID)
		req, err := http.NewRequest(http.MethodGet, "/inventory", nil)
		require.NoError(t, err)
		req.Header.Set("X-Home-Id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2 ORDER BY "user_homes"."user_id" LIMIT $3`)).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id"}).AddRow(userID, homeID))

		itemDefID := uuid.New()
		catID := uuid.New()
		unitID := uuid.New()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "inventory_items" WHERE home_id = $1 ORDER BY created_at DESC`)).
			WithArgs(homeID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "item_definition_id", "quantity"}).AddRow(uuid.New(), homeID, itemDefID, 5))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "item_definitions" WHERE "item_definitions"."id" = $1`)).
			WithArgs(itemDefID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "name", "category_id", "size_unit_id"}).AddRow(itemDefID, "Test Item", catID, unitID))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "categories" WHERE "categories"."id" = $1`)).
			WithArgs(catID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(catID, "Test Category"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "size_units" WHERE "size_units"."id" = $1`)).
			WithArgs(unitID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(unitID, "Test Unit"))

		handler.GetInventoryItems(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("filter expiring_before", func(t *testing.T) {
		handler, mock := setupInventoryTest(t)
		i18n.InvalidateUserLanguageCache(userID)
		targetDate := time.Now().AddDate(0, 0, 5).Format(time.RFC3339)
		req, err := http.NewRequest(http.MethodGet, "/inventory?expiring_before="+targetDate, nil)
		require.NoError(t, err)
		req.Header.Set("X-Home-Id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2 ORDER BY "user_homes"."user_id" LIMIT $3`)).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id"}).AddRow(userID, homeID))

		itemDefID := uuid.New()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "inventory_items" WHERE home_id = $1 AND expiration_date <= $2 ORDER BY created_at DESC`)).
			WithArgs(homeID, sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "item_definition_id", "quantity"}).AddRow(uuid.New(), homeID, itemDefID, 5))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "item_definitions" WHERE "item_definitions"."id" = $1`)).
			WithArgs(itemDefID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(itemDefID, "Test Item"))

		handler.GetInventoryItems(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("filter expiring_soon", func(t *testing.T) {
		handler, mock := setupInventoryTest(t)
		i18n.InvalidateUserLanguageCache(userID)
		req, err := http.NewRequest(http.MethodGet, "/inventory?filter=expiring_soon", nil)
		require.NoError(t, err)
		req.Header.Set("X-Home-Id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2 ORDER BY "user_homes"."user_id" LIMIT $3`)).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id"}).AddRow(userID, homeID))

		itemDefID := uuid.New()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "inventory_items" WHERE home_id = $1 AND (expiration_date > $2 AND expiration_date <= $3) ORDER BY created_at DESC`)).
			WithArgs(homeID, sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "item_definition_id", "quantity"}).AddRow(uuid.New(), homeID, itemDefID, 5))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "item_definitions" WHERE "item_definitions"."id" = $1`)).
			WithArgs(itemDefID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(itemDefID, "Test Item"))

		handler.GetInventoryItems(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("filter expiring_before invalid date", func(t *testing.T) {
		handler, mock := setupInventoryTest(t)
		i18n.InvalidateUserLanguageCache(userID)
		req, err := http.NewRequest(http.MethodGet, "/inventory?expiring_before=invalid", nil)
		require.NoError(t, err)
		req.Header.Set("X-Home-Id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2 ORDER BY "user_homes"."user_id" LIMIT $3`)).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id"}).AddRow(userID, homeID))

		expectI18n(mock, userID)

		handler.GetInventoryItems(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "Invalid date format")
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("missing home_id", func(t *testing.T) {
		handler, mock := setupInventoryTest(t)
		i18n.InvalidateUserLanguageCache(userID)
		req, err := http.NewRequest(http.MethodGet, "/inventory", nil)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		expectI18n(mock, userID)

		handler.GetInventoryItems(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("access denied", func(t *testing.T) {
		handler, mock := setupInventoryTest(t)
		i18n.InvalidateUserLanguageCache(userID)
		req, err := http.NewRequest(http.MethodGet, "/inventory", nil)
		require.NoError(t, err)
		req.Header.Set("X-Home-Id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2 ORDER BY "user_homes"."user_id" LIMIT $3`)).
			WithArgs(userID, homeID, 1).
			WillReturnError(errors.New("not found"))

		expectI18n(mock, userID)

		handler.GetInventoryItems(c)

		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("GetInventoryItems DB Error", func(t *testing.T) {
		handler, mock := setupInventoryTest(t)
		i18n.InvalidateUserLanguageCache(userID)
		req, err := http.NewRequest(http.MethodGet, "/inventory", nil)
		require.NoError(t, err)
		req.Header.Set("X-Home-Id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2 ORDER BY "user_homes"."user_id" LIMIT $3`)).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id"}).AddRow(userID, homeID))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "inventory_items" WHERE home_id = $1 ORDER BY created_at DESC`)).
			WithArgs(homeID).
			WillReturnError(errors.New("db error"))

		expectI18n(mock, userID)

		handler.GetInventoryItems(c)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "Failed to fetch inventory items")
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestGetExpiringItems(t *testing.T) {
	logger.InitLogger()
	gin.SetMode(gin.TestMode)
	userID := uuid.New()
	homeID := uuid.New()

	t.Run("success", func(t *testing.T) {
		handler, mock := setupInventoryTest(t)
		i18n.InvalidateUserLanguageCache(userID)
		req, err := http.NewRequest(http.MethodGet, "/inventory/expiring", nil)
		require.NoError(t, err)
		req.Header.Set("X-Home-Id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2 ORDER BY "user_homes"."user_id" LIMIT $3`)).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id"}).AddRow(userID, homeID))

		itemDefID := uuid.New()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "inventory_items" WHERE home_id = $1 AND expiration_date > $2 AND expiration_date <= $3 ORDER BY expiration_date ASC`)).
			WithArgs(homeID, sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "item_definition_id", "quantity"}).AddRow(uuid.New(), homeID, itemDefID, 5))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "item_definitions" WHERE "item_definitions"."id" = $1`)).
			WithArgs(itemDefID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(itemDefID, "Test Item"))

		handler.GetExpiringItems(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("access denied", func(t *testing.T) {
		handler, mock := setupInventoryTest(t)
		i18n.InvalidateUserLanguageCache(userID)
		req, err := http.NewRequest(http.MethodGet, "/inventory/expiring", nil)
		require.NoError(t, err)
		req.Header.Set("X-Home-Id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2 ORDER BY "user_homes"."user_id" LIMIT $3`)).
			WithArgs(userID, homeID, 1).
			WillReturnError(errors.New("not found"))

		expectI18n(mock, userID)

		handler.GetExpiringItems(c)

		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("missing home_id", func(t *testing.T) {
		handler, mock := setupInventoryTest(t)
		i18n.InvalidateUserLanguageCache(userID)
		req, err := http.NewRequest(http.MethodGet, "/inventory/expiring", nil)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		expectI18n(mock, userID)

		handler.GetExpiringItems(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		handler, mock := setupInventoryTest(t)
		i18n.InvalidateUserLanguageCache(userID)
		req, err := http.NewRequest(http.MethodGet, "/inventory/expiring", nil)
		require.NoError(t, err)
		req.Header.Set("X-Home-Id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2 ORDER BY "user_homes"."user_id" LIMIT $3`)).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id"}).AddRow(userID, homeID))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "inventory_items" WHERE home_id = $1 AND expiration_date > $2 AND expiration_date <= $3 ORDER BY expiration_date ASC`)).
			WithArgs(homeID, sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnError(errors.New("db error"))

		expectI18n(mock, userID)

		handler.GetExpiringItems(c)

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
		i18n.InvalidateUserLanguageCache(userID)

		quantity := 5.0
		body := CreateInventoryItemRequest{
			ItemDefinitionID: itemDefID,
			Quantity:         &quantity,
		}
		jsonBody, _ := json.Marshal(body)
		req, _ := http.NewRequest(http.MethodPost, "/inventory", bytes.NewBuffer(jsonBody))
		req.Header.Set("X-Home-Id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2 ORDER BY "user_homes"."user_id" LIMIT $3`)).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "owner"))

		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO "inventory_items"`)).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))
		mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO "inventory_transactions"`)).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))

		// shopping list sync logic
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "item_definitions" WHERE "item_definitions"."id" = $1 ORDER BY "item_definitions"."id" LIMIT $2`)).
			WithArgs(itemDefID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "name", "low_stock_threshold"}).AddRow(itemDefID, homeID, "Milk", nil))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "shopping_list_items" WHERE home_id = $1 AND item_definition_id = $2 AND is_auto_generated = $3 AND is_bought = $4 ORDER BY "shopping_list_items"."id" LIMIT $5`)).
			WillReturnError(gorm.ErrRecordNotFound)

		mock.ExpectCommit()

		handler.CreateInventoryItem(c)

		assert.Equal(t, http.StatusCreated, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
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
		i18n.InvalidateUserLanguageCache(userID)

		quantity := 10.0
		body := UpdateInventoryItemRequest{
			Quantity: &quantity,
		}
		jsonBody, _ := json.Marshal(body)
		req, _ := http.NewRequest(http.MethodPut, "/inventory/"+itemID.String(), bytes.NewBuffer(jsonBody))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: itemID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "inventory_items" WHERE "inventory_items"."id" = $1 ORDER BY "inventory_items"."id" LIMIT $2`)).
			WithArgs(itemID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "item_definition_id", "quantity"}).AddRow(itemID, homeID, itemDefID, 5.0))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2 ORDER BY "user_homes"."user_id" LIMIT $3`)).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "owner"))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "inventory_items" SET "expiration_date"=$1,"quantity"=$2,"updated_at"=$3 WHERE "id" = $4`)).
			WithArgs(sqlmock.AnyArg(), 10.0, sqlmock.AnyArg(), itemID).
			WillReturnResult(sqlmock.NewResult(1, 1))

		mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO "inventory_transactions"`)).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))

		// shopping list sync logic
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "item_definitions" WHERE "item_definitions"."id" = $1 ORDER BY "item_definitions"."id" LIMIT $2`)).
			WithArgs(itemDefID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "name", "low_stock_threshold"}).AddRow(itemDefID, homeID, "Milk", nil))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "shopping_list_items" WHERE home_id = $1 AND item_definition_id = $2 AND is_auto_generated = $3 AND is_bought = $4 ORDER BY "shopping_list_items"."id" LIMIT $5`)).
			WillReturnError(gorm.ErrRecordNotFound)

		mock.ExpectCommit()

		handler.UpdateInventoryItem(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("allows zero quantity", func(t *testing.T) {
		handler, mock := setupInventoryTest(t)
		i18n.InvalidateUserLanguageCache(userID)

		body := map[string]interface{}{"quantity": 0.0}
		jsonBody, _ := json.Marshal(body)
		req, _ := http.NewRequest(http.MethodPut, "/inventory/"+itemID.String(), bytes.NewBuffer(jsonBody))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: itemID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "inventory_items" WHERE "inventory_items"."id" = $1 ORDER BY "inventory_items"."id" LIMIT $2`)).
			WithArgs(itemID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "item_definition_id", "quantity"}).AddRow(itemID, homeID, itemDefID, 5.0))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2 ORDER BY "user_homes"."user_id" LIMIT $3`)).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "owner"))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "inventory_items" SET "expiration_date"=$1,"quantity"=$2,"updated_at"=$3 WHERE "id" = $4`)).
			WithArgs(sqlmock.AnyArg(), 0.0, sqlmock.AnyArg(), itemID).
			WillReturnResult(sqlmock.NewResult(1, 1))

		mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO "inventory_transactions"`)).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "item_definitions" WHERE "item_definitions"."id" = $1 ORDER BY "item_definitions"."id" LIMIT $2`)).
			WithArgs(itemDefID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "name", "low_stock_threshold"}).AddRow(itemDefID, homeID, "Milk", nil))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "shopping_list_items" WHERE home_id = $1 AND item_definition_id = $2 AND is_auto_generated = $3 AND is_bought = $4 ORDER BY "shopping_list_items"."id" LIMIT $5`)).
			WillReturnError(gorm.ErrRecordNotFound)

		mock.ExpectCommit()

		handler.UpdateInventoryItem(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
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
		i18n.InvalidateUserLanguageCache(userID)

		quantity := 10.0
		body := UpdateQuantityRequest{
			Quantity: &quantity,
		}
		jsonBody, _ := json.Marshal(body)
		req, _ := http.NewRequest(http.MethodPatch, "/inventory/"+itemID.String()+"/quantity", bytes.NewBuffer(jsonBody))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: itemID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "inventory_items" WHERE "inventory_items"."id" = $1 ORDER BY "inventory_items"."id" LIMIT $2`)).
			WithArgs(itemID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "item_definition_id", "quantity"}).AddRow(itemID, homeID, itemDefID, 5.0))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2 ORDER BY "user_homes"."user_id" LIMIT $3`)).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "owner"))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "inventory_items" SET "quantity"=$1,"updated_at"=$2 WHERE "id" = $3`)).
			WithArgs(10.0, sqlmock.AnyArg(), itemID).
			WillReturnResult(sqlmock.NewResult(1, 1))

		mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO "inventory_transactions"`)).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))

		// shopping list sync logic
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "item_definitions" WHERE "item_definitions"."id" = $1 ORDER BY "item_definitions"."id" LIMIT $2`)).
			WithArgs(itemDefID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "name", "low_stock_threshold"}).AddRow(itemDefID, homeID, "Milk", nil))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "shopping_list_items" WHERE home_id = $1 AND item_definition_id = $2 AND is_auto_generated = $3 AND is_bought = $4 ORDER BY "shopping_list_items"."id" LIMIT $5`)).
			WillReturnError(gorm.ErrRecordNotFound)

		mock.ExpectCommit()

		handler.UpdateInventoryItemQuantity(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
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
		i18n.InvalidateUserLanguageCache(userID)

		req, _ := http.NewRequest(http.MethodDelete, "/inventory/"+itemID.String(), nil)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: itemID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "inventory_items" WHERE "inventory_items"."id" = $1 ORDER BY "inventory_items"."id" LIMIT $2`)).
			WithArgs(itemID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "item_definition_id", "quantity"}).AddRow(itemID, homeID, itemDefID, 5.0))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2 ORDER BY "user_homes"."user_id" LIMIT $3`)).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "owner"))

		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO "inventory_transactions"`)).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))

		mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM "inventory_items" WHERE "inventory_items"."id" = $1`)).
			WithArgs(itemID).
			WillReturnResult(sqlmock.NewResult(0, 1))

		// shopping list sync logic
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "item_definitions" WHERE "item_definitions"."id" = $1 ORDER BY "item_definitions"."id" LIMIT $2`)).
			WithArgs(itemDefID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "name", "low_stock_threshold"}).AddRow(itemDefID, homeID, "Milk", nil))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "shopping_list_items" WHERE home_id = $1 AND item_definition_id = $2 AND is_auto_generated = $3 AND is_bought = $4 ORDER BY "shopping_list_items"."id" LIMIT $5`)).
			WithArgs(homeID, itemDefID, true, false, 1).
			WillReturnError(gorm.ErrRecordNotFound)

		mock.ExpectCommit()

		handler.DeleteInventoryItem(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

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
		i18n.InvalidateUserLanguageCache(userID)
		req, err := http.NewRequest(http.MethodGet, "/inventory/almost-finished", nil)
		require.NoError(t, err)
		req.Header.Set("X-Home-Id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2 ORDER BY "user_homes"."user_id" LIMIT $3`)).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id"}).AddRow(userID, homeID))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "item_definitions" WHERE home_id = $1`)).
			WithArgs(homeID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "name", "category_id", "size_unit_id"}).AddRow(itemDefID, homeID, "Test Item", catID, unitID))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "categories" WHERE "categories"."id" = $1`)).
			WithArgs(catID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(catID, "Test Category"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "size_units" WHERE "size_units"."id" = $1`)).
			WithArgs(unitID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(unitID, "Test Unit"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "inventory_items" WHERE home_id = $1`)).
			WithArgs(homeID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "item_definition_id", "quantity", "expiration_date"}).AddRow(uuid.New(), homeID, itemDefID, 5, nil))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT item_definition_id, SUM(-quantity_change) as total_consumed, MIN(created_at) as first_tx_time, MAX(created_at) as last_tx_time FROM "inventory_transactions" WHERE home_id = $1 AND quantity_change < 0 AND created_at >= $2 GROUP BY "item_definition_id"`)).
			WithArgs(homeID, sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"item_definition_id", "total_consumed", "first_tx_time", "last_tx_time"}).AddRow(itemDefID, 10.0, time.Now().Add(-time.Hour), time.Now().Add(-time.Hour)))

		handler.GetAlmostFinishedItems(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("missing header", func(t *testing.T) {
		handler, mock := setupInventoryTest(t)
		i18n.InvalidateUserLanguageCache(userID)
		req, err := http.NewRequest(http.MethodGet, "/inventory/almost-finished", nil)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		expectI18n(mock, userID)

		handler.GetAlmostFinishedItems(c)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("access denied", func(t *testing.T) {
		handler, mock := setupInventoryTest(t)
		i18n.InvalidateUserLanguageCache(userID)
		req, err := http.NewRequest(http.MethodGet, "/inventory/almost-finished", nil)
		require.NoError(t, err)
		req.Header.Set("X-Home-Id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2 ORDER BY "user_homes"."user_id" LIMIT $3`)).
			WithArgs(userID, homeID, 1).
			WillReturnError(errors.New("not found"))

		expectI18n(mock, userID)

		handler.GetAlmostFinishedItems(c)
		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error fetching item definitions", func(t *testing.T) {
		handler, mock := setupInventoryTest(t)
		i18n.InvalidateUserLanguageCache(userID)
		req, err := http.NewRequest(http.MethodGet, "/inventory/almost-finished", nil)
		require.NoError(t, err)
		req.Header.Set("X-Home-Id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2 ORDER BY "user_homes"."user_id" LIMIT $3`)).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id"}).AddRow(userID, homeID))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "item_definitions" WHERE home_id = $1`)).
			WithArgs(homeID).
			WillReturnError(errors.New("db error"))

		expectI18n(mock, userID)

		handler.GetAlmostFinishedItems(c)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error fetching inventory items", func(t *testing.T) {
		handler, mock := setupInventoryTest(t)
		i18n.InvalidateUserLanguageCache(userID)
		req, err := http.NewRequest(http.MethodGet, "/inventory/almost-finished", nil)
		require.NoError(t, err)
		req.Header.Set("X-Home-Id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2 ORDER BY "user_homes"."user_id" LIMIT $3`)).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id"}).AddRow(userID, homeID))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "item_definitions" WHERE home_id = $1`)).
			WithArgs(homeID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "name", "category_id", "size_unit_id"}).AddRow(itemDefID, homeID, "Test Item", catID, unitID))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "categories" WHERE "categories"."id" = $1`)).
			WithArgs(catID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(catID, "Test Category"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "size_units" WHERE "size_units"."id" = $1`)).
			WithArgs(unitID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(unitID, "Test Unit"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "inventory_items" WHERE home_id = $1`)).
			WithArgs(homeID).
			WillReturnError(errors.New("db error"))

		expectI18n(mock, userID)

		handler.GetAlmostFinishedItems(c)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("expired and expiring soon items", func(t *testing.T) {
		handler, mock := setupInventoryTest(t)
		i18n.InvalidateUserLanguageCache(userID)
		req, err := http.NewRequest(http.MethodGet, "/inventory/almost-finished", nil)
		require.NoError(t, err)
		req.Header.Set("X-Home-Id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2 ORDER BY "user_homes"."user_id" LIMIT $3`)).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id"}).AddRow(userID, homeID))

		expiredItemDefID := uuid.New()
		expiringSoonItemDefID := uuid.New()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "item_definitions" WHERE home_id = $1`)).
			WithArgs(homeID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "name"}).
				AddRow(expiredItemDefID, homeID, "Expired Item").
				AddRow(expiringSoonItemDefID, homeID, "Expiring Soon Item"))

		now := time.Now()
		expiredDate := now.Add(-time.Hour)
		expiringSoonDate := now.Add(24 * time.Hour)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "inventory_items" WHERE home_id = $1`)).
			WithArgs(homeID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "item_definition_id", "quantity", "expiration_date"}).
				AddRow(uuid.New(), homeID, expiredItemDefID, 10, expiredDate).
				AddRow(uuid.New(), homeID, expiringSoonItemDefID, 5, expiringSoonDate))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT item_definition_id, SUM(-quantity_change) as total_consumed, MIN(created_at) as first_tx_time, MAX(created_at) as last_tx_time FROM "inventory_transactions" WHERE home_id = $1 AND quantity_change < 0 AND created_at >= $2 GROUP BY "item_definition_id"`)).
			WithArgs(homeID, sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"item_definition_id", "total_consumed", "first_tx_time", "last_tx_time"}))

		handler.GetAlmostFinishedItems(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "expiring_soon")
		assert.Contains(t, w.Body.String(), "Expiring Soon Item")
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error fetching inventory transactions", func(t *testing.T) {
		handler, mock := setupInventoryTest(t)
		i18n.InvalidateUserLanguageCache(userID)
		req, err := http.NewRequest(http.MethodGet, "/inventory/almost-finished", nil)
		require.NoError(t, err)
		req.Header.Set("X-Home-Id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2 ORDER BY "user_homes"."user_id" LIMIT $3`)).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id"}).AddRow(userID, homeID))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "item_definitions" WHERE home_id = $1`)).
			WithArgs(homeID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "name", "category_id", "size_unit_id"}).AddRow(itemDefID, homeID, "Test Item", catID, unitID))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "categories" WHERE "categories"."id" = $1`)).
			WithArgs(catID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(catID, "Test Category"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "size_units" WHERE "size_units"."id" = $1`)).
			WithArgs(unitID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(unitID, "Test Unit"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "inventory_items" WHERE home_id = $1`)).
			WithArgs(homeID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "item_definition_id", "quantity", "expiration_date"}).AddRow(uuid.New(), homeID, itemDefID, 5, nil))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT item_definition_id, SUM(-quantity_change) as total_consumed, MIN(created_at) as first_tx_time, MAX(created_at) as last_tx_time FROM "inventory_transactions" WHERE home_id = $1 AND quantity_change < 0 AND created_at >= $2 GROUP BY "item_definition_id"`)).
			WithArgs(homeID, sqlmock.AnyArg()).
			WillReturnError(errors.New("db error"))

		expectI18n(mock, userID)

		handler.GetAlmostFinishedItems(c)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}
