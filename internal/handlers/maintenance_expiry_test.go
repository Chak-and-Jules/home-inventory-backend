package handlers

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/i18n"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestMaintenanceExpiredStockLocalizedFailure(t *testing.T) {
	f := newUnitFixture(t)
	stock := f.stock(t, 1, "kg")
	task := maintenanceTask(t, f, 0.1)
	require.NoError(t, f.db.Model(&stock).Update("expiration_date", time.Now().UTC().Add(-24*time.Hour)).Error)
	path := "/maintenance/" + task.ID.String() + "/complete"
	command := map[string]any{"request_id": uuid.New(), "expected_version": 0}
	w := f.request(t, "POST", path, command, 400)
	require.Contains(t, w.Body.String(), `"code":"insufficient_stock"`)
	require.Contains(t, w.Body.String(), "Insufficient unexpired stock")
	language := models.Language{ID: uuid.New(), Name: "Türkçe"}
	require.NoError(t, f.db.Create(&language).Error)
	require.NoError(t, f.db.Model(&models.Profile{}).Where("id = ?", f.user).Update("language_id", language.ID).Error)
	i18n.InvalidateUserLanguageCache(f.user)
	w = f.request(t, "POST", path, command, 400)
	require.Contains(t, w.Body.String(), "son kullanma tarihi geçmemiş stok yetersiz")
	require.NoError(t, f.db.First(&stock, stock.ID).Error)
	require.Equal(t, 1000.0, stock.Quantity)
	require.NoError(t, f.db.First(&task, task.ID).Error)
	require.False(t, task.IsCompleted)
	require.Zero(t, task.Version)
	require.Nil(t, task.CompletedAt)
	var n int64
	require.NoError(t, f.db.Model(&models.InventoryTransaction{}).Count(&n).Error)
	require.EqualValues(t, 1, n)
	for _, model := range []any{&models.MaintenanceExecution{}, &models.MaintenanceConsumption{}, &models.MaintenanceRequest{}, &models.ShoppingListItem{}} {
		require.NoError(t, f.db.Model(model).Count(&n).Error)
		require.Zero(t, n)
	}
}

func TestMaintenanceEligibleStockAcrossUnitFamilies(t *testing.T) {
	f := newUnitFixture(t)
	past := time.Now().UTC().Add(-24 * time.Hour)
	future := time.Now().UTC().Add(24 * time.Hour)
	expired := f.stock(t, 1, "kg")
	require.NoError(t, f.db.Model(&expired).Update("expiration_date", past).Error)
	first, second := f.stock(t, 0.1, "kg"), f.stock(t, 0.2, "kg")
	require.NoError(t, f.db.Model(&first).Update("expiration_date", future).Error)
	liter := f.units["L"]
	cleaner := models.ItemDefinition{ID: uuid.New(), HomeID: f.home, Name: "Cleaner", SizeUnitID: &liter}
	require.NoError(t, f.db.Create(&cleaner).Error)
	var liquid []models.InventoryItem
	for _, amount := range []float64{0.1, 0.2, 1} {
		w := f.request(t, "POST", "/inventory", map[string]any{"item_definition_id": cleaner.ID, "quantity": amount, "quantity_unit_id": liter}, 201)
		var item models.InventoryItem
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &item))
		liquid = append(liquid, item)
	}
	require.NoError(t, f.db.Model(&liquid[0]).Update("expiration_date", future).Error)
	require.NoError(t, f.db.Model(&liquid[2]).Update("expiration_date", past).Error)
	body := maintenanceBody(f, 0.25)
	body["dependencies"] = []any{
		map[string]any{"item_definition_id": f.def.ID, "quantity_required": 0.25, "quantity_unit_id": f.units["kg"]},
		map[string]any{"item_definition_id": f.def.ID, "quantity_required": 50, "quantity_unit_id": f.units["g"]},
		map[string]any{"item_definition_id": cleaner.ID, "quantity_required": 0.3, "quantity_unit_id": liter},
	}
	w := f.request(t, "POST", "/maintenance", body, 201)
	var task models.MaintenanceTask
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &task))
	f.request(t, "POST", "/maintenance/"+task.ID.String()+"/complete", map[string]any{}, 200)
	for _, item := range []models.InventoryItem{first, second, liquid[0], liquid[1]} {
		var saved models.InventoryItem
		require.NoError(t, f.db.First(&saved, item.ID).Error)
		require.Zero(t, saved.Quantity)
	}
	for _, item := range []models.InventoryItem{expired, liquid[2]} {
		var saved models.InventoryItem
		require.NoError(t, f.db.First(&saved, item.ID).Error)
		require.Equal(t, 1000.0, saved.Quantity)
	}
	var usages []models.MaintenanceConsumption
	require.NoError(t, f.db.Find(&usages).Error)
	require.Len(t, usages, 4)
	totals := map[string]float64{}
	for _, usage := range usages {
		totals[usage.BaseUnitName] += usage.Quantity
		require.NotEqual(t, expired.ID, usage.InventoryItemID)
		require.NotEqual(t, liquid[2].ID, usage.InventoryItemID)
	}
	require.Equal(t, map[string]float64{"g": 300, "mL": 300}, totals)
}

func TestMaintenanceForecastIgnoresExpiredAndEmptyStock(t *testing.T) {
	f := newUnitFixture(t)
	expired := f.stock(t, 1, "kg")
	require.NoError(t, f.db.Model(&expired).Update("expiration_date", time.Now().UTC().Add(-24*time.Hour)).Error)
	f.stock(t, 0, "kg")
	f.stock(t, 0.05, "kg")
	maintenanceTask(t, f, 0.1)
	inventory := &InventoryItemHandler{DB: f.db}
	f.router.GET("/restock", inventory.GetPredictiveRestockInsights)
	w := f.request(t, "GET", "/restock", nil, 200)
	var insights []RestockInsightResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &insights))
	require.Len(t, insights, 1)
	require.Equal(t, 50.0, insights[0].CurrentStock)
	require.Contains(t, insights[0].Reason, "Replace filter")
	shopping := &ShoppingListHandler{DB: f.db}
	f.router.GET("/shopping", shopping.GetShoppingList)
	w = f.request(t, "GET", "/shopping", nil, 200)
	var list []models.ShoppingListItem
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
	require.Len(t, list, 1)
	require.True(t, list[0].IsPredictive)
	require.NoError(t, f.db.First(&expired, expired.ID).Error)
	require.Equal(t, 1000.0, expired.Quantity)
	var n int64
	require.NoError(t, f.db.Model(&models.MaintenanceConsumption{}).Count(&n).Error)
	require.Zero(t, n)
}
