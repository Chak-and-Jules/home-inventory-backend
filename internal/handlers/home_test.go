package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestGetHomes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Setup mock DB
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	gormDB, err := gorm.Open(postgres.New(postgres.Config{
		Conn: db,
	}), &gorm.Config{})
	require.NoError(t, err)

	handler := &HomeHandler{DB: gormDB}

	userID := uuid.New()
	homeID := uuid.New()
	now := time.Now()

	t.Run("success", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodGet, "/homes", nil)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req

		// Set user ID in context (simulating auth middleware)
		c.Set("userID", userID)

		// Expect query for user_homes
		mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1`).
			WithArgs(userID).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role", "is_default", "created_at", "updated_at"}).
				AddRow(userID, homeID, "owner", true, now, now))

		// Expect preload query for homes
		mock.ExpectQuery(`SELECT \* FROM "homes" WHERE "homes"\."id" = \$1`).
			WithArgs(homeID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "name", "created_at", "updated_at"}).
				AddRow(homeID, "My Home", now, now))

		handler.GetHomes(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())

		var response []models.UserHome
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Len(t, response, 1)
		assert.Equal(t, homeID, response[0].HomeID)
		assert.Equal(t, "My Home", response[0].Home.Name)
	})

	t.Run("db error", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodGet, "/homes", nil)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req

		// Set user ID in context
		c.Set("userID", userID)

		// Expect query for user_homes to fail
		mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1`).
			WithArgs(userID).
			WillReturnError(errors.New("db error"))

		handler.GetHomes(c)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())

		var response map[string]string
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Equal(t, "Failed to fetch homes", response["error"])
	})
}
