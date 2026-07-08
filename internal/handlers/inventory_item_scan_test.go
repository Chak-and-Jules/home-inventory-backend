package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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

func TestScanInventoryItem(t *testing.T) {
	logger.InitLogger()
	gin.SetMode(gin.TestMode)
	userID := uuid.New()
	homeID := uuid.New()
	barcode := "123456"

	t.Run("success increment new item", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: db}), &gorm.Config{})
		require.NoError(t, err)
		handler := &InventoryItemHandler{DB: gormDB}

		reqBody := ScanInventoryRequest{
			Barcode: barcode,
			Change:  1,
		}
		jsonBody, _ := json.Marshal(reqBody)
		req, _ := http.NewRequest(http.MethodPost, "/inventory/scan", bytes.NewBuffer(jsonBody))
		req.Header.Set("X-Home-Id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND home_id = \$2`).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "owner"))

		itemDefID := uuid.New()
		mock.ExpectQuery(`SELECT \* FROM "item_definitions" WHERE home_id = \$1 AND barcode = \$2`).
			WithArgs(homeID, barcode, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "barcode"}).AddRow(itemDefID, homeID, barcode))

		mock.ExpectBegin()
		mock.ExpectQuery(`SELECT \* FROM "inventory_items" WHERE home_id = \$1 AND item_definition_id = \$2 ORDER BY expiration_date ASC NULLS LAST`).
			WithArgs(homeID, itemDefID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "item_definition_id", "quantity"})) // No rows found

		mock.ExpectQuery(`INSERT INTO "inventory_items"`).
			WithArgs(homeID, itemDefID, 1.0, nil, sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))

		mock.ExpectQuery(`INSERT INTO "inventory_transactions"`).
			WithArgs(homeID, itemDefID, sqlmock.AnyArg(), 1.0, sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))

		mock.ExpectQuery(`SELECT \* FROM "item_definitions" WHERE "item_definitions"\."id" = \$1`).
			WithArgs(itemDefID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "name"}).AddRow(itemDefID, homeID, "Milk"))

		mock.ExpectQuery(`SELECT \* FROM "shopping_list_items" WHERE home_id = \$1 AND item_definition_id = \$2 AND is_auto_generated = \$3 AND is_bought = \$4`).
			WithArgs(homeID, itemDefID, true, false, 1).
			WillReturnError(gorm.ErrRecordNotFound)

		mock.ExpectCommit()

		handler.ScanInventoryItem(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("item definition not found", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: db}), &gorm.Config{})
		require.NoError(t, err)
		handler := &InventoryItemHandler{DB: gormDB}

		i18n.InvalidateUserLanguageCache(userID)

		reqBody := ScanInventoryRequest{
			Barcode: "unknown",
			Change:  1,
		}
		jsonBody, _ := json.Marshal(reqBody)
		req, _ := http.NewRequest(http.MethodPost, "/inventory/scan", bytes.NewBuffer(jsonBody))
		req.Header.Set("X-Home-Id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND home_id = \$2`).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, "owner"))

		mock.ExpectQuery(`SELECT \* FROM "item_definitions" WHERE home_id = \$1 AND barcode = \$2`).
			WithArgs(homeID, "unknown", 1).
			WillReturnError(gorm.ErrRecordNotFound)

		mock.ExpectQuery(`SELECT "id","language_id" FROM "profiles" WHERE id = \$1 ORDER BY "profiles"\."id" LIMIT \$2`).
			WithArgs(userID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "language_id"}).AddRow(userID, nil))

		handler.ScanInventoryItem(c)

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}
