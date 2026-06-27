package handlers

import (
	"net/http"
	"sync/atomic"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/i18n"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type LanguageHandler struct {
	DB    *gorm.DB
	cache atomic.Value // ⚡ Bolt: Lock-free cache for completely replaced global data
}

func (h *LanguageHandler) GetLanguages(c *gin.Context) {
	if cached := h.cache.Load(); cached != nil {
		c.JSON(http.StatusOK, cached.([]models.Language))
		return
	}

	var languages []models.Language
	if err := h.DB.Find(&languages).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to fetch languages")})
		return
	}

	h.cache.Store(languages)

	c.JSON(http.StatusOK, languages)
}
