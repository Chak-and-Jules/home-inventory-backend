package models

import (
	"errors"
	"math"
	"testing"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/quantity"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func catalogMockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	conn, mock, err := sqlmock.New()
	require.NoError(t, err)
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: conn}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, mock.ExpectationsWereMet()); conn.Close() })
	return db, mock
}

func catalogSeeds() []SizeUnit {
	return []SizeUnit{
		{ID: uuid.New(), Name: "g", Family: "mass", IsBase: true, FactorToBase: 1},
		{ID: uuid.New(), Name: "kg", Family: "mass", FactorToBase: 1000},
		{ID: uuid.New(), Name: "mL", Family: "volume", IsBase: true, FactorToBase: 1},
		{ID: uuid.New(), Name: "L", Family: "volume", FactorToBase: 1000},
		{ID: uuid.New(), Name: "piece", Family: "count", IsBase: true, FactorToBase: 1},
	}
}

func catalogRows(units ...SizeUnit) *sqlmock.Rows {
	r := sqlmock.NewRows([]string{"id", "name", "family", "is_base", "factor_to_base"})
	for _, u := range units {
		r.AddRow(u.ID, u.Name, u.Family, u.IsBase, u.FactorToBase)
	}
	return r
}

func TestLoadUnitCatalog(t *testing.T) {
	db, mock := catalogMockDB(t)
	units := catalogSeeds()
	mock.ExpectQuery(`SELECT \* FROM "size_units"`).WillReturnRows(catalogRows(units...))
	catalog, err := Catalog(db)
	require.NoError(t, err)
	require.Len(t, catalog.Units, 5)
	for _, u := range units {
		require.Equal(t, catalog.Bases[u.Family], *catalog.Units[u.ID].BaseSizeUnitID)
	}
	attached := WithUnitCatalog(db, catalog)
	_, exists := cachedCatalog(db)
	require.False(t, exists, "attaching a catalog must not mutate the original DB session")
	cached, err := Catalog(attached.Session(&gorm.Session{}))
	require.NoError(t, err)
	require.Same(t, catalog, cached)
	mock.ExpectBegin()
	mock.ExpectCommit()
	require.NoError(t, attached.Transaction(func(tx *gorm.DB) error {
		inTransaction, err := Catalog(tx)
		require.Same(t, catalog, inTransaction)
		return err
	}))
}

func TestLoadUnitCatalogRejectsInvalidMetadata(t *testing.T) {
	for _, tc := range []struct {
		name  string
		units []SizeUnit
	}{
		{"empty catalog", nil},
		{"blank family", []SizeUnit{{ID: uuid.New(), Family: " ", IsBase: true, FactorToBase: 1}}},
		{"zero factor", []SizeUnit{{ID: uuid.New(), Family: "mass", FactorToBase: 0}}},
		{"negative factor", []SizeUnit{{ID: uuid.New(), Family: "mass", FactorToBase: -1}}},
		{"nonfinite factor", []SizeUnit{{ID: uuid.New(), Family: "mass", FactorToBase: math.NaN()}}},
		{"base factor is not one", []SizeUnit{{ID: uuid.New(), Family: "mass", IsBase: true, FactorToBase: 2}}},
		{"missing base", []SizeUnit{{ID: uuid.New(), Family: "mass", FactorToBase: 1000}}},
		{"duplicate base", []SizeUnit{{ID: uuid.New(), Family: "mass", IsBase: true, FactorToBase: 1}, {ID: uuid.New(), Family: "mass", IsBase: true, FactorToBase: 1}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := catalogMockDB(t)
			mock.ExpectQuery(`SELECT \* FROM "size_units"`).WillReturnRows(catalogRows(tc.units...))
			catalog, err := LoadUnitCatalog(db)
			require.ErrorIs(t, err, ErrUnit)
			require.Nil(t, catalog)
		})
	}
	t.Run("database failure", func(t *testing.T) {
		db, mock := catalogMockDB(t)
		failure := errors.New("catalog unavailable")
		mock.ExpectQuery(`SELECT \* FROM "size_units"`).WillReturnError(failure)
		_, err := LoadUnitCatalog(db)
		require.ErrorIs(t, err, failure)
	})
}

func TestInitializeUnitsIsIdempotentAndImmutable(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	conn, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })
	require.NoError(t, db.AutoMigrate(&SizeUnit{}))
	first, err := InitializeUnits(db)
	require.NoError(t, err)
	firstCatalog, err := Catalog(first)
	require.NoError(t, err)
	second, err := InitializeUnits(db)
	require.NoError(t, err)
	secondCatalog, err := Catalog(second)
	require.NoError(t, err)
	require.Equal(t, firstCatalog, secondCatalog, "restart must preserve all seeded identifiers")
	var count int64
	require.NoError(t, db.Model(&SizeUnit{}).Count(&count).Error)
	require.EqualValues(t, 5, count)
	for _, u := range secondCatalog.Units {
		require.ErrorIs(t, db.Model(&u).Update("factor_to_base", 2).Error, ErrUnit)
		require.ErrorIs(t, db.Delete(&u).Error, ErrUnit)
	}
}

func TestInitializeUnitsPropagatesFailures(t *testing.T) {
	for _, stage := range []string{"begin", "insert", "lookup", "conflict", "catalog"} {
		t.Run(stage, func(t *testing.T) {
			db, mock := catalogMockDB(t)
			failure := errors.New("injected " + stage + " failure")
			begin := mock.ExpectBegin()
			if stage == "begin" {
				begin.WillReturnError(failure)
			} else {
				for index, seed := range catalogSeeds() {
					insert := mock.ExpectQuery(`INSERT INTO "size_units"`)
					if stage == "insert" {
						insert.WillReturnError(failure)
						break
					}
					insert.WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(seed.ID))
					lookup := mock.ExpectQuery(`SELECT \* FROM "size_units" WHERE name = \$1`).WithArgs(seed.Name, 1)
					if stage == "lookup" {
						lookup.WillReturnError(failure)
						break
					}
					if stage == "conflict" {
						seed.Family = "conflicting-family"
					}
					lookup.WillReturnRows(catalogRows(seed))
					if stage == "conflict" || (stage != "catalog" && index == 0) {
						break
					}
				}
				if stage == "catalog" {
					mock.ExpectCommit()
					mock.ExpectQuery(`SELECT \* FROM "size_units"`).WillReturnError(failure)
				} else {
					mock.ExpectRollback()
				}
			}
			initialized, err := InitializeUnits(db)
			require.Nil(t, initialized)
			if stage == "conflict" {
				require.ErrorContains(t, err, "conflicts with seed g")
			} else {
				require.ErrorIs(t, err, failure)
			}
		})
	}
}

func TestCatalogConversionAndReadMetadata(t *testing.T) {
	db, mock := catalogMockDB(t)
	units := catalogSeeds()
	mock.ExpectQuery(`SELECT \* FROM "size_units"`).WillReturnRows(catalogRows(units...))
	catalog, err := LoadUnitCatalog(db)
	require.NoError(t, err)
	db = WithUnitCatalog(db, catalog)
	g, kg, ml, unknown := units[0].ID, units[1].ID, units[2].ID, uuid.New()
	require.True(t, catalog.Compatible(&g, &kg))
	require.False(t, catalog.Compatible(&g, &ml))
	require.False(t, catalog.Compatible(nil, &g))
	require.False(t, catalog.Compatible(&unknown, &g))
	require.Nil(t, catalog.BaseID(nil))
	require.Nil(t, catalog.BaseID(&unknown))
	require.Equal(t, g, *catalog.BaseID(&kg))
	value, err := catalog.Normalize(3, &kg, &g, false)
	require.NoError(t, err)
	require.Equal(t, 3000.0, value)
	value, err = catalog.Normalize(-0.25, &kg, &g, true)
	require.NoError(t, err)
	require.Equal(t, -250.0, value)
	_, err = catalog.Normalize(-1, &kg, &g, false)
	require.ErrorIs(t, err, ErrUnit)
	_, err = catalog.Normalize(1, &ml, &g, false)
	require.ErrorIs(t, err, ErrUnit)
	_, err = catalog.Normalize(0.1, &g, &kg, false, "0.10000000000000000001")
	require.ErrorIs(t, err, quantity.ErrInvalid)
	value, err = DefaultBaseAmount(db, ItemDefinition{SizeUnitID: &kg})
	require.NoError(t, err)
	require.Equal(t, 1000.0, value)
	require.Len(t, DisplayUnits(db, ItemDefinition{SizeUnitID: &kg}), 1)
	require.Nil(t, DisplayUnits(db, ItemDefinition{}))
	require.Nil(t, DisplayUnits(db, ItemDefinition{SizeUnitID: &unknown}))
	uncached, uncachedMock := catalogMockDB(t)
	require.Nil(t, DisplayUnits(uncached, ItemDefinition{SizeUnitID: &kg}))
	uncachedMock.ExpectQuery(`SELECT \* FROM "size_units"`).WillReturnError(errors.New("database offline"))
	_, err = DefaultBaseAmount(uncached, ItemDefinition{SizeUnitID: &kg})
	require.ErrorContains(t, err, "database offline")

	unit := units[1]
	require.NoError(t, unit.AfterFind(db))
	require.Equal(t, g, *unit.BaseSizeUnitID)
	def := ItemDefinition{SizeUnitID: &kg}
	require.NoError(t, def.AfterFind(db))
	require.Equal(t, g, *def.BaseSizeUnitID)
	entry := InventoryItem{ItemDefinition: def, Quantity: 800}
	require.NoError(t, entry.AfterFind(db))
	require.Equal(t, kg, *entry.EffectiveDisplaySizeUnitID)
	require.Equal(t, g, *entry.BaseSizeUnitID)
	entry.DisplaySizeUnitID = &g
	require.NoError(t, entry.AfterFind(db))
	require.Equal(t, g, *entry.EffectiveDisplaySizeUnitID)
	require.Equal(t, 800.0, entry.Quantity)
	shopping := ShoppingListItem{}
	require.NoError(t, shopping.AfterFind(db))
	require.Nil(t, shopping.BaseSizeUnitID)
	shopping.ItemDefinition = &def
	require.NoError(t, shopping.AfterFind(db))
	require.Equal(t, g, *shopping.BaseSizeUnitID)
	dependency := TaskItemDependency{ItemDefinition: def}
	require.NoError(t, dependency.AfterFind(db))
	require.Equal(t, g, *dependency.BaseSizeUnitID)
	ingredient := RecipeIngredient{ItemDefinition: def}
	require.NoError(t, ingredient.AfterFind(db))
	require.Equal(t, g, *ingredient.BaseSizeUnitID)
}
