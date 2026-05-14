package handlers

import (
	"net/http"
	"time"

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
	Quantity         float64    `json:"quantity" binding:"required"`
	ExpirationDate   *time.Time `json:"expiration_date"`
}

type UpdateInventoryItemRequest struct {
	Quantity       float64    `json:"quantity" binding:"required"`
	ExpirationDate *time.Time `json:"expiration_date"`
}

type UpdateQuantityRequest struct {
	Quantity float64 `json:"quantity" binding:"required"`
}

// verifyHomeAccess checks if the user has access to the home
func (h *InventoryItemHandler) verifyHomeAccess(c *gin.Context, homeID uuid.UUID) bool {
	userID := c.MustGet("userID").(uuid.UUID)
	var userHome models.UserHome
	if err := h.DB.Where("user_id = ? AND home_id = ?", userID, homeID).First(&userHome).Error; err != nil {
		return false
	}
	return true
}

// verifyHomeWriteAccess checks if the user has owner or editor access to the home
func (h *InventoryItemHandler) verifyHomeWriteAccess(c *gin.Context, homeID uuid.UUID) bool {
	userID := c.MustGet("userID").(uuid.UUID)
	var userHome models.UserHome
	if err := h.DB.Where("user_id = ? AND home_id = ?", userID, homeID).First(&userHome).Error; err != nil {
		return false
	}
	return userHome.Role == "owner" || userHome.Role == "editor"
}

func (h *InventoryItemHandler) GetInventoryItems(c *gin.Context) {
	homeID, ok := utils.ParseUUIDQuery(c, "home_id", "Invalid home_id")
	if !ok {
		return
	}

	if !h.verifyHomeAccess(c, homeID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied to this home"})
		return
	}

	var items []models.InventoryItem
	if err := h.DB.Preload("ItemDefinition.Category").Preload("ItemDefinition.SizeUnit").Where("home_id = ?", homeID).Find(&items).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch inventory items"})
		return
	}
	c.JSON(http.StatusOK, items)
}

func (h *InventoryItemHandler) CreateInventoryItem(c *gin.Context) {
	homeID, ok := utils.ParseUUIDQuery(c, "home_id", "Invalid home_id")
	if !ok {
		return
	}

	if !h.verifyHomeWriteAccess(c, homeID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Write access denied to this home"})
		return
	}

	var req CreateInventoryItemRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	item := models.InventoryItem{
		HomeID:           homeID,
		ItemDefinitionID: req.ItemDefinitionID,
		Quantity:         req.Quantity,
		ExpirationDate:   req.ExpirationDate,
	}

	if err := h.DB.Create(&item).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create inventory item"})
		return
	}

	c.JSON(http.StatusCreated, item)
}

func (h *InventoryItemHandler) UpdateInventoryItem(c *gin.Context) {
	id, ok := utils.ParseUUIDParam(c, "id", "Invalid inventory item ID")
	if !ok {
		return
	}

	// First find the item to check its home_id
	var item models.InventoryItem
	if err := h.DB.First(&item, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Inventory item not found"})
		return
	}

	if !h.verifyHomeWriteAccess(c, item.HomeID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Write access denied to this home"})
		return
	}

	var req UpdateInventoryItemRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	updates := map[string]interface{}{
		"quantity":        req.Quantity,
		"expiration_date": req.ExpirationDate,
	}

	if err := h.DB.Model(&item).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update inventory item"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Inventory item updated successfully"})
}

func (h *InventoryItemHandler) UpdateInventoryItemQuantity(c *gin.Context) {
	id, ok := utils.ParseUUIDParam(c, "id", "Invalid inventory item ID")
	if !ok {
		return
	}

	var item models.InventoryItem
	if err := h.DB.First(&item, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Inventory item not found"})
		return
	}

	if !h.verifyHomeWriteAccess(c, item.HomeID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Write access denied to this home"})
		return
	}

	var req UpdateQuantityRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.DB.Model(&item).Update("quantity", req.Quantity).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update quantity"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Quantity updated successfully"})
}

func (h *InventoryItemHandler) DeleteInventoryItem(c *gin.Context) {
	id, ok := utils.ParseUUIDParam(c, "id", "Invalid inventory item ID")
	if !ok {
		return
	}

	var item models.InventoryItem
	if err := h.DB.First(&item, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Inventory item not found"})
		return
	}

	if !h.verifyHomeWriteAccess(c, item.HomeID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Write access denied to this home"})
		return
	}

	if err := h.DB.Delete(&models.InventoryItem{}, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete inventory item"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Inventory item deleted successfully"})
}
