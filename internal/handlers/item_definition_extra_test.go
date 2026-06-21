package handlers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

// Add missing tests to hit coverage

func TestVerifyAdmin_MissingUserID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, _, err := setupTestDB()
	assert.NoError(t, err)

	sqlDB, err := db.DB()
	assert.NoError(t, err)
	defer sqlDB.Close()

	handler := &ItemDefinitionHandler{DB: db}
	homeID := uuid.New()

	router := gin.New()
	router.POST("/item-definitions", handler.CreateItemDefinition)

	req, _ := http.NewRequest(http.MethodPost, "/item-definitions", bytes.NewBufferString("{}"))
	req.Header.Set("X-Home-Id", homeID.String())
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestVerifyAdmin_InvalidUserIDType(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, _, err := setupTestDB()
	assert.NoError(t, err)

	sqlDB, err := db.DB()
	assert.NoError(t, err)
	defer sqlDB.Close()

	handler := &ItemDefinitionHandler{DB: db}
	homeID := uuid.New()

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("userID", "not-a-uuid")
		c.Next()
	})
	router.POST("/item-definitions", handler.CreateItemDefinition)

	req, _ := http.NewRequest(http.MethodPost, "/item-definitions", bytes.NewBufferString("{}"))
	req.Header.Set("X-Home-Id", homeID.String())
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestVerifyAdmin_DBError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock, err := setupTestDB()
	assert.NoError(t, err)

	sqlDB, err := db.DB()
	assert.NoError(t, err)
	defer sqlDB.Close()

	handler := &ItemDefinitionHandler{DB: db}
	userID := uuid.New()
	homeID := uuid.New()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "user_homes" WHERE user_id = $1 AND home_id = $2 ORDER BY "user_homes"."user_id" LIMIT $3`)).
		WithArgs(userID, homeID, 1).
		WillReturnError(gorm.ErrInvalidDB)

	router := gin.New()
	router.Use(authMiddleware(userID))
	router.POST("/item-definitions", handler.CreateItemDefinition)

	req, _ := http.NewRequest(http.MethodPost, "/item-definitions", bytes.NewBufferString("{}"))
	req.Header.Set("X-Home-Id", homeID.String())
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
}
