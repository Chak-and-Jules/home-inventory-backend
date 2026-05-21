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

func TestGetCategories(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	gormDB, err := gorm.Open(postgres.New(postgres.Config{
		Conn: db,
	}), &gorm.Config{})
	require.NoError(t, err)

	handler := &CategoryHandler{DB: gormDB}

	t.Run("success", func(t *testing.T) {
		handler.mu.Lock()
		handler.cacheValid = false
		handler.mu.Unlock()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodGet, "/categories", nil)
		c.Request = req

		mock.ExpectQuery(`SELECT \* FROM "categories"`).
			WillReturnRows(sqlmock.NewRows([]string{"id", "name", "parent_id"}).
				AddRow("123e4567-e89b-12d3-a456-426614174000", "Category 1", nil))

		handler.GetCategories(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		handler.mu.Lock()
		handler.cacheValid = false
		handler.mu.Unlock()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodGet, "/categories", nil)
		c.Request = req

		mock.ExpectQuery(`SELECT \* FROM "categories"`).
			WillReturnError(errors.New("db error"))

		handler.GetCategories(c)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}
func TestUpdateCategory(t *testing.T) {
	gin.SetMode(gin.TestMode)

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
			Name: "Updated Category",
		}
		jsonData, err := json.Marshal(reqBody)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodPut, "/categories/123e4567-e89b-12d3-a456-426614174000", bytes.NewBuffer(jsonData))
		req.Header.Set("Content-Type", "application/json")
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: "123e4567-e89b-12d3-a456-426614174000"}}

		mock.ExpectBegin()
		mock.ExpectExec(`UPDATE "categories" SET`).
			WithArgs("Updated Category", sqlmock.AnyArg(), "123e4567-e89b-12d3-a456-426614174000").
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		handler.UpdateCategory(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("invalid id", func(t *testing.T) {
		reqBody := CategoryRequest{
			Name: "Updated Category",
		}
		jsonData, err := json.Marshal(reqBody)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodPut, "/categories/invalid", bytes.NewBuffer(jsonData))
		req.Header.Set("Content-Type", "application/json")
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: "invalid"}}

		handler.UpdateCategory(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("invalid json", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodPut, "/categories/123e4567-e89b-12d3-a456-426614174000", bytes.NewBuffer([]byte("invalid")))
		req.Header.Set("Content-Type", "application/json")
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: "123e4567-e89b-12d3-a456-426614174000"}}

		handler.UpdateCategory(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("db error", func(t *testing.T) {
		reqBody := CategoryRequest{
			Name: "Updated Category",
		}
		jsonData, err := json.Marshal(reqBody)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodPut, "/categories/123e4567-e89b-12d3-a456-426614174000", bytes.NewBuffer(jsonData))
		req.Header.Set("Content-Type", "application/json")
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: "123e4567-e89b-12d3-a456-426614174000"}}

		mock.ExpectBegin()
		mock.ExpectExec(`UPDATE "categories" SET`).
			WithArgs("Updated Category", sqlmock.AnyArg(), "123e4567-e89b-12d3-a456-426614174000").
			WillReturnError(errors.New("db error"))
		mock.ExpectRollback()

		handler.UpdateCategory(c)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestDeleteCategory(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	gormDB, err := gorm.Open(postgres.New(postgres.Config{
		Conn: db,
	}), &gorm.Config{})
	require.NoError(t, err)

	handler := &CategoryHandler{DB: gormDB}

	t.Run("success", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodDelete, "/categories/123e4567-e89b-12d3-a456-426614174000", nil)
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: "123e4567-e89b-12d3-a456-426614174000"}}

		mock.ExpectBegin()
		mock.ExpectExec(`DELETE FROM "categories"`).
			WithArgs("123e4567-e89b-12d3-a456-426614174000").
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		handler.DeleteCategory(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("invalid id", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodDelete, "/categories/invalid", nil)
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: "invalid"}}

		handler.DeleteCategory(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("db error", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest(http.MethodDelete, "/categories/123e4567-e89b-12d3-a456-426614174000", nil)
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: "123e4567-e89b-12d3-a456-426614174000"}}

		mock.ExpectBegin()
		mock.ExpectExec(`DELETE FROM "categories"`).
			WithArgs("123e4567-e89b-12d3-a456-426614174000").
			WillReturnError(errors.New("db error"))
		mock.ExpectRollback()

		handler.DeleteCategory(c)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}
