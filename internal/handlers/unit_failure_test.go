package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Fail one database operation, then allow subsequent reads to verify rollback.
func failUnitOperation(t *testing.T, db *gorm.DB, operation, table string, occurrence int) {
	t.Helper()
	name := "test:unit-failure:" + uuid.NewString()
	calls := 0
	callback := func(tx *gorm.DB) {
		if tx.Statement.Table == table {
			calls++
			if calls == occurrence {
				tx.AddError(errors.New("injected database failure"))
			}
		}
	}
	switch operation {
	case "query":
		require.NoError(t, db.Callback().Query().Before("gorm:query").Register(name, callback))
		t.Cleanup(func() { db.Callback().Query().Remove(name) })
	case "update":
		require.NoError(t, db.Callback().Update().Before("gorm:update").Register(name, callback))
		t.Cleanup(func() { db.Callback().Update().Remove(name) })
	case "create":
		require.NoError(t, db.Callback().Create().Before("gorm:create").Register(name, callback))
		t.Cleanup(func() { db.Callback().Create().Remove(name) })
	}
	t.Cleanup(func() {
		require.GreaterOrEqual(t, calls, occurrence, "the intended database operation must be reached")
	})
}

func assertUnitStockUnchanged(t *testing.T, f *unitFixture, item models.InventoryItem) {
	t.Helper()
	var saved models.InventoryItem
	require.NoError(t, f.db.First(&saved, item.ID).Error)
	require.Equal(t, item.Quantity, saved.Quantity)
	var count int64
	require.NoError(t, f.db.Model(&models.InventoryTransaction{}).Where("inventory_item_id = ?", item.ID).Count(&count).Error)
	require.EqualValues(t, 1, count, "the initial receipt must remain the only transaction")
}

func TestUnitInventoryFailureRollback(t *testing.T) {
	for _, tc := range []struct {
		name, operation, table string
		occurrence             int
	}{
		{"stock lock", "query", "inventory_items", 2},
		{"definition lookup", "query", "item_definitions", 1},
		{"stock update", "update", "inventory_items", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newUnitFixture(t)
			item := f.stock(t, 0.8, "kg")
			failUnitOperation(t, f.db, tc.operation, tc.table, tc.occurrence)
			f.request(t, "PATCH", "/inventory/"+item.ID.String(), map[string]any{"quantity": 500, "quantity_unit_id": f.units["g"]}, 500)
			assertUnitStockUnchanged(t, f, item)
		})
	}
	t.Run("unknown and malformed entry", func(t *testing.T) {
		f := newUnitFixture(t)
		f.request(t, "PATCH", "/inventory/not-a-uuid", map[string]any{"display_size_unit_id": nil}, 400)
		f.request(t, "PATCH", "/inventory/"+uuid.NewString(), map[string]any{"display_size_unit_id": nil}, 404)
	})
	t.Run("create definition lookup", func(t *testing.T) {
		f := newUnitFixture(t)
		f.request(t, "POST", "/inventory", map[string]any{"item_definition_id": uuid.New(), "quantity": 1, "quantity_unit_id": f.units["kg"]}, 400)
	})
	t.Run("create incompatible display", func(t *testing.T) {
		f := newUnitFixture(t)
		f.request(t, "POST", "/inventory", map[string]any{"item_definition_id": f.def.ID, "quantity": 1, "quantity_unit_id": f.units["kg"], "display_size_unit_id": f.units["L"]}, 400)
		var count int64
		require.NoError(t, f.db.Model(&models.InventoryItem{}).Count(&count).Error)
		require.Zero(t, count)
	})
	t.Run("delete cannot acquire stock", func(t *testing.T) {
		f := newUnitFixture(t)
		item := f.stock(t, 0.8, "kg")
		h := &InventoryItemHandler{DB: f.db}
		f.router.DELETE("/delete/:id", h.DeleteInventoryItem)
		failUnitOperation(t, f.db, "query", "inventory_items", 2)
		f.request(t, "DELETE", "/delete/"+item.ID.String(), nil, 500)
		assertUnitStockUnchanged(t, f, item)
	})
}

func TestUnitBarcodeValidationAndRollback(t *testing.T) {
	for _, scenario := range []string{"wrong family", "zero change", "range overflow", "update failure"} {
		t.Run(scenario, func(t *testing.T) {
			f := newUnitFixture(t)
			amount := 0.8
			if scenario == "range overflow" {
				amount = 1_000_000
			}
			item := f.stock(t, amount, "kg")
			require.NoError(t, f.db.Model(&f.def).Update("barcode", "flour").Error)
			unit, change, status := f.units["g"], 1.0, 400
			switch scenario {
			case "wrong family":
				unit = f.units["L"]
			case "zero change":
				change, status = 0, 200
			case "update failure":
				status = 500
				failUnitOperation(t, f.db, "update", "inventory_items", 1)
			}
			f.request(t, "POST", "/inventory/scan", map[string]any{"barcode": "flour", "change": change, "quantity_unit_id": unit}, status)
			assertUnitStockUnchanged(t, f, item)
		})
	}
}

func TestUnitPredictionFailureRollback(t *testing.T) {
	for _, tc := range []struct{ name, operation, table string }{
		{"item lookup", "query", "inventory_items"},
		{"definition lookup", "query", "item_definitions"},
		{"prediction update", "update", "inventory_predictions"},
		{"transaction insert", "create", "inventory_transactions"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newUnitFixture(t)
			item := f.stock(t, 0.8, "kg")
			prediction := models.InventoryPrediction{ID: uuid.New(), InventoryItemID: item.ID, PredictedConsumedAmount: 100, Status: models.PredictionStatusPredicted}
			require.NoError(t, f.db.Create(&prediction).Error)
			failUnitOperation(t, f.db, tc.operation, tc.table, 1)
			f.request(t, "PUT", "/predictions/apply", map[string]any{"prediction_id": prediction.ID, "applied_amount": 0.1, "quantity_unit_id": f.units["kg"]}, 500)
			assertUnitStockUnchanged(t, f, item)
			var saved models.InventoryPrediction
			require.NoError(t, f.db.First(&saved, prediction.ID).Error)
			require.Equal(t, models.PredictionStatusPredicted, saved.Status)
		})
	}
}

func TestUnitPredictionConcurrentStatusChange(t *testing.T) {
	h, mock := setupInventoryTest(t)
	conn, err := h.DB.DB()
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close(); require.NoError(t, mock.ExpectationsWereMet()) })
	user, home, prediction, item, definition := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	mock.ExpectQuery(`SELECT \* FROM "user_homes"`).WithArgs(user, home, 1).WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(user, home, models.RoleOwner))
	mock.ExpectQuery(`SELECT .* FROM "inventory_predictions"`).WithArgs(prediction, home, 1).WillReturnRows(sqlmock.NewRows([]string{"id", "inventory_item_id", "status"}).AddRow(prediction, item, models.PredictionStatusPredicted))
	expectQuantityLock(mock, item, home, definition, 5)
	expectQuantityDefinition(mock, definition, home)
	mock.ExpectBegin()
	// Another request has applied or ignored the prediction since it was read.
	mock.ExpectExec(`UPDATE "inventory_predictions".*WHERE status = \$3 AND "id" = \$4`).WithArgs(models.PredictionStatusApplied, sqlmock.AnyArg(), models.PredictionStatusPredicted, prediction).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()
	expectProfileLookupInPredictionsTest(mock, user)
	body, err := json.Marshal(ApplyPredictionRequest{PredictionID: prediction, AppliedAmount: 1, QuantityUnitID: &mockCountUnitID})
	require.NoError(t, err)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("PUT", "/predictions/apply", bytes.NewReader(body))
	c.Request.Header.Set("X-Home-Id", home.String())
	c.Set("userID", user)
	h.ApplyPrediction(c)
	require.Equal(t, 404, w.Code)
	require.Contains(t, w.Body.String(), "Prediction not found")
}

func TestUnitPlanningRejectsIncompatibleRequirements(t *testing.T) {
	t.Run("recipe create and update", func(t *testing.T) {
		f := newUnitFixture(t)
		body := map[string]any{"name": "Bread", "ingredients": []any{map[string]any{"item_definition_id": f.def.ID, "quantity_required": 1, "quantity_unit_id": f.units["L"]}}}
		f.request(t, "POST", "/recipes", body, 400)
		recipe := models.Recipe{ID: uuid.New(), HomeID: f.home, Name: "Bread"}
		require.NoError(t, f.db.Create(&recipe).Error)
		f.request(t, "PUT", "/recipes/"+recipe.ID.String(), body, 400)
		var count int64
		require.NoError(t, f.db.Model(&models.RecipeIngredient{}).Count(&count).Error)
		require.Zero(t, count)
	})
	t.Run("maintenance create and update", func(t *testing.T) {
		f := newUnitFixture(t)
		now := time.Now().UTC().Add(time.Hour)
		body := map[string]any{"description": "Maintain", "frequency": "Once", "scheduled_date": now, "dependencies": []any{map[string]any{"item_definition_id": f.def.ID, "quantity_required": 1, "quantity_unit_id": f.units["L"]}}}
		f.request(t, "POST", "/maintenance", body, 400)
		task := models.MaintenanceTask{ID: uuid.New(), HomeID: f.home, Description: "Maintain", Frequency: "Once", ScheduledDate: now}
		require.NoError(t, f.db.Create(&task).Error)
		f.request(t, "PUT", "/maintenance/"+task.ID.String(), body, 400)
		var count int64
		require.NoError(t, f.db.Model(&models.TaskItemDependency{}).Count(&count).Error)
		require.Zero(t, count)
	})
	t.Run("receipt invalid default display and catalog", func(t *testing.T) {
		f := newUnitFixture(t)
		job := models.ReceiptJob{ID: uuid.New(), HomeID: f.home, UserID: f.user, Status: "completed"}
		require.NoError(t, f.db.Create(&job).Error)
		f.request(t, "POST", "/receipts/"+job.ID.String()+"/confirm", map[string]any{"items": []any{map[string]any{"raw_name": "Unknown", "quantity": 1, "quantity_unit_id": f.units["kg"], "size_unit_id": uuid.New()}}}, 400)
		f.request(t, "POST", "/receipts/"+job.ID.String()+"/confirm", map[string]any{"items": []any{map[string]any{"raw_name": "Flour", "item_definition_id": f.def.ID, "quantity": 1, "quantity_unit_id": f.units["kg"], "display_size_unit_id": f.units["L"]}}}, 400)
		h := &ReceiptHandler{DB: f.db.WithContext(context.Background())}
		f.router.POST("/uncached-receipt/:id", h.ConfirmReceiptJob)
		failUnitOperation(t, f.db, "query", "size_units", 1)
		f.request(t, "POST", "/uncached-receipt/"+job.ID.String(), map[string]any{"items": []any{map[string]any{"raw_name": "Unknown", "quantity": 1, "quantity_unit_id": f.units["kg"], "size_unit_id": f.units["kg"]}}}, 400)
		var count int64
		require.NoError(t, f.db.Model(&models.InventoryItem{}).Count(&count).Error)
		require.Zero(t, count)
	})
}

func TestUnitDefinitionThresholdUpdates(t *testing.T) {
	f := newUnitFixture(t)
	path := "/definitions/" + f.def.ID.String()
	f.request(t, "PUT", path, map[string]any{"name": "Flour", "size_unit_id": f.units["g"], "low_stock_threshold": 1, "target_quantity": 3, "quantity_unit_id": f.units["kg"]}, 200)
	var saved models.ItemDefinition
	require.NoError(t, f.db.First(&saved, f.def.ID).Error)
	require.Equal(t, 1000.0, *saved.LowStockThreshold)
	require.Equal(t, 3000.0, *saved.TargetQuantity)
	f.request(t, "PUT", path, map[string]any{"name": "Flour", "size_unit_id": f.units["g"], "low_stock_threshold": nil, "target_quantity": nil}, 200)
	var cleared models.ItemDefinition
	require.NoError(t, f.db.First(&cleared, f.def.ID).Error)
	require.Nil(t, cleared.LowStockThreshold)
	require.Nil(t, cleared.TargetQuantity)
}

func TestUnitDepletionReasonsUseDisplayAmounts(t *testing.T) {
	f := newUnitFixture(t)
	h := &InventoryItemHandler{DB: f.db}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	now := time.Now().UTC()
	unit := models.SizeUnit{Name: "kg", Family: "mass", FactorToBase: 1000}
	_, reason := h.projectDepletion(c, now, 0, 250, nil, f.def.ID, "Flour", unit)
	require.Contains(t, reason, "0.25 kg daily")
	task := models.MaintenanceTask{Description: "Bake", ScheduledDate: now.Add(time.Hour), Frequency: "Once", Dependencies: []models.TaskItemDependency{{ItemDefinitionID: f.def.ID, QuantityRequired: 1000}}}
	_, reason = h.projectDepletion(c, now, 800, 0, []models.MaintenanceTask{task}, f.def.ID, "Flour", unit)
	require.Contains(t, reason, "requires 1 kg")
	require.Contains(t, reason, "0.8 kg")
}
