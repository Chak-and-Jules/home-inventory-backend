package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/authentication"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/i18n"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func FirebaseAuthMiddleware(db *gorm.DB, client authentication.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		fields := strings.Fields(c.GetHeader("Authorization"))
		if len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": i18n.TranslateDB(nil, c, "Invalid authorization header format")})
			return
		}
		if client == nil || db == nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(nil, c, "Authentication is not configured")})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		token, err := client.VerifyIDTokenAndCheckRevoked(ctx, fields[1])
		cancel()
		if err != nil || token == nil || token.UID == "" || utf8.RuneCountInString(token.UID) > 128 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": i18n.TranslateDB(nil, c, "Invalid token")})
			return
		}
		email, _ := token.Claims["email"].(string)
		address, err := mail.ParseAddress(email)
		if err != nil || address.Address != email || len(email) > 254 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": i18n.TranslateDB(nil, c, "Email missing from token claims")})
			return
		}

		profile, err := resolveFirebaseProfile(db.WithContext(c.Request.Context()), token.UID, email)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(nil, c, "Failed to sync profile")})
			return
		}
		c.Set("userID", profile.ID)
		c.Set("firebaseUID", token.UID)
		c.Set("email", email)
		c.Next()
	}
}

func resolveFirebaseProfile(db *gorm.DB, uid, email string) (models.Profile, error) {
	var profile models.Profile
	err := db.Where("firebase_uid = ?", uid).First(&profile).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		profile = models.Profile{ID: uuid.New(), Email: email, FirebaseUID: &uid}
		// Concurrent first requests must resolve to the same internal UUID.
		// Email conflicts deliberately fail: email is never an account-linking key.
		if err = db.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "firebase_uid"}}, DoNothing: true,
		}).Create(&profile).Error; err != nil {
			return models.Profile{}, err
		}
		// Clear the candidate ID so GORM cannot add it as an implicit condition.
		profile = models.Profile{}
		err = db.Where("firebase_uid = ?", uid).First(&profile).Error
	}
	if err != nil {
		return models.Profile{}, err
	}
	if profile.Email != email {
		// The UID owns the profile, including after an email change in Firebase.
		if err := db.Model(&profile).Update("email", email).Error; err != nil {
			return models.Profile{}, err
		}
		profile.Email = email
	}
	return profile, nil
}
