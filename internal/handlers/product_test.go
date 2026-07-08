package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestGetProductLookup(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("Missing barcode", func(t *testing.T) {
		handler := &ProductHandler{}
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/api/v1/products/lookup", nil)

		handler.GetProductLookup(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("Product found", func(t *testing.T) {
		barcode := "12345678"
		mockResp := offResponse{
			Status: 1,
			Product: struct {
				ProductName string `json:"product_name"`
				Categories  string `json:"categories"`
				ImageURL    string `json:"image_url"`
			}{
				ProductName: "Test Product",
				Categories:  "Test Category",
				ImageURL:    "http://example.com/image.jpg",
			},
		}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, fmt.Sprintf("/api/v2/product/%s.json", barcode), r.URL.Path)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(mockResp)
		}))
		defer server.Close()

		handler := &ProductHandler{BaseURL: server.URL}
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/api/v1/products/lookup?barcode="+barcode, nil)

		handler.GetProductLookup(c)

		assert.Equal(t, http.StatusOK, w.Code)
		var resp ProductLookupResponse
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		assert.NoError(t, err)
		assert.Equal(t, barcode, resp.Barcode)
		assert.Equal(t, "Test Product", resp.Name)
	})

	t.Run("Cache hit", func(t *testing.T) {
		barcode := "99999999"
		mockResp := offResponse{
			Status: 1,
			Product: struct {
				ProductName string `json:"product_name"`
				Categories  string `json:"categories"`
				ImageURL    string `json:"image_url"`
			}{
				ProductName: "Cached Product",
				Categories:  "Category",
				ImageURL:    "http://example.com/image.jpg",
			},
		}

		callCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount++
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(mockResp)
		}))
		defer server.Close()

		handler := &ProductHandler{BaseURL: server.URL}

		// First call
		w1 := httptest.NewRecorder()
		c1, _ := gin.CreateTestContext(w1)
		c1.Request = httptest.NewRequest("GET", "/api/v1/products/lookup?barcode="+barcode, nil)
		handler.GetProductLookup(c1)
		assert.Equal(t, http.StatusOK, w1.Code)
		assert.Equal(t, 1, callCount)

		// Second call (cache hit)
		w2 := httptest.NewRecorder()
		c2, _ := gin.CreateTestContext(w2)
		c2.Request = httptest.NewRequest("GET", "/api/v1/products/lookup?barcode="+barcode, nil)
		handler.GetProductLookup(c2)
		assert.Equal(t, http.StatusOK, w2.Code)
		assert.Equal(t, 1, callCount)
	})
}
