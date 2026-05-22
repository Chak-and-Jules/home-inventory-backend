package handlers

import (
	"net/http"
	"sync"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type SizeUnitHandler struct {
	DB         *gorm.DB
	mu         sync.RWMutex
	cache      []models.SizeUnit
	cacheValid bool
}

func (h *SizeUnitHandler) GetSizeUnits(c *gin.Context) {
	h.mu.RLock()
	if h.cacheValid {
		units := h.cache
		h.mu.RUnlock()
		c.JSON(http.StatusOK, units)
		return
	}
	h.mu.RUnlock()

	var units []models.SizeUnit
	if err := h.DB.Find(&units).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch size units"})
		return
	}

	h.mu.Lock()
	h.cache = units
	h.cacheValid = true
	h.mu.Unlock()

	c.JSON(http.StatusOK, units)
}
