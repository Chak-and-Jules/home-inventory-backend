package utils

import (
	"errors"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestVerifyHomeAccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: db}), &gorm.Config{})
	require.NoError(t, err)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	userID := uuid.New()
	homeID := uuid.New()
	c.Set("userID", userID)

	t.Run("has access", func(t *testing.T) {
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2`)).
			WithArgs(userID.String(), homeID.String(), 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID.String(), homeID.String(), "viewer"))

		assert.True(t, VerifyHomeAccess(c, gormDB, homeID))
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("no access", func(t *testing.T) {
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2`)).
			WithArgs(userID.String(), homeID.String(), 1).
			WillReturnError(errors.New("not found"))

		assert.False(t, VerifyHomeAccess(c, gormDB, homeID))
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestVerifyHomeWriteAccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: db}), &gorm.Config{})
	require.NoError(t, err)

	userID := uuid.New()
	homeID := uuid.New()

	t.Run("owner access", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("userID", userID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2`)).
			WithArgs(userID.String(), homeID.String(), 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID.String(), homeID.String(), "owner"))

		assert.True(t, VerifyHomeWriteAccess(c, gormDB, homeID))
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("editor access", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("userID", userID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2`)).
			WithArgs(userID.String(), homeID.String(), 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID.String(), homeID.String(), "editor"))

		assert.True(t, VerifyHomeWriteAccess(c, gormDB, homeID))
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("viewer no write access", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("userID", userID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2`)).
			WithArgs(userID.String(), homeID.String(), 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID.String(), homeID.String(), "viewer"))

		assert.False(t, VerifyHomeWriteAccess(c, gormDB, homeID))
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("no record", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("userID", userID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2`)).
			WithArgs(userID.String(), homeID.String(), 1).
			WillReturnError(errors.New("not found"))

		assert.False(t, VerifyHomeWriteAccess(c, gormDB, homeID))
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}
