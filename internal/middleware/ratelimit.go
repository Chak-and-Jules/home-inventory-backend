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
	limiters sync.Map
	rate     rate.Limit
	burst    int
}

var limiter = &rateLimiter{
	limiters: sync.Map{},
	rate:     rate.Every(1 * time.Minute / 300), // 300 requests per minute
	burst:    300,
}

func (l *rateLimiter) getLimiter(ip string) *rate.Limiter {
	lim, exists := l.limiters.Load(ip)
	if !exists {
		// ⚡ Bolt: Use lock-free sync.Map for highly concurrent IP rate limiting,
		// replacing a global sync.Mutex which bottlenecks all requests.
		newLimiter := rate.NewLimiter(l.rate, l.burst)
		lim, _ = l.limiters.LoadOrStore(ip, newLimiter)
	}

	return lim.(*rate.Limiter)
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
