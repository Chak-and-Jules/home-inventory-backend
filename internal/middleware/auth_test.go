package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"firebase.google.com/go/v4/auth"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type authStub struct {
	token *auth.Token
	err   error
	calls int
}

func (s *authStub) VerifyIDTokenAndCheckRevoked(ctx context.Context, raw string) (*auth.Token, error) {
	s.calls++
	return s.token, s.err
}
func (s *authStub) DeleteUser(context.Context, string) error { return nil }

func authTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	config := &gorm.Config{TranslateError: true, Logger: logger.Default.LogMode(logger.Silent)}
	var db *gorm.DB
	var err error
	if dsn := os.Getenv("AUTH_TEST_POSTGRES_DSN"); dsn != "" {
		admin, err := gorm.Open(postgres.Open(dsn), config)
		require.NoError(t, err)
		conn, err := admin.DB()
		require.NoError(t, err)
		t.Cleanup(func() { conn.Close() })
		schema := "auth_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		require.NoError(t, admin.Exec("CREATE SCHEMA "+schema).Error)
		t.Cleanup(func() { require.NoError(t, admin.Exec("DROP SCHEMA "+schema+" CASCADE").Error) })
		db, err = gorm.Open(postgres.Open(dsn+" search_path="+schema), config)
		require.NoError(t, err)
	} else {
		db, err = gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), config)
		require.NoError(t, err)
	}
	conn, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })
	require.NoError(t, db.AutoMigrate(&models.Profile{}))
	return db
}

func TestFirebaseAuthMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := authTestDB(t)
	uid := "non-uuid-firebase-uid"
	email := "user@example.com"
	stub := &authStub{token: &auth.Token{UID: uid, Claims: map[string]interface{}{"email": email}}}
	router := gin.New()
	router.Use(FirebaseAuthMiddleware(db, stub))
	var resolved uuid.UUID
	router.GET("/test", func(c *gin.Context) {
		resolved = c.MustGet("userID").(uuid.UUID)
		require.Equal(t, uid, c.GetString("firebaseUID"))
		require.Equal(t, email, c.GetString("email"))
		c.Status(http.StatusOK)
	})
	request := func(header string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", header)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	for _, header := range []string{"", "Basic abc", "Bearer", "Bearer one two"} {
		require.Equal(t, http.StatusUnauthorized, request(header).Code)
	}
	require.Zero(t, stub.calls)
	stub.err = errors.New("bad signature / revoked token")
	require.Equal(t, http.StatusUnauthorized, request("Bearer invalid").Code)
	stub.err = nil
	for _, token := range []*auth.Token{nil, {UID: ""}, {UID: strings.Repeat("x", 129)}, {UID: uid}, {UID: uid, Claims: map[string]interface{}{"email": "invalid"}}} {
		stub.token = token
		require.Equal(t, http.StatusUnauthorized, request("Bearer invalid").Code)
	}
	var count int64
	require.NoError(t, db.Model(&models.Profile{}).Count(&count).Error)
	require.Zero(t, count)
	stub.token = &auth.Token{UID: uid, Claims: map[string]interface{}{"email": email}}
	require.Equal(t, http.StatusOK, request("Bearer verified").Code)
	require.NotEqual(t, uuid.Nil, resolved)
	firstID := resolved
	require.Equal(t, http.StatusOK, request("bearer verified").Code)
	require.Equal(t, firstID, resolved)
	require.NoError(t, db.Model(&models.Profile{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
	var profile models.Profile
	require.NoError(t, db.First(&profile, "id = ?", firstID).Error)
	require.Equal(t, uid, *profile.FirebaseUID)
	require.False(t, profile.IsAdmin)
	// Firebase email changes retain the same SQL identity.
	email = "changed@example.com"
	stub.token.Claims["email"] = email
	require.Equal(t, http.StatusOK, request("Bearer verified").Code)
	require.Equal(t, firstID, resolved)
	require.NoError(t, db.First(&profile, "id = ?", firstID).Error)
	require.Equal(t, email, profile.Email)
}

func TestFirebaseProfileNeverLinksByEmail(t *testing.T) {
	db := authTestDB(t)
	for _, linked := range []bool{false, true} {
		email := uuid.NewString() + "@example.com"
		originalUID := "original-" + uuid.NewString()
		profile := models.Profile{ID: uuid.New(), Email: email, IsAdmin: true}
		if linked {
			profile.FirebaseUID = &originalUID
		}
		require.NoError(t, db.Create(&profile).Error)
		_, err := resolveFirebaseProfile(db, "attacker-"+uuid.NewString(), email)
		require.Error(t, err)
		var unchanged models.Profile
		require.NoError(t, db.First(&unchanged, "id = ?", profile.ID).Error)
		require.Equal(t, profile.FirebaseUID, unchanged.FirebaseUID)
		require.True(t, unchanged.IsAdmin)
	}
}

func TestFirebaseExistingProfileKeepsInternalIdentity(t *testing.T) {
	db := authTestDB(t)
	uid := "existing-firebase-uid"
	theme := "dark"
	profile := models.Profile{ID: uuid.New(), Email: "old@example.com", FirebaseUID: &uid, IsAdmin: true, WebTheme: &theme}
	require.NoError(t, db.Create(&profile).Error)
	resolved, err := resolveFirebaseProfile(db, uid, "new@example.com")
	require.NoError(t, err)
	require.Equal(t, profile.ID, resolved.ID)
	require.True(t, resolved.IsAdmin)
	require.Equal(t, &theme, resolved.WebTheme)
	require.Equal(t, "new@example.com", resolved.Email)
}

func TestFirebaseProfileConcurrentFirstRequests(t *testing.T) {
	if os.Getenv("AUTH_TEST_POSTGRES_DSN") == "" {
		t.Skip("requires disposable PostgreSQL")
	}
	db := authTestDB(t)
	var wg sync.WaitGroup
	start := make(chan struct{})
	ids := make(chan uuid.UUID, 8)
	errs := make(chan error, 8)
	for range 8 {
		wg.Go(func() {
			<-start
			profile, err := resolveFirebaseProfile(db, "concurrent-firebase-uid", "same@example.com")
			ids <- profile.ID
			errs <- err
		})
	}
	close(start)
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	var first uuid.UUID
	for id := range ids {
		if first == uuid.Nil {
			first = id
		}
		require.Equal(t, first, id)
	}
	var count int64
	require.NoError(t, db.Model(&models.Profile{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestFirebaseAuthMissingConfiguration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, missing := range []string{"client", "database"} {
		t.Run(missing, func(t *testing.T) {
			db := authTestDB(t)
			client := &authStub{}
			if missing == "client" {
				client = nil
			} else {
				db = nil
			}
			router := gin.New()
			if client == nil {
				router.Use(FirebaseAuthMiddleware(db, nil))
			} else {
				router.Use(FirebaseAuthMiddleware(db, client))
			}
			router.GET("/test", func(c *gin.Context) { t.Error("misconfigured authentication reached the handler") })
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.Header.Set("Authorization", "Bearer token")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			require.Equal(t, http.StatusInternalServerError, w.Code)
			require.JSONEq(t, `{"error":"Authentication is not configured"}`, w.Body.String())
			if client != nil {
				require.Zero(t, client.calls)
			}
		})
	}
}

func TestFirebaseAuthDatabaseFailures(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, stage := range []string{"lookup", "insert", "reread", "email update"} {
		t.Run(stage, func(t *testing.T) {
			conn, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { conn.Close() })
			db, err := gorm.Open(postgres.New(postgres.Config{Conn: conn}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
			require.NoError(t, err)
			uid, email := "firebase-user", "current@example.com"
			query := `SELECT .* FROM "profiles" WHERE firebase_uid = \$1`
			failure := errors.New("private database failure detail")
			switch stage {
			case "lookup":
				mock.ExpectQuery(query).WithArgs(uid, 1).WillReturnError(failure)
			case "insert", "reread":
				mock.ExpectQuery(query).WithArgs(uid, 1).WillReturnRows(sqlmock.NewRows([]string{"id"}))
				mock.ExpectBegin()
				insert := mock.ExpectExec(`INSERT INTO "profiles".*ON CONFLICT \("firebase_uid"\) DO NOTHING`)
				if stage == "insert" {
					insert.WillReturnError(failure)
					mock.ExpectRollback()
				} else {
					insert.WillReturnResult(sqlmock.NewResult(0, 1))
					mock.ExpectCommit()
					mock.ExpectQuery(query).WithArgs(uid, 1).WillReturnError(failure)
				}
			case "email update":
				mock.ExpectQuery(query).WithArgs(uid, 1).WillReturnRows(sqlmock.NewRows([]string{"id", "email", "firebase_uid"}).AddRow(uuid.New(), "old@example.com", uid))
				mock.ExpectBegin()
				mock.ExpectExec(`UPDATE "profiles" SET "email"`).WillReturnError(failure)
				mock.ExpectRollback()
			}
			client := &authStub{token: &auth.Token{UID: uid, Claims: map[string]interface{}{"email": email}}}
			router := gin.New()
			router.Use(FirebaseAuthMiddleware(db, client))
			router.GET("/test", func(c *gin.Context) { t.Error("database failure reached the handler") })
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.Header.Set("Authorization", "Bearer verified")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			require.Equal(t, http.StatusInternalServerError, w.Code)
			require.JSONEq(t, `{"error":"Failed to sync profile"}`, w.Body.String())
			require.Equal(t, 1, client.calls)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestFirebaseEmailChangeCannotClaimAnotherProfile(t *testing.T) {
	db := authTestDB(t)
	uid, otherUID := "first-firebase-user", "second-firebase-user"
	first := models.Profile{ID: uuid.New(), Email: "first@example.com", FirebaseUID: &uid}
	second := models.Profile{ID: uuid.New(), Email: "second@example.com", FirebaseUID: &otherUID, IsAdmin: true}
	require.NoError(t, db.Create(&first).Error)
	require.NoError(t, db.Create(&second).Error)
	_, err := resolveFirebaseProfile(db, uid, second.Email)
	require.Error(t, err)
	for _, original := range []models.Profile{first, second} {
		var stored models.Profile
		require.NoError(t, db.First(&stored, "id = ?", original.ID).Error)
		require.Equal(t, original.Email, stored.Email)
		require.Equal(t, original.FirebaseUID, stored.FirebaseUID)
		require.Equal(t, original.IsAdmin, stored.IsAdmin)
	}
}
