package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"firebase.google.com/go/v4/auth"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/logger"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/middleware"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type firebaseLifecycleAuth struct {
	profileAuthStub
	token *auth.Token
}

func (s *firebaseLifecycleAuth) VerifyIDTokenAndCheckRevoked(context.Context, string) (*auth.Token, error) {
	return s.token, nil
}

func TestFirebaseProfileLifecycle(t *testing.T) {
	logger.InitLogger()
	gin.SetMode(gin.TestMode)
	db := openUnitTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.Profile{}, &models.Home{}, &models.UserHome{}))
	uid, email := "firebase-user-not-a-uuid", "user@example.com"
	client := &firebaseLifecycleAuth{token: &auth.Token{UID: uid, Claims: map[string]interface{}{"email": email}}}
	handler := &ProfileHandler{DB: db, Auth: client}
	router := gin.New()
	router.Use(middleware.FirebaseAuthMiddleware(db, client))
	router.POST("/sync", handler.SyncProfile)
	router.POST("/delete", handler.DeleteAccount)
	request := func(path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer verified-by-fixture")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	var internalID uuid.UUID
	for range 2 {
		w := request("/sync", `{"profile":{"id":"firebase-user-not-a-uuid","email":"user@example.com"}}`)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var response models.Profile
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		require.NotEqual(t, uuid.Nil, response.ID)
		require.NotContains(t, w.Body.String(), uid)
		if internalID == uuid.Nil {
			internalID = response.ID
		}
		require.Equal(t, internalID, response.ID)
	}
	var profile models.Profile
	require.NoError(t, db.First(&profile, "id = ?", internalID).Error)
	require.Equal(t, uid, *profile.FirebaseUID)
	var membership models.UserHome
	require.NoError(t, db.First(&membership, "user_id = ?", internalID).Error)
	require.Equal(t, models.RoleOwner, membership.Role)
	require.True(t, membership.IsDefault)
	var count int64
	require.NoError(t, db.Model(&models.Home{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
	// A supplied identity cannot replace the one established by the token.
	w := request("/sync", `{"profile":{"id":"another-firebase-user","email":"user@example.com"}}`)
	require.Equal(t, http.StatusForbidden, w.Code)
	deleteBody := `{"user_id":"` + internalID.String() + `","email":"user@example.com"}`
	client.err = errors.New("Firebase deletion failed")
	w = request("/delete", deleteBody)
	require.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
	require.NoError(t, db.First(&profile, "id = ?", internalID).Error)
	require.NoError(t, db.First(&membership, "user_id = ?", internalID).Error)
	require.Equal(t, uid, client.deletedUID)
	client.err = nil
	w = request("/delete", deleteBody)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	for _, model := range []interface{}{&models.Profile{}, &models.UserHome{}, &models.Home{}} {
		require.NoError(t, db.Model(model).Count(&count).Error)
		require.Zero(t, count)
	}
}
