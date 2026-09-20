package i18n

import (
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestLanguageAliases(t *testing.T) {
	for _, alias := range []string{"Türkçe", "Turkish", "tr", "türkçe", "turkish", "TR", "Tr"} {
		t.Run(alias, func(t *testing.T) { require.Equal(t, "Türkçe", getLanguageKey(alias)) })
	}
	for _, alias := range []string{"", "English", "en", "French"} {
		t.Run(alias, func(t *testing.T) { require.Equal(t, "English", getLanguageKey(alias)) })
	}
}

func TestTranslationTemplateFallbacks(t *testing.T) {
	for _, tt := range []struct {
		name         string
		translations map[string]string
		key, want    string
	}{
		{"exact precedes template", map[string]string{"home_id header is required": "exact", "id header is required": "id required"}, "home_id header is required", "exact"},
		{"missing query template", map[string]string{}, "home_id query parameter is required", "home_id query parameter is required"},
		{"missing header template", map[string]string{}, "home_id header is required", "home_id header is required"},
		{"template without placeholder", map[string]string{"id header is required": "required"}, "home_id header is required", "required"},
		{"first placeholder only", map[string]string{"id query parameter is required": "id and id"}, "home_id query parameter is required", "home_id and id"},
		{"unknown key", nil, "unknown", "unknown"},
	} {
		t.Run(tt.name, func(t *testing.T) { require.Equal(t, tt.want, executeTranslationTemplate(tt.translations, tt.key)) })
	}
}

func TestTranslateInvalidUserID(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	c.Set("userID", "not a UUID value")
	require.Equal(t, "Access denied to this home", TranslateDB(nil, c, "Access denied to this home"))
}

func TestUserLanguageDatabaseFallbackIsCached(t *testing.T) {
	for _, scenario := range []string{"database error", "missing profile", "no language", "empty language"} {
		t.Run(scenario, func(t *testing.T) {
			sqlDB, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = sqlDB.Close() })
			db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{})
			require.NoError(t, err)
			userID, languageID := uuid.New(), uuid.New()
			t.Cleanup(func() { InvalidateUserLanguageCache(userID) })
			query := mock.ExpectQuery(`SELECT "id","language_id" FROM "profiles" WHERE id = \$1 ORDER BY "profiles"\."id" LIMIT \$2`).WithArgs(userID, 1)
			switch scenario {
			case "database error":
				query.WillReturnError(errors.New("database offline"))
			case "missing profile":
				query.WillReturnRows(sqlmock.NewRows([]string{"id", "language_id"}))
			case "no language":
				query.WillReturnRows(sqlmock.NewRows([]string{"id", "language_id"}).AddRow(userID, nil))
			case "empty language":
				query.WillReturnRows(sqlmock.NewRows([]string{"id", "language_id"}).AddRow(userID, languageID))
				mock.ExpectQuery(`SELECT \* FROM "languages" WHERE "languages"\."id" = \$1`).WithArgs(languageID).
					WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(languageID, ""))
			}
			require.Equal(t, "English", getUserLanguage(db, userID))
			cached, ok := userLangCache.Load(userID)
			require.True(t, ok)
			require.Equal(t, "English", cached)
			require.Equal(t, "English", getUserLanguage(db, userID))
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
