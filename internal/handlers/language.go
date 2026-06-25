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
	cache atomic.Value
}

func (h *LanguageHandler) GetLanguages(c *gin.Context) {
	if val := h.cache.Load(); val != nil {
		c.JSON(http.StatusOK, val.([]models.Language))
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
