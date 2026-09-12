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
			expectedErr:  "X-Home-Id header is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			req, _ := http.NewRequest(http.MethodGet, "/", nil)
			if tt.headerValue != "" {
				req.Header.Set("X-Home-Id", tt.headerValue)
			}
			c.Request = req

			id, ok := ParseUUIDHeader(c, nil, "X-Home-Id", "Invalid ID")

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

func TestGetAuthUserID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name         string
		setupContext func(c *gin.Context)
		expectedID   uuid.UUID
		expectedBool bool
		expectedCode int
		expectedErr  string
	}{
		{
			name: "Valid User ID",
			setupContext: func(c *gin.Context) {
				c.Set("userID", uuid.MustParse("123e4567-e89b-12d3-a456-426614174000"))
			},
			expectedID:   uuid.MustParse("123e4567-e89b-12d3-a456-426614174000"),
			expectedBool: true,
			expectedCode: http.StatusOK,
		},
		{
			name: "Missing User ID",
			setupContext: func(c *gin.Context) {
				// Do not set userID
			},
			expectedID:   uuid.Nil,
			expectedBool: false,
			expectedCode: http.StatusUnauthorized,
			expectedErr:  "Unauthorized access",
		},
		{
			name: "Invalid User ID Type",
			setupContext: func(c *gin.Context) {
				c.Set("userID", "not-a-uuid-type")
			},
			expectedID:   uuid.Nil,
			expectedBool: false,
			expectedCode: http.StatusUnauthorized,
			expectedErr:  "Invalid user ID format in context",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			tt.setupContext(c)

			id, ok := GetAuthUserID(c, nil)

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

func TestGetAuthEmail(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name         string
		setupContext func(c *gin.Context)
		expectedStr  string
		expectedBool bool
		expectedCode int
		expectedErr  string
	}{
		{
			name: "Valid Email",
			setupContext: func(c *gin.Context) {
				c.Set("email", "test@example.com")
			},
			expectedStr:  "test@example.com",
			expectedBool: true,
			expectedCode: http.StatusOK,
		},
		{
			name: "Missing Email",
			setupContext: func(c *gin.Context) {
				// Do not set email
			},
			expectedStr:  "",
			expectedBool: false,
			expectedCode: http.StatusUnauthorized,
			expectedErr:  "Unauthorized access",
		},
		{
			name: "Invalid Email Type",
			setupContext: func(c *gin.Context) {
				c.Set("email", 123)
			},
			expectedStr:  "",
			expectedBool: false,
			expectedCode: http.StatusUnauthorized,
			expectedErr:  "Invalid email format in context",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			tt.setupContext(c)

			str, ok := GetAuthEmail(c, nil)

			assert.Equal(t, tt.expectedStr, str)
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
