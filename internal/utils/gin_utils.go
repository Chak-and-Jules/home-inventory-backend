package utils

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ParseUUIDParam extracts a path parameter, parses it as a UUID, and returns the result.
// If parsing fails, it sends a 400 Bad Request response and returns (uuid.Nil, false).
func ParseUUIDParam(c *gin.Context, name string, errorMessage string) (uuid.UUID, bool) {
	valStr := c.Param(name)
	id, err := uuid.Parse(valStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": errorMessage})
		return uuid.Nil, false
	}
	return id, true
}

// ParseUUIDQuery extracts a query parameter, parses it as a UUID, and returns the result.
// If the parameter is missing or invalid, it sends a 400 Bad Request response and returns (uuid.Nil, false).
func ParseUUIDQuery(c *gin.Context, name string, errorMessage string) (uuid.UUID, bool) {
	valStr := c.Query(name)
	if valStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": name + " query parameter is required"})
		return uuid.Nil, false
	}
	id, err := uuid.Parse(valStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": errorMessage})
		return uuid.Nil, false
	}
	return id, true
}
