package handlers

import (
	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var mockCountUnitID = uuid.MustParse("00000000-0000-4000-8000-000000000001")

func withMockUnits(db *gorm.DB) *gorm.DB {
	u := models.SizeUnit{ID: mockCountUnitID, Name: "piece", Family: "count", IsBase: true, FactorToBase: 1}
	return models.WithUnitCatalog(db, &models.UnitCatalog{Units: map[uuid.UUID]models.SizeUnit{u.ID: u}, Bases: map[string]uuid.UUID{"count": u.ID}})
}

func expectQuantityDefinition(mock sqlmock.Sqlmock, id, home uuid.UUID) {
	mock.ExpectQuery(`SELECT \* FROM "item_definitions" WHERE id = \$1 AND home_id = \$2`).WithArgs(id, home, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "name", "size_unit_id"}).AddRow(id, home, "Item", mockCountUnitID))
}

func expectQuantityLock(mock sqlmock.Sqlmock, id, home, definition uuid.UUID, value float64) {
	mock.ExpectQuery(`SELECT \* FROM "inventory_items" WHERE "inventory_items"\."id" = \$1`).WithArgs(id, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "item_definition_id", "quantity"}).AddRow(id, home, definition, value))
}
