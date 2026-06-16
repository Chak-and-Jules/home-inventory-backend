package handlers

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
		req.Header.Set("x-home-id", homeID.String())

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
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "item_definition_id", "quantity"}).AddRow(uuid.New(), homeID, itemDefID, 5))

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
		req.Header.Set("x-home-id", homeID.String())

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
		req.Header.Set("x-home-id", homeID.String())

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
		reqBody := `{"item_definition_id": "` + itemDefID.String() + `", "quantity": 5}`
		req, err := http.NewRequest(http.MethodPost, "/inventory", strings.NewReader(reqBody))
		require.NoError(t, err)
		req.Header.Set("x-home-id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND home_id = \$2 ORDER BY "user_homes"\."user_id" LIMIT \$3`).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, models.RoleEditor))

		mock.ExpectBegin()
		mock.ExpectQuery(`INSERT INTO "inventory_items" \("home_id","item_definition_id","quantity","expiration_date","created_at","updated_at"\) VALUES \(\$1,\$2,\$3,\$4,\$5,\$6\) RETURNING "id"`).
			WithArgs(homeID, itemDefID, float64(5), nil, sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))
		mock.ExpectCommit()

		handler.CreateInventoryItem(c)

		assert.Equal(t, http.StatusCreated, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("missing home_id", func(t *testing.T) {
		handler, _ := setupInventoryTest(t)
		req, err := http.NewRequest(http.MethodPost, "/inventory", strings.NewReader(`{"item_definition_id": "`+itemDefID.String()+`", "quantity": 5}`))
		require.NoError(t, err)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		handler.CreateInventoryItem(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("write access denied", func(t *testing.T) {
		handler, mock := setupInventoryTest(t)
		req, err := http.NewRequest(http.MethodPost, "/inventory", strings.NewReader(`{"item_definition_id": "`+itemDefID.String()+`", "quantity": 5}`))
		require.NoError(t, err)
		req.Header.Set("x-home-id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND home_id = \$2 ORDER BY "user_homes"\."user_id" LIMIT \$3`).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, models.RoleViewer))

		handler.CreateInventoryItem(c)

		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("invalid json", func(t *testing.T) {
		handler, mock := setupInventoryTest(t)
		req, err := http.NewRequest(http.MethodPost, "/inventory", strings.NewReader(`{"quantity": -5}`))
		require.NoError(t, err)
		req.Header.Set("x-home-id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND home_id = \$2 ORDER BY "user_homes"\."user_id" LIMIT \$3`).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, models.RoleOwner))

		handler.CreateInventoryItem(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("db error", func(t *testing.T) {
		handler, mock := setupInventoryTest(t)
		reqBody := `{"item_definition_id": "` + itemDefID.String() + `", "quantity": 5}`
		req, err := http.NewRequest(http.MethodPost, "/inventory", strings.NewReader(reqBody))
		require.NoError(t, err)
		req.Header.Set("x-home-id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND home_id = \$2 ORDER BY "user_homes"\."user_id" LIMIT \$3`).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, models.RoleEditor))

		mock.ExpectBegin()
		mock.ExpectQuery(`INSERT INTO "inventory_items" \("home_id","item_definition_id","quantity","expiration_date","created_at","updated_at"\) VALUES \(\$1,\$2,\$3,\$4,\$5,\$6\) RETURNING "id"`).
			WithArgs(homeID, itemDefID, float64(5), nil, sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnError(errors.New("db error"))
		mock.ExpectRollback()

		handler.CreateInventoryItem(c)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "Failed to create inventory item")
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestUpdateInventoryItem(t *testing.T) {
	logger.InitLogger()
	gin.SetMode(gin.TestMode)
	userID := uuid.New()
	homeID := uuid.New()
	itemID := uuid.New()

	t.Run("success", func(t *testing.T) {
		handler, mock := setupInventoryTest(t)
		reqBody := `{"quantity": 10}`
		req, err := http.NewRequest(http.MethodPut, "/inventory/"+itemID.String(), strings.NewReader(reqBody))
		require.NoError(t, err)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: itemID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "inventory_items" WHERE "inventory_items"\."id" = \$1 ORDER BY "inventory_items"\."id" LIMIT \$2`).
			WithArgs(itemID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id"}).AddRow(itemID, homeID))

		mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND home_id = \$2 ORDER BY "user_homes"\."user_id" LIMIT \$3`).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, models.RoleOwner))

		mock.ExpectBegin()
		mock.ExpectExec(`UPDATE "inventory_items" SET .*`).
			WithArgs(sqlmock.AnyArg(), float64(10), sqlmock.AnyArg(), itemID).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		handler.UpdateInventoryItem(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("invalid id", func(t *testing.T) {
		handler, _ := setupInventoryTest(t)
		req, err := http.NewRequest(http.MethodPut, "/inventory/invalid-id", strings.NewReader(`{"quantity": 10}`))
		require.NoError(t, err)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: "invalid-id"}}
		c.Set("userID", userID)

		handler.UpdateInventoryItem(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("item not found", func(t *testing.T) {
		handler, mock := setupInventoryTest(t)
		req, err := http.NewRequest(http.MethodPut, "/inventory/"+itemID.String(), strings.NewReader(`{"quantity": 10}`))
		require.NoError(t, err)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: itemID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "inventory_items" WHERE "inventory_items"\."id" = \$1 ORDER BY "inventory_items"\."id" LIMIT \$2`).
			WithArgs(itemID, 1).
			WillReturnError(errors.New("not found"))

		handler.UpdateInventoryItem(c)

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("write access denied", func(t *testing.T) {
		handler, mock := setupInventoryTest(t)
		req, err := http.NewRequest(http.MethodPut, "/inventory/"+itemID.String(), strings.NewReader(`{"quantity": 10}`))
		require.NoError(t, err)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: itemID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "inventory_items" WHERE "inventory_items"\."id" = \$1 ORDER BY "inventory_items"\."id" LIMIT \$2`).
			WithArgs(itemID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id"}).AddRow(itemID, homeID))

		mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND home_id = \$2 ORDER BY "user_homes"\."user_id" LIMIT \$3`).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, models.RoleViewer))

		handler.UpdateInventoryItem(c)

		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("invalid json", func(t *testing.T) {
		handler, mock := setupInventoryTest(t)
		req, err := http.NewRequest(http.MethodPut, "/inventory/"+itemID.String(), strings.NewReader(`{"quantity": -10}`))
		require.NoError(t, err)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: itemID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "inventory_items" WHERE "inventory_items"\."id" = \$1 ORDER BY "inventory_items"\."id" LIMIT \$2`).
			WithArgs(itemID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id"}).AddRow(itemID, homeID))

		mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND home_id = \$2 ORDER BY "user_homes"\."user_id" LIMIT \$3`).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, models.RoleOwner))

		handler.UpdateInventoryItem(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("update error", func(t *testing.T) {
		handler, mock := setupInventoryTest(t)
		reqBody := `{"quantity": 10}`
		req, err := http.NewRequest(http.MethodPut, "/inventory/"+itemID.String(), strings.NewReader(reqBody))
		require.NoError(t, err)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: itemID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "inventory_items" WHERE "inventory_items"\."id" = \$1 ORDER BY "inventory_items"\."id" LIMIT \$2`).
			WithArgs(itemID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id"}).AddRow(itemID, homeID))

		mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND home_id = \$2 ORDER BY "user_homes"\."user_id" LIMIT \$3`).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, models.RoleOwner))

		mock.ExpectBegin()
		mock.ExpectExec(`UPDATE "inventory_items" SET .*`).
			WithArgs(sqlmock.AnyArg(), float64(10), sqlmock.AnyArg(), itemID).
			WillReturnError(errors.New("update error"))
		mock.ExpectRollback()

		handler.UpdateInventoryItem(c)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestUpdateInventoryItemQuantity(t *testing.T) {
	logger.InitLogger()
	gin.SetMode(gin.TestMode)
	userID := uuid.New()
	homeID := uuid.New()
	itemID := uuid.New()

	t.Run("success", func(t *testing.T) {
		handler, mock := setupInventoryTest(t)
		reqBody := `{"quantity": 15}`
		req, err := http.NewRequest(http.MethodPatch, "/inventory/"+itemID.String()+"/quantity", strings.NewReader(reqBody))
		require.NoError(t, err)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: itemID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "inventory_items" WHERE "inventory_items"\."id" = \$1 ORDER BY "inventory_items"\."id" LIMIT \$2`).
			WithArgs(itemID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id"}).AddRow(itemID, homeID))

		mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND home_id = \$2 ORDER BY "user_homes"\."user_id" LIMIT \$3`).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, models.RoleOwner))

		mock.ExpectBegin()
		mock.ExpectExec(`UPDATE "inventory_items" SET .*`).
			WithArgs(float64(15), sqlmock.AnyArg(), itemID).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		handler.UpdateInventoryItemQuantity(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("invalid id", func(t *testing.T) {
		handler, _ := setupInventoryTest(t)
		req, err := http.NewRequest(http.MethodPatch, "/inventory/invalid-id/quantity", strings.NewReader(`{"quantity": 15}`))
		require.NoError(t, err)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: "invalid-id"}}
		c.Set("userID", userID)

		handler.UpdateInventoryItemQuantity(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("item not found", func(t *testing.T) {
		handler, mock := setupInventoryTest(t)
		req, err := http.NewRequest(http.MethodPatch, "/inventory/"+itemID.String()+"/quantity", strings.NewReader(`{"quantity": 15}`))
		require.NoError(t, err)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: itemID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "inventory_items" WHERE "inventory_items"\."id" = \$1 ORDER BY "inventory_items"\."id" LIMIT \$2`).
			WithArgs(itemID, 1).
			WillReturnError(errors.New("not found"))

		handler.UpdateInventoryItemQuantity(c)

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("write access denied", func(t *testing.T) {
		handler, mock := setupInventoryTest(t)
		req, err := http.NewRequest(http.MethodPatch, "/inventory/"+itemID.String()+"/quantity", strings.NewReader(`{"quantity": 15}`))
		require.NoError(t, err)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: itemID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "inventory_items" WHERE "inventory_items"\."id" = \$1 ORDER BY "inventory_items"\."id" LIMIT \$2`).
			WithArgs(itemID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id"}).AddRow(itemID, homeID))

		mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND home_id = \$2 ORDER BY "user_homes"\."user_id" LIMIT \$3`).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, models.RoleViewer))

		handler.UpdateInventoryItemQuantity(c)

		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("invalid json", func(t *testing.T) {
		handler, mock := setupInventoryTest(t)
		req, err := http.NewRequest(http.MethodPatch, "/inventory/"+itemID.String()+"/quantity", strings.NewReader(`{"quantity": -15}`))
		require.NoError(t, err)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: itemID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "inventory_items" WHERE "inventory_items"\."id" = \$1 ORDER BY "inventory_items"\."id" LIMIT \$2`).
			WithArgs(itemID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id"}).AddRow(itemID, homeID))

		mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND home_id = \$2 ORDER BY "user_homes"\."user_id" LIMIT \$3`).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, models.RoleOwner))

		handler.UpdateInventoryItemQuantity(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("update error", func(t *testing.T) {
		handler, mock := setupInventoryTest(t)
		reqBody := `{"quantity": 15}`
		req, err := http.NewRequest(http.MethodPatch, "/inventory/"+itemID.String()+"/quantity", strings.NewReader(reqBody))
		require.NoError(t, err)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: itemID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "inventory_items" WHERE "inventory_items"\."id" = \$1 ORDER BY "inventory_items"\."id" LIMIT \$2`).
			WithArgs(itemID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id"}).AddRow(itemID, homeID))

		mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND home_id = \$2 ORDER BY "user_homes"\."user_id" LIMIT \$3`).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, models.RoleOwner))

		mock.ExpectBegin()
		mock.ExpectExec(`UPDATE "inventory_items" SET .*`).
			WithArgs(float64(15), sqlmock.AnyArg(), itemID).
			WillReturnError(errors.New("update error"))
		mock.ExpectRollback()

		handler.UpdateInventoryItemQuantity(c)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestDeleteInventoryItem(t *testing.T) {
	logger.InitLogger()
	gin.SetMode(gin.TestMode)
	userID := uuid.New()
	homeID := uuid.New()
	itemID := uuid.New()

	t.Run("success", func(t *testing.T) {
		handler, mock := setupInventoryTest(t)
		req, err := http.NewRequest(http.MethodDelete, "/inventory/"+itemID.String(), nil)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: itemID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "inventory_items" WHERE "inventory_items"\."id" = \$1 ORDER BY "inventory_items"\."id" LIMIT \$2`).
			WithArgs(itemID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id"}).AddRow(itemID, homeID))

		mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND home_id = \$2 ORDER BY "user_homes"\."user_id" LIMIT \$3`).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, models.RoleOwner))

		mock.ExpectBegin()
		mock.ExpectExec(`DELETE FROM "inventory_items" WHERE ".*"."id" = \$1`).
			WithArgs(itemID).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()

		handler.DeleteInventoryItem(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("invalid id", func(t *testing.T) {
		handler, _ := setupInventoryTest(t)
		req, err := http.NewRequest(http.MethodDelete, "/inventory/invalid-id", nil)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: "invalid-id"}}
		c.Set("userID", userID)

		handler.DeleteInventoryItem(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("item not found", func(t *testing.T) {
		handler, mock := setupInventoryTest(t)
		req, err := http.NewRequest(http.MethodDelete, "/inventory/"+itemID.String(), nil)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: itemID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "inventory_items" WHERE "inventory_items"\."id" = \$1 ORDER BY "inventory_items"\."id" LIMIT \$2`).
			WithArgs(itemID, 1).
			WillReturnError(errors.New("not found"))

		handler.DeleteInventoryItem(c)

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("access denied", func(t *testing.T) {
		handler, mock := setupInventoryTest(t)
		req, err := http.NewRequest(http.MethodDelete, "/inventory/"+itemID.String(), nil)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: itemID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "inventory_items" WHERE "inventory_items"\."id" = \$1 ORDER BY "inventory_items"\."id" LIMIT \$2`).
			WithArgs(itemID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id"}).AddRow(itemID, homeID))

		mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND home_id = \$2 ORDER BY "user_homes"\."user_id" LIMIT \$3`).
			WithArgs(userID, homeID, 1).
			WillReturnError(errors.New("not found"))

		handler.DeleteInventoryItem(c)

		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("delete error", func(t *testing.T) {
		handler, mock := setupInventoryTest(t)
		req, err := http.NewRequest(http.MethodDelete, "/inventory/"+itemID.String(), nil)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: itemID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "inventory_items" WHERE "inventory_items"\."id" = \$1 ORDER BY "inventory_items"\."id" LIMIT \$2`).
			WithArgs(itemID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id"}).AddRow(itemID, homeID))

		mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND home_id = \$2 ORDER BY "user_homes"\."user_id" LIMIT \$3`).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, models.RoleOwner))

		mock.ExpectBegin()
		mock.ExpectExec(`DELETE FROM "inventory_items" WHERE ".*"."id" = \$1`).
			WithArgs(itemID).
			WillReturnError(errors.New("delete error"))
		mock.ExpectRollback()

		handler.DeleteInventoryItem(c)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}
