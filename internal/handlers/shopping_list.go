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

type ShoppingListHandler struct {
	DB *gorm.DB
}

type ShoppingListItemRequest struct {
	ItemDefinitionID *uuid.UUID `json:"item_definition_id"`
	Name             string     `json:"name"`
	Quantity         float64    `json:"quantity" binding:"required,gte=0"`
}

type UpdateShoppingListItemRequest struct {
	Quantity float64 `json:"quantity" binding:"required,gte=0"`
	IsBought bool    `json:"is_bought"`
}

func (h *ShoppingListHandler) generatePredictiveSuggestions(c *gin.Context, homeID uuid.UUID) {
	userID, ok := c.Get("userID")
	if !ok {
		return
	}
	userUUID, ok := userID.(uuid.UUID)
	if !ok {
		return
	}

	var profile models.Profile
	restockWindow := 7
	if err := h.DB.Select("restock_window").Where("id = ?", userUUID).First(&profile).Error; err == nil {
		if profile.RestockWindow != nil {
			restockWindow = *profile.RestockWindow
		}
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
	h.DB.Model(&models.InventoryTransaction{}).
		Select("item_definition_id, SUM(-quantity_change) as total_consumed, MIN(created_at) as first_tx_time, MAX(created_at) as last_tx_time").
		Where("home_id = ? AND quantity_change < 0 AND created_at >= ?", homeID, ninetyDaysAgo).
		Group("item_definition_id").
		Find(&consumptionStats)

	statsByDef := make(map[uuid.UUID]ConsumptionStat)
	for _, stat := range consumptionStats {
		statsByDef[stat.ItemDefinitionID] = stat
	}

	// All item definitions
	var itemDefs []models.ItemDefinition
	h.DB.Where("home_id = ?", homeID).Find(&itemDefs)

	// All inventory items for stock calculation (N+1 query avoided)
	var allItems []models.InventoryItem
	h.DB.Where("home_id = ?", homeID).Find(&allItems)
	itemsByDef := make(map[uuid.UUID][]models.InventoryItem)
	for _, item := range allItems {
		itemsByDef[item.ItemDefinitionID] = append(itemsByDef[item.ItemDefinitionID], item)
	}

	// All maintenance tasks
	var maintenanceTasks []models.MaintenanceTask
	h.DB.Preload("Dependencies").Where("home_id = ? AND is_completed = ?", homeID, false).Find(&maintenanceTasks)
	tasksByDef := make(map[uuid.UUID][]models.MaintenanceTask)
	for _, task := range maintenanceTasks {
		for _, dep := range task.Dependencies {
			tasksByDef[dep.ItemDefinitionID] = append(tasksByDef[dep.ItemDefinitionID], task)
		}
	}

	// All existing non-bought shopping list items
	var existingListItems []models.ShoppingListItem
	h.DB.Where("home_id = ? AND is_bought = ?", homeID, false).Find(&existingListItems)
	listItemsByDef := make(map[uuid.UUID][]models.ShoppingListItem)
	for _, item := range existingListItems {
		if item.ItemDefinitionID != nil {
			listItemsByDef[*item.ItemDefinitionID] = append(listItemsByDef[*item.ItemDefinitionID], item)
		}
	}

	invHandler := &InventoryItemHandler{DB: h.DB}

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

		// Calculate current stock (excluding expired items)
		items := itemsByDef[def.ID]
		var currentStock float64
		for _, item := range items {
			if item.ExpirationDate == nil || item.ExpirationDate.After(now) {
				currentStock += item.Quantity
			}
		}

		// Clean up or create
		if adc == 0 && len(tasks) == 0 {
			// No predictive criteria, clean up active suggestions
			for _, item := range listItemsByDef[def.ID] {
				if item.IsPredictive && !item.IsDismissed {
					h.DB.Delete(&item)
				}
			}
			continue
		}

		depDate, _ := invHandler.projectDepletion(c, now, currentStock, adc, tasks, def.ID, def.Name)
		if depDate != nil {
			daysLeft := int(depDate.Sub(now).Hours() / 24)
			if daysLeft < 0 {
				daysLeft = 0
			}

			if daysLeft <= restockWindow {
				// Should be suggested! Check if already on list
				hasActiveItem := false
				hasDismissedSuggestion := false
				for _, item := range listItemsByDef[def.ID] {
					if !item.IsDismissed {
						hasActiveItem = true
					} else if item.IsPredictive && item.IsDismissed {
						hasDismissedSuggestion = true
					}
				}

				if !hasActiveItem && !hasDismissedSuggestion {
					// Suggest add!
					neededQuantity := 1.0
					if def.TargetQuantity != nil && *def.TargetQuantity > currentStock {
						neededQuantity = *def.TargetQuantity - currentStock
					} else if def.LowStockThreshold != nil && *def.LowStockThreshold > currentStock {
						neededQuantity = *def.LowStockThreshold - currentStock
						if neededQuantity <= 0 {
							neededQuantity = 1.0
						}
					}

					newItem := models.ShoppingListItem{
						HomeID:           homeID,
						ItemDefinitionID: &def.ID,
						Name:             def.Name,
						Quantity:         neededQuantity,
						IsBought:         false,
						IsAutoGenerated:  false,
						IsPredictive:     true,
						IsDismissed:      false,
					}
					h.DB.Create(&newItem)
				}
			} else {
				// No longer within restock window, delete any active predictive suggestion
				for _, item := range listItemsByDef[def.ID] {
					if item.IsPredictive && !item.IsDismissed {
						h.DB.Delete(&item)
					}
				}
			}
		} else {
			// No depletion predicted, delete any active predictive suggestion
			for _, item := range listItemsByDef[def.ID] {
				if item.IsPredictive && !item.IsDismissed {
					h.DB.Delete(&item)
				}
			}
		}
	}
}

func (h *ShoppingListHandler) GetShoppingList(c *gin.Context) {
	homeID, ok := utils.ParseUUIDHeader(c, h.DB, "X-Home-Id", "Invalid home_id")
	if !ok {
		return
	}

	if !utils.VerifyHomeAccess(c, h.DB, homeID) {
		c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Access denied to this home")})
		return
	}

	// Dynamically generate/update suggestions
	h.generatePredictiveSuggestions(c, homeID)

	var items []models.ShoppingListItem
	if err := h.DB.Preload("ItemDefinition.SizeUnit").Preload("ItemDefinition.Category").Where("home_id = ? AND is_dismissed = ?", homeID, false).Order("is_bought ASC, created_at DESC").Find(&items).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to fetch shopping list items")})
		return
	}

	c.JSON(http.StatusOK, items)
}

func (h *ShoppingListHandler) CreateShoppingListItem(c *gin.Context) {
	homeID, ok := utils.ParseUUIDHeader(c, h.DB, "X-Home-Id", "Invalid home_id")
	if !ok {
		return
	}

	if !utils.VerifyHomeWriteAccess(c, h.DB, homeID) {
		c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Write access denied to this home")})
		return
	}

	var req ShoppingListItemRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(h.DB, c, "Invalid request payload")})
		return
	}

	if req.ItemDefinitionID == nil && req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(h.DB, c, "Item definition or name is required")})
		return
	}

	name := req.Name
	if req.ItemDefinitionID != nil {
		var def models.ItemDefinition
		if err := h.DB.First(&def, *req.ItemDefinitionID).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": i18n.TranslateDB(h.DB, c, "Item definition not found")})
			return
		}
		if def.HomeID != homeID {
			c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Item definition does not belong to this home")})
			return
		}
		if name == "" {
			name = def.Name
		}
	}

	item := models.ShoppingListItem{
		HomeID:           homeID,
		ItemDefinitionID: req.ItemDefinitionID,
		Name:             name,
		Quantity:         req.Quantity,
		IsBought:         false,
		IsAutoGenerated:  false,
	}

	if err := h.DB.Create(&item).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to create shopping list item")})
		return
	}

	c.JSON(http.StatusCreated, item)
}

func (h *ShoppingListHandler) UpdateShoppingListItem(c *gin.Context) {
	id, ok := utils.ParseUUIDParam(c, h.DB, "id", "Invalid shopping list item ID")
	if !ok {
		return
	}

	var item models.ShoppingListItem
	if err := h.DB.First(&item, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": i18n.TranslateDB(h.DB, c, "Shopping list item not found")})
		return
	}

	if !utils.VerifyHomeWriteAccess(c, h.DB, item.HomeID) {
		c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Write access denied to this home")})
		return
	}

	var req UpdateShoppingListItemRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(h.DB, c, "Invalid request payload")})
		return
	}

	updates := map[string]interface{}{
		"quantity":  req.Quantity,
		"is_bought": req.IsBought,
	}

	if err := h.DB.Model(&item).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to update shopping list item")})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": i18n.TranslateDB(h.DB, c, "Shopping list item updated successfully")})
}

func (h *ShoppingListHandler) ToggleShoppingListItemBought(c *gin.Context) {
	id, ok := utils.ParseUUIDParam(c, h.DB, "id", "Invalid shopping list item ID")
	if !ok {
		return
	}

	var item models.ShoppingListItem
	if err := h.DB.First(&item, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": i18n.TranslateDB(h.DB, c, "Shopping list item not found")})
		return
	}

	if !utils.VerifyHomeWriteAccess(c, h.DB, item.HomeID) {
		c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Write access denied to this home")})
		return
	}

	if err := h.DB.Model(&item).Update("is_bought", !item.IsBought).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to toggle bought status")})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": i18n.TranslateDB(h.DB, c, "Shopping list item status toggled successfully")})
}

func (h *ShoppingListHandler) DeleteShoppingListItem(c *gin.Context) {
	id, ok := utils.ParseUUIDParam(c, h.DB, "id", "Invalid shopping list item ID")
	if !ok {
		return
	}

	var item models.ShoppingListItem
	if err := h.DB.First(&item, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": i18n.TranslateDB(h.DB, c, "Shopping list item not found")})
		return
	}

	if !utils.VerifyHomeWriteAccess(c, h.DB, item.HomeID) {
		c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Write access denied to this home")})
		return
	}

	if err := h.DB.Delete(&models.ShoppingListItem{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to delete shopping list item")})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": i18n.TranslateDB(h.DB, c, "Shopping list item deleted successfully")})
}

func (h *ShoppingListHandler) AcceptShoppingListSuggestion(c *gin.Context) {
	id, ok := utils.ParseUUIDParam(c, h.DB, "id", "Invalid shopping list item ID")
	if !ok {
		return
	}

	var item models.ShoppingListItem
	if err := h.DB.First(&item, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": i18n.TranslateDB(h.DB, c, "Shopping list item not found")})
		return
	}

	if !utils.VerifyHomeWriteAccess(c, h.DB, item.HomeID) {
		c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Write access denied to this home")})
		return
	}

	if !item.IsPredictive {
		c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(h.DB, c, "Item is not a predictive suggestion")})
		return
	}

	if err := h.DB.Model(&item).Updates(map[string]interface{}{
		"is_predictive": false,
		"is_dismissed":  false,
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to accept suggestion")})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": i18n.TranslateDB(h.DB, c, "Shopping list suggestion accepted successfully")})
}

func (h *ShoppingListHandler) DismissShoppingListSuggestion(c *gin.Context) {
	id, ok := utils.ParseUUIDParam(c, h.DB, "id", "Invalid shopping list item ID")
	if !ok {
		return
	}

	var item models.ShoppingListItem
	if err := h.DB.First(&item, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": i18n.TranslateDB(h.DB, c, "Shopping list item not found")})
		return
	}

	if !utils.VerifyHomeWriteAccess(c, h.DB, item.HomeID) {
		c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Write access denied to this home")})
		return
	}

	if !item.IsPredictive {
		c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(h.DB, c, "Item is not a predictive suggestion")})
		return
	}

	if err := h.DB.Model(&item).Update("is_dismissed", true).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to dismiss suggestion")})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": i18n.TranslateDB(h.DB, c, "Shopping list suggestion dismissed successfully")})
}
