package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestCreateCategory(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Setup mock DB
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	gormDB, err := gorm.Open(postgres.New(postgres.Config{
		Conn: db,
	}), &gorm.Config{})
	require.NoError(t, err)

	handler := &CategoryHandler{DB: gormDB}

	t.Run("success", func(t *testing.T) {
		reqBody := CategoryRequest{
			Name: "Test Category",
		}
		jsonData, err := json.Marshal(reqBody)
		require.NoError(t, err)

		req, err := http.NewRequest(http.MethodPost, "/categories", bytes.NewBuffer(jsonData))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req

		// Expect the insert
		mock.ExpectBegin()
		mock.ExpectQuery(`INSERT INTO "categories"`).
			WithArgs("Test Category", nil, sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("123e4567-e89b-12d3-a456-426614174000"))
		mock.ExpectCommit()

		handler.CreateCategory(c)

		assert.Equal(t, http.StatusCreated, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("invalid json", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodPost, "/categories", bytes.NewBuffer([]byte("invalid")))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req

		handler.CreateCategory(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("missing required field", func(t *testing.T) {
		reqBody := CategoryRequest{}
		jsonData, err := json.Marshal(reqBody)
		require.NoError(t, err)

		req, err := http.NewRequest(http.MethodPost, "/categories", bytes.NewBuffer(jsonData))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req

		handler.CreateCategory(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("db error", func(t *testing.T) {
		reqBody := CategoryRequest{
			Name: "Test Category",
		}
		jsonData, err := json.Marshal(reqBody)
		require.NoError(t, err)

		req, err := http.NewRequest(http.MethodPost, "/categories", bytes.NewBuffer(jsonData))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req

		mock.ExpectBegin()
		mock.ExpectQuery(`INSERT INTO "categories"`).
			WithArgs("Test Category", nil, sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnError(errors.New("db error"))
		mock.ExpectRollback()

		handler.CreateCategory(c)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}
