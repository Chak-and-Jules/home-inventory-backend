package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
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

func TestCategoryHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	gormDB, err := gorm.Open(postgres.New(postgres.Config{
		Conn: db,
	}), &gorm.Config{})
	require.NoError(t, err)

	handler := &CategoryHandler{DB: gormDB}
	homeID := uuid.New()
	userID := uuid.New()
	catID := uuid.New()

	t.Run("CreateCategory success", func(t *testing.T) {
		reqBody := CategoryRequest{Name: "Test Category"}
		jsonData, _ := json.Marshal(reqBody)

		req, _ := http.NewRequest(http.MethodPost, "/categories?home_id="+homeID.String(), bytes.NewBuffer(jsonData))
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		// Mock auth
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2`)).
			WithArgs(userID.String(), homeID.String(), 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID.String(), homeID.String(), "owner"))

		// Mock insert
		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO "categories"`)).
			WithArgs(homeID.String(), "Test Category", nil, sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(catID.String()))
		mock.ExpectCommit()

		handler.CreateCategory(c)

		assert.Equal(t, http.StatusCreated, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("GetCategories success", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/categories?home_id="+homeID.String(), nil)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2`)).
			WithArgs(userID.String(), homeID.String(), 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID.String(), homeID.String(), "viewer"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "categories" WHERE home_id = $1`)).
			WithArgs(homeID.String()).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "name", "parent_id"}).
				AddRow(catID.String(), homeID.String(), "Category 1", nil))

		handler.GetCategories(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("UpdateCategory success", func(t *testing.T) {
		reqBody := CategoryRequest{Name: "Updated Category"}
		jsonData, _ := json.Marshal(reqBody)

		req, _ := http.NewRequest(http.MethodPut, "/categories/"+catID.String(), bytes.NewBuffer(jsonData))
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: catID.String()}}
		c.Set("userID", userID)

		// Mock category fetch
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "categories" WHERE "categories"."id" = $1`)).
			WithArgs(catID.String(), 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "name"}).AddRow(catID.String(), homeID.String(), "Category 1"))

		// Mock auth
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2`)).
			WithArgs(userID.String(), homeID.String(), 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID.String(), homeID.String(), "owner"))

		// Mock update
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "categories" SET`)).
			WithArgs("Updated Category", nil, sqlmock.AnyArg(), catID.String()).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		handler.UpdateCategory(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("DeleteCategory success", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodDelete, "/categories/"+catID.String(), nil)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: catID.String()}}
		c.Set("userID", userID)

		// Mock category fetch
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "categories" WHERE "categories"."id" = $1`)).
			WithArgs(catID.String(), 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "name"}).AddRow(catID.String(), homeID.String(), "Category 1"))

		// Mock auth
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2`)).
			WithArgs(userID.String(), homeID.String(), 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID.String(), homeID.String(), "owner"))

		// Mock delete
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM "categories" WHERE "categories"."id" = $1`)).
			WithArgs(catID.String()).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		handler.DeleteCategory(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("CreateCategory auth failure", func(t *testing.T) {
		reqBody := CategoryRequest{Name: "Test Category"}
		jsonData, _ := json.Marshal(reqBody)

		req, _ := http.NewRequest(http.MethodPost, "/categories?home_id="+homeID.String(), bytes.NewBuffer(jsonData))
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2`)).
			WithArgs(userID.String(), homeID.String(), 1).
			WillReturnError(errors.New("not found"))

		handler.CreateCategory(c)

		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
	t.Run("CreateCategory bind error", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, "/categories?home_id="+homeID.String(), bytes.NewBuffer([]byte("invalid")))
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2`)).
			WithArgs(userID.String(), homeID.String(), 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID.String(), homeID.String(), "owner"))

		handler.CreateCategory(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("UpdateCategory auth failure", func(t *testing.T) {
		reqBody := CategoryRequest{Name: "Updated Category"}
		jsonData, _ := json.Marshal(reqBody)

		req, _ := http.NewRequest(http.MethodPut, "/categories/"+catID.String(), bytes.NewBuffer(jsonData))
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: catID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "categories" WHERE "categories"."id" = $1`)).
			WithArgs(catID.String(), 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "name"}).AddRow(catID.String(), homeID.String(), "Category 1"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2`)).
			WithArgs(userID.String(), homeID.String(), 1).
			WillReturnError(errors.New("not found"))

		handler.UpdateCategory(c)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("UpdateCategory db error", func(t *testing.T) {
		reqBody := CategoryRequest{Name: "Updated Category"}
		jsonData, _ := json.Marshal(reqBody)

		req, _ := http.NewRequest(http.MethodPut, "/categories/"+catID.String(), bytes.NewBuffer(jsonData))
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: catID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "categories" WHERE "categories"."id" = $1`)).
			WithArgs(catID.String(), 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "name"}).AddRow(catID.String(), homeID.String(), "Category 1"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2`)).
			WithArgs(userID.String(), homeID.String(), 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID.String(), homeID.String(), "owner"))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "categories" SET`)).
			WithArgs("Updated Category", nil, sqlmock.AnyArg(), catID.String()).
			WillReturnError(errors.New("db err"))
		mock.ExpectRollback()

		handler.UpdateCategory(c)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("DeleteCategory not found", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodDelete, "/categories/"+catID.String(), nil)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: catID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "categories" WHERE "categories"."id" = $1`)).
			WithArgs(catID.String(), 1).
			WillReturnError(errors.New("db err"))

		handler.DeleteCategory(c)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("GetCategories missing home_id", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/categories", nil)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		handler.GetCategories(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("CreateCategory missing home_id", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, "/categories", nil)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		handler.CreateCategory(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

}
