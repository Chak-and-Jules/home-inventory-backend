package handlers

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func setupLocationTest(t *testing.T) (*LocationHandler, sqlmock.Sqlmock) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: db}), &gorm.Config{})
	require.NoError(t, err)
	handler := &LocationHandler{DB: gormDB}
	return handler, mock
}

func TestGetLocations(t *testing.T) {
	gin.SetMode(gin.TestMode)
	userID := uuid.New()
	homeID := uuid.New()

	t.Run("success", func(t *testing.T) {
		handler, mock := setupLocationTest(t)
		req, err := http.NewRequest(http.MethodGet, "/locations", nil)
		require.NoError(t, err)
		req.Header.Set("x-home-id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND home_id = \$2 ORDER BY "user_homes"\."user_id" LIMIT \$3`).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id"}).AddRow(userID, homeID))

		locationID := uuid.New()
		mock.ExpectQuery(`SELECT \* FROM "locations" WHERE home_id = \$1`).
			WithArgs(homeID).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "name"}).AddRow(locationID, homeID, "Kitchen"))

		handler.GetLocations(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("missing home_id", func(t *testing.T) {
		handler, _ := setupLocationTest(t)
		req, err := http.NewRequest(http.MethodGet, "/locations", nil)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		handler.GetLocations(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("access denied", func(t *testing.T) {
		handler, mock := setupLocationTest(t)
		req, err := http.NewRequest(http.MethodGet, "/locations", nil)
		require.NoError(t, err)
		req.Header.Set("x-home-id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND home_id = \$2 ORDER BY "user_homes"\."user_id" LIMIT \$3`).
			WithArgs(userID, homeID, 1).
			WillReturnError(errors.New("not found"))

		handler.GetLocations(c)

		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestCreateLocation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	userID := uuid.New()
	homeID := uuid.New()

	t.Run("success", func(t *testing.T) {
		handler, mock := setupLocationTest(t)
		reqBody := `{"name": "Pantry"}`
		req, err := http.NewRequest(http.MethodPost, "/locations", strings.NewReader(reqBody))
		require.NoError(t, err)
		req.Header.Set("x-home-id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND home_id = \$2 ORDER BY "user_homes"\."user_id" LIMIT \$3`).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, models.RoleOwner))

		mock.ExpectBegin()
		mock.ExpectQuery(`INSERT INTO "locations" \("home_id","name","created_at","updated_at"\) VALUES \(\$1,\$2,\$3,\$4\) RETURNING "id"`).
			WithArgs(homeID, "Pantry", sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))
		mock.ExpectCommit()

		handler.CreateLocation(c)

		assert.Equal(t, http.StatusCreated, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("invalid payload", func(t *testing.T) {
		handler, mock := setupLocationTest(t)
		reqBody := `{}` // Missing required name field
		req, err := http.NewRequest(http.MethodPost, "/locations", strings.NewReader(reqBody))
		require.NoError(t, err)
		req.Header.Set("x-home-id", homeID.String())

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND home_id = \$2 ORDER BY "user_homes"\."user_id" LIMIT \$3`).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, models.RoleOwner))

		handler.CreateLocation(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestUpdateLocation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	userID := uuid.New()
	homeID := uuid.New()
	locationID := uuid.New()

	t.Run("success", func(t *testing.T) {
		handler, mock := setupLocationTest(t)
		reqBody := `{"name": "Updated Pantry"}`
		req, err := http.NewRequest(http.MethodPut, "/locations/"+locationID.String(), strings.NewReader(reqBody))
		require.NoError(t, err)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: locationID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "locations" WHERE "locations"\."id" = \$1 ORDER BY "locations"\."id" LIMIT \$2`).
			WithArgs(locationID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id", "name"}).AddRow(locationID, homeID, "Pantry"))

		mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND home_id = \$2 ORDER BY "user_homes"\."user_id" LIMIT \$3`).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, models.RoleOwner))

		mock.ExpectBegin()
		mock.ExpectExec(`UPDATE "locations" SET .*`).
			WithArgs("Updated Pantry", sqlmock.AnyArg(), locationID).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		handler.UpdateLocation(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("invalid id", func(t *testing.T) {
		handler, _ := setupLocationTest(t)
		reqBody := `{"name": "Updated Pantry"}`
		req, err := http.NewRequest(http.MethodPut, "/locations/invalid-id", strings.NewReader(reqBody))
		require.NoError(t, err)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: "invalid-id"}}
		c.Set("userID", userID)

		handler.UpdateLocation(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestDeleteLocation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	userID := uuid.New()
	homeID := uuid.New()
	locationID := uuid.New()

	t.Run("success", func(t *testing.T) {
		handler, mock := setupLocationTest(t)
		req, err := http.NewRequest(http.MethodDelete, "/locations/"+locationID.String(), nil)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: locationID.String()}}
		c.Set("userID", userID)

		mock.ExpectQuery(`SELECT \* FROM "locations" WHERE "locations"\."id" = \$1 ORDER BY "locations"\."id" LIMIT \$2`).
			WithArgs(locationID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "home_id"}).AddRow(locationID, homeID))

		mock.ExpectQuery(`SELECT \* FROM "user_homes" WHERE user_id = \$1 AND home_id = \$2 ORDER BY "user_homes"\."user_id" LIMIT \$3`).
			WithArgs(userID, homeID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "home_id", "role"}).AddRow(userID, homeID, models.RoleOwner))

		mock.ExpectBegin()
		mock.ExpectExec(`DELETE FROM "locations" WHERE ".*"."id" = \$1`).
			WithArgs(locationID).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()

		handler.DeleteLocation(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("invalid id", func(t *testing.T) {
		handler, _ := setupLocationTest(t)
		req, err := http.NewRequest(http.MethodDelete, "/locations/invalid-id", nil)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Params = []gin.Param{{Key: "id", Value: "invalid-id"}}
		c.Set("userID", userID)

		handler.DeleteLocation(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}
