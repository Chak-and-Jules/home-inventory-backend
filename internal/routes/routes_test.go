package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"os"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"github.com/DATA-DOG/go-sqlmock"
)

func TestSetupRouter(t *testing.T) {
	// Set required environment variables for middleware
	os.Setenv("SUPABASE_URL", "http://localhost")
	defer os.Unsetenv("SUPABASE_URL")

	db, _, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: db}), &gorm.Config{})
	require.NoError(t, err)

	router := SetupRouter(gormDB)
	assert.NotNil(t, router)

	routes := router.Routes()
	assert.Greater(t, len(routes), 0)

	// verify some known routes exist
	paths := make(map[string]bool)
	for _, route := range routes {
		paths[route.Path] = true
	}

	assert.True(t, paths["/api/v1/homes"])
	assert.True(t, paths["/api/v1/categories"])
	assert.True(t, paths["/api/v1/item-definitions"])
	assert.True(t, paths["/api/v1/inventory"])

	// Test a 404 response
	req, _ := http.NewRequest(http.MethodGet, "/invalid-route", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}
