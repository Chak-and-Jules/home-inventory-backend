package handlers

import (
	"net/http"
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
	ItemDefinitionID uuid.UUID  `json:"item_definition_id" binding:"required"`
	Quantity         *float64   `json:"quantity" binding:"required"`
	ExpirationDate   *time.Time `json:"expiry_date"`
}

type UpdateInventoryItemRequest struct {
	Quantity       *float64   `json:"quantity" binding:"required"`
	ExpirationDate *time.Time `json:"expiry_date"`
}

type UpdateQuantityRequest struct {
	Quantity *float64 `json:"quantity" binding:"required"`
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

	item := models.InventoryItem{
		HomeID:           homeID,
		ItemDefinitionID: req.ItemDefinitionID,
		Quantity:         *req.Quantity,
		ExpirationDate:   req.ExpirationDate,
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

	c.JSON(http.StatusCreated, item)
}

func (h *InventoryItemHandler) UpdateInventoryItem(c *gin.Context) {
	id, ok := utils.ParseUUIDParam(c, h.DB, "id", "Invalid inventory item ID")
	if !ok {
		return
	}

	// First find the item to check its home_id
	var item models.InventoryItem
	if err := h.DB.First(&item, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": i18n.TranslateDB(h.DB, c, "Inventory item not found")})
		return
	}

	if !utils.VerifyHomeWriteAccess(c, h.DB, item.HomeID) {
		c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Write access denied to this home")})
		return
	}

	var req UpdateInventoryItemRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(h.DB, c, "Invalid request payload")})
		return
	}

	if req.Quantity == nil || *req.Quantity < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(h.DB, c, "Invalid request payload")})
		return
	}

	// Log transaction if quantity changed
	quantityChange := *req.Quantity - item.Quantity

	err := h.DB.Transaction(func(tx *gorm.DB) error {
		updates := map[string]interface{}{
			"quantity":        *req.Quantity,
			"expiration_date": req.ExpirationDate,
		}

		if err := tx.Model(&item).Updates(updates).Error; err != nil {
			return err
		}

		if quantityChange != 0 {
			txLog := models.InventoryTransaction{
				HomeID:           item.HomeID,
				ItemDefinitionID: item.ItemDefinitionID,
				InventoryItemID:  item.ID,
				QuantityChange:   quantityChange,
			}
			if err := tx.Create(&txLog).Error; err != nil {
				return err
			}
		}
		return utils.UpdateShoppingListForDefinition(tx, item.HomeID, item.ItemDefinitionID)
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to update inventory item")})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": i18n.TranslateDB(h.DB, c, "Inventory item updated successfully")})
}

func (h *InventoryItemHandler) UpdateInventoryItemQuantity(c *gin.Context) {
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

	var req UpdateQuantityRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(h.DB, c, "Invalid request payload")})
		return
	}

	if req.Quantity == nil || *req.Quantity < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(h.DB, c, "Invalid request payload")})
		return
	}

	// Log transaction if quantity changed
	quantityChange := *req.Quantity - item.Quantity

	err := h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&item).Update("quantity", *req.Quantity).Error; err != nil {
			return err
		}

		if quantityChange != 0 {
			txLog := models.InventoryTransaction{
				HomeID:           item.HomeID,
				ItemDefinitionID: item.ItemDefinitionID,
				InventoryItemID:  item.ID,
				QuantityChange:   quantityChange,
			}
			if err := tx.Create(&txLog).Error; err != nil {
				return err
			}
		}
		return utils.UpdateShoppingListForDefinition(tx, item.HomeID, item.ItemDefinitionID)
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to update quantity")})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": i18n.TranslateDB(h.DB, c, "Quantity updated successfully")})
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
			totalQuantity += item.Quantity
			if item.ExpirationDate == nil || item.ExpirationDate.After(now) {
				usableQuantity += item.Quantity
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
