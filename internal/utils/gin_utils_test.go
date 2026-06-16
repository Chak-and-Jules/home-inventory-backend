package utils

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestParseUUIDParam(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name         string
		paramValue   string
		expectedID   uuid.UUID
		expectedBool bool
		expectedCode int
		expectedErr  string
	}{
		{
			name:         "Valid UUID",
			paramValue:   "123e4567-e89b-12d3-a456-426614174000",
			expectedID:   uuid.MustParse("123e4567-e89b-12d3-a456-426614174000"),
			expectedBool: true,
			expectedCode: http.StatusOK,
		},
		{
			name:         "Invalid UUID",
			paramValue:   "invalid-uuid",
			expectedID:   uuid.Nil,
			expectedBool: false,
			expectedCode: http.StatusBadRequest,
			expectedErr:  "Invalid ID",
		},
		{
			name:         "Empty UUID",
			paramValue:   "",
			expectedID:   uuid.Nil,
			expectedBool: false,
			expectedCode: http.StatusBadRequest,
			expectedErr:  "Invalid ID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Params = []gin.Param{{Key: "id", Value: tt.paramValue}}

			id, ok := ParseUUIDParam(c, nil, "id", "Invalid ID")

			assert.Equal(t, tt.expectedID, id)
			assert.Equal(t, tt.expectedBool, ok)

			if !ok {
				assert.Equal(t, tt.expectedCode, w.Code)
				var response map[string]string
				err := json.Unmarshal(w.Body.Bytes(), &response)
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedErr, response["error"])
			}
		})
	}
}

func TestParseUUIDQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name         string
		queryValue   string
		expectedID   uuid.UUID
		expectedBool bool
		expectedCode int
		expectedErr  string
	}{
		{
			name:         "Valid UUID",
			queryValue:   "123e4567-e89b-12d3-a456-426614174000",
			expectedID:   uuid.MustParse("123e4567-e89b-12d3-a456-426614174000"),
			expectedBool: true,
			expectedCode: http.StatusOK,
		},
		{
			name:         "Invalid UUID",
			queryValue:   "invalid-uuid",
			expectedID:   uuid.Nil,
			expectedBool: false,
			expectedCode: http.StatusBadRequest,
			expectedErr:  "Invalid ID",
		},
		{
			name:         "Missing Query Parameter",
			queryValue:   "",
			expectedID:   uuid.Nil,
			expectedBool: false,
			expectedCode: http.StatusBadRequest,
			expectedErr:  "id query parameter is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			req, _ := http.NewRequest(http.MethodGet, "/test?id="+tt.queryValue, nil)
			if tt.queryValue == "" {
				req, _ = http.NewRequest(http.MethodGet, "/test", nil)
			}
			c.Request = req

			id, ok := ParseUUIDQuery(c, nil, "id", "Invalid ID")

			assert.Equal(t, tt.expectedID, id)
			assert.Equal(t, tt.expectedBool, ok)

			if !ok {
				assert.Equal(t, tt.expectedCode, w.Code)
				var response map[string]string
				err := json.Unmarshal(w.Body.Bytes(), &response)
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedErr, response["error"])
			}
		})
	}
}

func TestParseUUIDHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name         string
		headerValue  string
		expectedID   uuid.UUID
		expectedBool bool
		expectedCode int
		expectedErr  string
	}{
		{
			name:         "Valid UUID",
			headerValue:  "123e4567-e89b-12d3-a456-426614174000",
			expectedID:   uuid.MustParse("123e4567-e89b-12d3-a456-426614174000"),
			expectedBool: true,
			expectedCode: http.StatusOK,
		},
		{
			name:         "Invalid UUID",
			headerValue:  "invalid-uuid",
			expectedID:   uuid.Nil,
			expectedBool: false,
			expectedCode: http.StatusBadRequest,
			expectedErr:  "Invalid ID",
		},
		{
			name:         "Missing Header",
			headerValue:  "",
			expectedID:   uuid.Nil,
			expectedBool: false,
			expectedCode: http.StatusBadRequest,
			expectedErr:  "x-home-id header is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			req, _ := http.NewRequest(http.MethodGet, "/", nil)
			if tt.headerValue != "" {
				req.Header.Set("x-home-id", tt.headerValue)
			}
			c.Request = req

			id, ok := ParseUUIDHeader(c, nil, "x-home-id", "Invalid ID")

			assert.Equal(t, tt.expectedID, id)
			assert.Equal(t, tt.expectedBool, ok)

			if !ok {
				assert.Equal(t, tt.expectedCode, w.Code)
				var response map[string]string
				err := json.Unmarshal(w.Body.Bytes(), &response)
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedErr, response["error"])
			}
		})
	}
}
