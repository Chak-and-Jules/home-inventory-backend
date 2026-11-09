package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/guides"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/i18n"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/quantity"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/utils"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type GuideSetupDefinition struct {
	Name        string     `json:"name"`
	SizeUnitID  *uuid.UUID `json:"size_unit_id"`
	CategoryID  *uuid.UUID `json:"category_id,omitempty"`
	IsExpirable bool       `json:"is_expirable"`
}
type GuideSetupRequest struct {
	guides.Command
	DefinitionID *uuid.UUID                  `json:"definition_id,omitempty"`
	InventoryID  *uuid.UUID                  `json:"inventory_id,omitempty"`
	Definition   *GuideSetupDefinition       `json:"definition,omitempty"`
	Inventory    *CreateInventoryItemRequest `json:"inventory,omitempty"`
}

var errGuideSetupRecord = errors.New("Guide record not found in this home")

// Setup extends the merged lifecycle API with atomic business saves. It shares
// that service's aggregate, version checks and durable request receipts.
func (h *GuideHandler) Setup(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	user, ok := utils.GetAuthUserID(c, h.Service.DB)
	if !ok {
		return
	}
	var req GuideSetupRequest
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		h.fail(c, guides.ErrInvalid)
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		h.fail(c, guides.ErrInvalid)
		return
	}
	if c.Param("id") != "introduction" {
		h.fail(c, guides.ErrNotFound)
		return
	}
	if req.StepID != "" || req.DeferUntil != nil || req.HomeID != nil {
		h.fail(c, guides.ErrInvalid)
		return
	}
	home, err := uuid.Parse(c.GetHeader("X-Home-Id"))
	if err != nil || home == uuid.Nil {
		h.fail(c, guides.ErrInvalid)
		return
	}
	var steps []string
	for _, d := range h.Service.Definitions {
		if d.ID == "introduction" {
			steps = d.Variants[req.Platform].Steps
		}
	}
	if len(steps) != 4 {
		h.fail(c, guides.ErrInvalid)
		return
	}
	count := 0
	for _, present := range []bool{req.DefinitionID != nil, req.InventoryID != nil, req.Definition != nil, req.Inventory != nil} {
		if present {
			count++
		}
	}
	if count != 1 {
		h.fail(c, guides.ErrInvalid)
		return
	}
	command := req.Command
	command.Action, command.HomeID = "checkpoint", &home
	switch req.Action {
	case "choose_definition", "create_definition":
		if (req.Action == "choose_definition") != (req.DefinitionID != nil) || (req.Action == "create_definition") != (req.Definition != nil) {
			h.fail(c, guides.ErrInvalid)
			return
		}
		command.StepID = steps[2]
	case "choose_inventory", "create_inventory":
		if (req.Action == "choose_inventory") != (req.InventoryID != nil) || (req.Action == "create_inventory") != (req.Inventory != nil) {
			h.fail(c, guides.ErrInvalid)
			return
		}
		command.StepID = steps[3]
	default:
		h.fail(c, guides.ErrInvalid)
		return
	}
	// Preserve exact decimal text in receipt fingerprints as well as validation.
	payload := struct {
		Request      GuideSetupRequest
		QuantityText string
	}{Request: req}
	if req.Inventory != nil {
		payload.QuantityText = req.Inventory.raw("quantity")
	}
	view, err := h.Service.CheckpointWith(c.Request.Context(), user, "introduction", command, payload, func(tx *gorm.DB, state *guides.State) error {
		var member models.UserHome
		if err := tx.Where("user_id = ? AND home_id = ?", user, home).First(&member).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return guides.ErrHome
			}
			return err
		}
		canWrite := member.Role == models.RoleOwner || member.Role == models.RoleEditor
		if strings.HasPrefix(req.Action, "create_") && !canWrite {
			return guides.ErrHome
		}
		if req.Action == "choose_definition" || req.Action == "create_definition" {
			if state.StepID != steps[1] {
				return guides.ErrTransition
			}
			var definition models.ItemDefinition
			if req.DefinitionID != nil {
				if err := tx.Where("id = ? AND home_id = ?", req.DefinitionID, home).First(&definition).Error; err != nil {
					return guideSetupRecordError(err)
				}
			} else {
				input := req.Definition
				if strings.TrimSpace(input.Name) == "" || utf8.RuneCountInString(input.Name) > 255 {
					return guides.ErrInvalid
				}
				normalized := ItemDefinitionRequest{Name: input.Name, SizeUnitID: input.SizeUnitID, CategoryID: input.CategoryID, IsExpirable: input.IsExpirable}
				if err := normalizeDefinitionRequest(tx, &normalized, nil); err != nil {
					return err
				}
				if input.CategoryID != nil {
					var category models.Category
					if err := tx.Where("id = ? AND home_id = ?", input.CategoryID, home).First(&category).Error; err != nil {
						return guideSetupRecordError(err)
					}
				}
				definition = models.ItemDefinition{ID: uuid.New(), HomeID: home, Name: strings.TrimSpace(input.Name), SizeUnitID: normalized.SizeUnitID, CategoryID: input.CategoryID, IsExpirable: input.IsExpirable}
				if err := tx.Create(&definition).Error; err != nil {
					return err
				}
				if err := utils.UpdateShoppingListForDefinition(tx, home, definition.ID); err != nil {
					return err
				}
			}
			state.DefinitionID, state.InventoryID = &definition.ID, nil
			return nil
		}
		if state.StepID != steps[1] && state.StepID != steps[2] {
			return guides.ErrTransition
		}
		var item models.InventoryItem
		if req.InventoryID != nil {
			if err := tx.Where("id = ? AND home_id = ?", req.InventoryID, home).First(&item).Error; err != nil {
				return guideSetupRecordError(err)
			}
			if state.StepID == steps[2] && state.DefinitionID != nil && *state.DefinitionID != item.ItemDefinitionID {
				return guides.ErrTransition
			}
		} else {
			input := req.Inventory
			if state.StepID != steps[2] || state.DefinitionID == nil || input.ItemDefinitionID != *state.DefinitionID || input.Quantity == nil {
				return guides.ErrInvalid
			}
			var definition models.ItemDefinition
			if err := tx.Where("id = ? AND home_id = ?", state.DefinitionID, home).First(&definition).Error; err != nil {
				return guideSetupRecordError(err)
			}
			amount, err := normalizeForUnit(tx, *input.Quantity, input.QuantityUnitID, definition.SizeUnitID, false, input.raw("quantity"))
			if err != nil {
				return err
			}
			if err := validateDisplay(tx, input.DisplaySizeUnitID.ID, definition.SizeUnitID); err != nil {
				return err
			}
			item = models.InventoryItem{ID: uuid.New(), HomeID: home, ItemDefinitionID: definition.ID, Quantity: amount, DisplaySizeUnitID: input.DisplaySizeUnitID.ID, ExpirationDate: input.ExpirationDate}
			if err := tx.Create(&item).Error; err != nil {
				return err
			}
			if amount > 0 {
				if err := tx.Create(&models.InventoryTransaction{ID: uuid.New(), HomeID: home, ItemDefinitionID: definition.ID, InventoryItemID: item.ID, QuantityChange: amount}).Error; err != nil {
					return err
				}
			}
			if err := utils.UpdateShoppingListForDefinition(tx, home, definition.ID); err != nil {
				return err
			}
		}
		state.DefinitionID, state.InventoryID = &item.ItemDefinitionID, &item.ID
		return nil
	})
	if err != nil {
		switch {
		case errors.Is(err, models.ErrUnit), errors.Is(err, quantity.ErrInvalid):
			h.fail(c, guides.ErrInvalid)
		case errors.Is(err, errGuideSetupRecord):
			c.JSON(404, gin.H{"code": "guide_record_not_found", "error": i18n.TranslateDB(h.Service.DB, c, errGuideSetupRecord.Error())})
		default:
			h.fail(c, err)
		}
		return
	}
	c.JSON(http.StatusOK, view)
}

func guideSetupRecordError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return errGuideSetupRecord
	}
	return err
}
