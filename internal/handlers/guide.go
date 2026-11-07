package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/guides"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/i18n"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/logger"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/quantity"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/utils"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strconv"
)

type GuideHandler struct {
	DB      *gorm.DB
	Catalog []guides.Definition
	Now     func() time.Time
}
type guideContext struct {
	user     uuid.UUID
	home     *uuid.UUID
	canWrite bool
}
type GuideView struct {
	ID                string                    `json:"id"`
	Revision          int                       `json:"revision"`
	Platform          string                    `json:"platform"`
	PlatformRevision  int                       `json:"platform_revision"`
	Steps             []string                  `json:"steps"`
	Available         bool                      `json:"available"`
	UnavailableReason string                    `json:"unavailable_reason"`
	CanWrite          bool                      `json:"can_write"`
	Status            string                    `json:"status"`
	Version           uint64                    `json:"version"`
	Checkpoint        models.GuidePlatformState `json:"checkpoint"`
}
type GuideActionRequest struct {
	PlatformRevision int                         `json:"platform_revision"`
	Platform         string                      `json:"platform"`
	Revision         int                         `json:"revision"`
	ExpectedVersion  *uint64                     `json:"expected_version"`
	RequestID        uuid.UUID                   `json:"request_id"`
	Action           string                      `json:"action"`
	Step             string                      `json:"step,omitempty"`
	DefinitionID     *uuid.UUID                  `json:"definition_id,omitempty"`
	InventoryID      *uuid.UUID                  `json:"inventory_id,omitempty"`
	Definition       *ItemDefinitionRequest      `json:"definition,omitempty"`
	Inventory        *CreateInventoryItemRequest `json:"inventory,omitempty"`
}

var errGuideConflict = errors.New("Guide state changed; refresh and retry")
var errGuideUnavailable = errors.New("Guide is not available")
var errGuideAction = errors.New("Invalid guide action or checkpoint")
var errGuideRecord = errors.New("Guide record not found in this home")
var errGuideAccess = errors.New("Write access denied to this home")

func (h *GuideHandler) catalog() []guides.Definition {
	if h.Catalog != nil {
		return h.Catalog
	}
	return guides.Catalog()
}
func (h *GuideHandler) now() time.Time {
	if h.Now != nil {
		return h.Now().UTC()
	}
	return time.Now().UTC()
}
func (h *GuideHandler) context(c *gin.Context) (guideContext, bool) {
	user, ok := utils.GetAuthUserID(c, h.DB)
	if !ok {
		return guideContext{}, false
	}
	ctx := guideContext{user: user}
	if c.GetHeader("X-Home-Id") == "" {
		return ctx, true
	}
	home, ok := utils.ParseUUIDHeader(c, h.DB, "X-Home-Id", "Invalid home_id")
	if !ok {
		return ctx, false
	}
	member, err := utils.GetUserHome(c, h.DB, home)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			h.fail(c, http.StatusForbidden, "Access denied to this home")
		} else {
			h.fail(c, http.StatusInternalServerError, "Failed to load guides")
		}
		return ctx, false
	}
	ctx.home = &home
	ctx.canWrite = member.Role == models.RoleOwner || member.Role == models.RoleEditor
	return ctx, true
}
func (h *GuideHandler) fail(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{"error": i18n.TranslateDB(h.DB, c, message)})
}
func (h *GuideHandler) view(db *gorm.DB, d guides.Definition, p models.GuideProgress, platform string, ctx guideContext) (GuideView, error) {
	guides.Normalize(&p, d)
	available, reason := true, ""
	if ctx.home == nil {
		available = false
		reason = "no_home"
	} else if d.RequiresWrite && !ctx.canWrite {
		available = false
		reason = "permission"
	}
	for _, prerequisite := range d.Prerequisites {
		var prior models.GuideProgress
		err := db.Where("user_id = ? AND guide_id = ?", ctx.user, prerequisite).First(&prior).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return GuideView{}, err
		}
		for _, published := range h.catalog() {
			if published.ID == prerequisite {
				guides.Normalize(&prior, published)
			}
		}
		if prior.Outcome != "completed" {
			available = false
			reason = "prerequisite"
		}
	}
	variant := d.Variants[platform]
	return GuideView{ID: d.ID, Revision: d.Revision, Platform: platform, PlatformRevision: variant.Revision,
		Steps: variant.Steps, Available: available, UnavailableReason: reason, CanWrite: ctx.canWrite,
		Status: guides.Status(&p, platform), Version: p.Version, Checkpoint: *guides.Platform(&p, platform)}, nil
}
func (h *GuideHandler) List(c *gin.Context) {
	platform := c.Query("platform")
	if platform != "web" && platform != "mobile" {
		h.fail(c, 400, "Invalid guide action or checkpoint")
		return
	}
	ctx, ok := h.context(c)
	if !ok {
		return
	}
	if c.Query("capabilities") == "" {
		h.fail(c, 400, "Invalid guide action or checkpoint")
		return
	}
	views := []GuideView{}
	catalog := append([]guides.Definition{}, h.catalog()...)
	sort.SliceStable(catalog, func(i, j int) bool { return catalog[i].Priority < catalog[j].Priority })
	var invitation *string
	for _, d := range catalog {
		if !guides.Supported(d, platform, h.now()) || !guides.Contains(strings.Split(c.Query("capabilities"), ","), d.ID+":"+strconv.Itoa(d.Variants[platform].Revision)) {
			continue
		}
		p := models.GuideProgress{UserID: ctx.user, GuideID: d.ID}
		err := h.DB.Where("user_id = ? AND guide_id = ?", ctx.user, d.ID).First(&p).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			h.fail(c, 500, "Failed to load guides")
			return
		}
		v, err := h.view(h.DB, d, p, platform, ctx)
		if err != nil {
			h.fail(c, 500, "Failed to load guides")
			return
		}
		views = append(views, v)
		deferred := v.Checkpoint.DeferredUntil != nil && v.Checkpoint.DeferredUntil.After(h.now())
		if invitation == nil && v.Available && (v.Status == "available" || v.Status == "in_progress") && !deferred && guides.Contains(d.Contexts, c.Query("context")) {
			id := d.ID
			invitation = &id
		}
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(200, gin.H{"guides": views, "invitation_id": invitation})
}
func (h *GuideHandler) Act(c *gin.Context) {
	var req GuideActionRequest
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
	if err := c.ShouldBindJSON(&req); err != nil || req.ExpectedVersion == nil || req.RequestID == uuid.Nil || (req.Platform != "web" && req.Platform != "mobile") {
		h.fail(c, 400, "Invalid guide action or checkpoint")
		return
	}
	var definition *guides.Definition
	for _, d := range h.catalog() {
		if d.ID == c.Param("id") {
			copy := d
			definition = &copy
			break
		}
	}
	if definition == nil || !guides.Supported(*definition, req.Platform, h.now()) {
		h.fail(c, 404, "Guide is not available")
		return
	}
	if req.Revision != definition.Revision || req.PlatformRevision != definition.Variants[req.Platform].Revision {
		h.fail(c, 409, errGuideConflict.Error())
		return
	}
	ctx, ok := h.context(c)
	if !ok {
		return
	}
	bytes, _ := json.Marshal(req)
	homeKey := ""
	if ctx.home != nil {
		homeKey = ctx.home.String()
	}
	digest := sha256.Sum256(append(bytes, []byte(homeKey)...))
	hash := hex.EncodeToString(digest[:])
	var updated models.GuideProgress
	err := h.DB.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		p := models.GuideProgress{UserID: ctx.user, GuideID: definition.ID, Revision: definition.Revision, Outcome: ""}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&p).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ? AND guide_id = ?", ctx.user, definition.ID).First(&p).Error; err != nil {
			return guideRecordError(err)
		}
		guides.Normalize(&p, *definition)
		var receipt models.GuideMutation
		err := tx.Where("user_id = ? AND guide_id = ? AND request_id = ?", ctx.user, definition.ID, req.RequestID).First(&receipt).Error
		if err == nil {
			if receipt.PayloadHash != hash {
				return errGuideConflict
			}
			updated = p
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if p.Version != *req.ExpectedVersion {
			return errGuideConflict
		}
		view, err := h.view(tx, *definition, p, req.Platform, ctx)
		if err != nil {
			return err
		}
		if req.Action != "skip" && req.Action != "defer" && req.Action != "restore" && !view.Available {
			return errGuideUnavailable
		}
		if err := h.apply(tx, &p, *definition, req, ctx); err != nil {
			return err
		}
		result := tx.Model(&models.GuideProgress{}).Where("user_id = ? AND guide_id = ? AND version = ?", ctx.user, definition.ID, *req.ExpectedVersion).
			Updates(map[string]any{"revision": p.Revision, "version": p.Version + 1, "outcome": p.Outcome, "web": serializeGuideState(p.Web), "mobile": serializeGuideState(p.Mobile)})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errGuideConflict
		}
		if err := tx.Create(&models.GuideMutation{UserID: ctx.user, GuideID: definition.ID, RequestID: req.RequestID, PayloadHash: hash}).Error; err != nil {
			return err
		}
		p.Version++
		updated = p
		return nil
	})
	if err != nil {
		switch {
		case errors.Is(err, errGuideConflict):
			h.fail(c, 409, err.Error())
		case errors.Is(err, errGuideRecord):
			h.fail(c, 404, err.Error())
		case errors.Is(err, errGuideAccess):
			h.fail(c, 403, err.Error())
		case errors.Is(err, errGuideAction), errors.Is(err, errGuideUnavailable):
			h.fail(c, 400, err.Error())
		case errors.Is(err, models.ErrUnit), errors.Is(err, quantity.ErrInvalid), errors.Is(err, gorm.ErrRecordNotFound):
			quantityError(c, h.DB, err)
		default:
			logger.Log.Error("Failed to save guide progress", zap.Error(err))
			h.fail(c, 500, "Failed to save guide progress")
		}
		return
	}
	view, err := h.view(h.DB, *definition, updated, req.Platform, ctx)
	if err != nil {
		h.fail(c, 500, "Failed to load guides")
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(200, view)
}
func serializeGuideState(state models.GuidePlatformState) string {
	b, _ := json.Marshal(state)
	return string(b)
}
func (h *GuideHandler) apply(tx *gorm.DB, p *models.GuideProgress, d guides.Definition, r GuideActionRequest, ctx guideContext) error {
	s := guides.Platform(p, r.Platform)
	steps := d.Variants[r.Platform].Steps
	if len(steps) == 0 {
		return errGuideAction
	}
	status := guides.Status(p, r.Platform)
	switch r.Action {
	case "skip", "complete":
		if r.Action == "complete" {
			if status != "in_progress" || s.Step != steps[len(steps)-1] || ctx.home == nil || s.HomeID == nil || *ctx.home != *s.HomeID {
				return errGuideAction
			}
			if d.ID == guides.Introduction && ctx.canWrite {
				if s.InventoryID == nil {
					return errGuideAction
				}
				var item models.InventoryItem
				if err := tx.Where("id = ? AND home_id = ?", s.InventoryID, ctx.home).First(&item).Error; err != nil {
					return guideRecordError(err)
				}
			}
			p.Outcome = "completed"
		} else {
			p.Outcome = "skipped"
		}
		p.Web.Restored = false
		p.Mobile.Restored = false
		p.Web.DeferredUntil = nil
		p.Mobile.DeferredUntil = nil
		return nil
	case "restore":
		*s = models.GuidePlatformState{Revision: d.Variants[r.Platform].Revision, Status: "in_progress", Step: steps[0], Restored: true, HomeID: ctx.home}
		return nil
	case "defer":
		if status == "completed" || status == "skipped" {
			return errGuideAction
		}
		until := h.now().Add(24 * time.Hour)
		s.DeferredUntil = &until
		return nil
	case "start":
		if status == "completed" || status == "skipped" || ctx.home == nil {
			return errGuideAction
		}
		if s.HomeID == nil || *s.HomeID != *ctx.home {
			restored := s.Restored
			*s = models.GuidePlatformState{Revision: d.Variants[r.Platform].Revision, Restored: restored, Step: steps[0], HomeID: ctx.home}
		}
		s.Status = "in_progress"
		s.DeferredUntil = nil
		return nil
	}
	if status != "in_progress" || ctx.home == nil || s.HomeID == nil || *s.HomeID != *ctx.home {
		return errGuideAction
	}
	if r.Action == "checkpoint" {
		index := -1
		for i, step := range steps {
			if step == s.Step {
				index = i
			}
		}
		if index < 0 || index+1 >= len(steps) || r.Step != steps[index+1] {
			return errGuideAction
		}
		// Writers must actually save/select their definition and stock. Viewers can
		// read the explanations without being forced to perform forbidden writes.
		if d.ID == guides.Introduction && ctx.canWrite && index != 0 {
			return errGuideAction
		}
		s.Step = r.Step
		return nil
	}
	if d.ID != guides.Introduction {
		return errGuideAction
	}
	if r.Action == "choose_definition" || r.Action == "create_definition" {
		if s.Step != steps[1] {
			return errGuideAction
		}
		var def models.ItemDefinition
		if r.Action == "choose_definition" {
			if r.DefinitionID == nil {
				return errGuideAction
			}
			if err := tx.Where("id = ? AND home_id = ?", r.DefinitionID, ctx.home).First(&def).Error; err != nil {
				return guideRecordError(err)
			}
		} else {
			if !ctx.canWrite {
				return errGuideAccess
			}
			if r.Definition == nil || strings.TrimSpace(r.Definition.Name) == "" || len(r.Definition.Name) > 255 {
				return errGuideAction
			}
			req := r.Definition
			if err := normalizeDefinitionRequest(tx, req, nil); err != nil {
				return err
			}
			if req.CategoryID != nil {
				var category models.Category
				if err := tx.Where("id = ? AND home_id = ?", req.CategoryID, ctx.home).First(&category).Error; err != nil {
					return guideRecordError(err)
				}
			}
			def = models.ItemDefinition{ID: uuid.New(), HomeID: *ctx.home, Name: strings.TrimSpace(req.Name), SizeUnitID: req.SizeUnitID, CategoryID: req.CategoryID, IsExpirable: req.IsExpirable}
			if err := tx.Create(&def).Error; err != nil {
				return err
			}
			if err := utils.UpdateShoppingListForDefinition(tx, *ctx.home, def.ID); err != nil {
				return err
			}
		}
		s.DefinitionID = &def.ID
		s.InventoryID = nil
		s.Step = steps[2]
		return nil
	}
	if r.Action == "choose_inventory" || r.Action == "create_inventory" {
		if s.Step != steps[1] && s.Step != steps[2] {
			return errGuideAction
		}
		var item models.InventoryItem
		if r.Action == "choose_inventory" {
			if r.InventoryID == nil {
				return errGuideAction
			}
			if err := tx.Where("id = ? AND home_id = ?", r.InventoryID, ctx.home).First(&item).Error; err != nil {
				return guideRecordError(err)
			}
			if s.Step == steps[2] && s.DefinitionID != nil && *s.DefinitionID != item.ItemDefinitionID {
				return errGuideAction
			}
		} else {
			if !ctx.canWrite {
				return errGuideAccess
			}
			if s.Step != steps[2] || s.DefinitionID == nil || r.Inventory == nil || r.Inventory.ItemDefinitionID != *s.DefinitionID || r.Inventory.Quantity == nil {
				return errGuideAction
			}
			req := r.Inventory
			var def models.ItemDefinition
			if err := tx.Where("id = ? AND home_id = ?", s.DefinitionID, ctx.home).First(&def).Error; err != nil {
				return guideRecordError(err)
			}
			amount, err := normalizeForUnit(tx, *req.Quantity, req.QuantityUnitID, def.SizeUnitID, false, req.raw("quantity"))
			if err != nil {
				return err
			}
			if err := validateDisplay(tx, req.DisplaySizeUnitID.ID, def.SizeUnitID); err != nil {
				return err
			}
			item = models.InventoryItem{ID: uuid.New(), HomeID: *ctx.home, ItemDefinitionID: def.ID, Quantity: amount, DisplaySizeUnitID: req.DisplaySizeUnitID.ID, ExpirationDate: req.ExpirationDate}
			if err := tx.Create(&item).Error; err != nil {
				return err
			}
			if amount > 0 {
				if err := tx.Create(&models.InventoryTransaction{ID: uuid.New(), HomeID: *ctx.home, ItemDefinitionID: def.ID, InventoryItemID: item.ID, QuantityChange: amount}).Error; err != nil {
					return err
				}
			}
			if err := utils.UpdateShoppingListForDefinition(tx, *ctx.home, def.ID); err != nil {
				return err
			}
		}
		s.DefinitionID = &item.ItemDefinitionID
		s.InventoryID = &item.ID
		s.Step = steps[3]
		return nil
	}
	return errGuideAction
}

func guideRecordError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return errGuideRecord
	}
	return err
}
