package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/quantity"
	"gorm.io/gorm/clause"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/i18n"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/utils"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type InventoryItemHandler struct {
	DB *gorm.DB
}

type CreateInventoryItemRequest struct {
	quantityInputs
	DisplaySizeUnitID OptionalUnit `json:"display_size_unit_id,omitzero"`
	QuantityUnitID    *uuid.UUID   `json:"quantity_unit_id"`
	ItemDefinitionID  uuid.UUID    `json:"item_definition_id" binding:"required"`
	Quantity          *float64     `json:"quantity" binding:"required"`
	ExpirationDate    *time.Time   `json:"expiry_date"`
}

type UpdateInventoryItemRequest struct {
	quantityInputs
	DisplaySizeUnitID OptionalUnit `json:"display_size_unit_id,omitzero"`
	QuantityUnitID    *uuid.UUID   `json:"quantity_unit_id"`
	Quantity          *float64     `json:"quantity" binding:"required"`
	ExpirationDate    *time.Time   `json:"expiry_date"`
}

type UpdateQuantityRequest struct {
	quantityInputs
	QuantityUnitID *uuid.UUID `json:"quantity_unit_id"`
	Quantity       *float64   `json:"quantity" binding:"required"`
}

// PatchInventoryItem changes only submitted fields. Display edits never touch stock.
func (h *InventoryItemHandler) PatchInventoryItem(c *gin.Context) { h.updateInventory(c, "patch") }

func (h *InventoryItemHandler) updateInventory(c *gin.Context, mode string) {
	id, ok := utils.ParseUUIDParam(c, h.DB, "id", "Invalid inventory item ID")
	if !ok {
		return
	}
	var item models.InventoryItem
	if err := h.DB.First(&item, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": i18n.TranslateDB(h.DB, c, "Inventory item not found")})
		return
	}
	if !utils.VerifyHomeWriteAccess(c, h.DB, item.HomeID) {
		c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Write access denied to this home")})
		return
	}
	var req inventoryMutationRequest
	if err := c.ShouldBindJSON(&req); err != nil || (req.Quantity == nil && !req.DisplaySizeUnitID.Present && len(req.Expiry) == 0) || (req.Quantity == nil && req.QuantityUnitID != nil) || (mode != "patch" && req.Quantity == nil) {
		c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(h.DB, c, "Invalid request payload")})
		return
	}
	if req.Quantity != nil && req.QuantityUnitID == nil {
		quantityError(c, h.DB, models.ErrUnit)
		return
	}
	if mode == "quantity" {
		req.DisplaySizeUnitID = OptionalUnit{}
		req.Expiry = nil
	}
	if mode == "put" && len(req.Expiry) == 0 {
		req.Expiry = json.RawMessage("null")
	}
	var expiry *time.Time
	if len(req.Expiry) > 0 {
		if err := json.Unmarshal(req.Expiry, &expiry); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(h.DB, c, "Invalid request payload")})
			return
		}
	}
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		var locked models.InventoryItem
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&locked, id).Error; err != nil {
			return err
		}
		item = locked
		var def models.ItemDefinition
		if err := tx.Where("id = ? AND home_id = ?", item.ItemDefinitionID, item.HomeID).First(&def).Error; err != nil {
			return err
		}
		updates := map[string]interface{}{}
		if req.DisplaySizeUnitID.Present {
			if err := validateDisplay(tx, req.DisplaySizeUnitID.ID, def.SizeUnitID); err != nil {
				return err
			}
			updates["display_size_unit_id"] = req.DisplaySizeUnitID.ID
		}
		if len(req.Expiry) > 0 {
			updates["expiration_date"] = expiry
		}
		change := 0.0
		if req.Quantity != nil {
			value, err := normalizeForUnit(tx, *req.Quantity, req.QuantityUnitID, def.SizeUnitID, false, req.raw("quantity"))
			if err != nil {
				return err
			}
			updates["quantity"] = value
			change = quantity.Sub(value, item.Quantity)
		}
		if err := tx.Model(&item).Updates(updates).Error; err != nil {
			return err
		}
		if change != 0 {
			if err := tx.Create(&models.InventoryTransaction{ID: uuid.New(), HomeID: item.HomeID, ItemDefinitionID: item.ItemDefinitionID, InventoryItemID: item.ID, QuantityChange: change}).Error; err != nil {
				return err
			}
		}
		if change != 0 || len(req.Expiry) > 0 {
			return utils.UpdateShoppingListForDefinition(tx, item.HomeID, item.ItemDefinitionID)
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, models.ErrUnit) || errors.Is(err, quantity.ErrInvalid) {
			quantityError(c, h.DB, err)
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to update inventory item")})
		}
		return
	}
	key := "Inventory item updated successfully"
	if mode == "quantity" {
		key = "Quantity updated successfully"
	}
	c.JSON(http.StatusOK, gin.H{"message": i18n.TranslateDB(h.DB, c, key)})
}

type ScanInventoryRequest struct {
	quantityInputs
	QuantityUnitID *uuid.UUID `json:"quantity_unit_id"`
	Barcode        string     `json:"barcode" binding:"required"`
	Change         *float64   `json:"change" binding:"required"` // e.g. 1 to increment, -1 to decrement, 60 for a pack
}

func (h *InventoryItemHandler) GetInventoryItems(c *gin.Context) {
	// ⚡ Bolt: Pass Canonical MIME header key (X-Home-Id instead of x-home-id) to avoid runtime string allocations during normalization
	homeID, ok := utils.ParseUUIDHeader(c, h.DB, "X-Home-Id", "Invalid home_id")
	if !ok {
		return
	}

	if !utils.VerifyHomeAccess(c, h.DB, homeID) {
		c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Access denied to this home")})
		return
	}

	query := h.DB.Preload("ItemDefinition.Category").Preload("ItemDefinition.SizeUnit").Where("home_id = ?", homeID)

	// Filter
	filter := c.Query("filter")
	now := time.Now()
	if filter == "expired" {
		query = query.Where("expiration_date <= ?", now)
	} else if filter == "expiring_soon" {
		threeDaysFromNow := now.AddDate(0, 0, 3)
		query = query.Where("expiration_date > ? AND expiration_date <= ?", now, threeDaysFromNow)
	}

	expiringBefore := c.Query("expiring_before")
	if expiringBefore != "" {
		t, err := time.Parse(time.RFC3339, expiringBefore)
		if err != nil && strings.Contains(expiringBefore, " ") {
			t, err = time.Parse(time.RFC3339, strings.Replace(expiringBefore, " ", "+", 1))
		}
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(h.DB, c, "Invalid date format for expiring_before. Use RFC3339 (e.g. 2023-01-02T15:04:05Z)")})
			return
		}
		query = query.Where("expiration_date <= ?", t)
	}

	// Sort
	sort := c.Query("sort")
	if sort == "expiry" {
		query = query.Order("expiration_date ASC NULLS LAST")
	} else {
		query = query.Order("created_at DESC")
	}

	var items []models.InventoryItem
	if err := query.Find(&items).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to fetch inventory items")})
		return
	}
	c.JSON(http.StatusOK, items)
}

func (h *InventoryItemHandler) GetExpiringItems(c *gin.Context) {
	// ⚡ Bolt: Pass Canonical MIME header key (X-Home-Id instead of x-home-id) to avoid runtime string allocations during normalization
	homeID, ok := utils.ParseUUIDHeader(c, h.DB, "X-Home-Id", "Invalid home_id")
	if !ok {
		return
	}

	if !utils.VerifyHomeAccess(c, h.DB, homeID) {
		c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Access denied to this home")})
		return
	}

	now := time.Now()
	sevenDaysFromNow := now.AddDate(0, 0, 7)

	var items []models.InventoryItem
	if err := h.DB.Preload("ItemDefinition.Category").Preload("ItemDefinition.SizeUnit").
		Where("home_id = ? AND expiration_date > ? AND expiration_date <= ?", homeID, now, sevenDaysFromNow).
		Order("expiration_date ASC").
		Find(&items).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to fetch inventory items")})
		return
	}

	c.JSON(http.StatusOK, items)
}

func (h *InventoryItemHandler) CreateInventoryItem(c *gin.Context) {
	// ⚡ Bolt: Pass Canonical MIME header key (X-Home-Id instead of x-home-id) to avoid runtime string allocations during normalization
	homeID, ok := utils.ParseUUIDHeader(c, h.DB, "X-Home-Id", "Invalid home_id")
	if !ok {
		return
	}

	if !utils.VerifyHomeWriteAccess(c, h.DB, homeID) {
		c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Write access denied to this home")})
		return
	}

	var req CreateInventoryItemRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(h.DB, c, "Invalid request payload")})
		return
	}

	if req.Quantity == nil || *req.Quantity < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(h.DB, c, "Invalid request payload")})
		return
	}

	var def models.ItemDefinition
	if err := h.DB.Where("id = ? AND home_id = ?", req.ItemDefinitionID, homeID).First(&def).Error; err != nil {
		quantityError(c, h.DB, err)
		return
	}
	normalized, err := normalizeForUnit(h.DB, *req.Quantity, req.QuantityUnitID, def.SizeUnitID, false, req.raw("quantity"))
	if err != nil {
		quantityError(c, h.DB, err)
		return
	}
	if err := validateDisplay(h.DB, req.DisplaySizeUnitID.ID, def.SizeUnitID); err != nil {
		quantityError(c, h.DB, err)
		return
	}
	req.Quantity = &normalized
	item := models.InventoryItem{
		HomeID:            homeID,
		ItemDefinitionID:  req.ItemDefinitionID,
		Quantity:          *req.Quantity,
		ExpirationDate:    req.ExpirationDate,
		DisplaySizeUnitID: req.DisplaySizeUnitID.ID,
		BaseSizeUnitID:    def.BaseSizeUnitID,
	}

	if err := h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&item).Error; err != nil {
			return err
		}

		if item.Quantity > 0 {
			txLog := models.InventoryTransaction{
				HomeID:           homeID,
				ItemDefinitionID: req.ItemDefinitionID,
				InventoryItemID:  item.ID,
				QuantityChange:   item.Quantity,
			}
			if err := tx.Create(&txLog).Error; err != nil {
				return err
			}
		}
		return utils.UpdateShoppingListForDefinition(tx, homeID, item.ItemDefinitionID)
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to create inventory item")})
		return
	}

	item.ItemDefinition = def
	item.EffectiveDisplaySizeUnitID = item.DisplaySizeUnitID
	if item.EffectiveDisplaySizeUnitID == nil {
		item.EffectiveDisplaySizeUnitID = def.SizeUnitID
	}
	c.JSON(http.StatusCreated, item)
}

func (h *InventoryItemHandler) UpdateInventoryItem(c *gin.Context) { h.updateInventory(c, "put") }

func (h *InventoryItemHandler) UpdateInventoryItemQuantity(c *gin.Context) {
	h.updateInventory(c, "quantity")
}

func (h *InventoryItemHandler) DeleteInventoryItem(c *gin.Context) {
	id, ok := utils.ParseUUIDParam(c, h.DB, "id", "Invalid inventory item ID")
	if !ok {
		return
	}

	var item models.InventoryItem
	if err := h.DB.First(&item, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": i18n.TranslateDB(h.DB, c, "Inventory item not found")})
		return
	}

	if !utils.VerifyHomeWriteAccess(c, h.DB, item.HomeID) {
		c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Write access denied to this home")})
		return
	}

	err := h.DB.Transaction(func(tx *gorm.DB) error {
		var locked models.InventoryItem
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&locked, id).Error; err != nil {
			return err
		}
		item = locked
		if item.Quantity > 0 {
			txLog := models.InventoryTransaction{
				HomeID:           item.HomeID,
				ItemDefinitionID: item.ItemDefinitionID,
				InventoryItemID:  item.ID,
				QuantityChange:   -item.Quantity,
			}
			if err := tx.Create(&txLog).Error; err != nil {
				return err
			}
		}

		if err := tx.Delete(&models.InventoryItem{}, id).Error; err != nil {
			return err
		}
		return utils.UpdateShoppingListForDefinition(tx, item.HomeID, item.ItemDefinitionID)
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to delete inventory item")})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": i18n.TranslateDB(h.DB, c, "Inventory item deleted successfully")})
}

type AlmostFinishedItemResponse struct {
	ItemDefinition    models.ItemDefinition `json:"item_definition"`
	TotalQuantity     float64               `json:"total_quantity"`
	Reason            string                `json:"reason"` // "low_stock", "expiring_soon", "threshold_met"
	EstimatedDaysLeft *int                  `json:"estimated_days_left,omitempty"`
}

func (h *InventoryItemHandler) GetAlmostFinishedItems(c *gin.Context) {
	homeID, ok := utils.ParseUUIDHeader(c, h.DB, "X-Home-Id", "Invalid home_id")
	if !ok {
		return
	}

	if !utils.VerifyHomeAccess(c, h.DB, homeID) {
		c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Access denied to this home")})
		return
	}

	// 1. Fetch all Item Definitions with their inventory items and size unit / category
	var itemDefs []models.ItemDefinition
	if err := h.DB.Preload("Category").Preload("SizeUnit").Where("home_id = ?", homeID).Find(&itemDefs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to fetch item definitions")})
		return
	}

	var results []AlmostFinishedItemResponse
	now := time.Now()
	sixMonthsAgo := now.AddDate(0, -6, 0)

	threeDaysFromNow := now.AddDate(0, 0, 3)

	// ⚡ Bolt: Pre-fetch all inventory items for this home to avoid N+1 queries in the loop
	var allItems []models.InventoryItem
	if err := h.DB.Where("home_id = ?", homeID).Find(&allItems).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to fetch inventory items")})
		return
	}
	itemsByDef := make(map[uuid.UUID][]models.InventoryItem)
	for _, item := range allItems {
		itemsByDef[item.ItemDefinitionID] = append(itemsByDef[item.ItemDefinitionID], item)
	}

	// ⚡ Bolt: Offload inventory transaction aggregations (SUM, MIN, MAX) to the database to reduce memory allocation,
	// GC pressure, and CPU overhead from looping over thousands of individual records in Go space.
	type ConsumptionStat struct {
		ItemDefinitionID uuid.UUID
		TotalConsumed    float64
		FirstTxTime      time.Time
		LastTxTime       time.Time
	}
	var consumptionStats []ConsumptionStat
	if err := h.DB.Model(&models.InventoryTransaction{}).
		Select("item_definition_id, SUM(-quantity_change) as total_consumed, MIN(created_at) as first_tx_time, MAX(created_at) as last_tx_time").
		Where("home_id = ? AND quantity_change < 0 AND created_at >= ?", homeID, sixMonthsAgo).
		Group("item_definition_id").
		Find(&consumptionStats).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to fetch inventory transactions")})
		return
	}
	statsByDef := make(map[uuid.UUID]ConsumptionStat)
	for _, stat := range consumptionStats {
		statsByDef[stat.ItemDefinitionID] = stat
	}

	for _, def := range itemDefs {
		// Calculate total quantity and check for expiring items
		items := itemsByDef[def.ID]

		var usableQuantity float64
		var totalQuantity float64
		hasExpiringSoon := false

		for _, item := range items {
			totalQuantity = quantity.Add(totalQuantity, item.Quantity)
			if item.ExpirationDate == nil || item.ExpirationDate.After(now) {
				usableQuantity = quantity.Add(usableQuantity, item.Quantity)
				if item.ExpirationDate != nil && item.Quantity > 0 && item.ExpirationDate.Before(threeDaysFromNow) {
					hasExpiringSoon = true
				}
			}
		}

		// Check if threshold is set and met
		if def.LowStockThreshold != nil {
			if usableQuantity <= *def.LowStockThreshold {
				results = append(results, AlmostFinishedItemResponse{
					ItemDefinition: def,
					TotalQuantity:  usableQuantity,
					Reason:         "threshold_met",
				})
				continue
			}
		}

		// If it's expiring soon, flag it
		if hasExpiringSoon {
			results = append(results, AlmostFinishedItemResponse{
				ItemDefinition: def,
				TotalQuantity:  usableQuantity,
				Reason:         "expiring_soon",
			})
			continue
		}

		// If no threshold, or threshold not met, and not expiring soon, calculate based on consumption
		// Fetch consumption in the last 6 months (negative quantity changes)
		stat, hasStats := statsByDef[def.ID]

		// Only calculate if we have meaningful consumption data
		if hasStats && stat.TotalConsumed > 0 && !stat.FirstTxTime.IsZero() && !stat.LastTxTime.IsZero() {
			totalConsumed := stat.TotalConsumed
			firstTxTime := stat.FirstTxTime

			// Calculate days between first and last transaction, or fallback to days since first tx to now
			daysDiff := now.Sub(firstTxTime).Hours() / 24

			// We need a minimum window to calculate a realistic rate, let's say 7 days, unless it's the only data we have.
			if daysDiff < 1 {
				daysDiff = 1 // Avoid division by zero
			}

			dailyConsumptionRate := totalConsumed / daysDiff

			if dailyConsumptionRate > 0 {
				daysLeft := int(usableQuantity / dailyConsumptionRate)

				if daysLeft <= 28 {
					results = append(results, AlmostFinishedItemResponse{
						ItemDefinition:    def,
						TotalQuantity:     usableQuantity,
						Reason:            "low_stock",
						EstimatedDaysLeft: &daysLeft,
					})
				}
			}
		} else if usableQuantity == 0 && def.LowStockThreshold == nil {
			// If we have 0 quantity and no data and no threshold, might want to include it?
			// But maybe the user just doesn't want to track it. Let's omit for now unless threshold is set.
		}
	}

	c.JSON(http.StatusOK, results)
}

func (h *InventoryItemHandler) ScanInventoryItem(c *gin.Context) {
	homeID, ok := utils.ParseUUIDHeader(c, h.DB, "X-Home-Id", "Invalid home_id")
	if !ok {
		return
	}

	if !utils.VerifyHomeWriteAccess(c, h.DB, homeID) {
		c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Write access denied to this home")})
		return
	}

	var req ScanInventoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(h.DB, c, "Invalid request payload")})
		return
	}

	// 1. Find ItemDefinition by barcode
	var itemDef models.ItemDefinition
	if err := h.DB.Where("home_id = ? AND barcode = ?", homeID, req.Barcode).First(&itemDef).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": i18n.TranslateDB(h.DB, c, "Item definition not found")})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to fetch item definition")})
		return
	}

	normalized, convertErr := normalizeForUnit(h.DB, *req.Change, req.QuantityUnitID, itemDef.SizeUnitID, true, req.raw("change"))
	if convertErr != nil {
		quantityError(c, h.DB, convertErr)
		return
	}
	req.Change = &normalized
	// 2. Find or create InventoryItem for this definition
	var item models.InventoryItem
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("home_id = ? AND item_definition_id = ?", homeID, itemDef.ID).
			Order("expiration_date ASC NULLS LAST").
			First(&item).Error

		if err != nil && err != gorm.ErrRecordNotFound {
			return err
		}

		if err == gorm.ErrRecordNotFound {
			if *req.Change < 0 {
				return fmt.Errorf("insufficient stock")
			}
			item = models.InventoryItem{
				HomeID:           homeID,
				ItemDefinitionID: itemDef.ID,
				Quantity:         *req.Change,
			}
			if err := tx.Create(&item).Error; err != nil {
				return err
			}
		} else {
			newQuantity := quantity.Add(item.Quantity, *req.Change)
			if newQuantity < 0 {
				newQuantity = 0
			}

			if err := quantity.Validate(newQuantity); err != nil {
				return err
			}
			*req.Change = quantity.Sub(newQuantity, item.Quantity)
			if err := tx.Model(&item).Update("quantity", newQuantity).Error; err != nil {
				return err
			}
			item.Quantity = newQuantity
		}

		// Log the actual base-unit change.
		if *req.Change == 0 {
			return nil
		}
		txLog := models.InventoryTransaction{
			HomeID:           homeID,
			ItemDefinitionID: itemDef.ID,
			InventoryItemID:  item.ID,
			QuantityChange:   *req.Change,
		}
		if err := tx.Create(&txLog).Error; err != nil {
			return err
		}

		return utils.UpdateShoppingListForDefinition(tx, homeID, itemDef.ID)
	})

	if err != nil {
		if errors.Is(err, quantity.ErrInvalid) {
			quantityError(c, h.DB, err)
			return
		}
		if err.Error() == "insufficient stock" {
			c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(h.DB, c, "Insufficient stock")})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to update inventory via scan")})
		return
	}

	item.ItemDefinition = itemDef
	item.BaseSizeUnitID = itemDef.BaseSizeUnitID
	item.EffectiveDisplaySizeUnitID = item.DisplaySizeUnitID
	if item.EffectiveDisplaySizeUnitID == nil {
		item.EffectiveDisplaySizeUnitID = itemDef.SizeUnitID
	}
	c.JSON(http.StatusOK, item)
}

type RestockInsightResponse struct {
	ItemDefinition          models.ItemDefinition `json:"item_definition"`
	CurrentStock            float64               `json:"current_stock"`
	AverageDailyConsumption float64               `json:"average_daily_consumption"`
	PredictedDepletionDate  *time.Time            `json:"predicted_depletion_date"`
	DaysLeft                *int                  `json:"days_left,omitempty"`
	Reason                  string                `json:"reason"`
}

type projectedOccurrence struct {
	date             time.Time
	quantityRequired float64
	description      string
}

func (h *InventoryItemHandler) projectDepletion(
	c *gin.Context,
	now time.Time,
	currentStock float64,
	adc float64,
	tasks []models.MaintenanceTask,
	itemDefID uuid.UUID,
	itemName string,
	displayUnits ...models.SizeUnit,
) (*time.Time, string) {
	formatAmount := func(v float64) string {
		if len(displayUnits) > 0 {
			u := displayUnits[0]
			return strings.TrimRight(strings.TrimRight(strconv.FormatFloat(quantity.FromBase(v, u.FactorToBase), 'f', 9, 64), "0"), ".") + " " + u.Name
		}
		return fmt.Sprintf("%.2f units", v)
	}
	// Project occurrences for the next 90 days
	var occurrences []projectedOccurrence
	ninetyDaysFromNow := now.AddDate(0, 0, 90)

	for _, task := range tasks {
		var reqQty float64
		for _, dep := range task.Dependencies {
			if dep.ItemDefinitionID == itemDefID {
				reqQty = dep.QuantityRequired
				break
			}
		}
		if reqQty <= 0 {
			continue
		}

		// First occurrence is the task's ScheduledDate
		occDate := task.ScheduledDate
		// If ScheduledDate is in the past and task is incomplete, treat it as overdue (occurring now)
		if occDate.Before(now) {
			occDate = now
		}

		// Generate occurrences within 90 days
		for occDate.Before(ninetyDaysFromNow) {
			occurrences = append(occurrences, projectedOccurrence{
				date:             occDate,
				quantityRequired: reqQty,
				description:      task.Description,
			})

			nextDate, repeats := parseFrequencyAndAdvance(occDate, task)
			if !repeats {
				break
			}
			occDate = nextDate
		}
	}

	// ⚡ Bolt: Replaced O(n²) bubble sort with Go's optimized O(n log n) sort.Slice
	// Sort occurrences by date ascending
	sort.Slice(occurrences, func(i, j int) bool {
		return occurrences[i].date.Before(occurrences[j].date)
	})

	// Simulate day-by-day for 90 days
	stock := currentStock
	var firstDepletionDate *time.Time
	var depletionReason string

	for d := 0; d <= 90; d++ {
		currentDate := now.AddDate(0, 0, d)
		if d > 0 {
			stock -= adc
		}

		// Check task occurrences on this day
		for _, occ := range occurrences {
			if occ.date.Year() == currentDate.Year() && occ.date.YearDay() == currentDate.YearDay() {
				if stock < occ.quantityRequired {
					if firstDepletionDate == nil {
						depDate := currentDate
						firstDepletionDate = &depDate
						template := i18n.TranslateDB(h.DB, c, "Maintenance task '%s' scheduled on %s requires %s, but you will only have %s.")
						depletionReason = fmt.Sprintf(
							template,
							occ.description,
							occ.date.Format("2006-01-02"),
							formatAmount(occ.quantityRequired),
							formatAmount(stock),
						)
					}
				}
				stock = quantity.Sub(stock, occ.quantityRequired)
			}
		}

		if stock <= 0 {
			if firstDepletionDate == nil {
				depDate := currentDate
				firstDepletionDate = &depDate
				if currentStock == 0 {
					template := i18n.TranslateDB(h.DB, c, "You are out of %s. Based on your usage, you consume %s daily.")
					depletionReason = fmt.Sprintf(
						template,
						itemName,
						formatAmount(adc),
					)
				} else {
					template := i18n.TranslateDB(h.DB, c, "Based on your usage of %s, you consume %s daily. Your current stock of %s will run out on %s.")
					depletionReason = fmt.Sprintf(
						template,
						itemName,
						formatAmount(adc),
						formatAmount(currentStock),
						currentDate.Format("2006-01-02"),
					)
				}
			}
			break
		}
	}

	return firstDepletionDate, depletionReason
}

func (h *InventoryItemHandler) GetPredictiveRestockInsights(c *gin.Context) {
	homeID, ok := utils.ParseUUIDHeader(c, h.DB, "X-Home-Id", "Invalid home_id")
	if !ok {
		return
	}

	if !utils.VerifyHomeAccess(c, h.DB, homeID) {
		c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Access denied to this home")})
		return
	}

	now := time.Now()
	ninetyDaysAgo := now.AddDate(0, 0, -90)

	// Usage stats over last 90 days
	type ConsumptionStat struct {
		ItemDefinitionID uuid.UUID
		TotalConsumed    float64
		FirstTxTime      time.Time
		LastTxTime       time.Time
	}
	var consumptionStats []ConsumptionStat
	if err := h.DB.Model(&models.InventoryTransaction{}).
		Select("item_definition_id, SUM(-quantity_change) as total_consumed, MIN(created_at) as first_tx_time, MAX(created_at) as last_tx_time").
		Where("home_id = ? AND quantity_change < 0 AND created_at >= ?", homeID, ninetyDaysAgo).
		Group("item_definition_id").
		Find(&consumptionStats).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to fetch inventory transactions")})
		return
	}
	statsByDef := make(map[uuid.UUID]ConsumptionStat)
	for _, stat := range consumptionStats {
		statsByDef[stat.ItemDefinitionID] = stat
	}

	// All item definitions
	var itemDefs []models.ItemDefinition
	if err := h.DB.Preload("Category").Preload("SizeUnit").Where("home_id = ?", homeID).Find(&itemDefs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to fetch item definitions")})
		return
	}

	// All inventory items for stock calculation (N+1 query avoided)
	var allItems []models.InventoryItem
	if err := h.DB.Where("home_id = ?", homeID).Find(&allItems).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to fetch inventory items")})
		return
	}
	itemsByDef := make(map[uuid.UUID][]models.InventoryItem)
	for _, item := range allItems {
		itemsByDef[item.ItemDefinitionID] = append(itemsByDef[item.ItemDefinitionID], item)
	}

	// All maintenance tasks
	var maintenanceTasks []models.MaintenanceTask
	if err := h.DB.Preload("Dependencies").Where("home_id = ? AND is_completed = ?", homeID, false).Find(&maintenanceTasks).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to fetch maintenance tasks")})
		return
	}
	tasksByDef := make(map[uuid.UUID][]models.MaintenanceTask)
	for _, task := range maintenanceTasks {
		for _, dep := range task.Dependencies {
			tasksByDef[dep.ItemDefinitionID] = append(tasksByDef[dep.ItemDefinitionID], task)
		}
	}

	var results []RestockInsightResponse
	for _, def := range itemDefs {
		stat, hasStats := statsByDef[def.ID]
		tasks := tasksByDef[def.ID]

		var adc float64
		if hasStats && stat.TotalConsumed > 0 && !stat.FirstTxTime.IsZero() {
			daysDiff := now.Sub(stat.FirstTxTime).Hours() / 24
			if daysDiff < 1 {
				daysDiff = 1
			}
			if daysDiff > 90 {
				daysDiff = 90
			}
			adc = stat.TotalConsumed / daysDiff
		}

		// Skip if there's no consumption and no maintenance tasks
		if adc == 0 && len(tasks) == 0 {
			continue
		}

		// Calculate current stock (excluding expired items)
		items := itemsByDef[def.ID]
		var currentStock float64
		for _, item := range items {
			if item.ExpirationDate == nil || item.ExpirationDate.After(now) {
				currentStock = quantity.Add(currentStock, item.Quantity)
			}
		}

		depDate, reason := h.projectDepletion(c, now, currentStock, adc, tasks, def.ID, def.Name, models.DisplayUnits(h.DB, def)...)
		if depDate != nil {
			daysLeft := int(depDate.Sub(now).Hours() / 24)
			if daysLeft < 0 {
				daysLeft = 0
			}

			// Return items predicted to deplete within the next 14 days
			if daysLeft <= 14 {
				results = append(results, RestockInsightResponse{
					ItemDefinition:          def,
					CurrentStock:            currentStock,
					AverageDailyConsumption: adc,
					PredictedDepletionDate:  depDate,
					DaysLeft:                &daysLeft,
					Reason:                  reason,
				})
			}
		}
	}

	c.JSON(http.StatusOK, results)
}

type PredictionItemDetails struct {
	ID         uuid.UUID `json:"id"`
	Name       string    `json:"name"`
	PictureURL string    `json:"picture_url"`
}

type PredictionResponse struct {
	BaseSizeUnitID        *uuid.UUID            `json:"base_size_unit_id"`
	PredictionID          uuid.UUID             `json:"prediction_id"`
	ItemDefinitionDetails PredictionItemDetails `json:"item_definition_details"`
	PredictedAmount       float64               `json:"predicted_amount"`
	PredictionDate        time.Time             `json:"prediction_date"`
}

type IgnorePredictionRequest struct {
	PredictionID uuid.UUID `json:"prediction_id" binding:"required"`
}

type ApplyPredictionRequest struct {
	quantityInputs
	QuantityUnitID *uuid.UUID `json:"quantity_unit_id"`
	PredictionID   uuid.UUID  `json:"prediction_id" binding:"required"`
	AppliedAmount  float64    `json:"applied_amount" binding:"required"`
}

func (h *InventoryItemHandler) GetHomePredictions(c *gin.Context) {
	homeID, ok := utils.ParseUUIDHeader(c, h.DB, "X-Home-Id", "Invalid home_id")
	if !ok {
		return
	}

	if !utils.VerifyHomeAccess(c, h.DB, homeID) {
		c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Access denied to this home")})
		return
	}

	var predictions []models.InventoryPrediction
	if err := h.DB.Joins("InventoryItem").
		Preload("InventoryItem.ItemDefinition").
		Where(`"InventoryItem".home_id = ? AND "inventory_predictions".status = ?`, homeID, models.PredictionStatusPredicted).
		Order(`"inventory_predictions".created_at DESC`).
		Find(&predictions).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to fetch predictions")})
		return
	}

	var response []PredictionResponse
	for _, p := range predictions {
		response = append(response, PredictionResponse{
			PredictionID:   p.ID,
			BaseSizeUnitID: p.InventoryItem.ItemDefinition.BaseSizeUnitID,
			ItemDefinitionDetails: PredictionItemDetails{
				ID:         p.InventoryItem.ItemDefinition.ID,
				Name:       p.InventoryItem.ItemDefinition.Name,
				PictureURL: p.InventoryItem.ItemDefinition.ImageURL,
			},
			PredictedAmount: p.PredictedConsumedAmount,
			PredictionDate:  p.CreatedAt,
		})
	}

	if response == nil {
		response = []PredictionResponse{}
	}

	c.JSON(http.StatusOK, response)
}

func (h *InventoryItemHandler) IgnorePrediction(c *gin.Context) {
	homeID, ok := utils.ParseUUIDHeader(c, h.DB, "X-Home-Id", "Invalid home_id")
	if !ok {
		return
	}

	if !utils.VerifyHomeWriteAccess(c, h.DB, homeID) {
		c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Write access denied to this home")})
		return
	}

	var req IgnorePredictionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(h.DB, c, "Invalid request payload")})
		return
	}

	var prediction models.InventoryPrediction
	if err := h.DB.Joins("InventoryItem").
		Where(`"inventory_predictions".id = ? AND "InventoryItem".home_id = ?`, req.PredictionID, homeID).
		First(&prediction).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": i18n.TranslateDB(h.DB, c, "Prediction not found")})
		return
	}

	if err := h.DB.Model(&prediction).Update("status", models.PredictionStatusIgnored).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to ignore prediction")})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": i18n.TranslateDB(h.DB, c, "Prediction ignored successfully")})
}

func (h *InventoryItemHandler) ApplyPrediction(c *gin.Context) {
	homeID, ok := utils.ParseUUIDHeader(c, h.DB, "X-Home-Id", "Invalid home_id")
	if !ok {
		return
	}

	if !utils.VerifyHomeWriteAccess(c, h.DB, homeID) {
		c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Write access denied to this home")})
		return
	}

	var req ApplyPredictionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(h.DB, c, "Invalid request payload")})
		return
	}

	if req.AppliedAmount <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(h.DB, c, "Applied amount must be greater than zero")})
		return
	}

	var prediction models.InventoryPrediction
	if err := h.DB.Joins("InventoryItem").
		Where(`"inventory_predictions".id = ? AND "InventoryItem".home_id = ?`, req.PredictionID, homeID).
		First(&prediction).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": i18n.TranslateDB(h.DB, c, "Prediction not found")})
		return
	}

	if prediction.Status != models.PredictionStatusPredicted {
		c.JSON(http.StatusNotFound, gin.H{"error": i18n.TranslateDB(h.DB, c, "Prediction not found")})
		return
	}
	var definition models.InventoryItem
	if err := h.DB.First(&definition, prediction.InventoryItemID).Error; err != nil {
		quantityError(c, h.DB, err)
		return
	}
	normalized, err := quantityForDefinition(h.DB, definition.ItemDefinitionID, homeID, req.AppliedAmount, req.QuantityUnitID, false, req.raw("applied_amount"))
	if err != nil {
		quantityError(c, h.DB, err)
		return
	}
	req.AppliedAmount = normalized
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		// Update status to Applied
		result := tx.Model(&prediction).Where("status = ?", models.PredictionStatusPredicted).Update("status", models.PredictionStatusApplied)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}

		// Subtract applied_amount from InventoryItem quantity
		var item models.InventoryItem
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&item, prediction.InventoryItemID).Error; err != nil {
			return err
		}

		newQuantity := quantity.Sub(item.Quantity, req.AppliedAmount)
		if newQuantity < 0 {
			newQuantity = 0
		}

		actualChange := quantity.Sub(newQuantity, item.Quantity)
		if err := tx.Model(&item).Update("quantity", newQuantity).Error; err != nil {
			return err
		}

		// Create InventoryTransaction for this update
		txLog := models.InventoryTransaction{
			HomeID:           homeID,
			ItemDefinitionID: item.ItemDefinitionID,
			InventoryItemID:  item.ID,
			QuantityChange:   actualChange,
		}
		if txLog.QuantityChange != 0 {
			if err := tx.Create(&txLog).Error; err != nil {
				return err
			}
		}

		return utils.UpdateShoppingListForDefinition(tx, homeID, item.ItemDefinitionID)
	})

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": i18n.TranslateDB(h.DB, c, "Prediction not found")})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to apply prediction")})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": i18n.TranslateDB(h.DB, c, "Prediction applied successfully")})
}

func (r *CreateInventoryItemRequest) UnmarshalJSON(b []byte) error {
	type plain CreateInventoryItemRequest
	return decodeQuantityRequest(b, (*plain)(r), &r.quantityInputs)
}

func (r *UpdateInventoryItemRequest) UnmarshalJSON(b []byte) error {
	type plain UpdateInventoryItemRequest
	return decodeQuantityRequest(b, (*plain)(r), &r.quantityInputs)
}

func (r *UpdateQuantityRequest) UnmarshalJSON(b []byte) error {
	type plain UpdateQuantityRequest
	return decodeQuantityRequest(b, (*plain)(r), &r.quantityInputs)
}

func (r *ScanInventoryRequest) UnmarshalJSON(b []byte) error {
	type plain ScanInventoryRequest
	return decodeQuantityRequest(b, (*plain)(r), &r.quantityInputs)
}

func (r *ApplyPredictionRequest) UnmarshalJSON(b []byte) error {
	type plain ApplyPredictionRequest
	return decodeQuantityRequest(b, (*plain)(r), &r.quantityInputs)
}
