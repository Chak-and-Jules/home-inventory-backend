package models

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/quantity"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type unitCatalogContextKey struct{}

func WithUnitCatalog(db *gorm.DB, c *UnitCatalog) *gorm.DB {
	return db.WithContext(context.WithValue(db.Statement.Context, unitCatalogContextKey{}, c))
}

func cachedCatalog(db *gorm.DB) (*UnitCatalog, bool) {
	c, ok := db.Statement.Context.Value(unitCatalogContextKey{}).(*UnitCatalog)
	return c, ok
}

var ErrUnit = errors.New("Invalid or incompatible size unit")

// UnitCatalog is immutable after startup and shared by GORM sessions/transactions.
type UnitCatalog struct {
	Units map[uuid.UUID]SizeUnit
	Bases map[string]uuid.UUID
}

func LoadUnitCatalog(db *gorm.DB) (*UnitCatalog, error) {
	var rows []SizeUnit
	if err := db.Find(&rows).Error; err != nil {
		return nil, err
	}
	c := &UnitCatalog{Units: map[uuid.UUID]SizeUnit{}, Bases: map[string]uuid.UUID{}}
	for _, u := range rows {
		if strings.TrimSpace(u.Family) == "" || quantity.Validate(u.FactorToBase) != nil || u.FactorToBase <= 0 || (u.IsBase && u.FactorToBase != 1) {
			return nil, ErrUnit
		}
		if u.IsBase {
			if _, exists := c.Bases[u.Family]; exists {
				return nil, ErrUnit
			}
			c.Bases[u.Family] = u.ID
		}
		c.Units[u.ID] = u
	}
	for id, u := range c.Units {
		base, exists := c.Bases[u.Family]
		if !exists {
			return nil, ErrUnit
		}
		u.BaseSizeUnitID = &base
		c.Units[id] = u
	}
	if len(c.Units) == 0 {
		return nil, ErrUnit
	}
	return c, nil
}

func Catalog(db *gorm.DB) (*UnitCatalog, error) {
	if c, ok := cachedCatalog(db); ok {
		return c, nil
	}
	return LoadUnitCatalog(db)
}

// InitializeUnits seeds a fresh database without modifying catalog definitions.
func InitializeUnits(db *gorm.DB) (*gorm.DB, error) {
	seeds := []SizeUnit{
		{ID: uuid.New(), Name: "g", Family: "mass", IsBase: true, FactorToBase: 1},
		{ID: uuid.New(), Name: "kg", Family: "mass", FactorToBase: 1000},
		{ID: uuid.New(), Name: "mL", Family: "volume", IsBase: true, FactorToBase: 1},
		{ID: uuid.New(), Name: "L", Family: "volume", FactorToBase: 1000},
		{ID: uuid.New(), Name: "piece", Family: "count", IsBase: true, FactorToBase: 1},
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		for _, seed := range seeds {
			if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "name"}}, DoNothing: true}).Create(&seed).Error; err != nil {
				return err
			}
			var actual SizeUnit
			if err := tx.Where("name = ?", seed.Name).First(&actual).Error; err != nil {
				return err
			}
			if actual.Family != seed.Family || actual.IsBase != seed.IsBase || actual.FactorToBase != seed.FactorToBase {
				return fmt.Errorf("unit catalog conflicts with seed %s", seed.Name)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	c, err := LoadUnitCatalog(db)
	if err != nil {
		return nil, err
	}
	return WithUnitCatalog(db, c), nil
}

func (c *UnitCatalog) Compatible(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return false
	}
	ua, oka := c.Units[*a]
	ub, okb := c.Units[*b]
	return oka && okb && ua.Family == ub.Family
}

func (c *UnitCatalog) Normalize(v float64, input, definition *uuid.UUID, signed bool, raw ...string) (float64, error) {
	if !c.Compatible(input, definition) || (!signed && v < 0) {
		return 0, ErrUnit
	}
	if len(raw) > 0 && raw[0] != "" {
		return quantity.ToBaseText(raw[0], c.Units[*input].FactorToBase)
	}
	return quantity.ToBase(v, c.Units[*input].FactorToBase)
}

func (c *UnitCatalog) BaseID(unit *uuid.UUID) *uuid.UUID {
	if unit == nil {
		return nil
	}
	u, ok := c.Units[*unit]
	if !ok {
		return nil
	}
	id := c.Bases[u.Family]
	return &id
}

func (u *SizeUnit) AfterFind(tx *gorm.DB) error {
	if c, ok := cachedCatalog(tx); ok {
		u.BaseSizeUnitID = c.BaseID(&u.ID)
	}
	return nil
}

// The unit catalog is system metadata. Renaming or replacing units requires an
// explicit catalog deployment, never a runtime mutation of stored quantities.
func (u *SizeUnit) BeforeUpdate(tx *gorm.DB) error { return ErrUnit }
func (u *SizeUnit) BeforeDelete(tx *gorm.DB) error { return ErrUnit }

func (d *ItemDefinition) AfterFind(tx *gorm.DB) error {
	if c, ok := cachedCatalog(tx); ok {
		d.BaseSizeUnitID = c.BaseID(d.SizeUnitID)
	}
	return nil
}

func (i *InventoryItem) AfterFind(tx *gorm.DB) error {
	i.BaseSizeUnitID = i.ItemDefinition.BaseSizeUnitID
	i.EffectiveDisplaySizeUnitID = i.DisplaySizeUnitID
	if i.EffectiveDisplaySizeUnitID == nil {
		i.EffectiveDisplaySizeUnitID = i.ItemDefinition.SizeUnitID
	}
	return nil
}

// DefaultBaseAmount preserves the existing fallback of one definition unit.
func DefaultBaseAmount(db *gorm.DB, def ItemDefinition) (float64, error) {
	c, err := Catalog(db)
	if err != nil {
		return 0, err
	}
	return c.Normalize(1, def.SizeUnitID, def.SizeUnitID, false)
}

// DisplayUnits returns optional presentation metadata without extra read queries.
func DisplayUnits(db *gorm.DB, def ItemDefinition) []SizeUnit {
	if c, ok := cachedCatalog(db); ok && def.SizeUnitID != nil {
		if u, exists := c.Units[*def.SizeUnitID]; exists {
			return []SizeUnit{u}
		}
	}
	return nil
}

func (i *ShoppingListItem) AfterFind(tx *gorm.DB) error {
	if i.ItemDefinition != nil {
		i.BaseSizeUnitID = i.ItemDefinition.BaseSizeUnitID
	}
	return nil
}
func (i *TaskItemDependency) AfterFind(tx *gorm.DB) error {
	i.BaseSizeUnitID = i.ItemDefinition.BaseSizeUnitID
	return nil
}
func (i *RecipeIngredient) AfterFind(tx *gorm.DB) error {
	i.BaseSizeUnitID = i.ItemDefinition.BaseSizeUnitID
	return nil
}
