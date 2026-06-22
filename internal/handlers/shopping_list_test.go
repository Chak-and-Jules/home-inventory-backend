package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

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

func setupShoppingListTest(t *testing.T) (*ShoppingListHandler, sqlmock.Sqlmock) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: db}), &gorm.Config{})
	require.NoError(t, err)
	handler := &ShoppingListHandler{DB: gormDB}
	return handler, mock
}

func expectProfileLookupSL(mock sqlmock.Sqlmock, userID uuid.UUID) {
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT "id","language_id" FROM "profiles" WHERE id = $1 ORDER BY "profiles"."id" LIMIT $2`)).
		WithArgs(userID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "language_id"}).AddRow(userID, nil))
}

func expectHomeReadAccessSL(mock sqlmock.Sqlmock, userID, homeID uuid.UUID, allowed bool) {
	query := mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2 ORDER BY "user_homes"."user_id" LIMIT $3`))
	if allowed {
		query.WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, models.RoleOwner))
	} else {
		query.WithArgs(userID, homeID, 1).
			WillReturnError(gorm.ErrRecordNotFound)
	}
}

func expectHomeWriteAccessSL(mock sqlmock.Sqlmock, userID, homeID uuid.UUID, allowed bool) {
	query := mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2 ORDER BY "user_homes"."user_id" LIMIT $3`))
	if allowed {
		query.WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, models.RoleOwner))
	} else {
		query.WithArgs(userID, homeID, 1).
			WillReturnError(gorm.ErrRecordNotFound)
	}
}

func TestGetShoppingList(t *testing.T) {
	logger.InitLogger()
	gin.SetMode(gin.TestMode)

	t.Run("success", func(t *testing.T) {
		handler, mock := setupShoppingListTest(t)
		userID := uuid.New()
		homeID := uuid.New()
		i18n.InvalidateUserLanguageCache(userID)

		req, _ := http.NewRequest(http.MethodGet, "/shopping-list", nil)
		req.Header.Set("X-Home-Id", homeID.String())
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		expectHomeReadAccessSL(mock, userID, homeID, true)

		itemDefID := uuid.New()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "shopping_list_items" WHERE home_id = $1 ORDER BY is_bought ASC, created_at DESC`)).
			WithArgs(homeID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "item_definition_id", "name", "quantity", "is_bought", "is_auto_generated"}).
				AddRow(uuid.New(), homeID, &itemDefID, "Milk", 2.0, false, true))

		sizeUnitID := uuid.New()
		catID := uuid.New()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "item_definitions" WHERE "item_definitions"."id" = $1`)).
			WithArgs(itemDefID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "name", "size_unit_id", "category_id", "low_stock_threshold", "target_quantity", "priority"}).
				AddRow(itemDefID, homeID, "Milk", sizeUnitID, catID, nil, nil, "medium"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "categories" WHERE "categories"."id" = $1`)).
			WithArgs(catID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(catID, "Dairy"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "size_units" WHERE "size_units"."id" = $1`)).
			WithArgs(sizeUnitID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(sizeUnitID, "Liters"))

		handler.GetShoppingList(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		handler, mock := setupShoppingListTest(t)
		userID := uuid.New()
		homeID := uuid.New()
		i18n.InvalidateUserLanguageCache(userID)

		req, _ := http.NewRequest(http.MethodGet, "/shopping-list", nil)
		req.Header.Set("X-Home-Id", homeID.String())
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		expectHomeReadAccessSL(mock, userID, homeID, true)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "shopping_list_items" WHERE home_id = $1 ORDER BY is_bought ASC, created_at DESC`)).
			WithArgs(homeID).
			WillReturnError(errors.New("db error"))

		expectProfileLookupSL(mock, userID)

		handler.GetShoppingList(c)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("access denied", func(t *testing.T) {
		handler, mock := setupShoppingListTest(t)
		userID := uuid.New()
		homeID := uuid.New()
		i18n.InvalidateUserLanguageCache(userID)

		req, _ := http.NewRequest(http.MethodGet, "/shopping-list", nil)
		req.Header.Set("X-Home-Id", homeID.String())
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		expectHomeReadAccessSL(mock, userID, homeID, false)
		expectProfileLookupSL(mock, userID)

		handler.GetShoppingList(c)

		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("invalid home id header", func(t *testing.T) {
		handler, mock := setupShoppingListTest(t)
		userID := uuid.New()
		i18n.InvalidateUserLanguageCache(userID)

		req, _ := http.NewRequest(http.MethodGet, "/shopping-list", nil)
		req.Header.Set("X-Home-Id", "invalid-uuid")
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		expectProfileLookupSL(mock, userID)

		handler.GetShoppingList(c)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestCreateShoppingListItem(t *testing.T) {
	logger.InitLogger()
	gin.SetMode(gin.TestMode)

	t.Run("security - item definition from another home", func(t *testing.T) {
		handler, mock := setupShoppingListTest(t)
		userID := uuid.New()
		homeID := uuid.New()
		otherHomeID := uuid.New()
		itemDefID := uuid.New()
		i18n.InvalidateUserLanguageCache(userID)

		payload := ShoppingListItemRequest{
			ItemDefinitionID: &itemDefID,
			Quantity:         1,
		}
		jsonPayload, _ := json.Marshal(payload)
		req, _ := http.NewRequest(http.MethodPost, "/shopping-list", bytes.NewBuffer(jsonPayload))
		req.Header.Set("X-Home-Id", homeID.String())
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		expectHomeWriteAccessSL(mock, userID, homeID, true)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "item_definitions" WHERE "item_definitions"."id" = $1 ORDER BY "item_definitions"."id" LIMIT $2`)).
			WithArgs(itemDefID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "name", "low_stock_threshold", "target_quantity", "priority"}).
				AddRow(itemDefID, otherHomeID, "Other Item", nil, nil, "medium"))

		expectProfileLookupSL(mock, userID)

		handler.CreateShoppingListItem(c)

		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("success manual", func(t *testing.T) {
		handler, mock := setupShoppingListTest(t)
		userID := uuid.New()
		homeID := uuid.New()
		i18n.InvalidateUserLanguageCache(userID)

		payload := ShoppingListItemRequest{
			Name:     "Eggs",
			Quantity: 12,
		}
		jsonPayload, _ := json.Marshal(payload)
		req, _ := http.NewRequest(http.MethodPost, "/shopping-list", bytes.NewBuffer(jsonPayload))
		req.Header.Set("X-Home-Id", homeID.String())
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		expectHomeWriteAccessSL(mock, userID, homeID, true)

		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO "shopping_list_items"`)).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))
		mock.ExpectCommit()

		handler.CreateShoppingListItem(c)

		assert.Equal(t, http.StatusCreated, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("invalid payload", func(t *testing.T) {
		handler, mock := setupShoppingListTest(t)
		userID := uuid.New()
		homeID := uuid.New()
		i18n.InvalidateUserLanguageCache(userID)

		req, _ := http.NewRequest(http.MethodPost, "/shopping-list", bytes.NewBufferString("invalid"))
		req.Header.Set("X-Home-Id", homeID.String())
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		expectHomeWriteAccessSL(mock, userID, homeID, true)
		expectProfileLookupSL(mock, userID)

		handler.CreateShoppingListItem(c)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error on create", func(t *testing.T) {
		handler, mock := setupShoppingListTest(t)
		userID := uuid.New()
		homeID := uuid.New()
		i18n.InvalidateUserLanguageCache(userID)

		payload := ShoppingListItemRequest{Name: "Milk", Quantity: 1}
		jsonPayload, _ := json.Marshal(payload)
		req, _ := http.NewRequest(http.MethodPost, "/shopping-list", bytes.NewBuffer(jsonPayload))
		req.Header.Set("X-Home-Id", homeID.String())
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		expectHomeWriteAccessSL(mock, userID, homeID, true)
		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO "shopping_list_items"`)).
			WillReturnError(errors.New("db error"))
		mock.ExpectRollback()
		expectProfileLookupSL(mock, userID)
		handler.CreateShoppingListItem(c)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("invalid home id header", func(t *testing.T) {
		handler, mock := setupShoppingListTest(t)
		userID := uuid.New()
		i18n.InvalidateUserLanguageCache(userID)

		req, _ := http.NewRequest(http.MethodPost, "/shopping-list", nil)
		req.Header.Set("X-Home-Id", "invalid")
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		expectProfileLookupSL(mock, userID)

		handler.CreateShoppingListItem(c)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("unauthorized", func(t *testing.T) {
		handler, mock := setupShoppingListTest(t)
		userID := uuid.New()
		homeID := uuid.New()
		i18n.InvalidateUserLanguageCache(userID)

		req, _ := http.NewRequest(http.MethodPost, "/shopping-list", nil)
		req.Header.Set("X-Home-Id", homeID.String())
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		expectHomeWriteAccessSL(mock, userID, homeID, false)
		expectProfileLookupSL(mock, userID)

		handler.CreateShoppingListItem(c)
		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("item definition not found", func(t *testing.T) {
		handler, mock := setupShoppingListTest(t)
		userID := uuid.New()
		homeID := uuid.New()
		itemDefID := uuid.New()
		i18n.InvalidateUserLanguageCache(userID)

		payload := ShoppingListItemRequest{
			ItemDefinitionID: &itemDefID,
			Quantity:         1,
		}
		jsonPayload, _ := json.Marshal(payload)
		req, _ := http.NewRequest(http.MethodPost, "/shopping-list", bytes.NewBuffer(jsonPayload))
		req.Header.Set("X-Home-Id", homeID.String())
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		expectHomeWriteAccessSL(mock, userID, homeID, true)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "item_definitions" WHERE "item_definitions"."id" = $1 ORDER BY "item_definitions"."id" LIMIT $2`)).
			WithArgs(itemDefID, 1).
			WillReturnError(gorm.ErrRecordNotFound)

		expectProfileLookupSL(mock, userID)

		handler.CreateShoppingListItem(c)
		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("missing name and item def", func(t *testing.T) {
		handler, mock := setupShoppingListTest(t)
		userID := uuid.New()
		homeID := uuid.New()
		i18n.InvalidateUserLanguageCache(userID)

		payload := ShoppingListItemRequest{Quantity: 1}
		jsonPayload, _ := json.Marshal(payload)
		req, _ := http.NewRequest(http.MethodPost, "/shopping-list", bytes.NewBuffer(jsonPayload))
		req.Header.Set("X-Home-Id", homeID.String())
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		expectHomeWriteAccessSL(mock, userID, homeID, true)
		expectProfileLookupSL(mock, userID)

		handler.CreateShoppingListItem(c)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestUpdateShoppingListItem(t *testing.T) {
	logger.InitLogger()
	gin.SetMode(gin.TestMode)

	t.Run("success", func(t *testing.T) {
		handler, mock := setupShoppingListTest(t)
		userID := uuid.New()
		homeID := uuid.New()
		itemID := uuid.New()
		i18n.InvalidateUserLanguageCache(userID)

		payload := UpdateShoppingListItemRequest{
			Quantity: 5,
			IsBought: true,
		}
		jsonPayload, _ := json.Marshal(payload)
		req, _ := http.NewRequest(http.MethodPut, "/shopping-list/"+itemID.String(), bytes.NewBuffer(jsonPayload))
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)
		c.Params = gin.Params{{Key: "id", Value: itemID.String()}}

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "shopping_list_items" WHERE "shopping_list_items"."id" = $1 ORDER BY "shopping_list_items"."id" LIMIT $2`)).
			WithArgs(itemID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id"}).AddRow(itemID, homeID))

		expectHomeWriteAccessSL(mock, userID, homeID, true)

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "shopping_list_items" SET "is_bought"=$1,"quantity"=$2,"updated_at"=$3 WHERE "id" = $4`)).
			WithArgs(true, 5.0, sqlmock.AnyArg(), itemID).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		handler.UpdateShoppingListItem(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		handler, mock := setupShoppingListTest(t)
		userID := uuid.New()
		itemID := uuid.New()
		i18n.InvalidateUserLanguageCache(userID)

		req, _ := http.NewRequest(http.MethodPut, "/shopping-list/"+itemID.String(), nil)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: itemID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "shopping_list_items" WHERE "shopping_list_items"."id" = $1 ORDER BY "shopping_list_items"."id" LIMIT $2`)).
			WillReturnError(gorm.ErrRecordNotFound)

		expectProfileLookupSL(mock, userID)

		handler.UpdateShoppingListItem(c)
		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("invalid id param", func(t *testing.T) {
		handler, mock := setupShoppingListTest(t)
		userID := uuid.New()
		i18n.InvalidateUserLanguageCache(userID)

		req, _ := http.NewRequest(http.MethodPut, "/shopping-list/invalid", nil)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)
		c.Params = gin.Params{{Key: "id", Value: "invalid"}}

		expectProfileLookupSL(mock, userID)

		handler.UpdateShoppingListItem(c)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("unauthorized", func(t *testing.T) {
		handler, mock := setupShoppingListTest(t)
		userID := uuid.New()
		homeID := uuid.New()
		itemID := uuid.New()
		i18n.InvalidateUserLanguageCache(userID)

		req, _ := http.NewRequest(http.MethodPut, "/shopping-list/"+itemID.String(), nil)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)
		c.Params = gin.Params{{Key: "id", Value: itemID.String()}}

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "shopping_list_items" WHERE "shopping_list_items"."id" = $1 ORDER BY "shopping_list_items"."id" LIMIT $2`)).
			WithArgs(itemID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id"}).AddRow(itemID, homeID))

		expectHomeWriteAccessSL(mock, userID, homeID, false)
		expectProfileLookupSL(mock, userID)

		handler.UpdateShoppingListItem(c)
		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("invalid payload", func(t *testing.T) {
		handler, mock := setupShoppingListTest(t)
		userID := uuid.New()
		homeID := uuid.New()
		itemID := uuid.New()
		i18n.InvalidateUserLanguageCache(userID)

		req, _ := http.NewRequest(http.MethodPut, "/shopping-list/"+itemID.String(), bytes.NewBufferString("invalid"))
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)
		c.Params = gin.Params{{Key: "id", Value: itemID.String()}}

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "shopping_list_items" WHERE "shopping_list_items"."id" = $1 ORDER BY "shopping_list_items"."id" LIMIT $2`)).
			WithArgs(itemID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id"}).AddRow(itemID, homeID))

		expectHomeWriteAccessSL(mock, userID, homeID, true)
		expectProfileLookupSL(mock, userID)

		handler.UpdateShoppingListItem(c)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error on update", func(t *testing.T) {
		handler, mock := setupShoppingListTest(t)
		userID := uuid.New()
		homeID := uuid.New()
		itemID := uuid.New()
		i18n.InvalidateUserLanguageCache(userID)

		payload := UpdateShoppingListItemRequest{Quantity: 5}
		jsonPayload, _ := json.Marshal(payload)
		req, _ := http.NewRequest(http.MethodPut, "/shopping-list/"+itemID.String(), bytes.NewBuffer(jsonPayload))
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)
		c.Params = gin.Params{{Key: "id", Value: itemID.String()}}

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "shopping_list_items" WHERE "shopping_list_items"."id" = $1 ORDER BY "shopping_list_items"."id" LIMIT $2`)).
			WithArgs(itemID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id"}).AddRow(itemID, homeID))

		expectHomeWriteAccessSL(mock, userID, homeID, true)

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "shopping_list_items" SET "is_bought"=$1,"quantity"=$2,"updated_at"=$3 WHERE "id" = $4`)).
			WillReturnError(errors.New("db error"))
		mock.ExpectRollback()

		expectProfileLookupSL(mock, userID)

		handler.UpdateShoppingListItem(c)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestToggleShoppingListItemBought(t *testing.T) {
	logger.InitLogger()
	gin.SetMode(gin.TestMode)

	t.Run("success", func(t *testing.T) {
		handler, mock := setupShoppingListTest(t)
		userID := uuid.New()
		homeID := uuid.New()
		itemID := uuid.New()
		i18n.InvalidateUserLanguageCache(userID)

		req, _ := http.NewRequest(http.MethodPatch, "/shopping-list/"+itemID.String()+"/toggle-bought", nil)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)
		c.Params = gin.Params{{Key: "id", Value: itemID.String()}}

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "shopping_list_items" WHERE "shopping_list_items"."id" = $1 ORDER BY "shopping_list_items"."id" LIMIT $2`)).
			WithArgs(itemID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "is_bought"}).AddRow(itemID, homeID, false))

		expectHomeWriteAccessSL(mock, userID, homeID, true)

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "shopping_list_items" SET "is_bought"=$1,"updated_at"=$2 WHERE "id" = $3`)).
			WithArgs(true, sqlmock.AnyArg(), itemID).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		handler.ToggleShoppingListItemBought(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		handler, mock := setupShoppingListTest(t)
		userID := uuid.New()
		itemID := uuid.New()
		i18n.InvalidateUserLanguageCache(userID)

		req, _ := http.NewRequest(http.MethodPatch, "/shopping-list/"+itemID.String()+"/toggle-bought", nil)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: itemID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "shopping_list_items" WHERE "shopping_list_items"."id" = $1 ORDER BY "shopping_list_items"."id" LIMIT $2`)).
			WillReturnError(gorm.ErrRecordNotFound)

		expectProfileLookupSL(mock, userID)

		handler.ToggleShoppingListItemBought(c)
		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("invalid id param", func(t *testing.T) {
		handler, mock := setupShoppingListTest(t)
		userID := uuid.New()
		i18n.InvalidateUserLanguageCache(userID)

		req, _ := http.NewRequest(http.MethodPatch, "/shopping-list/invalid/toggle-bought", nil)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)
		c.Params = gin.Params{{Key: "id", Value: "invalid"}}

		expectProfileLookupSL(mock, userID)

		handler.ToggleShoppingListItemBought(c)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("unauthorized", func(t *testing.T) {
		handler, mock := setupShoppingListTest(t)
		userID := uuid.New()
		homeID := uuid.New()
		itemID := uuid.New()
		i18n.InvalidateUserLanguageCache(userID)

		req, _ := http.NewRequest(http.MethodPatch, "/shopping-list/"+itemID.String()+"/toggle-bought", nil)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)
		c.Params = gin.Params{{Key: "id", Value: itemID.String()}}

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "shopping_list_items" WHERE "shopping_list_items"."id" = $1 ORDER BY "shopping_list_items"."id" LIMIT $2`)).
			WithArgs(itemID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "is_bought"}).AddRow(itemID, homeID, false))

		expectHomeWriteAccessSL(mock, userID, homeID, false)
		expectProfileLookupSL(mock, userID)

		handler.ToggleShoppingListItemBought(c)
		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error on toggle", func(t *testing.T) {
		handler, mock := setupShoppingListTest(t)
		userID := uuid.New()
		homeID := uuid.New()
		itemID := uuid.New()
		i18n.InvalidateUserLanguageCache(userID)

		req, _ := http.NewRequest(http.MethodPatch, "/shopping-list/"+itemID.String()+"/toggle-bought", nil)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: itemID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "shopping_list_items" WHERE "shopping_list_items"."id" = $1 ORDER BY "shopping_list_items"."id" LIMIT $2`)).
			WithArgs(itemID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "is_bought"}).AddRow(itemID, homeID, false))

		expectHomeWriteAccessSL(mock, userID, homeID, true)

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "shopping_list_items" SET "is_bought"=$1,"updated_at"=$2 WHERE "id" = $3`)).
			WillReturnError(errors.New("db error"))
		mock.ExpectRollback()

		expectProfileLookupSL(mock, userID)

		handler.ToggleShoppingListItemBought(c)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestDeleteShoppingListItem(t *testing.T) {
	logger.InitLogger()
	gin.SetMode(gin.TestMode)

	t.Run("success", func(t *testing.T) {
		handler, mock := setupShoppingListTest(t)
		userID := uuid.New()
		homeID := uuid.New()
		itemID := uuid.New()
		i18n.InvalidateUserLanguageCache(userID)

		req, _ := http.NewRequest(http.MethodDelete, "/shopping-list/"+itemID.String(), nil)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)
		c.Params = gin.Params{{Key: "id", Value: itemID.String()}}

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "shopping_list_items" WHERE "shopping_list_items"."id" = $1 ORDER BY "shopping_list_items"."id" LIMIT $2`)).
			WithArgs(itemID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id"}).AddRow(itemID, homeID))

		expectHomeWriteAccessSL(mock, userID, homeID, true)

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM "shopping_list_items" WHERE "shopping_list_items"."id" = $1`)).
			WithArgs(itemID).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()

		handler.DeleteShoppingListItem(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		handler, mock := setupShoppingListTest(t)
		userID := uuid.New()
		itemID := uuid.New()
		i18n.InvalidateUserLanguageCache(userID)

		req, _ := http.NewRequest(http.MethodDelete, "/shopping-list/"+itemID.String(), nil)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: itemID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "shopping_list_items" WHERE "shopping_list_items"."id" = $1 ORDER BY "shopping_list_items"."id" LIMIT $2`)).
			WillReturnError(gorm.ErrRecordNotFound)

		expectProfileLookupSL(mock, userID)

		handler.DeleteShoppingListItem(c)
		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("invalid id param", func(t *testing.T) {
		handler, mock := setupShoppingListTest(t)
		userID := uuid.New()
		i18n.InvalidateUserLanguageCache(userID)

		req, _ := http.NewRequest(http.MethodDelete, "/shopping-list/invalid", nil)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)
		c.Params = gin.Params{{Key: "id", Value: "invalid"}}

		expectProfileLookupSL(mock, userID)

		handler.DeleteShoppingListItem(c)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("unauthorized", func(t *testing.T) {
		handler, mock := setupShoppingListTest(t)
		userID := uuid.New()
		homeID := uuid.New()
		itemID := uuid.New()
		i18n.InvalidateUserLanguageCache(userID)

		req, _ := http.NewRequest(http.MethodDelete, "/shopping-list/"+itemID.String(), nil)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)
		c.Params = gin.Params{{Key: "id", Value: itemID.String()}}

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "shopping_list_items" WHERE "shopping_list_items"."id" = $1 ORDER BY "shopping_list_items"."id" LIMIT $2`)).
			WithArgs(itemID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id"}).AddRow(itemID, homeID))

		expectHomeWriteAccessSL(mock, userID, homeID, false)
		expectProfileLookupSL(mock, userID)

		handler.DeleteShoppingListItem(c)
		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error on delete", func(t *testing.T) {
		handler, mock := setupShoppingListTest(t)
		userID := uuid.New()
		homeID := uuid.New()
		itemID := uuid.New()
		i18n.InvalidateUserLanguageCache(userID)

		req, _ := http.NewRequest(http.MethodDelete, "/shopping-list/"+itemID.String(), nil)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: itemID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "shopping_list_items" WHERE "shopping_list_items"."id" = $1 ORDER BY "shopping_list_items"."id" LIMIT $2`)).
			WithArgs(itemID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id"}).AddRow(itemID, homeID))

		expectHomeWriteAccessSL(mock, userID, homeID, true)

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM "shopping_list_items" WHERE "shopping_list_items"."id" = $1`)).
			WillReturnError(errors.New("db error"))
		mock.ExpectRollback()

		expectProfileLookupSL(mock, userID)

		handler.DeleteShoppingListItem(c)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}
