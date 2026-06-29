package utils

import (
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/logger"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestUpdateShoppingListForDefinition(t *testing.T) {
	logger.InitLogger()

	homeID := uuid.New()
	itemDefID := uuid.New()

	t.Run("threshold met - create new item", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: db}), &gorm.Config{})
		require.NoError(t, err)

		threshold := 5.0
		target := 10.0
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "item_definitions" WHERE "item_definitions"."id" = $1 ORDER BY "item_definitions"."id" LIMIT $2`)).
			WithArgs(itemDefID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "name", "low_stock_threshold", "target_quantity", "priority"}).
				AddRow(itemDefID, homeID, "Milk", &threshold, &target, "medium"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "shopping_list_items" WHERE home_id = $1 AND item_definition_id = $2 AND is_auto_generated = $3 AND is_bought = $4 ORDER BY "shopping_list_items"."id" LIMIT $5`)).
			WithArgs(homeID, itemDefID, true, false, 1).
			WillReturnError(gorm.ErrRecordNotFound)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT COALESCE(SUM(quantity), 0) FROM "inventory_items" WHERE home_id = $1 AND item_definition_id = $2 AND (expiration_date IS NULL OR expiration_date > NOW())`)).
			WithArgs(homeID, itemDefID).
			WillReturnRows(sqlmock.NewRows([]string{"sum"}).AddRow(2.0))

		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO "shopping_list_items"`)).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))
		mock.ExpectCommit()

		err = UpdateShoppingListForDefinition(gormDB, homeID, itemDefID)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("threshold met - update existing item", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: db}), &gorm.Config{})
		require.NoError(t, err)

		threshold := 5.0
		target := 10.0
		shoppingItemID := uuid.New()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "item_definitions" WHERE "item_definitions"."id" = $1 ORDER BY "item_definitions"."id" LIMIT $2`)).
			WithArgs(itemDefID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "name", "low_stock_threshold", "target_quantity", "priority"}).
				AddRow(itemDefID, homeID, "Milk", &threshold, &target, "medium"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "shopping_list_items" WHERE home_id = $1 AND item_definition_id = $2 AND is_auto_generated = $3 AND is_bought = $4 ORDER BY "shopping_list_items"."id" LIMIT $5`)).
			WithArgs(homeID, itemDefID, true, false, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "item_definition_id"}).AddRow(shoppingItemID, homeID, itemDefID))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT COALESCE(SUM(quantity), 0) FROM "inventory_items" WHERE home_id = $1 AND item_definition_id = $2 AND (expiration_date IS NULL OR expiration_date > NOW())`)).
			WithArgs(homeID, itemDefID).
			WillReturnRows(sqlmock.NewRows([]string{"sum"}).AddRow(2.0))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "shopping_list_items" SET "quantity"=$1,"updated_at"=$2 WHERE "id" = $3`)).
			WithArgs(8.0, sqlmock.AnyArg(), shoppingItemID).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		err = UpdateShoppingListForDefinition(gormDB, homeID, itemDefID)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("threshold NOT met - remove existing item", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: db}), &gorm.Config{})
		require.NoError(t, err)

		threshold := 5.0
		shoppingItemID := uuid.New()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "item_definitions" WHERE "item_definitions"."id" = $1 ORDER BY "item_definitions"."id" LIMIT $2`)).
			WithArgs(itemDefID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "name", "low_stock_threshold", "priority"}).
				AddRow(itemDefID, homeID, "Milk", &threshold, "medium"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "shopping_list_items" WHERE home_id = $1 AND item_definition_id = $2 AND is_auto_generated = $3 AND is_bought = $4 ORDER BY "shopping_list_items"."id" LIMIT $5`)).
			WithArgs(homeID, itemDefID, true, false, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "item_definition_id"}).AddRow(shoppingItemID, homeID, itemDefID))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT COALESCE(SUM(quantity), 0) FROM "inventory_items" WHERE home_id = $1 AND item_definition_id = $2 AND (expiration_date IS NULL OR expiration_date > NOW())`)).
			WithArgs(homeID, itemDefID).
			WillReturnRows(sqlmock.NewRows([]string{"sum"}).AddRow(10.0))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM "shopping_list_items" WHERE "shopping_list_items"."id" = $1`)).
			WithArgs(shoppingItemID).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()

		err = UpdateShoppingListForDefinition(gormDB, homeID, itemDefID)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("threshold removal - clean up", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: db}), &gorm.Config{})
		require.NoError(t, err)

		shoppingItemID := uuid.New()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "item_definitions" WHERE "item_definitions"."id" = $1 ORDER BY "item_definitions"."id" LIMIT $2`)).
			WithArgs(itemDefID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "name", "low_stock_threshold"}).
				AddRow(itemDefID, homeID, "Milk", nil))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "shopping_list_items" WHERE home_id = $1 AND item_definition_id = $2 AND is_auto_generated = $3 AND is_bought = $4 ORDER BY "shopping_list_items"."id" LIMIT $5`)).
			WithArgs(homeID, itemDefID, true, false, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "item_definition_id"}).AddRow(shoppingItemID, homeID, itemDefID))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM "shopping_list_items" WHERE "shopping_list_items"."id" = $1`)).
			WithArgs(shoppingItemID).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()

		err = UpdateShoppingListForDefinition(gormDB, homeID, itemDefID)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error - find item definition", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: db}), &gorm.Config{})
		require.NoError(t, err)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "item_definitions" WHERE "item_definitions"."id" = $1 ORDER BY "item_definitions"."id" LIMIT $2`)).
			WithArgs(itemDefID, 1).
			WillReturnError(errors.New("db error"))

		err = UpdateShoppingListForDefinition(gormDB, homeID, itemDefID)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error - find shopping list item", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: db}), &gorm.Config{})
		require.NoError(t, err)

		threshold := 5.0
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "item_definitions" WHERE "item_definitions"."id" = $1 ORDER BY "item_definitions"."id" LIMIT $2`)).
			WithArgs(itemDefID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "name", "low_stock_threshold"}).
				AddRow(itemDefID, homeID, "Milk", &threshold))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "shopping_list_items" WHERE home_id = $1 AND item_definition_id = $2 AND is_auto_generated = $3 AND is_bought = $4 ORDER BY "shopping_list_items"."id" LIMIT $5`)).
			WithArgs(homeID, itemDefID, true, false, 1).
			WillReturnError(errors.New("db error"))

		err = UpdateShoppingListForDefinition(gormDB, homeID, itemDefID)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error - inventory sum", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: db}), &gorm.Config{})
		require.NoError(t, err)

		threshold := 5.0
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "item_definitions" WHERE "item_definitions"."id" = $1 ORDER BY "item_definitions"."id" LIMIT $2`)).
			WithArgs(itemDefID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "name", "low_stock_threshold"}).
				AddRow(itemDefID, homeID, "Milk", &threshold))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "shopping_list_items" WHERE home_id = $1 AND item_definition_id = $2 AND is_auto_generated = $3 AND is_bought = $4 ORDER BY "shopping_list_items"."id" LIMIT $5`)).
			WithArgs(homeID, itemDefID, true, false, 1).
			WillReturnError(gorm.ErrRecordNotFound)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT COALESCE(SUM(quantity), 0) FROM "inventory_items" WHERE home_id = $1 AND item_definition_id = $2 AND (expiration_date IS NULL OR expiration_date > NOW())`)).
			WithArgs(homeID, itemDefID).
			WillReturnError(errors.New("db error"))

		err = UpdateShoppingListForDefinition(gormDB, homeID, itemDefID)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error - create item", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: db}), &gorm.Config{})
		require.NoError(t, err)

		threshold := 5.0
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "item_definitions" WHERE "item_definitions"."id" = $1 ORDER BY "item_definitions"."id" LIMIT $2`)).
			WithArgs(itemDefID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "name", "low_stock_threshold"}).
				AddRow(itemDefID, homeID, "Milk", &threshold))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "shopping_list_items" WHERE home_id = $1 AND item_definition_id = $2 AND is_auto_generated = $3 AND is_bought = $4 ORDER BY "shopping_list_items"."id" LIMIT $5`)).
			WithArgs(homeID, itemDefID, true, false, 1).
			WillReturnError(gorm.ErrRecordNotFound)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT COALESCE(SUM(quantity), 0) FROM "inventory_items" WHERE home_id = $1 AND item_definition_id = $2 AND (expiration_date IS NULL OR expiration_date > NOW())`)).
			WithArgs(homeID, itemDefID).
			WillReturnRows(sqlmock.NewRows([]string{"sum"}).AddRow(2.0))

		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO "shopping_list_items"`)).
			WillReturnError(errors.New("db error"))
		mock.ExpectRollback()

		err = UpdateShoppingListForDefinition(gormDB, homeID, itemDefID)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error - delete item", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: db}), &gorm.Config{})
		require.NoError(t, err)

		shoppingItemID := uuid.New()
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "item_definitions" WHERE "item_definitions"."id" = $1 ORDER BY "item_definitions"."id" LIMIT $2`)).
			WithArgs(itemDefID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "name", "low_stock_threshold"}).
				AddRow(itemDefID, homeID, "Milk", nil))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "shopping_list_items" WHERE home_id = $1 AND item_definition_id = $2 AND is_auto_generated = $3 AND is_bought = $4 ORDER BY "shopping_list_items"."id" LIMIT $5`)).
			WithArgs(homeID, itemDefID, true, false, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "item_definition_id"}).AddRow(shoppingItemID, homeID, itemDefID))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM "shopping_list_items" WHERE "shopping_list_items"."id" = $1`)).
			WillReturnError(errors.New("db error"))
		mock.ExpectRollback()

		err = UpdateShoppingListForDefinition(gormDB, homeID, itemDefID)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestRefreshAllShoppingLists(t *testing.T) {
	logger.InitLogger()

	t.Run("success", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: db}), &gorm.Config{})
		require.NoError(t, err)

		homeID := uuid.New()
		itemDefID := uuid.New()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "item_definitions" ORDER BY "item_definitions"."id" LIMIT $1`)).
			WithArgs(100).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "name", "low_stock_threshold"}).
				AddRow(itemDefID, homeID, "Milk", nil))

		mock.ExpectBegin()
		// Inside RefreshAllShoppingLists, check for expiring soon
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "inventory_items" WHERE home_id = $1 AND item_definition_id = $2 AND expiration_date > NOW() AND expiration_date <= $3`)).
			WithArgs(homeID, itemDefID, sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"id"}))

		// Inside UpdateShoppingListForDefinition
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "item_definitions" WHERE "item_definitions"."id" = $1 ORDER BY "item_definitions"."id" LIMIT $2`)).
			WithArgs(itemDefID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "name", "low_stock_threshold"}).
				AddRow(itemDefID, homeID, "Milk", nil))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "shopping_list_items" WHERE home_id = $1 AND item_definition_id = $2 AND is_auto_generated = $3 AND is_bought = $4 ORDER BY "shopping_list_items"."id" LIMIT $5`)).
			WithArgs(homeID, itemDefID, true, false, 1).
			WillReturnError(gorm.ErrRecordNotFound)
		mock.ExpectCommit()

		err = RefreshAllShoppingLists(gormDB)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("success with expiry notification", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: db}), &gorm.Config{})
		require.NoError(t, err)

		homeID := uuid.New()
		itemDefID := uuid.New()

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "item_definitions" ORDER BY "item_definitions"."id" LIMIT $1`)).
			WithArgs(100).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "name", "low_stock_threshold"}).
				AddRow(itemDefID, homeID, "Milk", nil))

		mock.ExpectBegin()
		// Inside RefreshAllShoppingLists, check for expiring soon
		now := time.Now()
		expiryDate := now.Add(24 * time.Hour)
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "inventory_items" WHERE home_id = $1 AND item_definition_id = $2 AND expiration_date > NOW() AND expiration_date <= $3`)).
			WithArgs(homeID, itemDefID, sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "item_definition_id", "expiration_date"}).
				AddRow(uuid.New(), homeID, itemDefID, expiryDate))

		// Inside UpdateShoppingListForDefinition
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "item_definitions" WHERE "item_definitions"."id" = $1 ORDER BY "item_definitions"."id" LIMIT $2`)).
			WithArgs(itemDefID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "name", "low_stock_threshold"}).
				AddRow(itemDefID, homeID, "Milk", nil))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "shopping_list_items" WHERE home_id = $1 AND item_definition_id = $2 AND is_auto_generated = $3 AND is_bought = $4 ORDER BY "shopping_list_items"."id" LIMIT $5`)).
			WithArgs(homeID, itemDefID, true, false, 1).
			WillReturnError(gorm.ErrRecordNotFound)
		mock.ExpectCommit()

		err = RefreshAllShoppingLists(gormDB)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error fetching item definitions", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: db}), &gorm.Config{})
		require.NoError(t, err)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "item_definitions" ORDER BY "item_definitions"."id" LIMIT $1`)).
			WithArgs(100).
			WillReturnError(errors.New("db error"))

		err = RefreshAllShoppingLists(gormDB)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestSendLowStockNotification(t *testing.T) {
	logger.InitLogger()
	homeID := uuid.New()
	SendLowStockNotification(homeID, "Milk", "medium")
	SendLowStockNotification(homeID, "Eggs", "high")
}
