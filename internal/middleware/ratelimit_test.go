package middleware

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"golang.org/x/time/rate"
)

func TestRateLimitMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Override limiter settings for testing
	limiter = &rateLimiter{
		limiters: sync.Map{},
		rate:     rate.Every(1 * time.Minute / 2), // 2 requests per minute
		burst:    2,
	}

	r := gin.New()
	r.Use(RateLimitMiddleware())
	r.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	// Request 1: Should succeed
	req, _ := http.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Request 2: Should succeed
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Request 3: Should fail (Rate limit exceeded)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusTooManyRequests, w.Code)
	assert.Contains(t, w.Body.String(), "Too many requests")

	// Request from a different IP: Should succeed
	req2, _ := http.NewRequest(http.MethodGet, "/test", nil)
	req2.RemoteAddr = "192.168.1.1:1234"
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req2)
	assert.Equal(t, http.StatusOK, w.Code)
}
