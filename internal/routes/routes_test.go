package routes

import (
	"github.com/Chak-and-Jules/home-inventory-backend/internal/logger"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestSetupRouter(t *testing.T) {
	logger.InitLogger()

	db, _, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: db}), &gorm.Config{})
	require.NoError(t, err)

	router := SetupRouter(gormDB, nil, nil)
	assert.NotNil(t, router)

	routes := router.Routes()
	assert.Greater(t, len(routes), 0)

	// verify some known routes exist
	paths := make(map[string]bool)
	methods := make(map[string]bool)
	for _, route := range routes {
		paths[route.Path] = true
		methods[route.Method+" "+route.Path] = true
	}

	assert.True(t, paths["/api/v1/homes"])
	assert.True(t, methods["GET /api/v1/guides"])
	assert.True(t, methods["POST /api/v1/guides/:id/actions"])
	assert.True(t, paths["/api/v1/profiles/sync"])
	assert.True(t, paths["/api/v1/categories"])
	assert.True(t, paths["/api/v1/item-definitions"])
	assert.True(t, paths["/api/v1/size-units"])
	assert.True(t, paths["/api/v1/inventory"])
	assert.True(t, methods["PATCH /api/v1/inventory/:id"])
	assert.True(t, methods["POST /api/v1/images"])
	assert.True(t, methods["DELETE /api/v1/images"])
	assert.True(t, methods["POST /api/v1/images/signed-urls"])

	// Test a 404 response
	req, _ := http.NewRequest(http.MethodGet, "/invalid-route", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)

	// Every route, including the deletion alias, must reject missing credentials.
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/homes"},
		{http.MethodGet, "/api/v1/guides?platform=web&client_version=1"},
		{http.MethodPost, "/api/v1/guides/introduction/actions"},
		{http.MethodPost, "/api/v1/profiles/sync"},
		{http.MethodPost, "/api/v1/profiles/delete-account"},
		{http.MethodPost, "/api/v1/account/delete-request"},
		{http.MethodPost, "/api/v1/images"},
		{http.MethodDelete, "/api/v1/images"},
		{http.MethodPost, "/api/v1/images/signed-urls"},
	} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(route.method, route.path, nil))
		assert.Equal(t, http.StatusUnauthorized, w.Code, route.path)
	}
}
