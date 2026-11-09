package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"firebase.google.com/go/v4/auth"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/logger"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestSyncProfile(t *testing.T) {
	t.Run("new user receives one personal and one private demo home", func(t *testing.T) {
		f := newDemoFixture(t)
		f.user = f.freshUser(t)
		theme := "Dark"
		require.NoError(t, f.db.Model(&models.Profile{}).Where("id = ?", f.user).Updates(map[string]any{"is_admin": true, "web_theme": theme}).Error)
		f.syncProfile(t, 200)
		f.syncProfile(t, 200)
		var membership []models.UserHome
		require.NoError(t, f.db.Preload("Home").Where("user_id = ?", f.user).Find(&membership).Error)
		require.Len(t, membership, 2)
		for _, m := range membership {
			require.Equal(t, models.RoleOwner, m.Role)
			require.Equal(t, !m.Home.IsDemo, m.IsDefault)
		}
		var profile models.Profile
		require.NoError(t, f.db.First(&profile, "id = ?", f.user).Error)
		require.True(t, profile.IsAdmin)
		require.Equal(t, &theme, profile.WebTheme)
	})
	t.Run("existing user retains current homes and default", func(t *testing.T) {
		f := newDemoFixture(t)
		f.syncProfile(t, 200)
		require.Equal(t, "not_created", f.status(t).Status)
		var count int64
		require.NoError(t, f.db.Model(&models.UserHome{}).Where("user_id = ?", f.user).Count(&count).Error)
		require.EqualValues(t, 1, count)
	})
	t.Run("provisioning failure rolls back both homes and can be retried", func(t *testing.T) {
		f := newDemoFixture(t)
		f.user = f.freshUser(t)
		require.NoError(t, f.db.Callback().Create().Before("gorm:create").Register("fail_signup_seed", func(tx *gorm.DB) {
			if tx.Statement.Table == "inventory_items" {
				tx.AddError(errors.New("seed failed"))
			}
		}))
		f.syncProfile(t, 500)
		var count int64
		require.NoError(t, f.db.Model(&models.UserHome{}).Where("user_id = ?", f.user).Count(&count).Error)
		require.Zero(t, count)
		require.Equal(t, "not_created", f.status(t).Status)
		require.NoError(t, f.db.Callback().Create().Remove("fail_signup_seed"))
		f.syncProfile(t, 200)
		require.Equal(t, "available", f.status(t).Status)
	})
	t.Run("identity and JSON validation", func(t *testing.T) {
		f := newDemoFixture(t)
		for _, entry := range []struct {
			body   any
			status int
		}{
			{map[string]any{}, 400},
			{map[string]any{"profile": map[string]string{"id": "different", "email": f.user.String() + "@example.com"}}, 403},
			{map[string]any{"profile": map[string]string{"id": "firebase-" + f.user.String(), "email": "wrong@example.com"}}, 403},
		} {
			f.send(t, "POST", "/profiles/sync", entry.body, entry.status)
		}
	})
	t.Run("profile upsert failure remains visible", func(t *testing.T) {
		logger.InitLogger()
		gin.SetMode(gin.TestMode)
		sqlDB, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer sqlDB.Close()
		db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{})
		require.NoError(t, err)
		user := uuid.New()
		mock.ExpectBegin()
		mock.ExpectExec(`INSERT INTO "profiles"`).WillReturnError(errors.New("insert failed"))
		mock.ExpectRollback()
		mock.ExpectQuery(`SELECT "id","language_id" FROM "profiles"`).WithArgs(user, 1).WillReturnRows(sqlmock.NewRows([]string{"id", "language_id"}).AddRow(user, nil))
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", "/profiles/sync", strings.NewReader(`{"profile":{"id":"uid","email":"user@example.com"}}`))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Set("userID", user)
		c.Set("email", "user@example.com")
		c.Set("firebaseUID", "uid")
		(&ProfileHandler{DB: db}).SyncProfile(c)
		require.Equal(t, 500, w.Code)
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestDeleteAccount(t *testing.T) {
	logger.InitLogger()
	gin.SetMode(gin.TestMode)

	userID := uuid.MustParse("123e4567-e89b-12d3-a456-426614174000")
	email := "user@example.com"
	firebaseUID := "firebase-user-not-a-uuid"

	setupTest := func(t *testing.T) (*ProfileHandler, sqlmock.Sqlmock, func()) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)

		gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: db}), &gorm.Config{})
		require.NoError(t, err)

		return &ProfileHandler{DB: gormDB, Auth: &profileAuthStub{}}, mock, func() {
			db.Close()
		}
	}

	setupContext := func(t *testing.T, body string) (*httptest.ResponseRecorder, *gin.Context) {
		req, err := http.NewRequest(http.MethodPost, "/profiles/delete-account", strings.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)
		c.Set("email", email)
		c.Set("firebaseUID", firebaseUID)

		return w, c
	}

	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "Firebase failure rolls back SQL"}[fail], func(t *testing.T) {
			handler, mock, closeDB := setupTest(t)
			defer closeDB()

			body := `{"user_id":"123e4567-e89b-12d3-a456-426614174000","email":"user@example.com"}`
			w, c := setupContext(t, body)

			mock.ExpectQuery(`SELECT \* FROM "profiles" WHERE id = \$1 AND email = \$2 ORDER BY "profiles"\."id" LIMIT \$3`).
				WithArgs(userID, email, 1).
				WillReturnRows(sqlmock.NewRows([]string{"id", "email", "firebase_uid"}).AddRow(userID, email, firebaseUID))

			mock.ExpectBegin()
			mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND role = \$2`).
				WithArgs(userID, models.RoleOwner).
				WillReturnRows(sqlmock.NewRows([]string{"home_id", "user_id", "role"}).AddRow(uuid.New(), userID, models.RoleOwner))

			mock.ExpectQuery(`SELECT count\(\*\) FROM "user_homes" WHERE home_id = \$1 AND role = \$2 AND user_id != \$3`).
				WithArgs(sqlmock.AnyArg(), models.RoleOwner, userID).
				WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

			mock.ExpectExec(`DELETE FROM "homes" WHERE id IN \(\$1\)`).
				WithArgs(sqlmock.AnyArg()).
				WillReturnResult(sqlmock.NewResult(1, 1))

			mock.ExpectExec(`DELETE FROM "user_homes" WHERE user_id = \$1`).
				WithArgs(userID).
				WillReturnResult(sqlmock.NewResult(1, 1))

			mock.ExpectExec(`DELETE FROM "profiles" WHERE id = \$1`).
				WithArgs(userID).
				WillReturnResult(sqlmock.NewResult(1, 1))
			if fail {
				mock.ExpectRollback()
				handler.Auth.(*profileAuthStub).err = errors.New("Firebase unavailable")
			} else {
				mock.ExpectCommit()
			}

			handler.DeleteAccount(c)

			if fail {
				assert.Equal(t, http.StatusInternalServerError, w.Code)
			} else {
				assert.Equal(t, http.StatusOK, w.Code)
			}
			assert.Equal(t, firebaseUID, handler.Auth.(*profileAuthStub).deletedUID)
			if !fail {
				assert.Contains(t, w.Body.String(), "Account deleted successfully")
			}
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
	t.Run("unauthorized user", func(t *testing.T) {
		handler, _, closeDB := setupTest(t)
		defer closeDB()

		body := `{"user_id":"223e4567-e89b-12d3-a456-426614174000","email":"user@example.com"}`
		w, c := setupContext(t, body)

		handler.DeleteAccount(c)

		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.Contains(t, w.Body.String(), "Unauthorized account deletion")
	})

	t.Run("unauthorized email", func(t *testing.T) {
		handler, _, closeDB := setupTest(t)
		defer closeDB()

		body := `{"user_id":"123e4567-e89b-12d3-a456-426614174000","email":"hacker@example.com"}`
		w, c := setupContext(t, body)

		handler.DeleteAccount(c)

		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.Contains(t, w.Body.String(), "Unauthorized account deletion")
	})

	t.Run("invalid json", func(t *testing.T) {
		handler, _, closeDB := setupTest(t)
		defer closeDB()

		body := `{"user_id": 123}` // Malformed
		w, c := setupContext(t, body)

		handler.DeleteAccount(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "Invalid request payload")
	})

	t.Run("invalid user ID format", func(t *testing.T) {
		handler, _, closeDB := setupTest(t)
		defer closeDB()

		body := `{"user_id":"invalid-uuid","email":"user@example.com"}`
		w, c := setupContext(t, body)

		handler.DeleteAccount(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "Invalid user ID format")
	})
}

func TestGetProfile(t *testing.T) {
	logger.InitLogger()
	gin.SetMode(gin.TestMode)

	userID := uuid.MustParse("123e4567-e89b-12d3-a456-426614174000")

	setupTest := func(t *testing.T) (*ProfileHandler, sqlmock.Sqlmock, func()) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)

		gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: db}), &gorm.Config{})
		require.NoError(t, err)

		return &ProfileHandler{DB: gormDB}, mock, func() {
			db.Close()
		}
	}

	setupContext := func(t *testing.T) (*httptest.ResponseRecorder, *gin.Context) {
		req, err := http.NewRequest(http.MethodGet, "/profiles", nil)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		return w, c
	}

	t.Run("success", func(t *testing.T) {
		handler, mock, closeDB := setupTest(t)
		defer closeDB()

		w, c := setupContext(t)

		theme := "dark"
		langID := uuid.New()
		window := 10

		rows := sqlmock.NewRows([]string{"web_theme", "mobile_theme", "language_id", "restock_window"}).
			AddRow(&theme, nil, &langID, &window)

		mock.ExpectQuery("^SELECT \"web_theme\",\"mobile_theme\",\"language_id\",\"restock_window\" FROM \"profiles\" WHERE id = \\$1 ORDER BY \"profiles\".\"id\" LIMIT \\$2$").
			WithArgs(userID, 1).
			WillReturnRows(rows)

		handler.GetProfile(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "dark")
		assert.Contains(t, w.Body.String(), langID.String())
		assert.Contains(t, w.Body.String(), `"restock_window":10`)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("profile not found", func(t *testing.T) {
		handler, mock, closeDB := setupTest(t)
		defer closeDB()

		w, c := setupContext(t)

		mock.ExpectQuery("^SELECT \"web_theme\",\"mobile_theme\",\"language_id\",\"restock_window\" FROM \"profiles\" WHERE id = \\$1 ORDER BY \"profiles\".\"id\" LIMIT \\$2$").
			WithArgs(userID, 1).
			WillReturnError(gorm.ErrRecordNotFound)

		handler.GetProfile(c)

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Contains(t, w.Body.String(), "Profile not found")
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		handler, mock, closeDB := setupTest(t)
		defer closeDB()

		w, c := setupContext(t)

		mock.ExpectQuery("^SELECT \"web_theme\",\"mobile_theme\",\"language_id\",\"restock_window\" FROM \"profiles\" WHERE id = \\$1 ORDER BY \"profiles\".\"id\" LIMIT \\$2$").
			WithArgs(userID, 1).
			WillReturnError(errors.New("db error"))

		handler.GetProfile(c)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestUpdateProfile(t *testing.T) {
	logger.InitLogger()
	gin.SetMode(gin.TestMode)

	userID := uuid.MustParse("123e4567-e89b-12d3-a456-426614174000")

	setupTest := func(t *testing.T) (*ProfileHandler, sqlmock.Sqlmock, func()) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)

		gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: db}), &gorm.Config{})
		require.NoError(t, err)

		return &ProfileHandler{DB: gormDB}, mock, func() {
			db.Close()
		}
	}

	setupContext := func(t *testing.T, body string) (*httptest.ResponseRecorder, *gin.Context) {
		req, err := http.NewRequest(http.MethodPut, "/profiles", strings.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		return w, c
	}

	t.Run("success update single field", func(t *testing.T) {
		handler, mock, closeDB := setupTest(t)
		defer closeDB()

		body := `{"web_theme":"light"}`
		w, c := setupContext(t, body)

		mock.ExpectBegin()
		mock.ExpectExec("^UPDATE \"profiles\" SET \"web_theme\"=\\$1,\"updated_at\"=\\$2 WHERE id = \\$3$").
			WithArgs("light", sqlmock.AnyArg(), userID).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		handler.UpdateProfile(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "Profile updated successfully")
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("success update restock_window preference", func(t *testing.T) {
		handler, mock, closeDB := setupTest(t)
		defer closeDB()

		body := `{"restock_window":14}`
		w, c := setupContext(t, body)

		mock.ExpectBegin()
		mock.ExpectExec("^UPDATE \"profiles\" SET \"restock_window\"=\\$1,\"updated_at\"=\\$2 WHERE id = \\$3$").
			WithArgs(14, sqlmock.AnyArg(), userID).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		handler.UpdateProfile(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "Profile updated successfully")
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("invalid restock_window value", func(t *testing.T) {
		handler, mock, closeDB := setupTest(t)
		defer closeDB()

		body := `{"restock_window":-5}`
		w, c := setupContext(t, body)

		mock.ExpectQuery(`SELECT "id","language_id" FROM "profiles" WHERE id = \$1`).
			WithArgs(userID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "language_id"}).AddRow(userID, nil))

		handler.UpdateProfile(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "Restock window must be a non-negative integer")
	})

	t.Run("success update multiple fields", func(t *testing.T) {
		handler, mock, closeDB := setupTest(t)
		defer closeDB()

		langID := uuid.New()
		body := `{"web_theme":"dark","mobile_theme":null,"language_id":"` + langID.String() + `"}`
		w, c := setupContext(t, body)

		mock.ExpectBegin()
		// GORM sorts map keys alphabetically for updates: language_id, mobile_theme, web_theme
		mock.ExpectExec("^UPDATE \"profiles\" SET \"language_id\"=\\$1,\"mobile_theme\"=\\$2,\"web_theme\"=\\$3,\"updated_at\"=\\$4 WHERE id = \\$5$").
			WithArgs(langID.String(), nil, "dark", sqlmock.AnyArg(), userID).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		handler.UpdateProfile(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("no valid fields to update", func(t *testing.T) {
		handler, _, closeDB := setupTest(t)
		defer closeDB()

		body := `{"other_field":"value"}`
		w, c := setupContext(t, body)

		handler.UpdateProfile(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "No valid fields to update")
	})

	t.Run("invalid json", func(t *testing.T) {
		handler, _, closeDB := setupTest(t)
		defer closeDB()

		w, c := setupContext(t, `{"web_theme":`)

		handler.UpdateProfile(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("db error on update", func(t *testing.T) {
		handler, mock, closeDB := setupTest(t)
		defer closeDB()

		body := `{"web_theme":"light"}`
		w, c := setupContext(t, body)

		mock.ExpectBegin()
		mock.ExpectExec("^UPDATE \"profiles\"").
			WillReturnError(errors.New("db error"))
		mock.ExpectRollback()

		handler.UpdateProfile(c)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

type profileAuthStub struct {
	err        error
	deletedUID string
}

func (s *profileAuthStub) VerifyIDTokenAndCheckRevoked(context.Context, string) (*auth.Token, error) {
	panic("unused")
}
func (s *profileAuthStub) DeleteUser(_ context.Context, uid string) error {
	s.deletedUID = uid
	return s.err
}
