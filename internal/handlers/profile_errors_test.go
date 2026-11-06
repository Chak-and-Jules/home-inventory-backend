package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/auth"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/i18n"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/logger"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlog "gorm.io/gorm/logger"
)

func TestProfileHandlersRequireIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := &ProfileHandler{}
	for name, action := range map[string]func(*gin.Context){
		"sync": handler.SyncProfile, "get": handler.GetProfile,
		"update": handler.UpdateProfile, "delete": handler.DeleteAccount,
	} {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/profile", nil)
			action(c)
			require.Equal(t, http.StatusUnauthorized, w.Code)
		})
	}
	for name, action := range map[string]func(*gin.Context){"sync": handler.SyncProfile, "delete": handler.DeleteAccount} {
		t.Run(name+" missing email", func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/profile", nil)
			c.Set("userID", uuid.New())
			action(c)
			require.Equal(t, http.StatusUnauthorized, w.Code)
		})
	}
}

func TestDeleteAccountIdentityGuards(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, scenario := range []string{"missing profile", "missing Firebase context", "unlinked profile", "different Firebase UID", "missing auth client"} {
		t.Run(scenario, func(t *testing.T) {
			db := openUnitTestDB(t)
			require.NoError(t, db.AutoMigrate(&models.Profile{}))
			uid, email := "firebase-identity", "user@example.com"
			profile := models.Profile{ID: uuid.New(), Email: email, FirebaseUID: &uid}
			if scenario == "unlinked profile" {
				profile.FirebaseUID = nil
			}
			if scenario == "different Firebase UID" {
				other := "other-firebase-identity"
				profile.FirebaseUID = &other
			}
			if scenario != "missing profile" {
				require.NoError(t, db.Create(&profile).Error)
			}
			client := &profileAuthStub{}
			handler := &ProfileHandler{DB: db, Auth: client}
			if scenario == "missing auth client" {
				handler.Auth = nil
			}
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/delete", strings.NewReader(`{"user_id":"`+profile.ID.String()+`","email":"user@example.com"}`))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Set("userID", profile.ID)
			c.Set("email", email)
			if scenario != "missing Firebase context" {
				c.Set("firebaseUID", uid)
			}
			handler.DeleteAccount(c)
			expected := http.StatusForbidden
			if scenario == "missing profile" {
				expected = http.StatusNotFound
			}
			if scenario == "missing auth client" {
				expected = http.StatusInternalServerError
			}
			require.Equal(t, expected, w.Code, w.Body.String())
			require.Empty(t, client.deletedUID, "identity guards must run before Firebase deletion")
			if scenario != "missing profile" {
				var stored models.Profile
				require.NoError(t, db.First(&stored, "id = ?", profile.ID).Error)
				require.Equal(t, profile.FirebaseUID, stored.FirebaseUID)
			}
		})
	}
}

// SQL failures must roll back before contacting Firebase, at every cleanup stage.
func TestDeleteAccountDatabaseFailures(t *testing.T) {
	logger.InitLogger()
	gin.SetMode(gin.TestMode)
	for _, stage := range []string{"memberships", "owner count", "homes", "user homes", "profile"} {
		t.Run(stage, func(t *testing.T) {
			conn, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { conn.Close() })
			db, err := gorm.Open(postgres.New(postgres.Config{Conn: conn}), &gorm.Config{Logger: gormlog.Default.LogMode(gormlog.Silent)})
			require.NoError(t, err)
			id, homeID := uuid.New(), uuid.New()
			uid, email := "firebase-identity", "user@example.com"
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/delete", strings.NewReader(`{"user_id":"`+id.String()+`","email":"user@example.com"}`))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Set("userID", id)
			c.Set("email", email)
			c.Set("firebaseUID", uid)
			// Cache the fixture's language so error responses require no extra queries.
			mock.ExpectQuery(`SELECT "id","language_id" FROM "profiles"`).WithArgs(id, 1).
				WillReturnRows(sqlmock.NewRows([]string{"id", "language_id"}).AddRow(id, nil))
			i18n.TranslateDB(db, c, "Failed to delete account")
			t.Cleanup(func() { i18n.InvalidateUserLanguageCache(id) })
			mock.ExpectQuery(`SELECT \* FROM "profiles" WHERE id = \$1 AND email = \$2`).WithArgs(id, email, 1).
				WillReturnRows(sqlmock.NewRows([]string{"id", "email", "firebase_uid"}).AddRow(id, email, uid))
			mock.ExpectBegin()
			failure := errors.New("database cleanup failed")
			membership := mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND role = \$2`).WithArgs(id, models.RoleOwner)
			if stage == "memberships" {
				membership.WillReturnError(failure)
			} else {
				membership.WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(id, homeID, models.RoleOwner))
				owners := mock.ExpectQuery(`SELECT count\(\*\) FROM "user_homes"`).WithArgs(homeID, models.RoleOwner, id)
				if stage == "owner count" {
					owners.WillReturnError(failure)
				} else {
					owners.WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
					for _, deletion := range []struct {
						name, query string
						arg         interface{}
					}{
						{"homes", `DELETE FROM "homes" WHERE id IN \(\$1\)`, homeID},
						{"user homes", `DELETE FROM "user_homes" WHERE user_id = \$1`, id},
						{"profile", `DELETE FROM "profiles" WHERE id = \$1`, id},
					} {
						expect := mock.ExpectExec(deletion.query).WithArgs(deletion.arg)
						if deletion.name == stage {
							expect.WillReturnError(failure)
							break
						}
						expect.WillReturnResult(sqlmock.NewResult(0, 1))
					}
				}
			}
			mock.ExpectRollback()
			client := &profileAuthStub{}
			(&ProfileHandler{DB: db, Auth: client}).DeleteAccount(c)
			require.Equal(t, http.StatusInternalServerError, w.Code)
			require.JSONEq(t, `{"error":"Failed to delete account"}`, w.Body.String())
			require.Empty(t, client.deletedUID)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestDeleteAccountAlreadyMissingFromFirebase(t *testing.T) {
	// Obtain the actual Admin SDK's USER_NOT_FOUND error from a local fixture.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/identitytoolkit.googleapis.com/v1/projects/test-project/accounts:delete" || r.Method != http.MethodPost {
			t.Errorf("unexpected Firebase deletion request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":{"code":400,"message":"USER_NOT_FOUND"}}`))
	}))
	defer server.Close()
	t.Setenv("FIREBASE_AUTH_EMULATOR_HOST", strings.TrimPrefix(server.URL, "http://"))
	app, err := firebase.NewApp(context.Background(), &firebase.Config{ProjectID: "test-project"})
	require.NoError(t, err)
	client, err := app.Auth(context.Background())
	require.NoError(t, err)
	uid := "firebase-identity"
	err = client.DeleteUser(context.Background(), uid)
	require.True(t, auth.IsUserNotFound(err), "%v", err)

	db := openUnitTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.Profile{}, &models.Home{}, &models.UserHome{}))
	profile := models.Profile{ID: uuid.New(), Email: "user@example.com", FirebaseUID: &uid}
	require.NoError(t, db.Create(&profile).Error)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/delete", strings.NewReader(`{"user_id":"`+profile.ID.String()+`","email":"USER@example.com"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("userID", profile.ID)
	c.Set("email", profile.Email)
	c.Set("firebaseUID", uid)
	// The handler should still commit SQL cleanup if Firebase was already deleted.
	stub := &profileAuthStub{err: err}
	(&ProfileHandler{DB: db, Auth: stub}).DeleteAccount(c)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, uid, stub.deletedUID)
	var count int64
	require.NoError(t, db.Model(&models.Profile{}).Count(&count).Error)
	require.Zero(t, count)
}
