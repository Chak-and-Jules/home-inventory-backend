package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/quantity"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestQuantityRequestDecoders(t *testing.T) {
	type request interface {
		UnmarshalJSON([]byte) error
		raw(string) string
	}
	for _, tc := range []struct {
		name, field string
		make        func() request
	}{
		{"inventory create", "quantity", func() request { return &CreateInventoryItemRequest{} }},
		{"inventory update", "quantity", func() request { return &UpdateInventoryItemRequest{} }},
		{"quick quantity", "quantity", func() request { return &UpdateQuantityRequest{} }},
		{"inventory patch", "quantity", func() request { return &inventoryMutationRequest{} }},
		{"scan", "change", func() request { return &ScanInventoryRequest{} }},
		{"prediction", "applied_amount", func() request { return &ApplyPredictionRequest{} }},
		{"definition", "low_stock_threshold", func() request { return &ItemDefinitionRequest{} }},
		{"shopping create", "quantity", func() request { return &ShoppingListItemRequest{} }},
		{"shopping update", "quantity", func() request { return &UpdateShoppingListItemRequest{} }},
		{"recipe", "quantity_required", func() request { return &RecipeIngredientRequest{} }},
		{"maintenance", "quantity_required", func() request { return &TaskItemDependencyRequest{} }},
		{"receipt", "quantity", func() request { return &ConfirmReceiptItem{} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := tc.make()
			require.NoError(t, json.Unmarshal([]byte(`{"`+tc.field+`":0.10000000000000000001}`), r))
			require.Equal(t, "0.10000000000000000001", r.raw(tc.field), "conversion must receive the exact JSON decimal")
			require.Error(t, json.Unmarshal([]byte(`{"`+tc.field+`":"not-a-number"}`), tc.make()))
		})
	}
	t.Run("override UUID validation", func(t *testing.T) {
		var unit OptionalUnit
		require.Error(t, json.Unmarshal([]byte(`"not-a-uuid"`), &unit))
		require.Nil(t, unit.ID)
		id := uuid.New()
		require.NoError(t, json.Unmarshal([]byte(`"`+id.String()+`"`), &unit))
		require.True(t, unit.Present)
		require.Equal(t, id, *unit.ID)
		require.NoError(t, json.Unmarshal([]byte(`null`), &unit))
		require.True(t, unit.Present)
		require.Nil(t, unit.ID)
	})
	t.Run("raw input must be an object", func(t *testing.T) {
		var inputs quantityInputs
		require.Error(t, inputs.read([]byte(`[]`)))
		require.NoError(t, inputs.read([]byte(`{"quantity":null,"target_quantity":1e-9}`)))
		require.Empty(t, inputs.raw("quantity"))
		require.Equal(t, "1e-9", inputs.raw("target_quantity"))
	})
	t.Run("shopping missing null and zero", func(t *testing.T) {
		for _, body := range []string{`{}`, `{"quantity":null}`, `{"quantity":0}`} {
			var create ShoppingListItemRequest
			var update UpdateShoppingListItemRequest
			require.NoError(t, json.Unmarshal([]byte(body), &create))
			require.NoError(t, json.Unmarshal([]byte(body), &update))
			require.Equal(t, body == `{"quantity":0}`, create.quantityPresent)
			require.Equal(t, create.quantityPresent, update.quantityPresent)
		}
	})
}

func TestQuantityValidationFailures(t *testing.T) {
	failure := errors.New("catalog database unavailable")
	for _, operation := range []string{"normalize", "display", "definition"} {
		t.Run(operation+" catalog lookup failure", func(t *testing.T) {
			h, mock := setupInventoryTest(t)
			conn, err := h.DB.DB()
			require.NoError(t, err)
			t.Cleanup(func() { conn.Close(); require.NoError(t, mock.ExpectationsWereMet()) })
			db := h.DB.WithContext(context.Background())
			mock.ExpectQuery(`SELECT \* FROM "size_units"`).WillReturnError(failure)
			switch operation {
			case "normalize":
				_, err = normalizeForUnit(db, 1, &mockCountUnitID, &mockCountUnitID, false)
			case "display":
				err = validateDisplay(db, &mockCountUnitID, &mockCountUnitID)
			case "definition":
				err = normalizeDefinitionRequest(db, &ItemDefinitionRequest{SizeUnitID: &mockCountUnitID}, nil)
			}
			require.ErrorIs(t, err, failure)
		})
	}
	t.Run("missing input avoids database lookup", func(t *testing.T) {
		_, err := normalizeForUnit(nil, 1, nil, &mockCountUnitID, false)
		require.ErrorIs(t, err, models.ErrUnit)
		_, err = quantityForDefinition(nil, uuid.New(), uuid.New(), 1, nil, false)
		require.ErrorIs(t, err, models.ErrUnit)
		require.NoError(t, validateDisplay(nil, nil, &mockCountUnitID))
	})
	t.Run("definition lookup failure", func(t *testing.T) {
		h, mock := setupInventoryTest(t)
		conn, err := h.DB.DB()
		require.NoError(t, err)
		defer conn.Close()
		mock.ExpectQuery(`SELECT \* FROM "item_definitions" WHERE id =`).WillReturnError(failure)
		_, err = quantityForDefinition(h.DB, uuid.New(), uuid.New(), 1, &mockCountUnitID, false)
		require.ErrorIs(t, err, failure)
		require.NoError(t, mock.ExpectationsWereMet())
	})
	t.Run("definition requires a known default", func(t *testing.T) {
		h, mock := setupInventoryTest(t)
		conn, err := h.DB.DB()
		require.NoError(t, err)
		defer conn.Close()
		require.ErrorIs(t, normalizeDefinitionRequest(h.DB, &ItemDefinitionRequest{}, nil), models.ErrUnit)
		unknown := uuid.New()
		require.ErrorIs(t, normalizeDefinitionRequest(h.DB, &ItemDefinitionRequest{SizeUnitID: &unknown}, nil), models.ErrUnit)
		require.NoError(t, mock.ExpectationsWereMet())
	})
	for _, tc := range []struct {
		name    string
		err     error
		status  int
		message string
	}{
		{"unit", models.ErrUnit, 400, "Invalid or incompatible size unit"},
		{"precision", quantity.ErrInvalid, 400, "Invalid quantity precision or range"},
		{"missing definition", gorm.ErrRecordNotFound, 400, "Invalid or incompatible size unit"},
		{"database", failure, 500, "Failed to validate quantity"},
	} {
		t.Run(tc.name+" HTTP response", func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			quantityError(c, nil, tc.err)
			require.Equal(t, tc.status, w.Code)
			require.JSONEq(t, `{"error":"`+tc.message+`"}`, w.Body.String())
		})
	}
}
