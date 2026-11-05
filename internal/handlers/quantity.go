package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/i18n"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/quantity"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// OptionalUnit distinguishes an omitted override from an explicit null.
type OptionalUnit struct {
	Present bool
	ID      *uuid.UUID
}

func (o *OptionalUnit) UnmarshalJSON(b []byte) error {
	o.Present = true
	if bytes.Equal(bytes.TrimSpace(b), []byte("null")) {
		o.ID = nil
		return nil
	}
	var id uuid.UUID
	if err := json.Unmarshal(b, &id); err != nil {
		return err
	}
	o.ID = &id
	return nil
}
func (o OptionalUnit) IsZero() bool { return !o.Present }

func (o OptionalUnit) MarshalJSON() ([]byte, error) { return json.Marshal(o.ID) }

type inventoryMutationRequest struct {
	quantityInputs
	Quantity          *float64        `json:"quantity"`
	QuantityUnitID    *uuid.UUID      `json:"quantity_unit_id"`
	DisplaySizeUnitID OptionalUnit    `json:"display_size_unit_id"`
	Expiry            json.RawMessage `json:"expiry_date"`
}

func (r *inventoryMutationRequest) UnmarshalJSON(b []byte) error {
	type plain inventoryMutationRequest
	if err := json.Unmarshal(b, (*plain)(r)); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(b, &fields); err != nil {
		return err
	}
	if v, ok := fields["quantity"]; ok && bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
		return errors.New("Invalid request payload")
	}
	return r.quantityInputs.read(b)
}

// quantityInputs retains exact JSON decimal text while existing DTO/ORM fields
// remain compatible with float64. Conversion uses this text when available.
type quantityInputs struct{ amounts map[string]string }

func (q *quantityInputs) read(b []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(b, &fields); err != nil {
		return err
	}
	q.amounts = map[string]string{}
	for _, key := range []string{"quantity", "quantity_required", "change", "applied_amount", "low_stock_threshold", "target_quantity"} {
		if v, ok := fields[key]; ok && !bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			q.amounts[key] = string(v)
		}
	}
	return nil
}
func (q quantityInputs) raw(key string) string { return q.amounts[key] }

func normalizeForUnit(db *gorm.DB, v float64, input, definition *uuid.UUID, signed bool, raw ...string) (float64, error) {
	if input == nil {
		return 0, models.ErrUnit
	}
	c, err := models.Catalog(db)
	if err != nil {
		return 0, err
	}
	return c.Normalize(v, input, definition, signed, raw...)
}

func quantityForDefinition(db *gorm.DB, id, home uuid.UUID, v float64, input *uuid.UUID, signed bool, raw ...string) (float64, error) {
	if input == nil {
		return 0, models.ErrUnit
	}
	var def models.ItemDefinition
	if err := db.Where("id = ? AND home_id = ?", id, home).First(&def).Error; err != nil {
		return 0, err
	}
	return normalizeForUnit(db, v, input, def.SizeUnitID, signed, raw...)
}

func quantityError(c *gin.Context, db *gorm.DB, err error) {
	status, key := http.StatusBadRequest, "Invalid or incompatible size unit"
	if errors.Is(err, quantity.ErrInvalid) {
		key = "Invalid quantity precision or range"
	} else if !errors.Is(err, models.ErrUnit) && !errors.Is(err, gorm.ErrRecordNotFound) {
		status, key = http.StatusInternalServerError, "Failed to validate quantity"
	}
	c.JSON(status, gin.H{"error": i18n.TranslateDB(db, c, key)})
}

func validateDisplay(db *gorm.DB, unit, definition *uuid.UUID) error {
	if unit == nil {
		return nil
	}
	c, err := models.Catalog(db)
	if err != nil {
		return err
	}
	if !c.Compatible(unit, definition) {
		return models.ErrUnit
	}
	return nil
}

func decodeQuantityRequest(b []byte, dest any, q *quantityInputs) error {
	if err := json.Unmarshal(b, dest); err != nil {
		return err
	}
	return q.read(b)
}
