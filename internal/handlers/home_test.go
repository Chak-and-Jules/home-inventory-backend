package handlers

import (
	"strings"

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
				AddRow(userID, homeID, models.RoleOwner, true, now, now))

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

func TestUpdateHome(t *testing.T) {
	gin.SetMode(gin.TestMode)
	userID := uuid.New()
	homeID := uuid.New()

	setupTest := func(t *testing.T) (*HomeHandler, sqlmock.Sqlmock) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: db}), &gorm.Config{})
		require.NoError(t, err)
		handler := &HomeHandler{DB: gormDB}
		return handler, mock
	}

	t.Run("success", func(t *testing.T) {
		handler, mock := setupTest(t)
		reqBody := `{"name": "Updated Home"}`
		req, err := http.NewRequest(http.MethodPut, "/homes/"+homeID.String(), strings.NewReader(reqBody))
		require.NoError(t, err)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: homeID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND home_id = \$2 ORDER BY "user_homes"\."user_id" LIMIT \$3`).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, models.RoleOwner))

		mock.ExpectBegin()
		mock.ExpectExec(`UPDATE "homes" SET .*`).
			WithArgs("Updated Home", sqlmock.AnyArg(), homeID).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		handler.UpdateHome(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("invalid id", func(t *testing.T) {
		handler, _ := setupTest(t)
		req, err := http.NewRequest(http.MethodPut, "/homes/invalid-id", strings.NewReader(`{"name": "Updated Home"}`))
		require.NoError(t, err)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: "invalid-id"}}
		c.Set("userID", userID)

		handler.UpdateHome(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("invalid json", func(t *testing.T) {
		handler, _ := setupTest(t)
		req, err := http.NewRequest(http.MethodPut, "/homes/"+homeID.String(), strings.NewReader(`{"name":}`))
		require.NoError(t, err)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: homeID.String()}}
		c.Set("userID", userID)

		handler.UpdateHome(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("access denied", func(t *testing.T) {
		handler, mock := setupTest(t)
		reqBody := `{"name": "Updated Home"}`
		req, err := http.NewRequest(http.MethodPut, "/homes/"+homeID.String(), strings.NewReader(reqBody))
		require.NoError(t, err)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: homeID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND home_id = \$2 ORDER BY "user_homes"\."user_id" LIMIT \$3`).
			WithArgs(userID, homeID, 1).
			WillReturnError(errors.New("not found"))

		handler.UpdateHome(c)

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("insufficient permissions", func(t *testing.T) {
		handler, mock := setupTest(t)
		reqBody := `{"name": "Updated Home"}`
		req, err := http.NewRequest(http.MethodPut, "/homes/"+homeID.String(), strings.NewReader(reqBody))
		require.NoError(t, err)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: homeID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND home_id = \$2 ORDER BY "user_homes"\."user_id" LIMIT \$3`).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, models.RoleViewer))

		handler.UpdateHome(c)

		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("update error", func(t *testing.T) {
		handler, mock := setupTest(t)
		reqBody := `{"name": "Updated Home"}`
		req, err := http.NewRequest(http.MethodPut, "/homes/"+homeID.String(), strings.NewReader(reqBody))
		require.NoError(t, err)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: homeID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND home_id = \$2 ORDER BY "user_homes"\."user_id" LIMIT \$3`).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, models.RoleOwner))

		mock.ExpectBegin()
		mock.ExpectExec(`UPDATE "homes" SET .*`).
			WithArgs("Updated Home", sqlmock.AnyArg(), homeID).
			WillReturnError(errors.New("update error"))
		mock.ExpectRollback()

		handler.UpdateHome(c)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestDeleteHome(t *testing.T) {
	gin.SetMode(gin.TestMode)
	userID := uuid.New()
	homeID := uuid.New()

	setupTest := func(t *testing.T) (*HomeHandler, sqlmock.Sqlmock) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: db}), &gorm.Config{})
		require.NoError(t, err)
		handler := &HomeHandler{DB: gormDB}
		return handler, mock
	}

	t.Run("success", func(t *testing.T) {
		handler, mock := setupTest(t)
		req, err := http.NewRequest(http.MethodDelete, "/homes/"+homeID.String(), nil)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: homeID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND home_id = \$2 ORDER BY "user_homes"\."user_id" LIMIT \$3`).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, models.RoleOwner))

		mock.ExpectBegin()
		mock.ExpectExec(`DELETE FROM "homes" WHERE ".*"."id" = \$1`).
			WithArgs(homeID).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()

		handler.DeleteHome(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("invalid id", func(t *testing.T) {
		handler, _ := setupTest(t)
		req, err := http.NewRequest(http.MethodDelete, "/homes/invalid-id", nil)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: "invalid-id"}}
		c.Set("userID", userID)

		handler.DeleteHome(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("access denied", func(t *testing.T) {
		handler, mock := setupTest(t)
		req, err := http.NewRequest(http.MethodDelete, "/homes/"+homeID.String(), nil)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: homeID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND home_id = \$2 ORDER BY "user_homes"\."user_id" LIMIT \$3`).
			WithArgs(userID, homeID, 1).
			WillReturnError(errors.New("not found"))

		handler.DeleteHome(c)

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("insufficient permissions", func(t *testing.T) {
		handler, mock := setupTest(t)
		req, err := http.NewRequest(http.MethodDelete, "/homes/"+homeID.String(), nil)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: homeID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND home_id = \$2 ORDER BY "user_homes"\."user_id" LIMIT \$3`).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, models.RoleEditor))

		handler.DeleteHome(c)

		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("delete error", func(t *testing.T) {
		handler, mock := setupTest(t)
		req, err := http.NewRequest(http.MethodDelete, "/homes/"+homeID.String(), nil)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: homeID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND home_id = \$2 ORDER BY "user_homes"\."user_id" LIMIT \$3`).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, models.RoleOwner))

		mock.ExpectBegin()
		mock.ExpectExec(`DELETE FROM "homes" WHERE ".*"."id" = \$1`).
			WithArgs(homeID).
			WillReturnError(errors.New("delete error"))
		mock.ExpectRollback()

		handler.DeleteHome(c)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestSetDefaultHome(t *testing.T) {
	gin.SetMode(gin.TestMode)
	userID := uuid.New()
	homeID := uuid.New()

	setupTest := func(t *testing.T) (*HomeHandler, sqlmock.Sqlmock) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: db}), &gorm.Config{})
		require.NoError(t, err)
		handler := &HomeHandler{DB: gormDB}
		return handler, mock
	}

	t.Run("success", func(t *testing.T) {
		handler, mock := setupTest(t)
		req, err := http.NewRequest(http.MethodPost, "/homes/"+homeID.String()+"/default", nil)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: homeID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND home_id = \$2 ORDER BY "user_homes"\."user_id" LIMIT \$3`).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id"}).AddRow(userID, homeID))

		mock.ExpectBegin()
		mock.ExpectExec(`UPDATE "user_homes" SET .*`).
			WithArgs(false, sqlmock.AnyArg(), userID).
			WillReturnResult(sqlmock.NewResult(0, 1))

		mock.ExpectExec(`UPDATE "user_homes" SET .*`).
			WithArgs(true, sqlmock.AnyArg(), userID, homeID).
			WillReturnResult(sqlmock.NewResult(0, 1))

		mock.ExpectCommit()

		handler.SetDefaultHome(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("invalid id", func(t *testing.T) {
		handler, _ := setupTest(t)
		req, err := http.NewRequest(http.MethodPost, "/homes/invalid-id/default", nil)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: "invalid-id"}}
		c.Set("userID", userID)

		handler.SetDefaultHome(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("access denied", func(t *testing.T) {
		handler, mock := setupTest(t)
		req, err := http.NewRequest(http.MethodPost, "/homes/"+homeID.String()+"/default", nil)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: homeID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND home_id = \$2 ORDER BY "user_homes"\."user_id" LIMIT \$3`).
			WithArgs(userID, homeID, 1).
			WillReturnError(errors.New("not found"))

		handler.SetDefaultHome(c)

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("transaction error update false", func(t *testing.T) {
		handler, mock := setupTest(t)
		req, err := http.NewRequest(http.MethodPost, "/homes/"+homeID.String()+"/default", nil)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: homeID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND home_id = \$2 ORDER BY "user_homes"\."user_id" LIMIT \$3`).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id"}).AddRow(userID, homeID))

		mock.ExpectBegin()
		mock.ExpectExec(`UPDATE "user_homes" SET .*`).
			WithArgs(false, sqlmock.AnyArg(), userID).
			WillReturnError(errors.New("update error"))
		mock.ExpectRollback()

		handler.SetDefaultHome(c)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("transaction error update true", func(t *testing.T) {
		handler, mock := setupTest(t)
		req, err := http.NewRequest(http.MethodPost, "/homes/"+homeID.String()+"/default", nil)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: homeID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND home_id = \$2 ORDER BY "user_homes"\."user_id" LIMIT \$3`).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id"}).AddRow(userID, homeID))

		mock.ExpectBegin()
		mock.ExpectExec(`UPDATE "user_homes" SET .*`).
			WithArgs(false, sqlmock.AnyArg(), userID).
			WillReturnResult(sqlmock.NewResult(0, 1))

		mock.ExpectExec(`UPDATE "user_homes" SET .*`).
			WithArgs(true, sqlmock.AnyArg(), userID, homeID).
			WillReturnError(errors.New("update error"))
		mock.ExpectRollback()

		handler.SetDefaultHome(c)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestCreateHome(t *testing.T) {
	gin.SetMode(gin.TestMode)
	userID := uuid.New()

	setupTest := func(t *testing.T) (*HomeHandler, sqlmock.Sqlmock) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: db}), &gorm.Config{})
		require.NoError(t, err)
		handler := &HomeHandler{DB: gormDB}
		return handler, mock
	}

	t.Run("success", func(t *testing.T) {
		handler, mock := setupTest(t)
		reqBody := `{"name": "New Home"}`
		req, err := http.NewRequest(http.MethodPost, "/homes", strings.NewReader(reqBody))
		require.NoError(t, err)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectBegin()
		mock.ExpectQuery(`INSERT INTO "homes" \("name","created_at","updated_at"\) VALUES \(\$1,\$2,\$3\) RETURNING "id"`).
			WithArgs("New Home", sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))

		mock.ExpectExec(`INSERT INTO "user_homes" \("user_id","home_id","role","is_default","created_at","updated_at"\) VALUES \(\$1,\$2,\$3,\$4,\$5,\$6\)`).
			WithArgs(userID, sqlmock.AnyArg(), models.RoleOwner, false, sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnResult(sqlmock.NewResult(1, 1))

		mock.ExpectCommit()

		handler.CreateHome(c)

		assert.Equal(t, http.StatusCreated, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("invalid json", func(t *testing.T) {
		handler, _ := setupTest(t)
		req, err := http.NewRequest(http.MethodPost, "/homes", strings.NewReader(`{"name":}`))
		require.NoError(t, err)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		handler.CreateHome(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("home insert error", func(t *testing.T) {
		handler, mock := setupTest(t)
		reqBody := `{"name": "New Home"}`
		req, err := http.NewRequest(http.MethodPost, "/homes", strings.NewReader(reqBody))
		require.NoError(t, err)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectBegin()
		mock.ExpectQuery(`INSERT INTO "homes" \("name","created_at","updated_at"\) VALUES \(\$1,\$2,\$3\) RETURNING "id"`).
			WithArgs("New Home", sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnError(errors.New("insert error"))
		mock.ExpectRollback()

		handler.CreateHome(c)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("user_home insert error", func(t *testing.T) {
		handler, mock := setupTest(t)
		reqBody := `{"name": "New Home"}`
		req, err := http.NewRequest(http.MethodPost, "/homes", strings.NewReader(reqBody))
		require.NoError(t, err)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectBegin()
		mock.ExpectQuery(`INSERT INTO "homes" \("name","created_at","updated_at"\) VALUES \(\$1,\$2,\$3\) RETURNING "id"`).
			WithArgs("New Home", sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))

		mock.ExpectExec(`INSERT INTO "user_homes" \("user_id","home_id","role","is_default","created_at","updated_at"\) VALUES \(\$1,\$2,\$3,\$4,\$5,\$6\)`).
			WithArgs(userID, sqlmock.AnyArg(), models.RoleOwner, false, sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnError(errors.New("insert error"))
		mock.ExpectRollback()

		handler.CreateHome(c)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}
