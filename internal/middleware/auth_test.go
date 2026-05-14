package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func TestSupabaseAuthMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Set a mock secret for testing
	os.Setenv("SUPABASE_JWT_SECRET", "mock-secret-for-testing-only")
	defer os.Unsetenv("SUPABASE_JWT_SECRET")

	mockSecret := []byte(os.Getenv("SUPABASE_JWT_SECRET"))

	tests := []struct {
		name           string
		setupAuth      func(req *http.Request)
		expectedStatus int
		expectUserID   bool
	}{
		{
			name: "Missing Authorization header",
			setupAuth: func(req *http.Request) {
				// Do not set Authorization header
			},
			expectedStatus: http.StatusUnauthorized,
			expectUserID:   false,
		},
		{
			name: "Invalid format (no Bearer)",
			setupAuth: func(req *http.Request) {
				req.Header.Set("Authorization", "InvalidFormatToken")
			},
			expectedStatus: http.StatusUnauthorized,
			expectUserID:   false,
		},
		{
			name: "Invalid token signature",
			setupAuth: func(req *http.Request) {
				token := generateToken("123e4567-e89b-12d3-a456-426614174000", []byte("wrong-secret"))
				req.Header.Set("Authorization", "Bearer "+token)
			},
			expectedStatus: http.StatusUnauthorized,
			expectUserID:   false,
		},
		{
			name: "Expired token",
			setupAuth: func(req *http.Request) {
				token := generateExpiredToken("123e4567-e89b-12d3-a456-426614174000", mockSecret)
				req.Header.Set("Authorization", "Bearer "+token)
			},
			expectedStatus: http.StatusUnauthorized,
			expectUserID:   false,
		},
		{
			name: "Missing subject",
			setupAuth: func(req *http.Request) {
				token := generateTokenNoSub(mockSecret)
				req.Header.Set("Authorization", "Bearer "+token)
			},
			expectedStatus: http.StatusUnauthorized,
			expectUserID:   false,
		},
		{
			name: "Invalid subject (not UUID)",
			setupAuth: func(req *http.Request) {
				token := generateToken("invalid-uuid", mockSecret)
				req.Header.Set("Authorization", "Bearer "+token)
			},
			expectedStatus: http.StatusUnauthorized,
			expectUserID:   false,
		},
		{
			name: "Valid token",
			setupAuth: func(req *http.Request) {
				token := generateToken("123e4567-e89b-12d3-a456-426614174000", mockSecret)
				req.Header.Set("Authorization", "Bearer "+token)
			},
			expectedStatus: http.StatusOK,
			expectUserID:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := gin.New()
			r.Use(SupabaseAuthMiddleware())
			r.GET("/protected", func(c *gin.Context) {
				userID, exists := c.Get("userID")
				if !exists {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "user ID not set"})
					return
				}
				c.JSON(http.StatusOK, gin.H{"userID": userID})
			})

			req, _ := http.NewRequest(http.MethodGet, "/protected", nil)
			tc.setupAuth(req)

			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != tc.expectedStatus {
				t.Errorf("Expected status %d, got %d", tc.expectedStatus, w.Code)
			}

			if tc.expectUserID {
				var response map[string]interface{}
				if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
					t.Fatalf("Failed to parse response body: %v", err)
				}

				uidStr, ok := response["userID"].(string)
				if !ok {
					t.Errorf("Response body does not contain userID string")
				} else {
					if _, err := uuid.Parse(uidStr); err != nil {
						t.Errorf("Returned userID is not a valid UUID: %v", err)
					}
				}
			}
		})
	}
}

// Helpers for token generation

func generateToken(sub string, secret []byte) string {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": sub,
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	tokenString, _ := token.SignedString(secret)
	return tokenString
}

func generateExpiredToken(sub string, secret []byte) string {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": sub,
		"exp": time.Now().Add(-time.Hour).Unix(),
	})
	tokenString, _ := token.SignedString(secret)
	return tokenString
}

func generateTokenNoSub(secret []byte) string {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	tokenString, _ := token.SignedString(secret)
	return tokenString
}
