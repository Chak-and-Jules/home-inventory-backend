package handlers

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
	gin.SetMode(gin.TestMode)

	userID := uuid.MustParse("123e4567-e89b-12d3-a456-426614174000")
	email := "user@example.com"

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
		req, err := http.NewRequest(http.MethodPost, "/profiles/sync", strings.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)
		c.Set("email", email)

		return w, c
	}

	t.Run("success creates or updates profile without changing immutable fields", func(t *testing.T) {
		handler, mock, closeDB := setupTest(t)
		defer closeDB()

		body := `{"profile":{"id":"123e4567-e89b-12d3-a456-426614174000","email":"user@example.com"}}`
		w, c := setupContext(t, body)

		mock.ExpectBegin()
		mock.ExpectExec(`INSERT INTO "profiles".*ON CONFLICT \("id"\) DO UPDATE SET "updated_at"="excluded"\."updated_at"`).
			WithArgs(userID, email, false, sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		mock.ExpectQuery(`SELECT 1 FROM "user_homes" WHERE user_id = \$1 LIMIT \$2`).
			WithArgs(userID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"1"}))

		mock.ExpectBegin()
		mock.ExpectQuery(`INSERT INTO "homes" \("name","created_at","updated_at"\) VALUES \(\$1,\$2,\$3\) RETURNING "id"`).
			WithArgs("My Home", sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))
		mock.ExpectExec(`INSERT INTO "user_homes" \("user_id","home_id","role","is_default","created_at","updated_at"\) VALUES \(\$1,\$2,\$3,\$4,\$5,\$6\)`).
			WithArgs(userID, sqlmock.AnyArg(), models.RoleOwner, true, sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		handler.SyncProfile(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("success creates or updates profile and skips home creation if user has homes", func(t *testing.T) {
		handler, mock, closeDB := setupTest(t)
		defer closeDB()

		body := `{"profile":{"id":"123e4567-e89b-12d3-a456-426614174000","email":"user@example.com"}}`
		w, c := setupContext(t, body)

		mock.ExpectBegin()
		mock.ExpectExec(`INSERT INTO "profiles".*ON CONFLICT \("id"\) DO UPDATE SET "updated_at"="excluded"\."updated_at"`).
			WithArgs(userID, email, false, sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		mock.ExpectQuery(`SELECT 1 FROM "user_homes" WHERE user_id = \$1 LIMIT \$2`).
			WithArgs(userID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"1"}).AddRow(1))

		handler.SyncProfile(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("invalid json", func(t *testing.T) {
		handler, _, closeDB := setupTest(t)
		defer closeDB()

		w, c := setupContext(t, `{"profile":`)

		handler.SyncProfile(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("mismatched user id", func(t *testing.T) {
		handler, _, closeDB := setupTest(t)
		defer closeDB()

		body := `{"profile":{"id":"223e4567-e89b-12d3-a456-426614174000","email":"user@example.com"}}`
		w, c := setupContext(t, body)

		handler.SyncProfile(c)

		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.Contains(t, w.Body.String(), "Profile user ID does not match authenticated user")
	})

	t.Run("mismatched email", func(t *testing.T) {
		handler, _, closeDB := setupTest(t)
		defer closeDB()

		body := `{"profile":{"id":"123e4567-e89b-12d3-a456-426614174000","email":"other@example.com"}}`
		w, c := setupContext(t, body)

		handler.SyncProfile(c)

		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.Contains(t, w.Body.String(), "Profile email does not match authenticated user")
	})

	t.Run("db error when checking home count", func(t *testing.T) {
		handler, mock, closeDB := setupTest(t)
		defer closeDB()

		body := `{"profile":{"id":"123e4567-e89b-12d3-a456-426614174000","email":"user@example.com"}}`
		w, c := setupContext(t, body)

		mock.ExpectBegin()
		mock.ExpectExec(`INSERT INTO "profiles".*ON CONFLICT \("id"\) DO UPDATE SET "updated_at"="excluded"\."updated_at"`).
			WithArgs(userID, email, false, sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		mock.ExpectQuery(`SELECT 1 FROM "user_homes" WHERE user_id = \$1 LIMIT \$2`).
			WithArgs(userID, 1).
			WillReturnError(errors.New("db error counting"))

		handler.SyncProfile(c)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "Failed to check homes:")
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error when creating home inside transaction", func(t *testing.T) {
		handler, mock, closeDB := setupTest(t)
		defer closeDB()

		body := `{"profile":{"id":"123e4567-e89b-12d3-a456-426614174000","email":"user@example.com"}}`
		w, c := setupContext(t, body)

		mock.ExpectBegin()
		mock.ExpectExec(`INSERT INTO "profiles".*ON CONFLICT \("id"\) DO UPDATE SET "updated_at"="excluded"\."updated_at"`).
			WithArgs(userID, email, false, sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		mock.ExpectQuery(`SELECT 1 FROM "user_homes" WHERE user_id = \$1 LIMIT \$2`).
			WithArgs(userID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"1"}))

		mock.ExpectBegin()
		mock.ExpectQuery(`INSERT INTO "homes" \("name","created_at","updated_at"\) VALUES \(\$1,\$2,\$3\) RETURNING "id"`).
			WithArgs("My Home", sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnError(errors.New("insert error inside tx"))
		mock.ExpectRollback()

		handler.SyncProfile(c)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "Failed to create default home")
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		handler, mock, closeDB := setupTest(t)
		defer closeDB()

		body := `{"profile":{"id":"123e4567-e89b-12d3-a456-426614174000","email":"user@example.com"}}`
		w, c := setupContext(t, body)

		mock.ExpectBegin()
		mock.ExpectExec(`INSERT INTO "profiles"`).
			WithArgs(userID, email, false, sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnError(errors.New("insert error"))
		mock.ExpectRollback()

		handler.SyncProfile(c)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}
