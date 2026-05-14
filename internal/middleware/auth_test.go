package middleware

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func TestSupabaseAuthMiddleware_EmptySecret(t *testing.T) {
	// Set Gin to release mode for cleaner test output
	gin.SetMode(gin.ReleaseMode)

	// Ensure secret is empty
	os.Unsetenv("SUPABASE_JWT_SECRET")

	// Create router with middleware
	r := gin.New()
	r.Use(SupabaseAuthMiddleware())
	r.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	// Create a token that might exploit the empty secret vulnerability
	// We'll create an HMAC signed token with an empty secret, which the library
	// allows if we don't block it.
	userID := uuid.New().String()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": userID,
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	tokenString, _ := token.SignedString([]byte(""))

	req, _ := http.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+tokenString)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	// We expect a 500 Internal Server Error because the secret is missing
	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status %d, got %d", http.StatusInternalServerError, w.Code)
	}

	expectedBody := `{"error":"JWT secret is not configured"}`
	if w.Body.String() != expectedBody {
		t.Errorf("Expected body %q, got %q", expectedBody, w.Body.String())
	}
}

func TestSupabaseAuthMiddleware_ValidSecret(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)

	// Set a valid secret
	secret := "super-secret-key"
	os.Setenv("SUPABASE_JWT_SECRET", secret)
	defer os.Unsetenv("SUPABASE_JWT_SECRET")

	r := gin.New()
	r.Use(SupabaseAuthMiddleware())
	r.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	userID := uuid.New().String()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": userID,
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	tokenString, _ := token.SignedString([]byte(secret))

	req, _ := http.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+tokenString)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	// We expect a 200 OK because the secret is valid and the token is valid
	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}
}
