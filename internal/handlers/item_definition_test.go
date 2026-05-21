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

func TestItemDefinitionHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	gormDB, err := gorm.Open(postgres.New(postgres.Config{
		Conn: db,
	}), &gorm.Config{})
	require.NoError(t, err)

	handler := &ItemDefinitionHandler{DB: gormDB}
	homeID := uuid.New()
	userID := uuid.New()
	defID := uuid.New()
	catID := uuid.New()
	sizeUnitID := uuid.New()

	t.Run("CreateItemDefinition success", func(t *testing.T) {
		reqBody := ItemDefinitionRequest{
			Name:        "Test Item",
			Description: "Test Desc",
			CategoryID:  &catID,
			SizeUnitID:  &sizeUnitID,
			IsExpirable: false,
			ImageURL:    "http://test.com/img.jpg",
		}
		jsonData, _ := json.Marshal(reqBody)

		req, _ := http.NewRequest(http.MethodPost, "/item-definitions?home_id="+homeID.String(), bytes.NewBuffer(jsonData))
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2`)).
			WithArgs(userID.String(), homeID.String(), 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID.String(), homeID.String(), "owner"))

		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO "item_definitions"`)).
			WithArgs(homeID.String(), "Test Item", "Test Desc", catID.String(), sizeUnitID.String(), false, "http://test.com/img.jpg", sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(defID.String()))
		mock.ExpectCommit()

		handler.CreateItemDefinition(c)

		assert.Equal(t, http.StatusCreated, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("GetItemDefinitions success", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/item-definitions?home_id="+homeID.String(), nil)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2`)).
			WithArgs(userID.String(), homeID.String(), 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID.String(), homeID.String(), "viewer"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "item_definitions" WHERE home_id = $1`)).
			WithArgs(homeID.String()).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "name"}).
				AddRow(defID.String(), homeID.String(), "Test Item"))

		handler.GetItemDefinitions(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("UpdateItemDefinition success", func(t *testing.T) {
		reqBody := ItemDefinitionRequest{Name: "Updated Item", SizeUnitID: &sizeUnitID}
		jsonData, _ := json.Marshal(reqBody)

		req, _ := http.NewRequest(http.MethodPut, "/item-definitions/"+defID.String(), bytes.NewBuffer(jsonData))
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: defID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "item_definitions" WHERE "item_definitions"."id" = $1`)).
			WithArgs(defID.String(), 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "name"}).AddRow(defID.String(), homeID.String(), "Test Item"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2`)).
			WithArgs(userID.String(), homeID.String(), 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID.String(), homeID.String(), "owner"))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "item_definitions" SET`)).
			WithArgs(nil, "", "", false, "Updated Item", sizeUnitID.String(), sqlmock.AnyArg(), defID.String()).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		handler.UpdateItemDefinition(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("DeleteItemDefinition success", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodDelete, "/item-definitions/"+defID.String(), nil)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: defID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "item_definitions" WHERE "item_definitions"."id" = $1`)).
			WithArgs(defID.String(), 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "name"}).AddRow(defID.String(), homeID.String(), "Test Item"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2`)).
			WithArgs(userID.String(), homeID.String(), 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID.String(), homeID.String(), "owner"))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM "item_definitions" WHERE "item_definitions"."id" = $1`)).
			WithArgs(defID.String()).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		handler.DeleteItemDefinition(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("CreateItemDefinition auth failure", func(t *testing.T) {
		reqBody := ItemDefinitionRequest{Name: "Test Item", SizeUnitID: &sizeUnitID}
		jsonData, _ := json.Marshal(reqBody)

		req, _ := http.NewRequest(http.MethodPost, "/item-definitions?home_id="+homeID.String(), bytes.NewBuffer(jsonData))
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2`)).
			WithArgs(userID.String(), homeID.String(), 1).
			WillReturnError(errors.New("not found"))

		handler.CreateItemDefinition(c)

		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
	t.Run("CreateItemDefinition bind error", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, "/item-definitions?home_id="+homeID.String(), bytes.NewBuffer([]byte("invalid")))
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2`)).
			WithArgs(userID.String(), homeID.String(), 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID.String(), homeID.String(), "owner"))

		handler.CreateItemDefinition(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("UpdateItemDefinition auth failure", func(t *testing.T) {
		reqBody := ItemDefinitionRequest{Name: "Updated Item", SizeUnitID: &sizeUnitID}
		jsonData, _ := json.Marshal(reqBody)

		req, _ := http.NewRequest(http.MethodPut, "/item-definitions/"+defID.String(), bytes.NewBuffer(jsonData))
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: defID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "item_definitions" WHERE "item_definitions"."id" = $1`)).
			WithArgs(defID.String(), 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "name"}).AddRow(defID.String(), homeID.String(), "Test Item"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2`)).
			WithArgs(userID.String(), homeID.String(), 1).
			WillReturnError(errors.New("not found"))

		handler.UpdateItemDefinition(c)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("UpdateItemDefinition db error", func(t *testing.T) {
		reqBody := ItemDefinitionRequest{Name: "Updated Item", SizeUnitID: &sizeUnitID}
		jsonData, _ := json.Marshal(reqBody)

		req, _ := http.NewRequest(http.MethodPut, "/item-definitions/"+defID.String(), bytes.NewBuffer(jsonData))
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: defID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "item_definitions" WHERE "item_definitions"."id" = $1`)).
			WithArgs(defID.String(), 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "name"}).AddRow(defID.String(), homeID.String(), "Test Item"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2`)).
			WithArgs(userID.String(), homeID.String(), 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID.String(), homeID.String(), "owner"))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE "item_definitions" SET`)).
			WithArgs(nil, "", "", false, "Updated Item", sizeUnitID.String(), sqlmock.AnyArg(), defID.String()).
			WillReturnError(errors.New("db err"))
		mock.ExpectRollback()

		handler.UpdateItemDefinition(c)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("DeleteItemDefinition not found", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodDelete, "/item-definitions/"+defID.String(), nil)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: defID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "item_definitions" WHERE "item_definitions"."id" = $1`)).
			WithArgs(defID.String(), 1).
			WillReturnError(errors.New("db err"))

		handler.DeleteItemDefinition(c)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("GetItemDefinitions missing home_id", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/item-definitions", nil)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		handler.GetItemDefinitions(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("CreateItemDefinition missing home_id", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, "/item-definitions", nil)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		handler.CreateItemDefinition(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("DeleteItemDefinition auth failure", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodDelete, "/item-definitions/"+defID.String(), nil)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: defID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "item_definitions" WHERE "item_definitions"."id" = $1`)).
			WithArgs(defID.String(), 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "name"}).AddRow(defID.String(), homeID.String(), "Test Item"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2`)).
			WithArgs(userID.String(), homeID.String(), 1).
			WillReturnError(errors.New("not found"))

		handler.DeleteItemDefinition(c)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("UpdateItemDefinition not found", func(t *testing.T) {
		reqBody := ItemDefinitionRequest{Name: "Updated Item", SizeUnitID: &sizeUnitID}
		jsonData, _ := json.Marshal(reqBody)

		req, _ := http.NewRequest(http.MethodPut, "/item-definitions/"+defID.String(), bytes.NewBuffer(jsonData))
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: defID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "item_definitions" WHERE "item_definitions"."id" = $1`)).
			WithArgs(defID.String(), 1).
			WillReturnError(errors.New("db err"))

		handler.UpdateItemDefinition(c)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("GetItemDefinitions DB error", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/item-definitions?home_id="+homeID.String(), nil)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2`)).
			WithArgs(userID.String(), homeID.String(), 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID.String(), homeID.String(), "viewer"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "item_definitions" WHERE home_id = $1`)).
			WithArgs(homeID.String()).
			WillReturnError(errors.New("db err"))

		handler.GetItemDefinitions(c)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("DeleteItemDefinition auth failure", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodDelete, "/item-definitions/"+defID.String(), nil)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: defID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "item_definitions" WHERE "item_definitions"."id" = $1`)).
			WithArgs(defID.String(), 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "name"}).AddRow(defID.String(), homeID.String(), "Test Item"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2`)).
			WithArgs(userID.String(), homeID.String(), 1).
			WillReturnError(errors.New("not found"))

		handler.DeleteItemDefinition(c)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("UpdateItemDefinition not found", func(t *testing.T) {
		reqBody := ItemDefinitionRequest{Name: "Updated Item", SizeUnitID: &sizeUnitID}
		jsonData, _ := json.Marshal(reqBody)

		req, _ := http.NewRequest(http.MethodPut, "/item-definitions/"+defID.String(), bytes.NewBuffer(jsonData))
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: defID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "item_definitions" WHERE "item_definitions"."id" = $1`)).
			WithArgs(defID.String(), 1).
			WillReturnError(errors.New("db err"))

		handler.UpdateItemDefinition(c)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("GetItemDefinitions DB error", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/item-definitions?home_id="+homeID.String(), nil)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2`)).
			WithArgs(userID.String(), homeID.String(), 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID.String(), homeID.String(), "viewer"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "item_definitions" WHERE home_id = $1`)).
			WithArgs(homeID.String()).
			WillReturnError(errors.New("db err"))

		handler.GetItemDefinitions(c)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("CreateItemDefinition DB error", func(t *testing.T) {
		reqBody := ItemDefinitionRequest{
			Name: "Test Item",
			Description: "Test Desc",
			CategoryID: &catID,
			SizeUnitID: &sizeUnitID,
			IsExpirable: false,
			ImageURL: "http://test.com/img.jpg",
		}
		jsonData, _ := json.Marshal(reqBody)

		req, _ := http.NewRequest(http.MethodPost, "/item-definitions?home_id="+homeID.String(), bytes.NewBuffer(jsonData))
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2`)).
			WithArgs(userID.String(), homeID.String(), 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID.String(), homeID.String(), "owner"))

		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO "item_definitions"`)).
			WithArgs(homeID.String(), "Test Item", "Test Desc", catID.String(), sizeUnitID.String(), false, "http://test.com/img.jpg", sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnError(errors.New("db err"))
		mock.ExpectRollback()

		handler.CreateItemDefinition(c)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("DeleteItemDefinition DB error", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodDelete, "/item-definitions/"+defID.String(), nil)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: defID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "item_definitions" WHERE "item_definitions"."id" = $1`)).
			WithArgs(defID.String(), 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "name"}).AddRow(defID.String(), homeID.String(), "Test Item"))

		mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2`)).
			WithArgs(userID.String(), homeID.String(), 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID.String(), homeID.String(), "owner"))

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM "item_definitions" WHERE "item_definitions"."id" = $1`)).
			WithArgs(defID.String()).
			WillReturnError(errors.New("db err"))
		mock.ExpectRollback()

		handler.DeleteItemDefinition(c)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

}
