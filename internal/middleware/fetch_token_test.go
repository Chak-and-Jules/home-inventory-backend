package middleware

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

// Reset global variables to ensure test isolation
func resetGlobals() {
	supabaseUrl = ""
	jwksURL = ""
	jwtSecret = ""
	jwtSecretBytes = nil
	jwksCache.Range(func(key, value interface{}) bool {
		jwksCache.Delete(key)
		return true
	})
}

// Generate an ECDSA key for testing
func generateECDSAPrivateKey(t *testing.T) *ecdsa.PrivateKey {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate ECDSA key: %v", err)
	}
	return privateKey
}

func getECDSABase64Coords(pub *ecdsa.PublicKey) (string, string) {
	xBytes := pub.X.Bytes()
	yBytes := pub.Y.Bytes()
	xB64 := base64.RawURLEncoding.EncodeToString(xBytes)
	yB64 := base64.RawURLEncoding.EncodeToString(yBytes)
	return xB64, yB64
}

func TestFetchAndVerifyToken_HMAC(t *testing.T) {
	resetGlobals()

	t.Run("Valid HMAC token", func(t *testing.T) {
		jwtSecret = "supersecret"
		jwtSecretBytes = []byte(jwtSecret)

		token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"sub": uuid.New().String(),
			"exp": time.Now().Add(time.Hour).Unix(),
		})
		tokenString, _ := token.SignedString(jwtSecretBytes)

		claims, err := FetchAndVerifyToken(tokenString)
		assert.NoError(t, err)
		assert.NotNil(t, claims)
	})

	t.Run("HMAC token missing secret", func(t *testing.T) {
		jwtSecret = "" // Empty secret

		token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"sub": uuid.New().String(),
			"exp": time.Now().Add(time.Hour).Unix(),
		})
		tokenString, _ := token.SignedString([]byte("some-secret"))

		claims, err := FetchAndVerifyToken(tokenString)
		assert.Error(t, err)
		assert.Nil(t, claims)
		assert.Contains(t, err.Error(), "JWT secret is not configured")
	})
}

func TestFetchAndVerifyToken_ECDSA(t *testing.T) {
	privateKey := generateECDSAPrivateKey(t)
	kid := "test-kid"

	t.Run("Valid token from JWKS URL", func(t *testing.T) {
		resetGlobals()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			xB64, yB64 := getECDSABase64Coords(&privateKey.PublicKey)
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"keys": [{"kty": "EC", "use": "sig", "crv": "P-256", "kid": "` + kid + `", "x": "` + xB64 + `", "y": "` + yB64 + `", "alg": "ES256"}]}`))
		}))
		defer server.Close()
		jwksURL = server.URL

		token := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
			"sub": uuid.New().String(),
			"exp": time.Now().Add(time.Hour).Unix(),
		})
		token.Header["kid"] = kid
		tokenString, _ := token.SignedString(privateKey)

		claims, err := FetchAndVerifyToken(tokenString)
		assert.NoError(t, err)
		assert.NotNil(t, claims)
	})

	t.Run("Valid token from Cache", func(t *testing.T) {
		resetGlobals()
		jwksCache.Store(kid, &privateKey.PublicKey)

		token := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
			"sub": uuid.New().String(),
			"exp": time.Now().Add(time.Hour).Unix(),
		})
		token.Header["kid"] = kid
		tokenString, _ := token.SignedString(privateKey)

		claims, err := FetchAndVerifyToken(tokenString)
		assert.NoError(t, err)
		assert.NotNil(t, claims)
	})

	t.Run("Missing kid in header", func(t *testing.T) {
		resetGlobals()

		token := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
			"sub": uuid.New().String(),
			"exp": time.Now().Add(time.Hour).Unix(),
		})
		tokenString, _ := token.SignedString(privateKey)

		claims, err := FetchAndVerifyToken(tokenString)
		assert.Error(t, err)
		assert.Nil(t, claims)
		assert.Contains(t, err.Error(), "kid not found")
	})

	t.Run("Key not found in JWKS", func(t *testing.T) {
		resetGlobals()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"keys": [{"kid": "different-kid"}]}`))
		}))
		defer server.Close()
		jwksURL = server.URL

		token := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
			"sub": uuid.New().String(),
			"exp": time.Now().Add(time.Hour).Unix(),
		})
		token.Header["kid"] = kid
		tokenString, _ := token.SignedString(privateKey)

		claims, err := FetchAndVerifyToken(tokenString)
		assert.Error(t, err)
		assert.Nil(t, claims)
		assert.Contains(t, err.Error(), "key not found")
	})

	t.Run("JWKS fetch error", func(t *testing.T) {
		resetGlobals()
		jwksURL = "http://invalid-url-that-does-not-exist"

		token := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
			"sub": uuid.New().String(),
			"exp": time.Now().Add(time.Hour).Unix(),
		})
		token.Header["kid"] = kid
		tokenString, _ := token.SignedString(privateKey)

		claims, err := FetchAndVerifyToken(tokenString)
		assert.Error(t, err)
		assert.Nil(t, claims)
		assert.Contains(t, err.Error(), "could not fetch JWKS")
	})
}

func TestFetchAndVerifyToken_InvalidTokens(t *testing.T) {
	resetGlobals()

	t.Run("Malformed token string", func(t *testing.T) {
		claims, err := FetchAndVerifyToken("not-a-valid-token")
		assert.Error(t, err)
		assert.Nil(t, claims)
	})

	t.Run("Invalid signature", func(t *testing.T) {
		jwtSecret = "supersecret"
		jwtSecretBytes = []byte(jwtSecret)

		token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"sub": uuid.New().String(),
			"exp": time.Now().Add(time.Hour).Unix(),
		})
		tokenString, _ := token.SignedString([]byte("wrong-secret"))

		claims, err := FetchAndVerifyToken(tokenString)
		assert.Error(t, err)
		assert.Nil(t, claims)
	})

	t.Run("Valid token but no claims", func(t *testing.T) {
		// To hit `if claims, ok := token.Claims.(*jwt.MapClaims); ok && token.Valid` failure
		jwtSecret = "supersecret"
		jwtSecretBytes = []byte(jwtSecret)

		// Use an expired token to make token.Valid evaluate to false during claims verification
		expiredToken := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"sub": uuid.New().String(),
			"exp": time.Now().Add(-time.Hour).Unix(), // Expired
		})
		expiredTokenString, _ := expiredToken.SignedString(jwtSecretBytes)

		claims, err := FetchAndVerifyToken(expiredTokenString)
		assert.Error(t, err)
		assert.Nil(t, claims)
	})
}
