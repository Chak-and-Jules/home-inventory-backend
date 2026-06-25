package handlers

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestGetLanguages(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: db}), &gorm.Config{})
	require.NoError(t, err)

	handler := &LanguageHandler{DB: gormDB}

	t.Run("success", func(t *testing.T) {
		// Clear cache
		handler = &LanguageHandler{DB: gormDB}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodGet, "/languages", nil)
		c.Request = req

		mock.ExpectQuery(`SELECT \* FROM "languages"`).
			WillReturnRows(sqlmock.NewRows([]string{"id", "name", "created_at", "updated_at"}).
				AddRow("123e4567-e89b-12d3-a456-426614174000", "English", nil, nil))

		handler.GetLanguages(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("cache hit", func(t *testing.T) {
		handler := &LanguageHandler{DB: gormDB}
		handler.cache.Store([]models.Language{{Name: "Cached Language"}})

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodGet, "/languages", nil)
		c.Request = req

		handler.GetLanguages(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "Cached Language")
	})

	t.Run("db error", func(t *testing.T) {
		handler := &LanguageHandler{DB: gormDB}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodGet, "/languages", nil)
		c.Request = req

		mock.ExpectQuery(`SELECT \* FROM "languages"`).
			WillReturnError(errors.New("db error"))

		handler.GetLanguages(c)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}
