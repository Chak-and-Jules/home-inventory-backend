package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
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

func setupShoppingListTest(t *testing.T) (*ShoppingListHandler, sqlmock.Sqlmock) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: db}), &gorm.Config{})
	require.NoError(t, err)
	handler := &ShoppingListHandler{DB: gormDB}
	return handler, mock
}

func TestGetShoppingList(t *testing.T) {
	logger.InitLogger()
	gin.SetMode(gin.TestMode)
	userID := uuid.New()
	homeID := uuid.New()

	t.Run("success", func(t *testing.T) {
		handler, mock := setupShoppingListTest(t)
		req, err := http.NewRequest(http.MethodGet, "/shopping-list", nil)
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

		// Preloads for ItemDefinition - GORM alphabetical order: Category then SizeUnit
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

	t.Run("access denied", func(t *testing.T) {
		handler, mock := setupShoppingListTest(t)
		req, err := http.NewRequest(http.MethodGet, "/shopping-list", nil)
		require.NoError(t, err)
		req.Header.Set("X-Home-Id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2 ORDER BY "user_homes"."user_id" LIMIT $3`)).
			WithArgs(userID, homeID, 1).
			WillReturnError(errors.New("not found"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT "id","language_id" FROM "profiles" WHERE id = $1 ORDER BY "profiles"."id" LIMIT $2`)).
			WithArgs(userID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "language_id"}).AddRow(userID, nil))

		handler.GetShoppingList(c)

		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestCreateShoppingListItem(t *testing.T) {
	logger.InitLogger()
	gin.SetMode(gin.TestMode)
	userID := uuid.New()
	homeID := uuid.New()

	t.Run("security - item definition from another home", func(t *testing.T) {
		handler, mock := setupShoppingListTest(t)
		otherHomeID := uuid.New()
		itemDefID := uuid.New()
		payload := ShoppingListItemRequest{
			ItemDefinitionID: &itemDefID,
			Quantity:         1,
		}
		jsonPayload, _ := json.Marshal(payload)
		req, err := http.NewRequest(http.MethodPost, "/shopping-list", bytes.NewBuffer(jsonPayload))
		require.NoError(t, err)
		req.Header.Set("X-Home-Id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2 ORDER BY "user_homes"."user_id" LIMIT $3`)).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, models.RoleOwner))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "item_definitions" WHERE "item_definitions"."id" = $1 ORDER BY "item_definitions"."id" LIMIT $2`)).
			WithArgs(itemDefID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "name", "low_stock_threshold", "target_quantity", "priority"}).
				AddRow(itemDefID, otherHomeID, "Other Item", nil, nil, "medium"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT "id","language_id" FROM "profiles" WHERE id = $1 ORDER BY "profiles"."id" LIMIT $2`)).
			WithArgs(userID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "language_id"}).AddRow(userID, nil))

		handler.CreateShoppingListItem(c)

		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.Contains(t, w.Body.String(), "Item definition does not belong to this home")
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("success manual", func(t *testing.T) {
		handler, mock := setupShoppingListTest(t)
		payload := ShoppingListItemRequest{
			Name:     "Eggs",
			Quantity: 12,
		}
		jsonPayload, _ := json.Marshal(payload)
		req, err := http.NewRequest(http.MethodPost, "/shopping-list", bytes.NewBuffer(jsonPayload))
		require.NoError(t, err)
		req.Header.Set("X-Home-Id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2 ORDER BY "user_homes"."user_id" LIMIT $3`)).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, models.RoleOwner))

		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO "shopping_list_items"`)).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))
		mock.ExpectCommit()

		handler.CreateShoppingListItem(c)

		assert.Equal(t, http.StatusCreated, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestToggleShoppingListItemBought(t *testing.T) {
	logger.InitLogger()
	gin.SetMode(gin.TestMode)
	userID := uuid.New()
	homeID := uuid.New()
	itemID := uuid.New()

	t.Run("success", func(t *testing.T) {
		handler, mock := setupShoppingListTest(t)
		req, err := http.NewRequest(http.MethodPatch, "/shopping-list/"+itemID.String()+"/toggle-bought", nil)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)
		c.Params = gin.Params{{Key: "id", Value: itemID.String()}}

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "shopping_list_items" WHERE "shopping_list_items"."id" = $1 ORDER BY "shopping_list_items"."id" LIMIT $2`)).
			WithArgs(itemID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "is_bought"}).AddRow(itemID, homeID, false))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2 ORDER BY "user_homes"."user_id" LIMIT $3`)).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, models.RoleOwner))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "shopping_list_items" SET "is_bought"=$1,"updated_at"=$2 WHERE "id" = $3`)).
			WithArgs(true, sqlmock.AnyArg(), itemID).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		handler.ToggleShoppingListItemBought(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}
