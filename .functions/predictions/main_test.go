package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestHandler_MethodNotAllowed(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()

	Handler(rr, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rr.Code)
}

func TestHandler_NoDBURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	req, _ := http.NewRequest(http.MethodPost, "/", nil)
	rr := httptest.NewRecorder()

	Handler(rr, req)

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Contains(t, rr.Body.String(), "DATABASE_URL not configured")
}

func TestProcessPredictions(t *testing.T) {
	dbMock, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer dbMock.Close()

	gormDB, err := gorm.Open(postgres.New(postgres.Config{
		Conn: dbMock,
	}), &gorm.Config{})
	assert.NoError(t, err)

	itemID := uuid.New()
	itemDefID := uuid.New()
	homeID := uuid.New()

	// 1. Delete old predictions
	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM "inventory_predictions" WHERE created_at < \$1`).
		WithArgs(sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	// 2. Find inventory items
	now := time.Now()
	mock.ExpectQuery(`SELECT \* FROM "inventory_items"`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "item_definition_id", "quantity", "created_at", "updated_at"}).
			AddRow(itemID, homeID, itemDefID, 10.0, now.AddDate(0, -1, 0), now))

	// 3. Find transactions for item
	mock.ExpectQuery(`SELECT \* FROM "inventory_transactions" WHERE inventory_item_id = \$1 AND created_at >= \$2 ORDER BY created_at desc`).
		WithArgs(itemID, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "item_definition_id", "inventory_item_id", "quantity_change", "created_at"}).
			AddRow(uuid.New(), homeID, itemDefID, itemID, 10.0, now.AddDate(0, 0, -5)).
			AddRow(uuid.New(), homeID, itemDefID, itemID, -2.0, now.AddDate(0, 0, -4)).
			AddRow(uuid.New(), homeID, itemDefID, itemID, -2.0, now.AddDate(0, 0, -3)).
			AddRow(uuid.New(), homeID, itemDefID, itemID, -2.0, now.AddDate(0, 0, -2)).
			AddRow(uuid.New(), homeID, itemDefID, itemID, -2.0, now.AddDate(0, 0, -1)))

	// 4. Fetch recent predictions for item
	mock.ExpectQuery(`SELECT \* FROM "inventory_predictions" WHERE inventory_item_id = \$1 AND created_at >= \$2 ORDER BY created_at desc`).
		WithArgs(itemID, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "inventory_item_id", "predicted_consumed_amount", "status", "created_at", "updated_at"}))

	// 5. Update prediction transaction
	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM "inventory_predictions" WHERE inventory_item_id = \$1`).
		WithArgs(itemID).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`INSERT INTO "inventory_predictions"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))
	mock.ExpectCommit()

	err = ProcessPredictions(gormDB)
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}
