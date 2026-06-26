package handlers

import (
	"net/http"
	"sync/atomic"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/i18n"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type SizeUnitHandler struct {
	DB    *gorm.DB
	cache atomic.Value // ⚡ Bolt: Replaced sync.RWMutex with lock-free atomic.Value for read-heavy global cache
}

func (h *SizeUnitHandler) GetSizeUnits(c *gin.Context) {
	if val := h.cache.Load(); val != nil {
		c.JSON(http.StatusOK, val.([]models.SizeUnit))
		return
	}

	var units []models.SizeUnit
	if err := h.DB.Find(&units).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to fetch size units")})
		return
	}

	h.cache.Store(units)

	c.JSON(http.StatusOK, units)
}
