package middleware

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
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

func TestSupabaseAuthMiddleware_ValidECDSAToken(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)
	os.Unsetenv("SUPABASE_JWT_SECRET")
	defer os.Unsetenv("SUPABASE_JWT_PUBLIC_KEY")

	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	publicKeyBytes, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}

	publicKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: publicKeyBytes,
	})

	os.Setenv("SUPABASE_JWT_PUBLIC_KEY", string(publicKeyPEM))

	r := gin.New()
	r.Use(SupabaseAuthMiddleware())
	r.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	userID := uuid.New().String()
	token := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
		"sub": userID,
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	tokenString, err := token.SignedString(privateKey)
	if err != nil {
		t.Fatal(err)
	}

	req, _ := http.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+tokenString)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}
}

func TestSupabaseAuthMiddleware_Format(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(SupabaseAuthMiddleware())
	r.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	tests := []struct {
		name       string
		authHeader string
		wantCode   int
	}{
		{
			name:       "Missing header",
			authHeader: "",
			wantCode:   http.StatusUnauthorized,
		},
		{
			name:       "Invalid prefix",
			authHeader: "Basic something",
			wantCode:   http.StatusUnauthorized,
		},
		{
			name:       "Extra spaces",
			authHeader: "Bearer  something",
			wantCode:   http.StatusUnauthorized,
		},
		{
			name:       "Multiple tokens",
			authHeader: "Bearer token1 token2",
			wantCode:   http.StatusUnauthorized,
		},
		{
			name:       "Just Bearer",
			authHeader: "Bearer",
			wantCode:   http.StatusUnauthorized,
		},
		{
			name:       "Valid format (fails at secret check)",
			authHeader: "Bearer valid_token_format",
			wantCode:   http.StatusInternalServerError, // Fails at secret check
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest("GET", "/test", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != tt.wantCode {
				t.Errorf("Expected status %d, got %d", tt.wantCode, w.Code)
			}
		})
	}
}

func TestSupabaseAuthMiddleware_FullCoverage(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)
	secret := "super-secret-key"
	os.Setenv("SUPABASE_JWT_SECRET", secret)
	defer os.Unsetenv("SUPABASE_JWT_SECRET")

	r := gin.New()
	r.Use(SupabaseAuthMiddleware())
	r.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	t.Run("Invalid Token", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer invalid-token")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("Expected status %d, got %d", http.StatusUnauthorized, w.Code)
		}
	})

	t.Run("Wrong Signing Method", func(t *testing.T) {
		token := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.MapClaims{
			"sub": uuid.New().String(),
		})
		tokenString, _ := token.SignedString(jwt.UnsafeAllowNoneSignatureType)

		req, _ := http.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer "+tokenString)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("Expected status %d, got %d", http.StatusUnauthorized, w.Code)
		}
	})

	t.Run("No Claims", func(t *testing.T) {
		token := jwt.New(jwt.SigningMethodHS256)
		tokenString, _ := token.SignedString([]byte(secret))

		req, _ := http.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer "+tokenString)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("Expected status %d, got %d", http.StatusUnauthorized, w.Code)
		}
	})

	t.Run("Missing Sub Claim", func(t *testing.T) {
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"exp": time.Now().Add(time.Hour).Unix(),
		})
		tokenString, _ := token.SignedString([]byte(secret))

		req, _ := http.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer "+tokenString)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("Expected status %d, got %d", http.StatusUnauthorized, w.Code)
		}
	})

	t.Run("Invalid UUID in Sub Claim", func(t *testing.T) {
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"sub": "not-a-uuid",
			"exp": time.Now().Add(time.Hour).Unix(),
		})
		tokenString, _ := token.SignedString([]byte(secret))

		req, _ := http.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer "+tokenString)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("Expected status %d, got %d", http.StatusUnauthorized, w.Code)
		}
	})
}

func TestSupabaseAuthMiddleware_FailedParseClaims(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)
	secret := "super-secret-key"
	os.Setenv("SUPABASE_JWT_SECRET", secret)
	defer os.Unsetenv("SUPABASE_JWT_SECRET")

	r := gin.New()
	r.Use(SupabaseAuthMiddleware())
	r.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	// To hit the `!ok` block for claims, we could implement custom claims struct instead of MapClaims
	// Then jwt.Parse will result in token.Claims not being jwt.MapClaims
	type CustomClaims struct {
		jwt.RegisteredClaims
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, CustomClaims{})
	tokenString, _ := token.SignedString([]byte(secret))

	req, _ := http.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+tokenString)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status %d, got %d", http.StatusUnauthorized, w.Code)
	}
}
