package maintenance

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Compare persisted facts as well as counts: a failed attempt must preserve
// pre-existing shopping and inventory history, including expired stock rows.
func persistedState(t *testing.T, db *gorm.DB) map[string]string {
	t.Helper()
	state := map[string]string{}
	for _, entry := range []struct {
		model any
		order string
	}{
		{&[]models.MaintenanceTask{}, "id"}, {&[]models.TaskItemDependency{}, "id"},
		{&[]models.InventoryItem{}, "id"}, {&[]models.InventoryTransaction{}, "id"},
		{&[]models.ShoppingListItem{}, "id"}, {&[]models.MaintenanceExecution{}, "id"},
		{&[]models.MaintenanceConsumption{}, "id"}, {&[]models.MaintenanceRequest{}, "user_id, request_id"},
	} {
		require.NoError(t, db.Order(entry.order).Find(entry.model).Error)
		data, err := json.Marshal(entry.model)
		require.NoError(t, err)
		state[fmt.Sprintf("%T", entry.model)] = string(data)
	}
	return state
}

func TestCompleteEligibleStock(t *testing.T) {
	for _, tc := range []struct {
		name         string
		expiryOffset *time.Duration
		quantity     float64
		succeeds     bool
	}{
		{"expired", duration(-time.Microsecond), 1000, false},
		{"at expiry", duration(0), 1000, false},
		{"just unexpired", duration(time.Microsecond), 100, true},
		{"non-expiring", nil, 100, true},
		{"zero unexpired", duration(time.Hour), 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			now := time.Date(2030, 5, 1, 12, 0, 0, 123456000, time.UTC)
			f.Now = func() time.Time { return now }
			var expiry *time.Time
			if tc.expiryOffset != nil {
				value := now.Add(*tc.expiryOffset)
				expiry = &value
			}
			require.NoError(t, f.DB.Model(&f.stock).Updates(map[string]any{"expiration_date": expiry, "quantity": tc.quantity}).Error)
			before := persistedState(t, f.DB)
			result, err := f.Complete(context.Background(), f.user, f.task.ID, identified())
			if !tc.succeeds {
				require.ErrorIs(t, err, ErrInsufficient)
				require.Equal(t, before, persistedState(t, f.DB))
				return
			}
			require.NoError(t, err)
			require.True(t, now.Equal(*result.CompletedAt))
			f.assertStock(t, tc.quantity-100)
			var used []models.MaintenanceConsumption
			require.NoError(t, f.DB.Find(&used).Error)
			require.Len(t, used, 1)
			require.Equal(t, f.stock.ID, used[0].InventoryItemID)
			require.Equal(t, 100.0, used[0].Quantity)
		})
	}
}
func duration(d time.Duration) *time.Duration { return &d }

func TestCompleteSkipsExpiredAndForeignStock(t *testing.T) {
	f := newFixture(t)
	now := time.Date(2030, 5, 1, 12, 0, 0, 0, time.UTC)
	f.Now = func() time.Time { return now }
	// All dates are in the real server's future. Only the captured operation time
	// can distinguish these expired and valid batches, including shopping totals.
	past := now.Add(-time.Microsecond)
	future := now.Add(time.Hour)
	require.NoError(t, f.DB.Model(&f.stock).Update("expiration_date", past).Error)
	fresh := models.InventoryItem{ID: uuid.New(), HomeID: f.home, ItemDefinitionID: f.def.ID, Quantity: 150, ExpirationDate: &future}
	require.NoError(t, f.DB.Create(&fresh).Error)
	otherHome := models.Home{ID: uuid.New(), Name: "Other"}
	require.NoError(t, f.DB.Create(&otherHome).Error)
	foreign := models.InventoryItem{ID: uuid.New(), HomeID: otherHome.ID, ItemDefinitionID: f.def.ID, Quantity: 1000}
	require.NoError(t, f.DB.Create(&foreign).Error)
	otherDef := f.def
	otherDef.ID = uuid.New()
	otherDef.Name = "Other supply"
	require.NoError(t, f.DB.Create(&otherDef).Error)
	different := models.InventoryItem{ID: uuid.New(), HomeID: f.home, ItemDefinitionID: otherDef.ID, Quantity: 1000}
	require.NoError(t, f.DB.Create(&different).Error)
	command := identified()
	result, err := f.Complete(context.Background(), f.user, f.task.ID, command)
	require.NoError(t, err)
	f.assertStock(t, 1000)
	require.NoError(t, f.DB.First(&fresh, fresh.ID).Error)
	require.Equal(t, 50.0, fresh.Quantity)
	for _, untouched := range []models.InventoryItem{foreign, different} {
		var got models.InventoryItem
		require.NoError(t, f.DB.First(&got, untouched.ID).Error)
		require.Equal(t, untouched.Quantity, got.Quantity)
	}
	var usage models.MaintenanceConsumption
	require.NoError(t, f.DB.First(&usage).Error)
	require.Equal(t, fresh.ID, usage.InventoryItemID)
	require.Equal(t, 100.0, usage.Quantity)
	require.True(t, future.Equal(*usage.ExpirationDate))
	var logs []models.InventoryTransaction
	require.NoError(t, f.DB.Find(&logs).Error)
	require.Len(t, logs, 1)
	require.Equal(t, fresh.ID, logs[0].InventoryItemID)
	require.Equal(t, -100.0, logs[0].QuantityChange)
	var shopping models.ShoppingListItem
	require.NoError(t, f.DB.First(&shopping).Error)
	require.Equal(t, 1450.0, shopping.Quantity)
	// Eligibility is re-evaluated only for new work; a receipt remains an ack.
	before := persistedState(t, f.DB)
	f.Now = func() time.Time { return now.Add(2 * time.Hour) }
	replay, err := f.Complete(context.Background(), f.user, f.task.ID, command)
	require.NoError(t, err)
	require.True(t, replay.Replayed)
	require.Equal(t, result.ExecutionID, replay.ExecutionID)
	require.Equal(t, before, persistedState(t, f.DB))
}

func TestCompleteStableExpiryOrderAndExactSplits(t *testing.T) {
	f := newFixture(t)
	now := f.Now()
	earliest := now.Add(time.Hour)
	later := now.Add(2 * time.Hour)
	require.NoError(t, f.DB.Delete(&f.stock).Error)
	// Insert opposite to UUID order; equal expiries must always use the lower ID.
	batches := []models.InventoryItem{
		{ID: uuid.MustParse("00000000-0000-0000-0000-000000000002"), HomeID: f.home, ItemDefinitionID: f.def.ID, Quantity: 0.2, ExpirationDate: &earliest},
		{ID: uuid.MustParse("00000000-0000-0000-0000-000000000001"), HomeID: f.home, ItemDefinitionID: f.def.ID, Quantity: 0.1, ExpirationDate: &earliest},
		{ID: uuid.New(), HomeID: f.home, ItemDefinitionID: f.def.ID, Quantity: 1, ExpirationDate: &later},
		{ID: uuid.New(), HomeID: f.home, ItemDefinitionID: f.def.ID, Quantity: 1},
		{ID: uuid.New(), HomeID: f.home, ItemDefinitionID: f.def.ID, Quantity: 0, ExpirationDate: &earliest},
	}
	require.NoError(t, f.DB.Create(&batches).Error)
	require.NoError(t, f.DB.Model(&models.TaskItemDependency{}).Where("maintenance_task_id = ?", f.task.ID).Update("quantity_required", 0.15).Error)
	require.NoError(t, f.DB.Create(&models.TaskItemDependency{ID: uuid.New(), MaintenanceTaskID: f.task.ID, ItemDefinitionID: f.def.ID, QuantityRequired: 0.1}).Error)
	_, err := f.Complete(context.Background(), f.user, f.task.ID, identified())
	require.NoError(t, err)
	for i, want := range []float64{0.05, 0, 1, 1, 0} {
		var got models.InventoryItem
		require.NoError(t, f.DB.First(&got, batches[i].ID).Error)
		require.Equal(t, want, got.Quantity)
	}
	var usage []models.MaintenanceConsumption
	require.NoError(t, f.DB.Order("inventory_item_id").Find(&usage).Error)
	require.Len(t, usage, 2)
	require.Equal(t, 0.1, usage[0].Quantity)
	require.Equal(t, 0.15, usage[1].Quantity)
}

func TestCompleteExpiredSecondSupplyPreservesAllState(t *testing.T) {
	f := newFixture(t)
	now := f.Now()
	past := now.Add(-time.Hour)
	second := f.def
	second.ID = uuid.MustParse("ffffffff-ffff-ffff-ffff-ffffffffffff")
	second.Name = "Expired supply"
	require.NoError(t, f.DB.Create(&second).Error)
	require.NoError(t, f.DB.Create(&models.InventoryItem{ID: uuid.New(), HomeID: f.home, ItemDefinitionID: second.ID, Quantity: 1000, ExpirationDate: &past}).Error)
	require.NoError(t, f.DB.Create(&models.TaskItemDependency{ID: uuid.New(), MaintenanceTaskID: f.task.ID, ItemDefinitionID: second.ID, QuantityRequired: 1}).Error)
	require.NoError(t, f.DB.Create(&models.InventoryTransaction{ID: uuid.New(), HomeID: f.home, ItemDefinitionID: f.def.ID, InventoryItemID: f.stock.ID, QuantityChange: 1000}).Error)
	require.NoError(t, f.DB.Create(&models.ShoppingListItem{ID: uuid.New(), HomeID: f.home, ItemDefinitionID: &f.def.ID, Name: f.def.Name, Quantity: 77, IsAutoGenerated: true}).Error)
	before := persistedState(t, f.DB)
	_, err := f.Complete(context.Background(), f.user, f.task.ID, identified())
	require.ErrorIs(t, err, ErrInsufficient)
	require.Equal(t, before, persistedState(t, f.DB))
}
