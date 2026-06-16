package handlers

import (
	"net/http"
	"sync"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/i18n"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type LanguageHandler struct {
	DB         *gorm.DB
	mu         sync.RWMutex
	cache      []models.Language
	cacheValid bool
}

func (h *LanguageHandler) GetLanguages(c *gin.Context) {
	h.mu.RLock()
	if h.cacheValid {
		languages := h.cache
		h.mu.RUnlock()
		c.JSON(http.StatusOK, languages)
		return
	}
	h.mu.RUnlock()

	var languages []models.Language
	if err := h.DB.Find(&languages).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to fetch languages")})
		return
	}

	h.mu.Lock()
	h.cache = languages
	h.cacheValid = true
	h.mu.Unlock()

	c.JSON(http.StatusOK, languages)
}
