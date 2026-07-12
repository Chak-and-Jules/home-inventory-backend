package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/i18n"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/logger"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type ProductHandler struct {
	DB      *gorm.DB
	BaseURL string
	cache   sync.Map
}

type ProductLookupResponse struct {
	Barcode  string `json:"barcode"`
	Name     string `json:"name"`
	Category string `json:"category"`
	ImageURL string `json:"image_url"`
}

type offResponse struct {
	Status  int `json:"status"`
	Product struct {
		ProductName string `json:"product_name"`
		Categories  string `json:"categories"`
		ImageURL    string `json:"image_url"`
	} `json:"product"`
}

type cachedProduct struct {
	product   ProductLookupResponse
	expiresAt time.Time
}

func (h *ProductHandler) GetProductLookup(c *gin.Context) {
	barcode := c.Query("barcode")
	if barcode == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(h.DB, c, "barcode query parameter is required")})
		return
	}

	// Check cache
	if val, ok := h.cache.Load(barcode); ok {
		cp := val.(cachedProduct)
		if time.Now().Before(cp.expiresAt) {
			c.JSON(http.StatusOK, cp.product)
			return
		}
		h.cache.Delete(barcode)
	}

	// Fetch from Open Food Facts
	baseURL := h.BaseURL
	if baseURL == "" {
		baseURL = "https://world.openfoodfacts.org"
	}
	url := fmt.Sprintf("%s/api/v2/product/%s.json", baseURL, barcode)

	// ⚡ Sentinel: Use custom http.Client with timeout to prevent goroutine exhaustion
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	resp, err := client.Get(url)
	if err != nil {
		logger.Log.Error("Failed to fetch product from Open Food Facts", zap.String("barcode", barcode), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to fetch product info")})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": i18n.TranslateDB(h.DB, c, "Product not found")})
			return
		}
		logger.Log.Error("Open Food Facts returned non-200 status", zap.String("barcode", barcode), zap.Int("status", resp.StatusCode))
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to fetch product info")})
		return
	}

	var off offResponse
	if err := json.NewDecoder(resp.Body).Decode(&off); err != nil {
		logger.Log.Error("Failed to decode Open Food Facts response", zap.String("barcode", barcode), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to fetch product info")})
		return
	}

	if off.Status == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": i18n.TranslateDB(h.DB, c, "Product not found")})
		return
	}

	product := ProductLookupResponse{
		Barcode:  barcode,
		Name:     off.Product.ProductName,
		Category: off.Product.Categories,
		ImageURL: off.Product.ImageURL,
	}

	// Cache the result for 24 hours
	h.cache.Store(barcode, cachedProduct{
		product:   product,
		expiresAt: time.Now().Add(24 * time.Hour),
	})

	c.JSON(http.StatusOK, product)
}
