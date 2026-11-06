package handlers

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestPredictiveShoppingUsesBaseUnitAmounts(t *testing.T) {
	for _, tc := range []struct {
		name              string
		target, threshold *float64
		want              float64
		invalidCatalog    bool
	}{
		{name: "one default kg", want: 1000},
		{name: "target shortage", target: numberPointer(3000), want: 2000},
		{name: "threshold shortage", threshold: numberPointer(5000), want: 4000},
		{name: "missing catalog metadata", invalidCatalog: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, mock := setupInventoryTest(t)
			conn, err := h.DB.DB()
			require.NoError(t, err)
			t.Cleanup(func() { conn.Close(); require.NoError(t, mock.ExpectationsWereMet()) })
			g, kg, home, user, definition := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
			units := map[uuid.UUID]models.SizeUnit{g: {ID: g, Name: "g", Family: "mass", IsBase: true, FactorToBase: 1}, kg: {ID: kg, Name: "kg", Family: "mass", FactorToBase: 1000}}
			if tc.invalidCatalog {
				delete(units, kg)
			}
			db := models.WithUnitCatalog(h.DB, &models.UnitCatalog{Units: units, Bases: map[string]uuid.UUID{"mass": g}})
			mock.ExpectQuery(`SELECT "restock_window" FROM "profiles"`).WithArgs(user, 1).WillReturnRows(sqlmock.NewRows([]string{"id", "restock_window"}).AddRow(user, 7))
			mock.ExpectQuery(`SELECT item_definition_id, SUM\(-quantity_change\)`).WithArgs(home, sqlmock.AnyArg()).WillReturnRows(sqlmock.NewRows([]string{"item_definition_id", "total_consumed", "first_tx_time", "last_tx_time"}).AddRow(definition, 2000, time.Now().Add(-48*time.Hour), time.Now()))
			mock.ExpectQuery(`SELECT \* FROM "item_definitions" WHERE home_id =`).WithArgs(home).WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "name", "size_unit_id", "target_quantity", "low_stock_threshold"}).AddRow(definition, home, "Flour", kg, tc.target, tc.threshold))
			mock.ExpectQuery(`SELECT \* FROM "inventory_items" WHERE home_id =`).WithArgs(home).WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "item_definition_id", "quantity"}).AddRow(uuid.New(), home, definition, 1000))
			mock.ExpectQuery(`SELECT \* FROM "maintenance_tasks"`).WithArgs(home, false).WillReturnRows(sqlmock.NewRows([]string{"id", "home_id"}))
			mock.ExpectQuery(`SELECT \* FROM "shopping_list_items"`).WithArgs(home, false).WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "item_definition_id"}))
			expectProfileLookupInPredictionsTest(mock, user)
			if !tc.invalidCatalog {
				mock.ExpectBegin()
				mock.ExpectQuery(`INSERT INTO "shopping_list_items"`).WithArgs(home, definition, "Flour", tc.want, false, false, true, false, sqlmock.AnyArg(), sqlmock.AnyArg()).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))
				mock.ExpectCommit()
			}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Set("userID", user)
			(&ShoppingListHandler{DB: db}).generatePredictiveSuggestions(c, home)
		})
	}
}

func numberPointer(v float64) *float64 { return &v }

func TestUnitShoppingUpdateValidationAndMealPlan(t *testing.T) {
	t.Run("linked and unlinked validation", func(t *testing.T) {
		f := newUnitFixture(t)
		linked := models.ShoppingListItem{ID: uuid.New(), HomeID: f.home, ItemDefinitionID: &f.def.ID, Name: "Flour", Quantity: 500}
		unlinked := models.ShoppingListItem{ID: uuid.New(), HomeID: f.home, Name: "Sponges", Quantity: 3}
		require.NoError(t, f.db.Create(&linked).Error)
		require.NoError(t, f.db.Create(&unlinked).Error)
		f.request(t, "PUT", "/shopping/"+linked.ID.String(), map[string]any{"quantity": 1, "quantity_unit_id": f.units["L"]}, 400)
		f.request(t, "PUT", "/shopping/"+unlinked.ID.String(), map[string]any{"quantity": 0.0000001}, 400)
		var saved models.ShoppingListItem
		require.NoError(t, f.db.First(&saved, linked.ID).Error)
		require.Equal(t, 500.0, saved.Quantity)
		var count models.ShoppingListItem
		require.NoError(t, f.db.First(&count, unlinked.ID).Error)
		require.Equal(t, 3.0, count.Quantity)
	})
	t.Run("meal plan adds shortage to an existing base amount", func(t *testing.T) {
		f := newUnitFixture(t)
		f.stock(t, 0.2, "kg")
		recipe := models.Recipe{ID: uuid.New(), HomeID: f.home, Name: "Bread"}
		require.NoError(t, f.db.Create(&recipe).Error)
		require.NoError(t, f.db.Create(&models.RecipeIngredient{ID: uuid.New(), RecipeID: recipe.ID, ItemDefinitionID: f.def.ID, QuantityRequired: 1000}).Error)
		shopping := models.ShoppingListItem{ID: uuid.New(), HomeID: f.home, ItemDefinitionID: &f.def.ID, Name: "Flour", Quantity: 500}
		require.NoError(t, f.db.Create(&shopping).Error)
		h := &RecipeHandler{DB: f.db}
		f.router.POST("/meal-plan/:id", h.AddRecipeToShoppingList)
		f.request(t, "POST", "/meal-plan/"+recipe.ID.String(), nil, 200)
		var saved models.ShoppingListItem
		require.NoError(t, f.db.First(&saved, shopping.ID).Error)
		require.Equal(t, 1300.0, saved.Quantity, "500 g already planned plus an 800 g shortage")
	})
}
