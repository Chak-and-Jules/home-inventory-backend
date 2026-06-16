package i18n

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestTranslateDB(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("nil context", func(t *testing.T) {
		res := TranslateDB(nil, nil, "Access denied to this home")
		assert.Equal(t, "Access denied to this home", res)
	})

	t.Run("nil db and empty context", func(t *testing.T) {
		c, _ := gin.CreateTestContext(nil)
		res := TranslateDB(nil, c, "Access denied to this home")
		assert.Equal(t, "Access denied to this home", res)
	})

	t.Run("query parameter substitution", func(t *testing.T) {
		c, _ := gin.CreateTestContext(nil)
		res := TranslateDB(nil, c, "my_id query parameter is required")
		assert.Equal(t, "my_id query parameter is required", res)
	})

	t.Run("header parameter substitution", func(t *testing.T) {
		c, _ := gin.CreateTestContext(nil)
		res := TranslateDB(nil, c, "x-home-id header is required")
		assert.Equal(t, "x-home-id header is required", res)
	})

	t.Run("with cache hit - english", func(t *testing.T) {
		c, _ := gin.CreateTestContext(nil)
		userID := uuid.New()
		c.Set("userID", userID)

		userLangCache.Store(userID, "English")

		res := TranslateDB(nil, c, "Access denied to this home")
		assert.Equal(t, "Access denied to this home", res)
	})

	t.Run("with cache hit - turkish", func(t *testing.T) {
		c, _ := gin.CreateTestContext(nil)
		userID := uuid.New()
		c.Set("userID", userID)

		userLangCache.Store(userID, "Türkçe")

		res := TranslateDB(nil, c, "Access denied to this home")
		assert.Equal(t, "Bu eve erişim reddedildi", res)
	})

	t.Run("with db query - turkish", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		assert.NoError(t, err)
		defer db.Close()

		gormDB, err := gorm.Open(postgres.New(postgres.Config{
			Conn: db,
		}), &gorm.Config{})
		assert.NoError(t, err)

		c, _ := gin.CreateTestContext(nil)
		userID := uuid.New()
		langID := uuid.New()
		c.Set("userID", userID)

		// Clear cache for this user
		userLangCache.Delete(userID)

		// Mock the query
		mock.ExpectQuery(`SELECT "id","language_id" FROM "profiles" WHERE id = \$1 ORDER BY "profiles"."id" LIMIT \$2`).
			WithArgs(userID, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "language_id"}).AddRow(userID, langID))

        mock.ExpectQuery(`SELECT \* FROM "languages" WHERE "languages"."id" = \$1`).
            WithArgs(langID).
            WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(langID, "Türkçe"))

		res := TranslateDB(gormDB, c, "Access denied to this home")
		assert.Equal(t, "Bu eve erişim reddedildi", res)
	})

	t.Run("query parameter translation with turkish", func(t *testing.T) {
		c, _ := gin.CreateTestContext(nil)
		userID := uuid.New()
		c.Set("userID", userID)
		userLangCache.Store(userID, "Türkçe")

		res := TranslateDB(nil, c, "my_id query parameter is required")
		assert.Equal(t, "my_id sorgu parametresi gereklidir", res)
	})

	t.Run("header parameter translation with turkish", func(t *testing.T) {
		c, _ := gin.CreateTestContext(nil)
		userID := uuid.New()
		c.Set("userID", userID)
		userLangCache.Store(userID, "Türkçe")

		res := TranslateDB(nil, c, "x-home-id header is required")
		assert.Equal(t, "x-home-id başlığı gereklidir", res)
	})

    t.Run("missing translation in turkish", func(t *testing.T) {
		c, _ := gin.CreateTestContext(nil)
		userID := uuid.New()
		c.Set("userID", userID)
		userLangCache.Store(userID, "Türkçe")

		res := TranslateDB(nil, c, "Some random error")
		assert.Equal(t, "Some random error", res)
	})
}

func TestInvalidateUserLanguageCache(t *testing.T) {
	userID := uuid.New()
	userLangCache.Store(userID, "Türkçe")

	_, ok := userLangCache.Load(userID)
	assert.True(t, ok)

	InvalidateUserLanguageCache(userID)

	_, ok = userLangCache.Load(userID)
	assert.False(t, ok)
}
