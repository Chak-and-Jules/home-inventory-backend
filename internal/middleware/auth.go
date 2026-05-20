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

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

var supabaseUrl string
var jwksURL string
var jwtSecret string

func SupabaseAuthMiddleware() gin.HandlerFunc {
	supabaseUrl = os.Getenv("SUPABASE_URL")
	if supabaseUrl == "" {
		panic("SUPABASE_URL environment variable is not set")
	}

	jwksURL = supabaseUrl + "/auth/v1/.well-known/jwks.json"
	jwtSecret = os.Getenv("SUPABASE_JWT_SECRET")

	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authorization header is missing"})
			return
		}

		if !strings.HasPrefix(authHeader, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid authorization header format"})
			return
		}

		tokenString := authHeader[7:]
		if strings.Contains(tokenString, " ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid authorization header format"})
			return
		}

		if jwtSecret == "" {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "JWT secret is not configured"})
			return
		}

		claims, err := FetchAndVerifyToken(tokenString)

		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
			return
		}

		if claims == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Failed to parse token claims"})
			return
		}

		sub, ok := (*claims)["sub"].(string)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Subject missing from token claims"})
			return
		}

		userID, err := uuid.Parse(sub)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid user ID in token"})
			return
		}

		email, ok := (*claims)["email"].(string)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Email missing from token claims"})
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
			return []byte(jwtSecret), nil
		}

		// Fetch JWKS from Supabase for ECDSA verification
		resp, err := http.Get(jwksURL)
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

		kid, ok := token.Header["kid"].(string)
		if !ok {
			return nil, fmt.Errorf("kid not found in token header")
		}

		for _, key := range keys {
			keyMap := key.(map[string]interface{})
			if keyMap["kid"] == kid {
				pubKey, err := parseECDSAPublicKey(keyMap)
				if err != nil {
					return nil, err
				}
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
