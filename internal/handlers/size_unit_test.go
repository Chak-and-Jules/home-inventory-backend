package handlers

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestGetSizeUnits(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: db}), &gorm.Config{})
	require.NoError(t, err)

	handler := &SizeUnitHandler{DB: gormDB}

	t.Run("success", func(t *testing.T) {
		handler.cache = atomic.Value{}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodGet, "/size-units", nil)
		c.Request = req

		mock.ExpectQuery(`SELECT \* FROM "size_units"`).
			WillReturnRows(sqlmock.NewRows([]string{"id", "name", "abbreviation", "created_at", "updated_at"}).
				AddRow("123e4567-e89b-12d3-a456-426614174000", "Kilogram", "kg", nil, nil))

		handler.GetSizeUnits(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("cache hit", func(t *testing.T) {
		handler.cache.Store([]models.SizeUnit{{Name: "Cached Unit"}})

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodGet, "/size-units", nil)
		c.Request = req

		handler.GetSizeUnits(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "Cached Unit")
	})

	t.Run("db error", func(t *testing.T) {
		handler.cache = atomic.Value{}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodGet, "/size-units", nil)
		c.Request = req

		mock.ExpectQuery(`SELECT \* FROM "size_units"`).
			WillReturnError(errors.New("db error"))

		handler.GetSizeUnits(c)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}
