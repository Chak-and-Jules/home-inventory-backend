package middleware

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/i18n"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

var supabaseUrl string
var jwksURL string
var jwtSecret string
var jwtSecretBytes []byte // ⚡ Bolt: Cached byte slice to avoid per-request memory allocation during HMAC verification

var (
	jwksCache sync.Map // ⚡ Bolt: Replaced map+RWMutex with sync.Map for lock-free, concurrent reads of ECDSA public keys
)

func SupabaseAuthMiddleware() gin.HandlerFunc {
	supabaseUrl = os.Getenv("SUPABASE_URL")
	if supabaseUrl == "" {
		// Don't panic in library code or tests; return a middleware that
		// responds with 500 so callers (tests/servers) can handle it.
		return func(c *gin.Context) {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(nil, c, "SUPABASE_URL is not configured")})
		}
	}

	jwksURL = supabaseUrl + "/auth/v1/.well-known/jwks.json"
	jwtSecret = os.Getenv("SUPABASE_JWT_SECRET")
	jwtSecretBytes = []byte(jwtSecret) // Cache byte slice

	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": i18n.TranslateDB(nil, c, "Authorization header is missing")})
			return
		}

		if !strings.HasPrefix(authHeader, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": i18n.TranslateDB(nil, c, "Invalid authorization header format")})
			return
		}

		tokenString := authHeader[7:]
		// ⚡ Bolt: Use IndexByte instead of Contains string since we're just checking for a space character
		if strings.IndexByte(tokenString, ' ') != -1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": i18n.TranslateDB(nil, c, "Invalid authorization header format")})
			return
		}

		if jwtSecret == "" {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(nil, c, "JWT secret is not configured")})
			return
		}

		claims, err := FetchAndVerifyToken(tokenString)

		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": i18n.TranslateDB(nil, c, "Invalid token")})
			return
		}

		if claims == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": i18n.TranslateDB(nil, c, "Failed to parse token claims")})
			return
		}

		sub, ok := (*claims)["sub"].(string)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": i18n.TranslateDB(nil, c, "Subject missing from token claims")})
			return
		}

		userID, err := uuid.Parse(sub)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": i18n.TranslateDB(nil, c, "Invalid user ID in token")})
			return
		}

		email, ok := (*claims)["email"].(string)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": i18n.TranslateDB(nil, c, "Email missing from token claims")})
			return
		}

		// Set the user ID in the context so subsequent handlers can access it
		c.Set("userID", userID)
		c.Set("email", email)
		c.Next()
	}
}

// FetchAndVerifyToken validates a Supabase token using the ECDSA public key
func FetchAndVerifyToken(tokenString string) (*jwt.MapClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &jwt.MapClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); ok {
			if jwtSecret == "" {
				return nil, fmt.Errorf("JWT secret is not configured")
			}
			return jwtSecretBytes, nil // ⚡ Bolt: Return cached bytes to avoid allocation
		}

		kid, ok := token.Header["kid"].(string)
		if !ok {
			return nil, fmt.Errorf("kid not found in token header")
		}

		// Check cache first
		if pubKeyInterface, exists := jwksCache.Load(kid); exists {
			return pubKeyInterface.(*ecdsa.PublicKey), nil
		}

		// Fetch JWKS from Supabase for ECDSA verification
		client := &http.Client{
			Timeout: 10 * time.Second,
		}
		resp, err := client.Get(jwksURL)
		if err != nil {
			return nil, fmt.Errorf("could not fetch JWKS: %v", err)
		}
		defer resp.Body.Close()

		var jwks map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
			return nil, fmt.Errorf("could not parse JWKS: %v", err)
		}

		keys, ok := jwks["keys"].([]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid JWKS format")
		}

		for _, key := range keys {
			keyMap := key.(map[string]interface{})
			if keyMap["kid"] == kid {
				pubKey, err := parseECDSAPublicKey(keyMap)
				if err != nil {
					return nil, err
				}

				// Update cache
				jwksCache.Store(kid, pubKey)

				return pubKey, nil
			}
		}

		return nil, fmt.Errorf("key not found")
	})

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*jwt.MapClaims); ok && token.Valid {
		return claims, nil
	}

	return nil, fmt.Errorf("invalid token")
}

// parseECDSAPublicKey extracts ECDSA public key from JWKS key data
func parseECDSAPublicKey(keyMap map[string]interface{}) (*ecdsa.PublicKey, error) {
	x, ok := keyMap["x"].(string)
	if !ok {
		return nil, fmt.Errorf("x coordinate not found in JWKS key")
	}

	y, ok := keyMap["y"].(string)
	if !ok {
		return nil, fmt.Errorf("y coordinate not found in JWKS key")
	}

	xBytes, err := base64.RawURLEncoding.DecodeString(x)
	if err != nil {
		return nil, fmt.Errorf("failed to decode x: %v", err)
	}

	yBytes, err := base64.RawURLEncoding.DecodeString(y)
	if err != nil {
		return nil, fmt.Errorf("failed to decode y: %v", err)
	}

	pubKey := &ecdsa.PublicKey{
		Curve: elliptic.P256(),
		X:     new(big.Int).SetBytes(xBytes),
		Y:     new(big.Int).SetBytes(yBytes),
	}

	return pubKey, nil
}
