package middleware

import (
	"github.com/gin-gonic/gin"
)

// SecurityHeadersMiddleware adds standard security headers to HTTP responses
func SecurityHeadersMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Prevent clickjacking by forbidding rendering inside iframes
		c.Writer.Header().Set("X-Frame-Options", "DENY")
		// Prevent MIME-sniffing
		c.Writer.Header().Set("X-Content-Type-Options", "nosniff")
		// Enforce HTTP Strict Transport Security (HSTS)
		c.Writer.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")

		c.Next()
	}
}
