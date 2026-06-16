package utils

import (
	"net/http"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/i18n"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ParseUUIDParam extracts a path parameter, parses it as a UUID, and returns the result.
// If parsing fails, it sends a 400 Bad Request response and returns (uuid.Nil, false).
func ParseUUIDParam(c *gin.Context, db *gorm.DB, name string, errorMessage string) (uuid.UUID, bool) {
	valStr := c.Param(name)
	id, err := uuid.Parse(valStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(db, c, errorMessage)})
		return uuid.Nil, false
	}
	return id, true
}

// ParseUUIDQuery extracts a query parameter, parses it as a UUID, and returns the result.
// If the parameter is missing or invalid, it sends a 400 Bad Request response and returns (uuid.Nil, false).
func ParseUUIDQuery(c *gin.Context, db *gorm.DB, name string, errorMessage string) (uuid.UUID, bool) {
	valStr := c.Query(name)
	if valStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(db, c, name+" query parameter is required")})
		return uuid.Nil, false
	}
	id, err := uuid.Parse(valStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(db, c, errorMessage)})
		return uuid.Nil, false
	}
	return id, true
}

// ParseUUIDHeader extracts a header value, parses it as a UUID, and returns the result.
// If the header is missing or invalid, it sends a 400 Bad Request response and returns (uuid.Nil, false).
func ParseUUIDHeader(c *gin.Context, db *gorm.DB, name string, errorMessage string) (uuid.UUID, bool) {
	valStr := c.GetHeader(name)
	if valStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(db, c, name+" header is required")})
		return uuid.Nil, false
	}
	id, err := uuid.Parse(valStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(db, c, errorMessage)})
		return uuid.Nil, false
	}
	return id, true
}
