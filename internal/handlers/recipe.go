package handlers

import (
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/i18n"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/utils"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type RecipeHandler struct {
	DB *gorm.DB
}

type RecipeIngredientRequest struct {
	ItemDefinitionID uuid.UUID `json:"item_definition_id" binding:"required"`
	QuantityRequired float64   `json:"quantity_required" binding:"required"`
}

type RecipeRequest struct {
	Name         string                    `json:"name" binding:"required"`
	Instructions string                    `json:"instructions"`
	Servings     int                       `json:"servings"`
	Ingredients  []RecipeIngredientRequest `json:"ingredients"`
}

type RecipeIngredientDetail struct {
	ItemDefinitionID   uuid.UUID `json:"item_definition_id"`
	ItemDefinitionName string    `json:"item_definition_name"`
	QuantityRequired   float64   `json:"quantity_required"`
	QuantityAvailable  float64   `json:"quantity_available"`
	QuantityMissing    float64   `json:"quantity_missing"`
	UsesExpiringSoon   bool      `json:"uses_expiring_soon"`
}

type RecipeSuggestion struct {
	Recipe            models.Recipe            `json:"recipe"`
	CanCook           bool                     `json:"can_cook"`
	UsesExpiringSoon  bool                     `json:"uses_expiring_soon"`
	MissingCount      int                      `json:"missing_count"`
	IngredientDetails []RecipeIngredientDetail `json:"ingredient_details"`
}

func (h *RecipeHandler) GetRecipes(c *gin.Context) {
	homeID, ok := utils.ParseUUIDHeader(c, h.DB, "X-Home-Id", "Invalid home_id")
	if !ok {
		return
	}

	if !utils.VerifyHomeAccess(c, h.DB, homeID) {
		c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Access denied to this home")})
		return
	}

	var recipes []models.Recipe
	if err := h.DB.Preload("Ingredients.ItemDefinition").Where("home_id = ?", homeID).Order("name ASC").Find(&recipes).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to fetch recipes")})
		return
	}

	c.JSON(http.StatusOK, recipes)
}

func (h *RecipeHandler) GetRecipe(c *gin.Context) {
	id, ok := utils.ParseUUIDParam(c, h.DB, "id", "Invalid recipe ID")
	if !ok {
		return
	}

	var recipe models.Recipe
	if err := h.DB.Preload("Ingredients.ItemDefinition").First(&recipe, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": i18n.TranslateDB(h.DB, c, "Recipe not found")})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to fetch recipe")})
		}
		return
	}

	if !utils.VerifyHomeAccess(c, h.DB, recipe.HomeID) {
		c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Access denied to this home")})
		return
	}

	c.JSON(http.StatusOK, recipe)
}

func (h *RecipeHandler) CreateRecipe(c *gin.Context) {
	homeID, ok := utils.ParseUUIDHeader(c, h.DB, "X-Home-Id", "Invalid home_id")
	if !ok {
		return
	}

	if !utils.VerifyHomeWriteAccess(c, h.DB, homeID) {
		c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Write access denied to this home")})
		return
	}

	var req RecipeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(h.DB, c, "Invalid request payload")})
		return
	}

	if strings.TrimSpace(req.Name) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(h.DB, c, "Invalid request payload")})
		return
	}

	for _, ing := range req.Ingredients {
		if ing.QuantityRequired <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(h.DB, c, "Invalid request payload")})
			return
		}
		var itemDef models.ItemDefinition
		if err := h.DB.Where("id = ? AND home_id = ?", ing.ItemDefinitionID, homeID).First(&itemDef).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(h.DB, c, "Item definition does not belong to this home")})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to fetch item definition")})
			}
			return
		}
	}

	servings := req.Servings
	if servings <= 0 {
		servings = 1
	}

	recipe := models.Recipe{
		ID:           uuid.New(),
		HomeID:       homeID,
		Name:         strings.TrimSpace(req.Name),
		Instructions: req.Instructions,
		Servings:     servings,
	}

	err := h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&recipe).Error; err != nil {
			return err
		}

		for _, ing := range req.Ingredients {
			ingredient := models.RecipeIngredient{
				ID:               uuid.New(),
				RecipeID:         recipe.ID,
				ItemDefinitionID: ing.ItemDefinitionID,
				QuantityRequired: ing.QuantityRequired,
			}
			if err := tx.Create(&ingredient).Error; err != nil {
				return err
			}
		}

		return nil
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to create recipe")})
		return
	}

	var result models.Recipe
	h.DB.Preload("Ingredients.ItemDefinition").First(&result, recipe.ID)

	c.JSON(http.StatusCreated, result)
}

func (h *RecipeHandler) UpdateRecipe(c *gin.Context) {
	id, ok := utils.ParseUUIDParam(c, h.DB, "id", "Invalid recipe ID")
	if !ok {
		return
	}

	var recipe models.Recipe
	if err := h.DB.First(&recipe, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": i18n.TranslateDB(h.DB, c, "Recipe not found")})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to update recipe")})
		}
		return
	}

	if !utils.VerifyHomeWriteAccess(c, h.DB, recipe.HomeID) {
		c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Write access denied to this home")})
		return
	}

	var req RecipeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(h.DB, c, "Invalid request payload")})
		return
	}

	if strings.TrimSpace(req.Name) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(h.DB, c, "Invalid request payload")})
		return
	}

	for _, ing := range req.Ingredients {
		if ing.QuantityRequired <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(h.DB, c, "Invalid request payload")})
			return
		}
		var itemDef models.ItemDefinition
		if err := h.DB.Where("id = ? AND home_id = ?", ing.ItemDefinitionID, recipe.HomeID).First(&itemDef).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(h.DB, c, "Item definition does not belong to this home")})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to fetch item definition")})
			}
			return
		}
	}

	servings := req.Servings
	if servings <= 0 {
		servings = 1
	}

	updates := map[string]interface{}{
		"name":         strings.TrimSpace(req.Name),
		"instructions": req.Instructions,
		"servings":     servings,
	}

	err := h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&recipe).Updates(updates).Error; err != nil {
			return err
		}

		if req.Ingredients != nil {
			if err := tx.Where("recipe_id = ?", recipe.ID).Delete(&models.RecipeIngredient{}).Error; err != nil {
				return err
			}

			for _, ing := range req.Ingredients {
				ingredient := models.RecipeIngredient{
					ID:               uuid.New(),
					RecipeID:         recipe.ID,
					ItemDefinitionID: ing.ItemDefinitionID,
					QuantityRequired: ing.QuantityRequired,
				}
				if err := tx.Create(&ingredient).Error; err != nil {
					return err
				}
			}
		}

		return nil
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to update recipe")})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": i18n.TranslateDB(h.DB, c, "Recipe updated successfully")})
}

func (h *RecipeHandler) DeleteRecipe(c *gin.Context) {
	id, ok := utils.ParseUUIDParam(c, h.DB, "id", "Invalid recipe ID")
	if !ok {
		return
	}

	var recipe models.Recipe
	if err := h.DB.First(&recipe, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": i18n.TranslateDB(h.DB, c, "Recipe not found")})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to fetch recipe")})
		}
		return
	}

	if !utils.VerifyHomeWriteAccess(c, h.DB, recipe.HomeID) {
		c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Write access denied to this home")})
		return
	}

	if err := h.DB.Delete(&models.Recipe{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to delete recipe")})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": i18n.TranslateDB(h.DB, c, "Recipe deleted successfully")})
}

func (h *RecipeHandler) GetRecipeSuggestions(c *gin.Context) {
	homeID, ok := utils.ParseUUIDHeader(c, h.DB, "X-Home-Id", "Invalid home_id")
	if !ok {
		return
	}

	if !utils.VerifyHomeAccess(c, h.DB, homeID) {
		c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Access denied to this home")})
		return
	}

	var recipes []models.Recipe
	if err := h.DB.Preload("Ingredients.ItemDefinition").Where("home_id = ?", homeID).Find(&recipes).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to fetch recipes")})
		return
	}

	now := time.Now()
	threeDaysFromNow := now.AddDate(0, 0, 3)

	var invItems []models.InventoryItem
	if err := h.DB.Where("home_id = ? AND (expiration_date IS NULL OR expiration_date > ?)", homeID, now).Find(&invItems).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to fetch inventory items")})
		return
	}

	stockByDef := make(map[uuid.UUID]float64)
	expiringSoonDefs := make(map[uuid.UUID]bool)

	for _, item := range invItems {
		stockByDef[item.ItemDefinitionID] += item.Quantity
		if item.ExpirationDate != nil && item.ExpirationDate.After(now) && !item.ExpirationDate.After(threeDaysFromNow) {
			expiringSoonDefs[item.ItemDefinitionID] = true
		}
	}

	suggestions := make([]RecipeSuggestion, 0, len(recipes))

	for _, recipe := range recipes {
		canCook := true
		usesExpiringSoon := false
		missingCount := 0
		details := make([]RecipeIngredientDetail, 0, len(recipe.Ingredients))

		for _, ing := range recipe.Ingredients {
			avail := stockByDef[ing.ItemDefinitionID]
			missing := math.Max(0, ing.QuantityRequired-avail)
			if missing > 0 {
				canCook = false
				missingCount++
			}

			isExpiringSoon := expiringSoonDefs[ing.ItemDefinitionID] && avail > 0
			if isExpiringSoon {
				usesExpiringSoon = true
			}

			details = append(details, RecipeIngredientDetail{
				ItemDefinitionID:   ing.ItemDefinitionID,
				ItemDefinitionName: ing.ItemDefinition.Name,
				QuantityRequired:   ing.QuantityRequired,
				QuantityAvailable:  avail,
				QuantityMissing:    missing,
				UsesExpiringSoon:   isExpiringSoon,
			})
		}

		suggestions = append(suggestions, RecipeSuggestion{
			Recipe:            recipe,
			CanCook:           canCook,
			UsesExpiringSoon:  usesExpiringSoon,
			MissingCount:      missingCount,
			IngredientDetails: details,
		})
	}

	// Sort suggestions:
	// 1. UsesExpiringSoon descending
	// 2. CanCook descending
	// 3. MissingCount ascending
	// 4. Recipe Name ascending
	sort.Slice(suggestions, func(i, j int) bool {
		if suggestions[i].UsesExpiringSoon != suggestions[j].UsesExpiringSoon {
			return suggestions[i].UsesExpiringSoon
		}
		if suggestions[i].CanCook != suggestions[j].CanCook {
			return suggestions[i].CanCook
		}
		if suggestions[i].MissingCount != suggestions[j].MissingCount {
			return suggestions[i].MissingCount < suggestions[j].MissingCount
		}
		return suggestions[i].Recipe.Name < suggestions[j].Recipe.Name
	})

	c.JSON(http.StatusOK, suggestions)
}

func (h *RecipeHandler) CookRecipe(c *gin.Context) {
	id, ok := utils.ParseUUIDParam(c, h.DB, "id", "Invalid recipe ID")
	if !ok {
		return
	}

	var recipe models.Recipe
	if err := h.DB.Preload("Ingredients").First(&recipe, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": i18n.TranslateDB(h.DB, c, "Recipe not found")})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to fetch recipe")})
		}
		return
	}

	if !utils.VerifyHomeWriteAccess(c, h.DB, recipe.HomeID) {
		c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Write access denied to this home")})
		return
	}

	err := h.DB.Transaction(func(tx *gorm.DB) error {
		now := time.Now()

		for _, ing := range recipe.Ingredients {
			var items []models.InventoryItem
			if err := tx.Where("home_id = ? AND item_definition_id = ? AND (expiration_date IS NULL OR expiration_date > ?)", recipe.HomeID, ing.ItemDefinitionID, now).
				Order("expiration_date ASC NULLS LAST").
				Find(&items).Error; err != nil {
				return err
			}

			var totalAvailable float64
			for _, item := range items {
				totalAvailable += item.Quantity
			}

			if totalAvailable < ing.QuantityRequired {
				return fmt.Errorf("insufficient stock for recipe ingredient %s", ing.ItemDefinitionID)
			}

			remainingToDeduct := ing.QuantityRequired
			for _, item := range items {
				if remainingToDeduct <= 0 {
					break
				}

				deduction := item.Quantity
				if deduction > remainingToDeduct {
					deduction = remainingToDeduct
				}

				if err := tx.Model(&item).Update("quantity", item.Quantity-deduction).Error; err != nil {
					return err
				}

				txLog := models.InventoryTransaction{
					HomeID:           recipe.HomeID,
					ItemDefinitionID: ing.ItemDefinitionID,
					InventoryItemID:  item.ID,
					QuantityChange:   -deduction,
				}
				if err := tx.Create(&txLog).Error; err != nil {
					return err
				}

				remainingToDeduct -= deduction
			}

			if err := utils.UpdateShoppingListForDefinition(tx, recipe.HomeID, ing.ItemDefinitionID); err != nil {
				return err
			}
		}

		return nil
	})

	if err != nil {
		if strings.HasPrefix(err.Error(), "insufficient stock") {
			c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(h.DB, c, "Insufficient stock for recipe ingredients")})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to cook recipe")})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": i18n.TranslateDB(h.DB, c, "Recipe cooked successfully")})
}

func (h *RecipeHandler) AddRecipeToShoppingList(c *gin.Context) {
	id, ok := utils.ParseUUIDParam(c, h.DB, "id", "Invalid recipe ID")
	if !ok {
		return
	}

	var recipe models.Recipe
	if err := h.DB.Preload("Ingredients.ItemDefinition").First(&recipe, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": i18n.TranslateDB(h.DB, c, "Recipe not found")})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to fetch recipe")})
		}
		return
	}

	if !utils.VerifyHomeWriteAccess(c, h.DB, recipe.HomeID) {
		c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Write access denied to this home")})
		return
	}

	now := time.Now()

	err := h.DB.Transaction(func(tx *gorm.DB) error {
		for _, ing := range recipe.Ingredients {
			var totalAvailable float64
			if err := tx.Model(&models.InventoryItem{}).
				Where("home_id = ? AND item_definition_id = ? AND (expiration_date IS NULL OR expiration_date > ?)", recipe.HomeID, ing.ItemDefinitionID, now).
				Select("COALESCE(SUM(quantity), 0)").
				Scan(&totalAvailable).Error; err != nil {
				return err
			}

			missing := ing.QuantityRequired - totalAvailable
			if missing <= 0 {
				continue
			}

			var existingItem models.ShoppingListItem
			err := tx.Where("home_id = ? AND item_definition_id = ? AND is_bought = ?", recipe.HomeID, ing.ItemDefinitionID, false).
				First(&existingItem).Error

			if err == gorm.ErrRecordNotFound {
				newItem := models.ShoppingListItem{
					ID:               uuid.New(),
					HomeID:           recipe.HomeID,
					ItemDefinitionID: &ing.ItemDefinitionID,
					Name:             ing.ItemDefinition.Name,
					Quantity:         missing,
					IsBought:         false,
					IsAutoGenerated:  false,
				}
				if err := tx.Create(&newItem).Error; err != nil {
					return err
				}
			} else if err == nil {
				if err := tx.Model(&existingItem).Update("quantity", existingItem.Quantity+missing).Error; err != nil {
					return err
				}
			} else {
				return err
			}
		}

		return nil
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to add recipe to shopping list")})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": i18n.TranslateDB(h.DB, c, "Recipe ingredients added to shopping list successfully")})
}
