package middleware

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
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

func TestSupabaseAuthMiddleware_ECDSACoverageExtra(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)

	secret := "super-secret-key"
	os.Setenv("SUPABASE_JWT_SECRET", secret)
	defer os.Unsetenv("SUPABASE_JWT_SECRET")

	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	xB64 := base64.RawURLEncoding.EncodeToString(privateKey.PublicKey.X.Bytes())
	yB64 := base64.RawURLEncoding.EncodeToString(privateKey.PublicKey.Y.Bytes())
	kid := "test-key"

	// Clear sync.Map cache for tests to force fetch
	jwksCache.Range(func(key, value interface{}) bool {
		jwksCache.Delete(key)
		return true
	})

	t.Run("Invalid Key Fetch", func(t *testing.T) {
		serverInvalidKey := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"keys": []}`))
		}))
		defer serverInvalidKey.Close()

		os.Setenv("SUPABASE_URL", serverInvalidKey.URL)

		jwksCache.Range(func(key, value interface{}) bool {
			jwksCache.Delete(key)
			return true
		})

		token := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
			"sub":   uuid.New().String(),
			"email": "test@example.com",
			"exp":   time.Now().Add(time.Hour).Unix(),
		})
		token.Header["kid"] = kid

		tokenString, _ := token.SignedString(privateKey)

		r := gin.New()
		r.Use(SupabaseAuthMiddleware())
		r.GET("/test", func(c *gin.Context) {
			c.Status(http.StatusOK)
		})

		req, _ := http.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer "+tokenString)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("Expected status %d, got %d", http.StatusUnauthorized, w.Code)
		}
	})

	t.Run("Missing Kid", func(t *testing.T) {
		os.Setenv("SUPABASE_URL", "http://example.com")

		token := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
			"sub":   uuid.New().String(),
			"email": "test@example.com",
			"exp":   time.Now().Add(time.Hour).Unix(),
		})

		tokenString, _ := token.SignedString(privateKey)

		r := gin.New()
		r.Use(SupabaseAuthMiddleware())
		r.GET("/test", func(c *gin.Context) {
			c.Status(http.StatusOK)
		})

		req, _ := http.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer "+tokenString)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("Expected status %d, got %d", http.StatusUnauthorized, w.Code)
		}
	})

	t.Run("Invalid Format JWKS", func(t *testing.T) {
		serverInvalidFormat := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"keys": "invalid"}`))
		}))
		defer serverInvalidFormat.Close()

		os.Setenv("SUPABASE_URL", serverInvalidFormat.URL)

		token := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
			"sub":   uuid.New().String(),
			"email": "test@example.com",
			"exp":   time.Now().Add(time.Hour).Unix(),
		})
		token.Header["kid"] = kid

		tokenString, _ := token.SignedString(privateKey)

		r := gin.New()
		r.Use(SupabaseAuthMiddleware())
		r.GET("/test", func(c *gin.Context) {
			c.Status(http.StatusOK)
		})

		req, _ := http.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer "+tokenString)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("Expected status %d, got %d", http.StatusUnauthorized, w.Code)
		}
	})

	t.Run("Invalid JSON Response", func(t *testing.T) {
		serverInvalidJSON := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`invalid-json`))
		}))
		defer serverInvalidJSON.Close()

		os.Setenv("SUPABASE_URL", serverInvalidJSON.URL)

		token := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
			"sub":   uuid.New().String(),
			"email": "test@example.com",
			"exp":   time.Now().Add(time.Hour).Unix(),
		})
		token.Header["kid"] = kid

		tokenString, _ := token.SignedString(privateKey)

		r := gin.New()
		r.Use(SupabaseAuthMiddleware())
		r.GET("/test", func(c *gin.Context) {
			c.Status(http.StatusOK)
		})

		req, _ := http.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer "+tokenString)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("Expected status %d, got %d", http.StatusUnauthorized, w.Code)
		}
	})

	t.Run("Failed to connect URL", func(t *testing.T) {
		os.Setenv("SUPABASE_URL", "http://127.0.0.1:0")

		token := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
			"sub":   uuid.New().String(),
			"email": "test@example.com",
			"exp":   time.Now().Add(time.Hour).Unix(),
		})
		token.Header["kid"] = kid

		tokenString, _ := token.SignedString(privateKey)

		r := gin.New()
		r.Use(SupabaseAuthMiddleware())
		r.GET("/test", func(c *gin.Context) {
			c.Status(http.StatusOK)
		})

		req, _ := http.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer "+tokenString)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("Expected status %d, got %d", http.StatusUnauthorized, w.Code)
		}
	})

	t.Run("Invalid parse formats", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			jwks := map[string]interface{}{
				"keys": []map[string]interface{}{
					// Add an invalid key format to trigger err handling
					{
						"kty": "EC",
						"use": "sig",
						"crv": "P-256",
						"kid": "invalid-kid-x",
						"x":   123, // invalid type
						"y":   yB64,
						"alg": "ES256",
					},
					{
						"kty": "EC",
						"use": "sig",
						"crv": "P-256",
						"kid": "invalid-kid-y",
						"x":   xB64,
						"y":   123, // invalid type
						"alg": "ES256",
					},
					{
						"kty": "EC",
						"use": "sig",
						"crv": "P-256",
						"kid": "invalid-kid-decode-x",
						"x":   "invalid-base64",
						"y":   yB64,
						"alg": "ES256",
					},
					{
						"kty": "EC",
						"use": "sig",
						"crv": "P-256",
						"kid": "invalid-kid-decode-y",
						"x":   xB64,
						"y":   "invalid-base64",
						"alg": "ES256",
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(jwks)
		}))
		defer server.Close()
		os.Setenv("SUPABASE_URL", server.URL)

		tests := []string{"invalid-kid-x", "invalid-kid-y", "invalid-kid-decode-x", "invalid-kid-decode-y"}

		for _, invalidKid := range tests {
			token := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
				"sub":   uuid.New().String(),
				"email": "test@example.com",
				"exp":   time.Now().Add(time.Hour).Unix(),
			})
			token.Header["kid"] = invalidKid

			tokenString, _ := token.SignedString(privateKey)

			r := gin.New()
			r.Use(SupabaseAuthMiddleware())
			r.GET("/test", func(c *gin.Context) {
				c.Status(http.StatusOK)
			})

			req, _ := http.NewRequest("GET", "/test", nil)
			req.Header.Set("Authorization", "Bearer "+tokenString)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != http.StatusUnauthorized {
				t.Errorf("Expected status %d for %s, got %d", http.StatusUnauthorized, invalidKid, w.Code)
			}
		}
	})

	t.Run("JSON Format but no keys field", func(t *testing.T) {
		serverInvalidKey := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"other_field": []}`))
		}))
		defer serverInvalidKey.Close()

		os.Setenv("SUPABASE_URL", serverInvalidKey.URL)

		jwksCache.Range(func(key, value interface{}) bool {
			jwksCache.Delete(key)
			return true
		})

		token := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
			"sub":   uuid.New().String(),
			"email": "test@example.com",
			"exp":   time.Now().Add(time.Hour).Unix(),
		})
		token.Header["kid"] = kid

		tokenString, _ := token.SignedString(privateKey)

		r := gin.New()
		r.Use(SupabaseAuthMiddleware())
		r.GET("/test", func(c *gin.Context) {
			c.Status(http.StatusOK)
		})

		req, _ := http.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer "+tokenString)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("Expected status %d, got %d", http.StatusUnauthorized, w.Code)
		}
	})

	t.Run("Valid Token From Cache", func(t *testing.T) {
		os.Setenv("SUPABASE_URL", "http://fake.supabase.com")

		// preload
		jwksCache.Store(kid, &privateKey.PublicKey)

		token := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
			"sub":   uuid.New().String(),
			"email": "test@example.com",
			"exp":   time.Now().Add(time.Hour).Unix(),
		})
		token.Header["kid"] = kid

		tokenString, _ := token.SignedString(privateKey)

		r := gin.New()
		r.Use(SupabaseAuthMiddleware())
		r.GET("/test", func(c *gin.Context) {
			c.Status(http.StatusOK)
		})

		req, _ := http.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer "+tokenString)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
		}
	})

	t.Run("Key not found", func(t *testing.T) {
		serverInvalidKey := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"keys": [{"kid": "different-key"}]}`))
		}))
		defer serverInvalidKey.Close()

		os.Setenv("SUPABASE_URL", serverInvalidKey.URL)

		jwksCache.Range(func(key, value interface{}) bool {
			jwksCache.Delete(key)
			return true
		})

		token := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
			"sub":   uuid.New().String(),
			"email": "test@example.com",
			"exp":   time.Now().Add(time.Hour).Unix(),
		})
		token.Header["kid"] = kid

		tokenString, _ := token.SignedString(privateKey)

		r := gin.New()
		r.Use(SupabaseAuthMiddleware())
		r.GET("/test", func(c *gin.Context) {
			c.Status(http.StatusOK)
		})

		req, _ := http.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer "+tokenString)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("Expected status %d, got %d", http.StatusUnauthorized, w.Code)
		}
	})

	t.Run("HMAC Method Empty Secret", func(t *testing.T) {
		os.Unsetenv("SUPABASE_JWT_SECRET")

		token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"sub":   uuid.New().String(),
			"email": "test@example.com",
			"exp":   time.Now().Add(time.Hour).Unix(),
		})

		// Use empty secret because it will be parsed without issue before the failure logic is hit
		tokenString, _ := token.SignedString([]byte(""))

		r := gin.New()
		r.Use(SupabaseAuthMiddleware())
		r.GET("/test", func(c *gin.Context) {
			c.Status(http.StatusOK)
		})

		req, _ := http.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer "+tokenString)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusInternalServerError {
			t.Errorf("Expected status %d, got %d", http.StatusInternalServerError, w.Code)
		}
	})
}

func TestSupabaseAuthMiddleware_ECDSACoverageExtra6(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)

	secret := "super-secret-key"
	os.Setenv("SUPABASE_JWT_SECRET", secret)
	defer os.Unsetenv("SUPABASE_JWT_SECRET")

	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	kid := "test-key"

	// Clear sync.Map cache for tests to force fetch
	jwksCache.Range(func(key, value interface{}) bool {
		jwksCache.Delete(key)
		return true
	})

	t.Run("Failed to parse base64 x coord", func(t *testing.T) {
		serverInvalidKey := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"keys": [{"kid": "` + kid + `", "x": "invalid_base64!", "y": "valid_base64"}]}`))
		}))
		defer serverInvalidKey.Close()

		os.Setenv("SUPABASE_URL", serverInvalidKey.URL)

		token := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
			"sub":   uuid.New().String(),
			"email": "test@example.com",
			"exp":   time.Now().Add(time.Hour).Unix(),
		})
		token.Header["kid"] = kid

		tokenString, _ := token.SignedString(privateKey)

		r := gin.New()
		r.Use(SupabaseAuthMiddleware())
		r.GET("/test", func(c *gin.Context) {
			c.Status(http.StatusOK)
		})

		req, _ := http.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer "+tokenString)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("Expected status %d, got %d", http.StatusUnauthorized, w.Code)
		}
	})

	t.Run("Failed to parse base64 y coord", func(t *testing.T) {
		serverInvalidKey := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"keys": [{"kid": "` + kid + `", "x": "valid_base64", "y": "invalid_base64!"}]}`))
		}))
		defer serverInvalidKey.Close()

		os.Setenv("SUPABASE_URL", serverInvalidKey.URL)

		token := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
			"sub":   uuid.New().String(),
			"email": "test@example.com",
			"exp":   time.Now().Add(time.Hour).Unix(),
		})
		token.Header["kid"] = kid

		tokenString, _ := token.SignedString(privateKey)

		r := gin.New()
		r.Use(SupabaseAuthMiddleware())
		r.GET("/test", func(c *gin.Context) {
			c.Status(http.StatusOK)
		})

		req, _ := http.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer "+tokenString)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("Expected status %d, got %d", http.StatusUnauthorized, w.Code)
		}
	})

}

func TestSupabaseAuthMiddleware_MissingURLCoverage(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)
	os.Unsetenv("SUPABASE_URL")

	r := gin.New()
	r.Use(SupabaseAuthMiddleware())
	r.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req, _ := http.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status %d, got %d", http.StatusInternalServerError, w.Code)
	}
}

func TestSupabaseAuthMiddleware_MissingEmail(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)

	secret := "super-secret-key"
	os.Setenv("SUPABASE_JWT_SECRET", secret)
	defer os.Unsetenv("SUPABASE_JWT_SECRET")

	os.Setenv("SUPABASE_URL", "http://fake.supabase.com")
	defer os.Unsetenv("SUPABASE_URL")

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": uuid.New().String(),
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	tokenString, _ := token.SignedString([]byte(secret))

	r := gin.New()
	r.Use(SupabaseAuthMiddleware())
	r.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req, _ := http.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+tokenString)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status %d, got %d", http.StatusUnauthorized, w.Code)
	}
}
