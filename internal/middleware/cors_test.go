package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCORSMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("OPTIONS Request", func(t *testing.T) {
		r := gin.New()
		r.Use(CORSMiddleware())
		r.OPTIONS("/test", func(c *gin.Context) {
			c.String(http.StatusOK, "should not be reached")
		})

		req, _ := http.NewRequest(http.MethodOptions, "/test", nil)
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		if w.Code != http.StatusNoContent {
			t.Errorf("Expected status %d, got %d", http.StatusNoContent, w.Code)
		}

		checkCORSHeaders(t, w)
	})

	t.Run("GET Request", func(t *testing.T) {
		r := gin.New()
		r.Use(CORSMiddleware())
		r.GET("/test", func(c *gin.Context) {
			c.String(http.StatusOK, "success")
		})

		req, _ := http.NewRequest(http.MethodGet, "/test", nil)
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
		}

		if w.Body.String() != "success" {
			t.Errorf("Expected body 'success', got '%s'", w.Body.String())
		}

		checkCORSHeaders(t, w)
	})
}

func checkCORSHeaders(t *testing.T, w *httptest.ResponseRecorder) {
	headers := w.Header()

	expectedHeaders := map[string]string{
		"Access-Control-Allow-Origin":      "*",
		"Access-Control-Allow-Credentials": "true",
		"Access-Control-Allow-Headers":     "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With",
		"Access-Control-Allow-Methods":     "POST, OPTIONS, GET, PUT, PATCH, DELETE",
	}

	for key, expectedValue := range expectedHeaders {
		if headers.Get(key) != expectedValue {
			t.Errorf("Expected header %s to be '%s', got '%s'", key, expectedValue, headers.Get(key))
		}
	}
}
