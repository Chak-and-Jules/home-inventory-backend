package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/i18n"
	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

type rateLimiter struct {
	limiters map[string]*rate.Limiter
	mu       sync.Mutex
	rate     rate.Limit
	burst    int
}

var limiter = &rateLimiter{
	limiters: make(map[string]*rate.Limiter),
	rate:     rate.Every(1 * time.Minute / 300), // 300 requests per minute
	burst:    300,
}

func (l *rateLimiter) getLimiter(ip string) *rate.Limiter {
	l.mu.Lock()
	defer l.mu.Unlock()

	lim, exists := l.limiters[ip]
	if !exists {
		lim = rate.NewLimiter(l.rate, l.burst)
		l.limiters[ip] = lim
	}

	return lim
}

// RateLimitMiddleware applies IP-based rate limiting
func RateLimitMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()
		lim := limiter.getLimiter(ip)

		if !lim.Allow() {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": i18n.TranslateDB(nil, c, "Too many requests"),
			})
			return
		}

		c.Next()
	}
}
