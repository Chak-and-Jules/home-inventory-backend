package demos

import (
	"strconv"
	"time"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/utils"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func (s *Service) seed(tx *gorm.DB, home uuid.UUID, labels Labels) error {
	catalog, err := models.Catalog(tx)
	if err != nil {
		return err
	}
	units := map[string]uuid.UUID{}
	for id, unit := range catalog.Units {
		units[unit.Name] = id
	}
	food := models.Category{ID: uuid.New(), HomeID: home, Name: label(labels, "Food")}
	household := models.Category{ID: uuid.New(), HomeID: home, Name: label(labels, "Household")}
	if err := tx.Create(&[]models.Category{food, household}).Error; err != nil {
		return err
	}
	now := s.Now().UTC()
	type batch struct {
		amount float64
		days   int
	}
	items := []struct {
		name, unit        string
		category          uuid.UUID
		expirable         bool
		threshold, target *float64
		batches           []batch
	}{
		{"Rice", "kg", food.ID, true, number(.5), number(2), []batch{{1, 90}, {.5, 120}}},
		{"Milk", "L", food.ID, true, nil, nil, []batch{{1, 2}}},
		{"Eggs", "piece", food.ID, true, nil, nil, []batch{{6, 10}}},
		{"Dish soap", "mL", household.ID, false, number(250), number(1000), []batch{{100, 0}}},
		{"Sponges", "piece", household.ID, false, nil, nil, []batch{{3, 0}}},
	}
	for _, entry := range items {
		unit, ok := units[entry.unit]
		if !ok {
			return models.ErrUnit
		}
		normalize := func(value float64) (float64, error) {
			return catalog.Normalize(value, &unit, &unit, false, strconv.FormatFloat(value, 'f', -1, 64))
		}
		definition := models.ItemDefinition{ID: uuid.New(), HomeID: home, Name: label(labels, entry.name), CategoryID: &entry.category, SizeUnitID: &unit, IsExpirable: entry.expirable, Priority: "medium"}
		for _, pair := range []struct {
			in  *float64
			out **float64
		}{{entry.threshold, &definition.LowStockThreshold}, {entry.target, &definition.TargetQuantity}} {
			if pair.in != nil {
				value, err := normalize(*pair.in)
				if err != nil {
					return err
				}
				*pair.out = &value
			}
		}
		if err := tx.Create(&definition).Error; err != nil {
			return err
		}
		for _, batch := range entry.batches {
			amount, err := normalize(batch.amount)
			if err != nil {
				return err
			}
			var expiry *time.Time
			if batch.days > 0 {
				date := now.AddDate(0, 0, batch.days)
				expiry = &date
			}
			item := models.InventoryItem{ID: uuid.New(), HomeID: home, ItemDefinitionID: definition.ID, Quantity: amount, ExpirationDate: expiry}
			if err := tx.Create(&item).Error; err != nil {
				return err
			}
			if err := tx.Create(&models.InventoryTransaction{ID: uuid.New(), HomeID: home, ItemDefinitionID: definition.ID, InventoryItemID: item.ID, QuantityChange: amount}).Error; err != nil {
				return err
			}
		}
		if err := utils.UpdateShoppingListForDefinition(tx, home, definition.ID); err != nil {
			return err
		}
	}
	return nil
}

func number(n float64) *float64 { return &n }
