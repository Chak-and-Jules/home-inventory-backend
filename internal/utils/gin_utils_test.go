package utils

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestParseUUIDParam(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name         string
		paramValue   string
		expectStatus int
		expectValid  bool
	}{
		{
			name:         "Valid UUID",
			paramValue:   "123e4567-e89b-12d3-a456-426614174000",
			expectStatus: http.StatusOK,
			expectValid:  true,
		},
		{
			name:         "Invalid UUID",
			paramValue:   "invalid-uuid",
			expectStatus: http.StatusBadRequest,
			expectValid:  false,
		},
		{
			name:         "Empty string",
			paramValue:   "",
			expectStatus: http.StatusBadRequest,
			expectValid:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Params = gin.Params{gin.Param{Key: "id", Value: tc.paramValue}}

			id, ok := ParseUUIDParam(c, "id", "Invalid ID")

			if tc.expectValid {
				if !ok {
					t.Errorf("Expected valid UUID but got invalid")
				}
				if id.String() != tc.paramValue {
					t.Errorf("Expected ID %s, got %s", tc.paramValue, id.String())
				}
			} else {
				if ok {
					t.Errorf("Expected invalid UUID but got valid")
				}
				if id != uuid.Nil {
					t.Errorf("Expected Nil UUID, got %v", id)
				}
				if w.Code != tc.expectStatus {
					t.Errorf("Expected status %d, got %d", tc.expectStatus, w.Code)
				}
			}
		})
	}
}

func TestParseUUIDQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name         string
		queryValue   string
		expectStatus int
		expectValid  bool
	}{
		{
			name:         "Valid UUID",
			queryValue:   "123e4567-e89b-12d3-a456-426614174000",
			expectStatus: http.StatusOK,
			expectValid:  true,
		},
		{
			name:         "Invalid UUID",
			queryValue:   "invalid-uuid",
			expectStatus: http.StatusBadRequest,
			expectValid:  false,
		},
		{
			name:         "Empty value",
			queryValue:   "",
			expectStatus: http.StatusBadRequest,
			expectValid:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			if tc.queryValue == "" {
				c.Request, _ = http.NewRequest(http.MethodGet, "/", nil)
			} else {
				c.Request, _ = http.NewRequest(http.MethodGet, "/?id="+tc.queryValue, nil)
			}

			id, ok := ParseUUIDQuery(c, "id", "Invalid ID")

			if tc.expectValid {
				if !ok {
					t.Errorf("Expected valid UUID but got invalid")
				}
				if id.String() != tc.queryValue {
					t.Errorf("Expected ID %s, got %s", tc.queryValue, id.String())
				}
			} else {
				if ok {
					t.Errorf("Expected invalid UUID but got valid")
				}
				if id != uuid.Nil {
					t.Errorf("Expected Nil UUID, got %v", id)
				}
				if w.Code != tc.expectStatus {
					t.Errorf("Expected status %d, got %d", tc.expectStatus, w.Code)
				}
			}
		})
	}
}
