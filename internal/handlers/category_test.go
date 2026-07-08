package handlers

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/logger"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func setupCategoryTest(t *testing.T) (*CategoryHandler, sqlmock.Sqlmock) {
	t.Helper()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: db}), &gorm.Config{})
	require.NoError(t, err)

	return &CategoryHandler{DB: gormDB}, mock
}

func expectCategoryAccess(mock sqlmock.Sqlmock, userID, homeID uuid.UUID, role string) {
	mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND home_id = \$2 ORDER BY "user_homes"\."user_id" LIMIT \$3`).
		WithArgs(userID, homeID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role", "is_default", "created_at", "updated_at"}).
			AddRow(userID, homeID, role, false, time.Now(), time.Now()))
}

func expectCategoryByID(mock sqlmock.Sqlmock, categoryID, homeID uuid.UUID) {
	mock.ExpectQuery(`SELECT \* FROM "categories" WHERE "categories"\."id" = \$1 ORDER BY "categories"\."id" LIMIT \$2`).
		WithArgs(categoryID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "name", "parent_id", "created_at", "updated_at"}).
			AddRow(categoryID, homeID, "Test Category", nil, time.Now(), time.Now()))
}

func TestCreateCategory(t *testing.T) {
	logger.InitLogger()
	gin.SetMode(gin.TestMode)
	userID := uuid.New()
	homeID := uuid.New()

	t.Run("success", func(t *testing.T) {
		handler, mock := setupCategoryTest(t)
		expectCategoryAccess(mock, userID, homeID, models.RoleOwner)

		mock.ExpectQuery(`SELECT count\(\*\) FROM "categories" WHERE \(home_id = \$1 AND name = \$2\) AND parent_id IS NULL`).
			WithArgs(homeID, "Test Category").
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

		mock.ExpectBegin()
		mock.ExpectQuery(`INSERT INTO "categories" \("home_id","name","parent_id","created_at","updated_at"\) VALUES \(\$1,\$2,\$3,\$4,\$5\) RETURNING "id"`).
			WithArgs(homeID, "Test Category", nil, sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))
		mock.ExpectCommit()

		req, err := http.NewRequest(http.MethodPost, "/categories", strings.NewReader(`{"name":"Test Category"}`))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Home-Id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		handler.CreateCategory(c)

		assert.Equal(t, http.StatusCreated, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("success with parent", func(t *testing.T) {
		handler, mock := setupCategoryTest(t)
		expectCategoryAccess(mock, userID, homeID, models.RoleOwner)
		parentID := uuid.New()

		mock.ExpectQuery(`SELECT "id","home_id" FROM "categories" WHERE id = \$1 ORDER BY "categories"\."id" LIMIT \$2`).
			WithArgs(parentID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id"}).AddRow(parentID, homeID))

		mock.ExpectQuery(`SELECT count\(\*\) FROM "categories" WHERE \(home_id = \$1 AND name = \$2\) AND parent_id = \$3`).
			WithArgs(homeID, "Test Category", parentID).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

		mock.ExpectBegin()
		mock.ExpectQuery(`INSERT INTO "categories" \("home_id","name","parent_id","created_at","updated_at"\) VALUES \(\$1,\$2,\$3,\$4,\$5\) RETURNING "id"`).
			WithArgs(homeID, "Test Category", parentID, sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))
		mock.ExpectCommit()

		req, err := http.NewRequest(http.MethodPost, "/categories", strings.NewReader(`{"name":"Test Category", "parent_id":"`+parentID.String()+`"}`))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Home-Id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		handler.CreateCategory(c)

		assert.Equal(t, http.StatusCreated, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("count error", func(t *testing.T) {
		handler, mock := setupCategoryTest(t)
		expectCategoryAccess(mock, userID, homeID, models.RoleOwner)

		mock.ExpectQuery(`SELECT count\(\*\) FROM "categories" WHERE \(home_id = \$1 AND name = \$2\) AND parent_id IS NULL`).
			WithArgs(homeID, "Test Category").
			WillReturnError(errors.New("db error"))

		req, err := http.NewRequest(http.MethodPost, "/categories", strings.NewReader(`{"name":"Test Category"}`))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Home-Id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		handler.CreateCategory(c)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
	t.Run("conflict", func(t *testing.T) {
		handler, mock := setupCategoryTest(t)
		expectCategoryAccess(mock, userID, homeID, models.RoleOwner)

		mock.ExpectQuery(`SELECT count\(\*\) FROM "categories" WHERE \(home_id = \$1 AND name = \$2\) AND parent_id IS NULL`).
			WithArgs(homeID, "Test Category").
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

		req, err := http.NewRequest(http.MethodPost, "/categories", strings.NewReader(`{"name":"Test Category"}`))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Home-Id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		handler.CreateCategory(c)

		assert.Equal(t, http.StatusConflict, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("invalid json", func(t *testing.T) {
		handler, mock := setupCategoryTest(t)
		expectCategoryAccess(mock, userID, homeID, models.RoleOwner)

		req, err := http.NewRequest(http.MethodPost, "/categories", strings.NewReader("invalid"))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Home-Id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		handler.CreateCategory(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("missing required field", func(t *testing.T) {
		handler, mock := setupCategoryTest(t)
		expectCategoryAccess(mock, userID, homeID, models.RoleOwner)

		req, err := http.NewRequest(http.MethodPost, "/categories", strings.NewReader(`{}`))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Home-Id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		handler.CreateCategory(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		handler, mock := setupCategoryTest(t)
		expectCategoryAccess(mock, userID, homeID, models.RoleEditor)

		mock.ExpectQuery(`SELECT count\(\*\) FROM "categories" WHERE \(home_id = \$1 AND name = \$2\) AND parent_id IS NULL`).
			WithArgs(homeID, "Test Category").
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

		mock.ExpectBegin()
		mock.ExpectQuery(`INSERT INTO "categories" \("home_id","name","parent_id","created_at","updated_at"\) VALUES \(\$1,\$2,\$3,\$4,\$5\) RETURNING "id"`).
			WithArgs(homeID, "Test Category", nil, sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnError(errors.New("db error"))
		mock.ExpectRollback()

		req, err := http.NewRequest(http.MethodPost, "/categories", strings.NewReader(`{"name":"Test Category"}`))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Home-Id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		handler.CreateCategory(c)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("parent not found", func(t *testing.T) {
		handler, mock := setupCategoryTest(t)
		expectCategoryAccess(mock, userID, homeID, models.RoleOwner)
		parentID := uuid.New()

		mock.ExpectQuery(`SELECT "id","home_id" FROM "categories" WHERE id = \$1 ORDER BY "categories"\."id" LIMIT \$2`).
			WithArgs(parentID, 1).
			WillReturnError(gorm.ErrRecordNotFound)

		req, err := http.NewRequest(http.MethodPost, "/categories", strings.NewReader(`{"name":"Test Category", "parent_id":"`+parentID.String()+`"}`))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Home-Id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		handler.CreateCategory(c)

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("cross-home parent denied", func(t *testing.T) {
		handler, mock := setupCategoryTest(t)
		expectCategoryAccess(mock, userID, homeID, models.RoleOwner)
		parentID := uuid.New()
		otherHomeID := uuid.New()

		mock.ExpectQuery(`SELECT "id","home_id" FROM "categories" WHERE id = \$1 ORDER BY "categories"\."id" LIMIT \$2`).
			WithArgs(parentID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id"}).AddRow(parentID, otherHomeID))

		req, err := http.NewRequest(http.MethodPost, "/categories", strings.NewReader(`{"name":"Test Category", "parent_id":"`+parentID.String()+`"}`))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Home-Id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		handler.CreateCategory(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestGetCategories(t *testing.T) {
	logger.InitLogger()
	gin.SetMode(gin.TestMode)
	userID := uuid.New()
	homeID := uuid.New()

	t.Run("success", func(t *testing.T) {
		handler, mock := setupCategoryTest(t)
		expectCategoryAccess(mock, userID, homeID, models.RoleViewer)

		mock.ExpectQuery(`SELECT \* FROM "categories" WHERE home_id = \$1`).
			WithArgs(homeID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "name", "parent_id", "created_at", "updated_at"}).
				AddRow(uuid.New(), homeID, "Category 1", nil, time.Now(), time.Now()))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, err := http.NewRequest(http.MethodGet, "/categories", nil)
		require.NoError(t, err)
		req.Header.Set("X-Home-Id", homeID.String())
		c.Request = req
		c.Set("userID", userID)

		handler.GetCategories(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("access denied", func(t *testing.T) {
		handler, mock := setupCategoryTest(t)

		mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND home_id = \$2 ORDER BY "user_homes"\."user_id" LIMIT \$3`).
			WithArgs(userID, homeID, 1).
			WillReturnError(errors.New("not found"))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, err := http.NewRequest(http.MethodGet, "/categories", nil)
		require.NoError(t, err)
		req.Header.Set("X-Home-Id", homeID.String())
		c.Request = req
		c.Set("userID", userID)

		handler.GetCategories(c)

		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		handler, mock := setupCategoryTest(t)
		expectCategoryAccess(mock, userID, homeID, models.RoleOwner)

		mock.ExpectQuery(`SELECT \* FROM "categories" WHERE home_id = \$1`).
			WithArgs(homeID).
			WillReturnError(errors.New("db error"))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, err := http.NewRequest(http.MethodGet, "/categories", nil)
		require.NoError(t, err)
		req.Header.Set("X-Home-Id", homeID.String())
		c.Request = req
		c.Set("userID", userID)

		handler.GetCategories(c)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("category not found", func(t *testing.T) {
		handler, mock := setupCategoryTest(t)
		categoryID := uuid.New()

		mock.ExpectQuery(`SELECT \* FROM "categories" WHERE "categories"\."id" = \$1 ORDER BY "categories"\."id" LIMIT \$2`).
			WithArgs(categoryID, 1).
			WillReturnError(gorm.ErrRecordNotFound)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, err := http.NewRequest(http.MethodDelete, "/categories/"+categoryID.String(), nil)
		require.NoError(t, err)
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: categoryID.String()}}
		c.Set("userID", userID)

		handler.DeleteCategory(c)

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("access denied", func(t *testing.T) {
		handler, mock := setupCategoryTest(t)
		categoryID := uuid.New()
		homeID := uuid.New()
		expectCategoryByID(mock, categoryID, homeID)

		mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND home_id = \$2 ORDER BY "user_homes"\."user_id" LIMIT \$3`).
			WithArgs(userID, homeID, 1).
			WillReturnError(errors.New("not found"))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, err := http.NewRequest(http.MethodDelete, "/categories/"+categoryID.String(), nil)
		require.NoError(t, err)
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: categoryID.String()}}
		c.Set("userID", userID)

		handler.DeleteCategory(c)

		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestUpdateCategory(t *testing.T) {
	logger.InitLogger()
	gin.SetMode(gin.TestMode)
	userID := uuid.New()
	homeID := uuid.New()
	categoryID := uuid.New()

	t.Run("self parenting denied", func(t *testing.T) {
		handler, mock := setupCategoryTest(t)
		expectCategoryByID(mock, categoryID, homeID)
		expectCategoryAccess(mock, userID, homeID, models.RoleOwner)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, err := http.NewRequest(http.MethodPut, "/categories/"+categoryID.String(), strings.NewReader(`{"name":"Updated Category", "parent_id":"`+categoryID.String()+`"}`))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: categoryID.String()}}
		c.Set("userID", userID)

		handler.UpdateCategory(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("parent not found", func(t *testing.T) {
		handler, mock := setupCategoryTest(t)
		expectCategoryByID(mock, categoryID, homeID)
		expectCategoryAccess(mock, userID, homeID, models.RoleEditor)
		parentID := uuid.New()

		mock.ExpectQuery(`SELECT "id","home_id" FROM "categories" WHERE id = \$1 ORDER BY "categories"\."id" LIMIT \$2`).
			WithArgs(parentID, 1).
			WillReturnError(gorm.ErrRecordNotFound)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, err := http.NewRequest(http.MethodPut, "/categories/"+categoryID.String(), strings.NewReader(`{"name":"Updated Category", "parent_id":"`+parentID.String()+`"}`))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: categoryID.String()}}
		c.Set("userID", userID)

		handler.UpdateCategory(c)

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("parent generic error", func(t *testing.T) {
		handler, mock := setupCategoryTest(t)
		expectCategoryByID(mock, categoryID, homeID)
		expectCategoryAccess(mock, userID, homeID, models.RoleEditor)
		parentID := uuid.New()

		mock.ExpectQuery(`SELECT "id","home_id" FROM "categories" WHERE id = \$1 ORDER BY "categories"\."id" LIMIT \$2`).
			WithArgs(parentID, 1).
			WillReturnError(errors.New("db error"))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, err := http.NewRequest(http.MethodPut, "/categories/"+categoryID.String(), strings.NewReader(`{"name":"Updated Category", "parent_id":"`+parentID.String()+`"}`))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: categoryID.String()}}
		c.Set("userID", userID)

		handler.UpdateCategory(c)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("cross-home parent denied", func(t *testing.T) {
		handler, mock := setupCategoryTest(t)
		expectCategoryByID(mock, categoryID, homeID)
		expectCategoryAccess(mock, userID, homeID, models.RoleEditor)
		parentID := uuid.New()
		otherHomeID := uuid.New()

		mock.ExpectQuery(`SELECT "id","home_id" FROM "categories" WHERE id = \$1 ORDER BY "categories"\."id" LIMIT \$2`).
			WithArgs(parentID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id"}).AddRow(parentID, otherHomeID))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, err := http.NewRequest(http.MethodPut, "/categories/"+categoryID.String(), strings.NewReader(`{"name":"Updated Category", "parent_id":"`+parentID.String()+`"}`))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: categoryID.String()}}
		c.Set("userID", userID)

		handler.UpdateCategory(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("success", func(t *testing.T) {
		handler, mock := setupCategoryTest(t)
		expectCategoryByID(mock, categoryID, homeID)
		expectCategoryAccess(mock, userID, homeID, models.RoleEditor)

		mock.ExpectQuery(`SELECT count\(\*\) FROM "categories" WHERE \(home_id = \$1 AND name = \$2 AND id != \$3\) AND parent_id IS NULL`).
			WithArgs(homeID, "Updated Category", categoryID).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

		mock.ExpectBegin()
		mock.ExpectExec(`UPDATE "categories" SET .*`).
			WithArgs("Updated Category", nil, sqlmock.AnyArg(), categoryID).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, err := http.NewRequest(http.MethodPut, "/categories/"+categoryID.String(), strings.NewReader(`{"name":"Updated Category"}`))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: categoryID.String()}}
		c.Set("userID", userID)

		handler.UpdateCategory(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("success with parent", func(t *testing.T) {
		handler, mock := setupCategoryTest(t)
		expectCategoryByID(mock, categoryID, homeID)
		expectCategoryAccess(mock, userID, homeID, models.RoleEditor)
		parentID := uuid.New()

		mock.ExpectQuery(`SELECT "id","home_id" FROM "categories" WHERE id = \$1 ORDER BY "categories"\."id" LIMIT \$2`).
			WithArgs(parentID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id"}).AddRow(parentID, homeID))

		mock.ExpectQuery(`SELECT count\(\*\) FROM "categories" WHERE \(home_id = \$1 AND name = \$2 AND id != \$3\) AND parent_id = \$4`).
			WithArgs(homeID, "Updated Category", categoryID, parentID).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

		mock.ExpectBegin()
		mock.ExpectExec(`UPDATE "categories" SET .*`).
			WithArgs("Updated Category", parentID, sqlmock.AnyArg(), categoryID).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, err := http.NewRequest(http.MethodPut, "/categories/"+categoryID.String(), strings.NewReader(`{"name":"Updated Category", "parent_id":"`+parentID.String()+`"}`))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: categoryID.String()}}
		c.Set("userID", userID)

		handler.UpdateCategory(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("count error", func(t *testing.T) {
		handler, mock := setupCategoryTest(t)
		expectCategoryByID(mock, categoryID, homeID)
		expectCategoryAccess(mock, userID, homeID, models.RoleEditor)

		mock.ExpectQuery(`SELECT count\(\*\) FROM "categories" WHERE \(home_id = \$1 AND name = \$2 AND id != \$3\) AND parent_id IS NULL`).
			WithArgs(homeID, "Updated Category", categoryID).
			WillReturnError(errors.New("db error"))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, err := http.NewRequest(http.MethodPut, "/categories/"+categoryID.String(), strings.NewReader(`{"name":"Updated Category"}`))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: categoryID.String()}}
		c.Set("userID", userID)

		handler.UpdateCategory(c)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
	t.Run("conflict", func(t *testing.T) {
		handler, mock := setupCategoryTest(t)
		expectCategoryByID(mock, categoryID, homeID)
		expectCategoryAccess(mock, userID, homeID, models.RoleEditor)

		mock.ExpectQuery(`SELECT count\(\*\) FROM "categories" WHERE \(home_id = \$1 AND name = \$2 AND id != \$3\) AND parent_id IS NULL`).
			WithArgs(homeID, "Updated Category", categoryID).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, err := http.NewRequest(http.MethodPut, "/categories/"+categoryID.String(), strings.NewReader(`{"name":"Updated Category"}`))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: categoryID.String()}}
		c.Set("userID", userID)

		handler.UpdateCategory(c)

		assert.Equal(t, http.StatusConflict, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("invalid id", func(t *testing.T) {
		handler, _ := setupCategoryTest(t)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, err := http.NewRequest(http.MethodPut, "/categories/invalid", strings.NewReader(`{"name":"Updated Category"}`))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: "invalid"}}
		c.Set("userID", userID)

		handler.UpdateCategory(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("invalid json", func(t *testing.T) {
		handler, mock := setupCategoryTest(t)
		expectCategoryByID(mock, categoryID, homeID)
		expectCategoryAccess(mock, userID, homeID, models.RoleOwner)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, err := http.NewRequest(http.MethodPut, "/categories/"+categoryID.String(), strings.NewReader("invalid"))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: categoryID.String()}}
		c.Set("userID", userID)

		handler.UpdateCategory(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		handler, mock := setupCategoryTest(t)
		expectCategoryByID(mock, categoryID, homeID)
		expectCategoryAccess(mock, userID, homeID, models.RoleOwner)

		mock.ExpectQuery(`SELECT count\(\*\) FROM "categories" WHERE \(home_id = \$1 AND name = \$2 AND id != \$3\) AND parent_id IS NULL`).
			WithArgs(homeID, "Updated Category", categoryID).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

		mock.ExpectBegin()
		mock.ExpectExec(`UPDATE "categories" SET .*`).
			WithArgs("Updated Category", nil, sqlmock.AnyArg(), categoryID).
			WillReturnError(errors.New("db error"))
		mock.ExpectRollback()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, err := http.NewRequest(http.MethodPut, "/categories/"+categoryID.String(), strings.NewReader(`{"name":"Updated Category"}`))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: categoryID.String()}}
		c.Set("userID", userID)

		handler.UpdateCategory(c)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("invalid home id", func(t *testing.T) {
		handler, _ := setupCategoryTest(t)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, err := http.NewRequest(http.MethodGet, "/categories", nil)
		require.NoError(t, err)
		req.Header.Set("X-Home-Id", "invalid")
		c.Request = req
		c.Set("userID", userID)

		handler.GetCategories(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestDeleteCategory(t *testing.T) {
	logger.InitLogger()
	gin.SetMode(gin.TestMode)
	userID := uuid.New()
	homeID := uuid.New()
	categoryID := uuid.New()

	t.Run("success", func(t *testing.T) {
		handler, mock := setupCategoryTest(t)
		expectCategoryByID(mock, categoryID, homeID)
		expectCategoryAccess(mock, userID, homeID, models.RoleOwner)

		mock.ExpectBegin()
		mock.ExpectExec(`DELETE FROM "categories" WHERE ".*"."id" = \$1`).
			WithArgs(categoryID).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, err := http.NewRequest(http.MethodDelete, "/categories/"+categoryID.String(), nil)
		require.NoError(t, err)
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: categoryID.String()}}
		c.Set("userID", userID)

		handler.DeleteCategory(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("invalid id", func(t *testing.T) {
		handler, _ := setupCategoryTest(t)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, err := http.NewRequest(http.MethodDelete, "/categories/invalid", nil)
		require.NoError(t, err)
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: "invalid"}}
		c.Set("userID", userID)

		handler.DeleteCategory(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("db error", func(t *testing.T) {
		handler, mock := setupCategoryTest(t)
		expectCategoryByID(mock, categoryID, homeID)
		expectCategoryAccess(mock, userID, homeID, models.RoleOwner)

		mock.ExpectBegin()
		mock.ExpectExec(`DELETE FROM "categories" WHERE ".*"."id" = \$1`).
			WithArgs(categoryID).
			WillReturnError(errors.New("db error"))
		mock.ExpectRollback()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, err := http.NewRequest(http.MethodDelete, "/categories/"+categoryID.String(), nil)
		require.NoError(t, err)
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: categoryID.String()}}
		c.Set("userID", userID)

		handler.DeleteCategory(c)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("parent generic error", func(t *testing.T) {
		handler, mock := setupCategoryTest(t)
		expectCategoryAccess(mock, userID, homeID, models.RoleOwner)
		parentID := uuid.New()

		mock.ExpectQuery(`SELECT "id","home_id" FROM "categories" WHERE id = \$1 ORDER BY "categories"\."id" LIMIT \$2`).
			WithArgs(parentID, 1).
			WillReturnError(errors.New("db error"))

		req, err := http.NewRequest(http.MethodPost, "/categories", strings.NewReader(`{"name":"Test Category", "parent_id":"`+parentID.String()+`"}`))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Home-Id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		handler.CreateCategory(c)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}
