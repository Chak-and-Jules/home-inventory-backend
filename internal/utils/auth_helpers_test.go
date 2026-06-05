package utils

import (
	"errors"
	"net/http/httptest"
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

func setupTestDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: db}), &gorm.Config{})
	require.NoError(t, err)
	return gormDB, mock
}

func TestGetUserHome(t *testing.T) {
	gin.SetMode(gin.TestMode)
	userID := uuid.New()
	homeID := uuid.New()

	t.Run("missing user id", func(t *testing.T) {
		db, _ := setupTestDB(t)
		c, _ := gin.CreateTestContext(httptest.NewRecorder())

		_, err := GetUserHome(c, db, homeID)
		assert.EqualError(t, err, "missing user id")
	})

	t.Run("invalid user id type", func(t *testing.T) {
		db, _ := setupTestDB(t)
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Set("userID", "not-a-uuid")

		_, err := GetUserHome(c, db, homeID)
		assert.EqualError(t, err, "invalid user id type")
	})

	t.Run("db error", func(t *testing.T) {
		db, mock := setupTestDB(t)
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND home_id = \$2 ORDER BY "user_homes"\."user_id" LIMIT \$3`).
			WithArgs(userID, homeID, 1).
			WillReturnError(errors.New("db error"))

		_, err := GetUserHome(c, db, homeID)
		assert.EqualError(t, err, "db error")
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("success", func(t *testing.T) {
		db, mock := setupTestDB(t)
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND home_id = \$2 ORDER BY "user_homes"\."user_id" LIMIT \$3`).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, models.RoleOwner))

		userHome, err := GetUserHome(c, db, homeID)
		assert.NoError(t, err)
		assert.NotNil(t, userHome)
		assert.Equal(t, models.RoleOwner, userHome.Role)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func setupAuthHelpersTest(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	gormDB, err := gorm.Open(postgres.New(postgres.Config{
		Conn: db,
	}), &gorm.Config{})
	require.NoError(t, err)
	return gormDB, mock
}

func TestVerifyHomeAccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	userID := uuid.New()
	homeID := uuid.New()

	t.Run("has access", func(t *testing.T) {
		gormDB, mock := setupAuthHelpersTest(t)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND home_id = \$2 ORDER BY "user_homes"\."user_id" LIMIT \$3`).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, models.RoleViewer))

		access := VerifyHomeAccess(c, gormDB, homeID)

		assert.True(t, access)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("no access", func(t *testing.T) {
		gormDB, mock := setupAuthHelpersTest(t)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND home_id = \$2 ORDER BY "user_homes"\."user_id" LIMIT \$3`).
			WithArgs(userID, homeID, 1).
			WillReturnError(errors.New("not found"))

		access := VerifyHomeAccess(c, gormDB, homeID)

		assert.False(t, access)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("missing user id", func(t *testing.T) {
		gormDB, _ := setupAuthHelpersTest(t)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		// Don't set userID

		access := VerifyHomeAccess(c, gormDB, homeID)

		assert.False(t, access)
	})
}

func TestVerifyHomeWriteAccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	userID := uuid.New()
	homeID := uuid.New()

	t.Run("owner access", func(t *testing.T) {
		gormDB, mock := setupAuthHelpersTest(t)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND home_id = \$2 ORDER BY "user_homes"\."user_id" LIMIT \$3`).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, models.RoleOwner))

		access := VerifyHomeWriteAccess(c, gormDB, homeID)

		assert.True(t, access)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("editor access", func(t *testing.T) {
		gormDB, mock := setupAuthHelpersTest(t)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND home_id = \$2 ORDER BY "user_homes"\."user_id" LIMIT \$3`).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, models.RoleEditor))

		access := VerifyHomeWriteAccess(c, gormDB, homeID)

		assert.True(t, access)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("viewer access", func(t *testing.T) {
		gormDB, mock := setupAuthHelpersTest(t)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND home_id = \$2 ORDER BY "user_homes"\."user_id" LIMIT \$3`).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, models.RoleViewer))

		access := VerifyHomeWriteAccess(c, gormDB, homeID)

		assert.False(t, access)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("no access", func(t *testing.T) {
		gormDB, mock := setupAuthHelpersTest(t)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND home_id = \$2 ORDER BY "user_homes"\."user_id" LIMIT \$3`).
			WithArgs(userID, homeID, 1).
			WillReturnError(errors.New("not found"))

		access := VerifyHomeWriteAccess(c, gormDB, homeID)

		assert.False(t, access)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}
