package utils

import (
	"testing"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/logger"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestShoppingFallbackUsesOneDefinitionUnit(t *testing.T) {
	logger.InitLogger()
	for _, invalidUnit := range []bool{false, true} {
		name := "target below stock uses one kg"
		if invalidUnit {
			name = "unknown default unit prevents a shopping write"
		}
		t.Run(name, func(t *testing.T) {
			db, mock := setupShoppingTestDB(t)
			conn, err := db.DB()
			require.NoError(t, err)
			t.Cleanup(func() { conn.Close(); require.NoError(t, mock.ExpectationsWereMet()) })
			g, kg, home, definition, shopping := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
			units := map[uuid.UUID]models.SizeUnit{g: {ID: g, Name: "g", Family: "mass", IsBase: true, FactorToBase: 1}, kg: {ID: kg, Name: "kg", Family: "mass", FactorToBase: 1000}}
			if invalidUnit {
				delete(units, kg)
			}
			db = models.WithUnitCatalog(db, &models.UnitCatalog{Units: units, Bases: map[string]uuid.UUID{"mass": g}})
			threshold, target := 5000.0, 1000.0
			def := models.ItemDefinition{ID: definition, HomeID: home, Name: "Flour", SizeUnitID: &kg, LowStockThreshold: &threshold, TargetQuantity: &target}
			mock.ExpectQuery(`SELECT \* FROM "shopping_list_items"`).WithArgs(home, definition, true, false, 1).WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "item_definition_id"}).AddRow(shopping, home, definition))
			mock.ExpectQuery(`SELECT COALESCE\(SUM\(quantity\), 0\)`).WithArgs(home, definition).WillReturnRows(sqlmock.NewRows([]string{"sum"}).AddRow(3000))
			if !invalidUnit {
				mock.ExpectBegin()
				mock.ExpectExec(`UPDATE "shopping_list_items"`).WithArgs(1000.0, sqlmock.AnyArg(), shopping).WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectCommit()
			}
			err = updateShoppingListWithDef(db, home, &def)
			if invalidUnit {
				require.ErrorIs(t, err, models.ErrUnit)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
