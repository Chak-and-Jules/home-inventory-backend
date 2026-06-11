package middleware_test

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

	"github.com/Chak-and-Jules/home-inventory-backend/internal/middleware"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func BenchmarkSupabaseAuthMiddleware_ECDSAToken(b *testing.B) {
	gin.SetMode(gin.ReleaseMode)
	secret := "super-secret-key"
	os.Setenv("SUPABASE_JWT_SECRET", secret)
	defer os.Unsetenv("SUPABASE_JWT_SECRET")

	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		b.Fatal(err)
	}

	xB64 := base64.RawURLEncoding.EncodeToString(privateKey.PublicKey.X.Bytes())
	yB64 := base64.RawURLEncoding.EncodeToString(privateKey.PublicKey.Y.Bytes())
	kid := uuid.New().String()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/auth/v1/.well-known/jwks.json" {
			http.NotFound(w, r)
			return
		}
		jwks := map[string]interface{}{
			"keys": []map[string]interface{}{
				{
					"kty": "EC",
					"use": "sig",
					"crv": "P-256",
					"kid": kid,
					"x":   xB64,
					"y":   yB64,
					"alg": "ES256",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(jwks)
	}))
	defer server.Close()

	os.Setenv("SUPABASE_URL", server.URL)
	defer os.Unsetenv("SUPABASE_URL")

	r := gin.New()
	r.Use(middleware.SupabaseAuthMiddleware())
	r.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	userID := uuid.New().String()
	token := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
		"sub":   userID,
		"email": "test@example.com",
		"exp":   time.Now().Add(time.Hour).Unix(),
	})
	token.Header["kid"] = kid

	tokenString, err := token.SignedString(privateKey)
	if err != nil {
		b.Fatal(err)
	}

	req, _ := http.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+tokenString)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			b.Fatalf("Expected status %d, got %d", http.StatusOK, w.Code)
		}
	}
}
