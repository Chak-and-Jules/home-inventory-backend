package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/guides"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/i18n"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/logger"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func guideHTTPFixture(t *testing.T) (*guides.Service, *gin.Engine, uuid.UUID, uuid.UUID) {
	t.Helper()
	logger.InitLogger()
	gin.SetMode(gin.TestMode)
	db := openUnitTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.Language{}, &models.Profile{}, &models.Home{}, &models.UserHome{}, &models.GuideProgress{}, &models.GuideRequest{}))
	user, home := uuid.New(), uuid.New()
	require.NoError(t, db.Create(&models.Profile{ID: user, Email: uuid.NewString() + "@example.com"}).Error)
	require.NoError(t, db.Create(&models.Home{ID: home, Name: "Home"}).Error)
	require.NoError(t, db.Create(&models.UserHome{UserID: user, HomeID: home, Role: models.RoleOwner}).Error)
	s := guides.New(db)
	s.Now = func() time.Time { return time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC) }
	h := &GuideHandler{Service: s}
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("userID", user) })
	r.GET("/guides", h.List)
	r.POST("/guides/:id/actions", h.Act)
	return s, r, user, home
}

func guideRequest(r *gin.Engine, method, path, body string, home *uuid.UUID) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if home != nil {
		req.Header.Set("X-Home-Id", home.String())
	}
	r.ServeHTTP(w, req)
	return w
}

func TestGuideHTTPContract(t *testing.T) {
	_, r, _, home := guideHTTPFixture(t)
	w := guideRequest(r, "GET", "/guides?platform=mobile&client_version=1", "", &home)
	require.Equal(t, 200, w.Code, w.Body.String())
	var list []guides.View
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
	require.Len(t, list, 1)
	require.Equal(t, guides.Mobile, list[0].Platform)
	require.Contains(t, list[0].Variant.Steps, "inventory")
	require.NotContains(t, list[0].Variant.Steps, "stock")
	require.True(t, list[0].ShouldOffer)
	require.Equal(t, guides.NotStarted, list[0].State.Status)
	version := int64(0)
	c := guides.Command{RequestID: uuid.New(), Platform: guides.Web, ClientVersion: 1, Revision: 1, ExpectedVersion: &version, Action: "start", HomeID: &home}
	b, err := json.Marshal(c)
	require.NoError(t, err)
	w = guideRequest(r, "POST", "/guides/introduction/actions", string(b), nil)
	require.Equal(t, 200, w.Code, w.Body.String())
	var v guides.View
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &v))
	require.Equal(t, guides.InProgress, v.State.Status)
	require.Equal(t, &home, v.State.HomeID)
	require.EqualValues(t, 1, v.Version)
	// Same request ID/payload retries without incrementing version.
	w = guideRequest(r, "POST", "/guides/introduction/actions", string(b), nil)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &v))
	require.EqualValues(t, 1, v.Version)
	c.Action = "skip"
	c.HomeID = nil
	b, _ = json.Marshal(c)
	w = guideRequest(r, "POST", "/guides/introduction/actions", string(b), nil)
	require.Equal(t, 409, w.Code)
	require.Contains(t, w.Body.String(), "request_id_reused")
	c.Action = "complete"
	c.RequestID = uuid.New()
	version = 1
	b, _ = json.Marshal(c)
	w = guideRequest(r, "POST", "/guides/introduction/actions", string(b), nil)
	require.Equal(t, 409, w.Code)
	require.Contains(t, w.Body.String(), "invalid_transition")
	version = 0
	c.RequestID = uuid.New()
	c.Action = "skip"
	c.HomeID = nil
	b, _ = json.Marshal(c)
	w = guideRequest(r, "POST", "/guides/introduction/actions", string(b), nil)
	require.Equal(t, 409, w.Code)
	require.Contains(t, w.Body.String(), "state_conflict")
}

func TestGuideHTTPRejectsMalformedAndIneligibleRequests(t *testing.T) {
	_, r, _, home := guideHTTPFixture(t)
	for _, query := range []string{"", "?platform=web", "?platform=android&client_version=1", "?platform=web&client_version=0", "?platform=web&client_version=1.5"} {
		w := guideRequest(r, "GET", "/guides"+query, "", nil)
		require.Equal(t, 400, w.Code, w.Body.String())
	}
	for _, body := range []string{"", "{}", "null", "{", `{"user_id":"injected"}`, strings.Repeat(" ", 4097) + "{}", `{} {}`} {
		w := guideRequest(r, "POST", "/guides/introduction/actions", body, nil)
		require.Equal(t, 400, w.Code, w.Body.String())
	}
	c := guides.Command{RequestID: uuid.New(), Platform: guides.Web, ClientVersion: 1, Revision: 1, Action: "start"}
	zero := int64(0)
	c.ExpectedVersion = &zero
	b, _ := json.Marshal(c)
	w := guideRequest(r, "POST", "/guides/introduction/actions", string(b), nil)
	require.Equal(t, 403, w.Code)
	c.HomeID = &home
	b, _ = json.Marshal(c)
	w = guideRequest(r, "POST", "/guides/missing/actions", string(b), nil)
	require.Equal(t, 404, w.Code)
	req := httptest.NewRequest("GET", "/guides?platform=web&client_version=1", nil)
	req.Header.Set("X-Home-Id", "invalid")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, 400, w.Code)
}

func TestGuideHTTPUsesAuthenticatedUserAndLocalizedErrors(t *testing.T) {
	s, r, user, _ := guideHTTPFixture(t)
	language := models.Language{ID: uuid.New(), Name: "Türkçe"}
	require.NoError(t, s.DB.Create(&language).Error)
	require.NoError(t, s.DB.Model(&models.Profile{}).Where("id=?", user).Update("language_id", language.ID).Error)
	i18n.InvalidateUserLanguageCache(user)
	w := guideRequest(r, "GET", "/guides?platform=unknown&client_version=1", "", nil)
	require.Equal(t, 400, w.Code)
	require.Contains(t, w.Body.String(), "Geçersiz rehber isteği")
	// Without an authenticated context, the handler cannot read/write state.
	h := &GuideHandler{Service: s}
	anonymous := gin.New()
	anonymous.GET("/guides", h.List)
	anonymous.POST("/guides/:id/actions", h.Act)
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		path := "/guides"
		if method == http.MethodPost {
			path += "/introduction/actions"
		}
		w = guideRequest(anonymous, method, path, "{}", nil)
		require.Equal(t, 401, w.Code)
	}
	// A missing storage table is a visible failure, never an empty successful list.
	require.NoError(t, s.DB.Migrator().DropTable(&models.GuideProgress{}))
	w = guideRequest(r, "GET", "/guides?platform=web&client_version=1", "", nil)
	require.Equal(t, 500, w.Code)
	require.Contains(t, w.Body.String(), "Rehber durumu işlenemedi")
}

func TestGuideHTTPOversizedBodyCannotWriteState(t *testing.T) {
	s, r, user, _ := guideHTTPFixture(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/guides/introduction/actions", bytes.NewReader(bytes.Repeat([]byte("x"), 5000)))
	r.ServeHTTP(w, req)
	require.Equal(t, 400, w.Code)
	var count int64
	require.NoError(t, s.DB.Model(&models.GuideRequest{}).Where("user_id=?", user).Count(&count).Error)
	require.Zero(t, count)
}
