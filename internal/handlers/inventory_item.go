package handlers

import (
	"fmt"
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

type ScanInventoryRequest struct {
	Barcode string   `json:"barcode" binding:"required"`
	Change  *float64 `json:"change" binding:"required"` // e.g. 1 to increment, -1 to decrement, 60 for a pack
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

	// 2. Find or create InventoryItem for this definition
	var item models.InventoryItem
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		err := tx.Where("home_id = ? AND item_definition_id = ?", homeID, itemDef.ID).
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
			newQuantity := item.Quantity + *req.Change
			if newQuantity < 0 {
				newQuantity = 0
			}

			if err := tx.Model(&item).Update("quantity", newQuantity).Error; err != nil {
				return err
			}
			item.Quantity = newQuantity
		}

		// Log transaction
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
		if err.Error() == "insufficient stock" {
			c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(h.DB, c, "Insufficient stock")})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to update inventory via scan")})
		return
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
) (*time.Time, string) {
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

			if task.Frequency == "once" || task.Frequency == "" {
				break
			} else if task.Frequency == "monthly" {
				occDate = occDate.AddDate(0, 1, 0)
			} else if task.Frequency == "yearly" {
				occDate = occDate.AddDate(1, 0, 0)
			} else {
				break
			}
		}
	}

	// Sort occurrences by date ascending
	for i := 0; i < len(occurrences); i++ {
		for j := i + 1; j < len(occurrences); j++ {
			if occurrences[i].date.After(occurrences[j].date) {
				occurrences[i], occurrences[j] = occurrences[j], occurrences[i]
			}
		}
	}

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
						template := i18n.TranslateDB(h.DB, c, "Maintenance task '%s' scheduled on %s requires %.2f units, but you will only have %.2f units.")
						depletionReason = fmt.Sprintf(
							template,
							occ.description,
							occ.date.Format("2006-01-02"),
							occ.quantityRequired,
							stock,
						)
					}
				}
				stock -= occ.quantityRequired
			}
		}

		if stock <= 0 {
			if firstDepletionDate == nil {
				depDate := currentDate
				firstDepletionDate = &depDate
				if currentStock == 0 {
					template := i18n.TranslateDB(h.DB, c, "You are out of %s. Based on your usage, you consume %.2f daily.")
					depletionReason = fmt.Sprintf(
						template,
						itemName,
						adc,
					)
				} else {
					template := i18n.TranslateDB(h.DB, c, "Based on your usage of %s, you consume %.2f daily. Your current stock of %.2f will run out on %s.")
					depletionReason = fmt.Sprintf(
						template,
						itemName,
						adc,
						currentStock,
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
				currentStock += item.Quantity
			}
		}

		depDate, reason := h.projectDepletion(c, now, currentStock, adc, tasks, def.ID, def.Name)
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
