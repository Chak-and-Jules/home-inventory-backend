package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/i18n"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func categoryFixture(t *testing.T) *unitFixture {
	t.Helper()
	f := newUnitFixture(t)
	h := &CategoryHandler{DB: f.db}
	f.router.GET("/categories", h.GetCategories)
	f.router.POST("/categories", h.CreateCategory)
	f.router.PUT("/categories/:id", h.UpdateCategory)
	f.router.DELETE("/categories/:id", h.DeleteCategory)
	return f
}

func createCategory(t *testing.T, f *unitFixture, name string, parent *uuid.UUID) models.Category {
	t.Helper()
	w := f.request(t, "POST", "/categories", CategoryRequest{Name: name, ParentID: parent}, 201)
	var result models.Category
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	return result
}

func TestCategoryHierarchyRejectsInvalidWrites(t *testing.T) {
	f := categoryFixture(t)
	root := createCategory(t, f, "Kitchen", nil)
	child := createCategory(t, f, "Drinks", &root.ID)
	other := createCategory(t, f, "Household", nil)
	leaf := createCategory(t, f, "Soda", nil)
	require.NoError(t, f.db.Model(&f.def).Update("category_id", child.ID).Error)
	foreignHome := models.Home{ID: uuid.New(), Name: "Other home"}
	require.NoError(t, f.db.Create(&foreignHome).Error)
	foreign := models.Category{ID: uuid.New(), HomeID: foreignHome.ID, Name: "Private category"}
	require.NoError(t, f.db.Create(&foreign).Error)
	missing := uuid.New()
	for _, tc := range []struct {
		name, method, path string
		body               CategoryRequest
		status             int
		message            string
	}{
		{"create third level", "POST", "/categories", CategoryRequest{"Juice", &child.ID}, 400, "Parent category must be a top-level category"},
		{"move leaf to third level", "PUT", "/categories/" + leaf.ID.String(), CategoryRequest{"Soda", &child.ID}, 400, "Parent category must be a top-level category"},
		{"move branch under root", "PUT", "/categories/" + root.ID.String(), CategoryRequest{"Moved kitchen", &other.ID}, 400, "A category with children cannot become a child category"},
		{"ancestor cycle", "PUT", "/categories/" + root.ID.String(), CategoryRequest{"Kitchen", &child.ID}, 400, "Parent category must be a top-level category"},
		{"self parent", "PUT", "/categories/" + leaf.ID.String(), CategoryRequest{"Soda", &leaf.ID}, 400, "A category cannot be its own parent"},
		{"cross-home create", "POST", "/categories", CategoryRequest{"Juice", &foreign.ID}, 400, "Parent category must belong to the same home"},
		{"cross-home update", "PUT", "/categories/" + leaf.ID.String(), CategoryRequest{"Soda", &foreign.ID}, 400, "Parent category must belong to the same home"},
		{"missing create parent", "POST", "/categories", CategoryRequest{"Juice", &missing}, 404, "Parent category not found"},
		{"missing update parent", "PUT", "/categories/" + leaf.ID.String(), CategoryRequest{"Soda", &missing}, 404, "Parent category not found"},
		{"duplicate root", "POST", "/categories", CategoryRequest{"K i T c H e N", nil}, 409, "A category with this name already exists at this level"},
		{"duplicate sibling", "POST", "/categories", CategoryRequest{"d r i n k s", &root.ID}, 409, "A category with this name already exists at this level"},
		{"duplicate rename", "PUT", "/categories/" + leaf.ID.String(), CategoryRequest{"kitchen", nil}, 409, "A category with this name already exists at this level"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var before, after []models.Category
			require.NoError(t, f.db.Order("id").Find(&before).Error)
			w := f.request(t, tc.method, tc.path, tc.body, tc.status)
			require.Contains(t, w.Body.String(), tc.message)
			require.NoError(t, f.db.Order("id").Find(&after).Error)
			require.Equal(t, before, after, "rejection must not rename, reparent or insert anything")
			var definition models.ItemDefinition
			require.NoError(t, f.db.First(&definition, "id = ?", f.def.ID).Error)
			require.Equal(t, &child.ID, definition.CategoryID)
		})
	}
}

func TestCategoryHierarchyValidOperationsAndPermissions(t *testing.T) {
	for _, role := range []string{models.RoleOwner, models.RoleEditor} {
		t.Run(role, func(t *testing.T) {
			f := categoryFixture(t)
			require.NoError(t, f.db.Model(&models.UserHome{}).Where("user_id = ? AND home_id = ?", f.user, f.home).Update("role", role).Error)
			first := createCategory(t, f, "Kitchen", nil)
			second := createCategory(t, f, "Living room", nil)
			child := createCategory(t, f, "Drinks", &first.ID)
			createCategory(t, f, "Snacks", &first.ID)
			createCategory(t, f, "Snacks", &second.ID) // Same name in different sibling groups.
			for _, move := range []CategoryRequest{{"Beverages", &first.ID}, {"Beverages", &second.ID}, {"Beverages", nil}} {
				f.request(t, "PUT", "/categories/"+child.ID.String(), move, 200)
				var persisted models.Category
				require.NoError(t, f.db.First(&persisted, "id = ?", child.ID).Error)
				require.Equal(t, move.ParentID, persisted.ParentID)
				require.Equal(t, move.Name, persisted.Name)
			}
			// A root with children may still be renamed while remaining a root.
			f.request(t, "PUT", "/categories/"+first.ID.String(), CategoryRequest{"Renamed kitchen", nil}, 200)
			f.request(t, "DELETE", "/categories/"+child.ID.String(), nil, 200)
			f.request(t, "GET", "/categories", nil, 200)
		})
	}
	f := categoryFixture(t)
	root := createCategory(t, f, "Kitchen", nil)
	require.NoError(t, f.db.Model(&models.UserHome{}).Where("user_id = ? AND home_id = ?", f.user, f.home).Update("role", models.RoleViewer).Error)
	f.request(t, "GET", "/categories", nil, 200)
	for _, method := range []string{"POST", "PUT", "DELETE"} {
		path := "/categories"
		if method != "POST" {
			path += "/" + root.ID.String()
		}
		f.request(t, method, path, CategoryRequest{Name: "Unauthorized"}, 403)
	}
	f.user = uuid.New() // A nonmember cannot list or modify this home.
	f.request(t, "GET", "/categories", nil, 403)
	f.request(t, "POST", "/categories", CategoryRequest{Name: "Unauthorized"}, 403)
	f.request(t, "PUT", "/categories/"+root.ID.String(), CategoryRequest{Name: "Unauthorized"}, 403)
}

func TestCategoryHierarchyLegacyListingIsBoundedAndReadOnly(t *testing.T) {
	for _, corruption := range []string{"deep tree", "two-node cycle", "self cycle", "cross-home parent"} {
		t.Run(corruption, func(t *testing.T) {
			f := categoryFixture(t)
			root := createCategory(t, f, "Kitchen", nil)
			child := createCategory(t, f, "Drinks", &root.ID)
			switch corruption {
			case "deep tree":
				require.NoError(t, f.db.Create(&models.Category{ID: uuid.New(), HomeID: f.home, Name: "Soda", ParentID: &child.ID}).Error)
			case "two-node cycle":
				require.NoError(t, f.db.Model(&root).Update("parent_id", child.ID).Error)
			case "self cycle":
				require.NoError(t, f.db.Model(&root).Update("parent_id", root.ID).Error)
			case "cross-home parent":
				other := models.Home{ID: uuid.New(), Name: "Private"}
				require.NoError(t, f.db.Create(&other).Error)
				foreign := models.Category{ID: uuid.New(), HomeID: other.ID, Name: "Secret"}
				require.NoError(t, f.db.Create(&foreign).Error)
				require.NoError(t, f.db.Model(&root).Update("parent_id", foreign.ID).Error)
			}
			var before, after []models.Category
			require.NoError(t, f.db.Where("home_id = ?", f.home).Order("id").Find(&before).Error)
			w := f.request(t, "GET", "/categories", nil, 200)
			require.True(t, json.Valid(w.Body.Bytes()))
			var listed []models.Category
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &listed))
			require.Len(t, listed, len(before))
			for _, category := range listed {
				require.Equal(t, f.home, category.HomeID)
				if category.Parent != nil {
					require.Equal(t, f.home, category.Parent.HomeID)
					require.Equal(t, category.ParentID, &category.Parent.ID)
					require.Nil(t, category.Parent.Parent)
				}
			}
			require.NotContains(t, w.Body.String(), "Secret")
			require.NoError(t, f.db.Where("home_id = ?", f.home).Order("id").Find(&after).Error)
			require.Equal(t, before, after)
		})
	}
}

func TestCategoryHierarchyRollbackAndLocalization(t *testing.T) {
	f := categoryFixture(t)
	root := createCategory(t, f, "Kitchen", nil)
	child := createCategory(t, f, "Drinks", &root.ID)
	other := createCategory(t, f, "Household", nil)
	lang := models.Language{ID: uuid.New(), Name: "Türkçe"}
	require.NoError(t, f.db.Create(&lang).Error)
	require.NoError(t, f.db.Model(&models.Profile{}).Where("id = ?", f.user).Update("language_id", lang.ID).Error)
	i18n.InvalidateUserLanguageCache(f.user)
	w := f.request(t, "POST", "/categories", CategoryRequest{"Juice", &child.ID}, 400)
	require.Contains(t, w.Body.String(), "Üst kategori en üst düzeyde bir kategori olmalıdır")
	w = f.request(t, "PUT", "/categories/"+root.ID.String(), CategoryRequest{"Kitchen", &other.ID}, 400)
	require.Contains(t, w.Body.String(), "Alt kategorileri olan bir kategori, alt kategori yapılamaz")
	require.NoError(t, f.db.Callback().Update().After("gorm:update").Register("category149:rollback", func(tx *gorm.DB) {
		if tx.Statement.Table == "categories" {
			tx.AddError(errors.New("injected failure after category update"))
		}
	}))
	f.request(t, "PUT", "/categories/"+child.ID.String(), CategoryRequest{"Changed", &other.ID}, 500)
	require.NoError(t, f.db.Callback().Update().Remove("category149:rollback"))
	var persisted models.Category
	require.NoError(t, f.db.First(&persisted, "id = ?", child.ID).Error)
	require.Equal(t, "Drinks", persisted.Name)
	require.Equal(t, &root.ID, persisted.ParentID)
	// The same operation works after rollback; no lock or partial write remains.
	f.request(t, "PUT", "/categories/"+child.ID.String(), CategoryRequest{"Changed", &other.ID}, 200)
}

func TestCategoryHierarchyConcurrentWritesPostgres(t *testing.T) {
	if os.Getenv("UNIT_TEST_POSTGRES_DSN") == "" {
		t.Skip("requires disposable PostgreSQL for row-lock concurrency")
	}
	f := categoryFixture(t)
	for _, scenario := range []string{"opposing moves", "create versus move"} {
		t.Run(scenario, func(t *testing.T) {
			for i := range 8 {
				a := createCategory(t, f, fmt.Sprintf("%s A%d", scenario, i), nil)
				b := createCategory(t, f, fmt.Sprintf("%s B%d", scenario, i), nil)
				commands := []struct {
					method, path string
					body         CategoryRequest
				}{
					{"PUT", "/categories/" + a.ID.String(), CategoryRequest{a.Name, &b.ID}},
					{"PUT", "/categories/" + b.ID.String(), CategoryRequest{b.Name, &a.ID}},
				}
				if scenario == "create versus move" {
					commands[1].method, commands[1].path = "POST", "/categories"
					commands[1].body = CategoryRequest{fmt.Sprintf("Child%d", i), &a.ID}
				}
				ready := make(chan struct{})
				results := make(chan *httptest.ResponseRecorder, 2)
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				for _, command := range commands {
					body, err := json.Marshal(command.body)
					require.NoError(t, err)
					req := httptest.NewRequest(command.method, command.path, bytes.NewReader(body)).WithContext(ctx)
					req.Header.Set("Content-Type", "application/json")
					req.Header.Set("X-Home-Id", f.home.String())
					go func() {
						<-ready
						w := httptest.NewRecorder()
						f.router.ServeHTTP(w, req)
						results <- w
					}()
				}
				close(ready)
				statuses := map[int]int{}
				for range 2 {
					w := <-results
					require.Contains(t, []int{200, 201, 400}, w.Code, w.Body.String())
					statuses[w.Code]++
				}
				cancel()
				require.Equal(t, 1, statuses[400], "one conflicting operation must be rejected")
				var violations int64
				require.NoError(t, f.db.Raw("SELECT count(*) FROM categories c JOIN categories p ON p.id = c.parent_id WHERE c.home_id = ? AND p.parent_id IS NOT NULL", f.home).Scan(&violations).Error)
				require.Zero(t, violations)
			}
		})
	}
}
