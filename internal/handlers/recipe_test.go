package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/logger"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func setupTestDBForRecipe(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	logger.InitLogger()
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)

	gormDB, err := gorm.Open(postgres.New(postgres.Config{
		Conn: db,
	}), &gorm.Config{})
	assert.NoError(t, err)

	return gormDB, mock
}

func setupTestRouterForRecipe(handler *RecipeHandler, userID uuid.UUID) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userID", userID)
		c.Next()
	})

	v1 := r.Group("/api/v1")
	{
		recipes := v1.Group("/recipes")
		{
			recipes.GET("", handler.GetRecipes)
			recipes.GET("/suggestions", handler.GetRecipeSuggestions)
			recipes.POST("", handler.CreateRecipe)
			recipes.GET("/:id", handler.GetRecipe)
			recipes.PUT("/:id", handler.UpdateRecipe)
			recipes.DELETE("/:id", handler.DeleteRecipe)
			recipes.POST("/:id/cook", handler.CookRecipe)
			recipes.POST("/:id/meal-plan", handler.AddRecipeToShoppingList)
		}
	}
	return r
}

func mockVerifyHomeAccessForRecipe(mock sqlmock.Sqlmock, homeID, userID uuid.UUID, role string) {
	rows := sqlmock.NewRows([]string{"user_id", "home_id", "role", "is_default", "created_at", "updated_at"}).
		AddRow(userID, homeID, role, true, time.Now(), time.Now())
	mock.ExpectQuery(`(?i)SELECT \* FROM "user_homes" WHERE .*user_id.* = \$1 AND .*home_id.* = \$2.*`).
		WithArgs(userID, homeID, 1).
		WillReturnRows(rows)
}

func TestGetRecipes(t *testing.T) {
	gormDB, mock := setupTestDBForRecipe(t)
	handler := &RecipeHandler{DB: gormDB}
	userID := uuid.New()
	homeID := uuid.New()
	router := setupTestRouterForRecipe(handler, userID)

	t.Run("Success", func(t *testing.T) {
		mockVerifyHomeAccessForRecipe(mock, homeID, userID, "owner")

		recipeID := uuid.New()
		recipeRows := sqlmock.NewRows([]string{"id", "home_id", "name", "instructions", "servings", "created_at", "updated_at"}).
			AddRow(recipeID, homeID, "Pasta Carbonara", "Boil pasta and mix with sauce", 2, time.Now(), time.Now())

		mock.ExpectQuery(`(?i)SELECT \* FROM "recipes" WHERE home_id = \$1 ORDER BY name ASC`).
			WithArgs(homeID).
			WillReturnRows(recipeRows)

		itemDefID := uuid.New()
		ingRows := sqlmock.NewRows([]string{"id", "recipe_id", "item_definition_id", "quantity_required", "created_at", "updated_at"}).
			AddRow(uuid.New(), recipeID, itemDefID, 200, time.Now(), time.Now())

		mock.ExpectQuery(`(?i)SELECT \* FROM "recipe_ingredients" WHERE "recipe_ingredients"\."recipe_id" = \$1`).
			WithArgs(recipeID).
			WillReturnRows(ingRows)

		itemDefRows := sqlmock.NewRows([]string{"id", "home_id", "name"}).
			AddRow(itemDefID, homeID, "Pasta")

		mock.ExpectQuery(`(?i)SELECT \* FROM "item_definitions" WHERE "item_definitions"\."id" = \$1`).
			WithArgs(itemDefID).
			WillReturnRows(itemDefRows)

		req, _ := http.NewRequest("GET", "/api/v1/recipes", nil)
		req.Header.Set("X-Home-Id", homeID.String())
		resp := httptest.NewRecorder()

		router.ServeHTTP(resp, req)

		assert.Equal(t, http.StatusOK, resp.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("Missing Home Header", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/v1/recipes", nil)
		resp := httptest.NewRecorder()

		router.ServeHTTP(resp, req)

		assert.Equal(t, http.StatusBadRequest, resp.Code)
	})
}

func TestGetRecipe(t *testing.T) {
	gormDB, mock := setupTestDBForRecipe(t)
	handler := &RecipeHandler{DB: gormDB}
	userID := uuid.New()
	homeID := uuid.New()
	recipeID := uuid.New()
	itemDefID := uuid.New()
	router := setupTestRouterForRecipe(handler, userID)

	t.Run("Success", func(t *testing.T) {
		recipeRows := sqlmock.NewRows([]string{"id", "home_id", "name", "instructions", "servings", "created_at", "updated_at"}).
			AddRow(recipeID, homeID, "Pasta Carbonara", "Boil pasta", 2, time.Now(), time.Now())

		mock.ExpectQuery(`(?i)SELECT \* FROM "recipes" WHERE "recipes"\."id" = \$1 ORDER BY "recipes"\."id" LIMIT \$2`).
			WithArgs(recipeID, 1).
			WillReturnRows(recipeRows)

		ingRows := sqlmock.NewRows([]string{"id", "recipe_id", "item_definition_id", "quantity_required", "created_at", "updated_at"}).
			AddRow(uuid.New(), recipeID, itemDefID, 200, time.Now(), time.Now())

		mock.ExpectQuery(`(?i)SELECT \* FROM "recipe_ingredients" WHERE "recipe_ingredients"\."recipe_id" = \$1`).
			WithArgs(recipeID).
			WillReturnRows(ingRows)

		itemDefRows := sqlmock.NewRows([]string{"id", "home_id", "name"}).
			AddRow(itemDefID, homeID, "Pasta")

		mock.ExpectQuery(`(?i)SELECT \* FROM "item_definitions" WHERE "item_definitions"\."id" = \$1`).
			WithArgs(itemDefID).
			WillReturnRows(itemDefRows)

		mockVerifyHomeAccessForRecipe(mock, homeID, userID, "owner")

		req, _ := http.NewRequest("GET", "/api/v1/recipes/"+recipeID.String(), nil)
		resp := httptest.NewRecorder()

		router.ServeHTTP(resp, req)

		assert.Equal(t, http.StatusOK, resp.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("Not Found", func(t *testing.T) {
		mock.ExpectQuery(`(?i)SELECT \* FROM "recipes" WHERE "recipes"\."id" = \$1 ORDER BY "recipes"\."id" LIMIT \$2`).
			WithArgs(recipeID, 1).
			WillReturnError(gorm.ErrRecordNotFound)

		req, _ := http.NewRequest("GET", "/api/v1/recipes/"+recipeID.String(), nil)
		resp := httptest.NewRecorder()

		router.ServeHTTP(resp, req)

		assert.Equal(t, http.StatusNotFound, resp.Code)
	})
}

func TestCreateRecipe(t *testing.T) {
	gormDB, mock := setupTestDBForRecipe(t)
	handler := &RecipeHandler{DB: gormDB}
	userID := uuid.New()
	homeID := uuid.New()
	itemDefID := uuid.New()
	router := setupTestRouterForRecipe(handler, userID)

	t.Run("Success", func(t *testing.T) {
		mockVerifyHomeAccessForRecipe(mock, homeID, userID, "owner")

		defRows := sqlmock.NewRows([]string{"id", "home_id", "name", "created_at", "updated_at"}).
			AddRow(itemDefID, homeID, "Spaghetti", time.Now(), time.Now())
		mock.ExpectQuery(`(?i)SELECT \* FROM "item_definitions" WHERE id = \$1 AND home_id = \$2.*`).
			WithArgs(itemDefID, homeID, 1).
			WillReturnRows(defRows)

		mock.ExpectBegin()
		mock.ExpectQuery(`(?i)INSERT INTO "recipes"`).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))
		mock.ExpectQuery(`(?i)INSERT INTO "recipe_ingredients"`).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))
		mock.ExpectCommit()

		recipeRows := sqlmock.NewRows([]string{"id", "home_id", "name", "instructions", "servings", "created_at", "updated_at"}).
			AddRow(uuid.New(), homeID, "Spaghetti Bolognese", "Cook sauce", 4, time.Now(), time.Now())
		mock.ExpectQuery(`(?i)SELECT \* FROM "recipes" WHERE "recipes"\."id" = \$1 ORDER BY "recipes"\."id" LIMIT \$2`).
			WillReturnRows(recipeRows)

		ingRows := sqlmock.NewRows([]string{"id", "recipe_id", "item_definition_id", "quantity_required", "created_at", "updated_at"}).
			AddRow(uuid.New(), uuid.New(), itemDefID, 500, time.Now(), time.Now())
		mock.ExpectQuery(`(?i)SELECT \* FROM "recipe_ingredients" WHERE "recipe_ingredients"\."recipe_id" = \$1`).
			WillReturnRows(ingRows)

		body := RecipeRequest{
			Name:         "Spaghetti Bolognese",
			Instructions: "Cook sauce",
			Servings:     4,
			Ingredients: []RecipeIngredientRequest{
				{ItemDefinitionID: itemDefID, QuantityRequired: 500},
			},
		}
		jsonBody, _ := json.Marshal(body)

		req, _ := http.NewRequest("POST", "/api/v1/recipes", bytes.NewBuffer(jsonBody))
		req.Header.Set("X-Home-Id", homeID.String())
		req.Header.Set("Content-Type", "application/json")
		resp := httptest.NewRecorder()

		router.ServeHTTP(resp, req)

		assert.Equal(t, http.StatusCreated, resp.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("Viewer Access Denied", func(t *testing.T) {
		mockVerifyHomeAccessForRecipe(mock, homeID, userID, "viewer")

		body := RecipeRequest{Name: "Spaghetti Bolognese"}
		jsonBody, _ := json.Marshal(body)

		req, _ := http.NewRequest("POST", "/api/v1/recipes", bytes.NewBuffer(jsonBody))
		req.Header.Set("X-Home-Id", homeID.String())
		req.Header.Set("Content-Type", "application/json")
		resp := httptest.NewRecorder()

		router.ServeHTTP(resp, req)

		assert.Equal(t, http.StatusForbidden, resp.Code)
	})
}

func TestDeleteRecipe(t *testing.T) {
	gormDB, mock := setupTestDBForRecipe(t)
	handler := &RecipeHandler{DB: gormDB}
	userID := uuid.New()
	homeID := uuid.New()
	recipeID := uuid.New()
	router := setupTestRouterForRecipe(handler, userID)

	t.Run("Success", func(t *testing.T) {
		recipeRows := sqlmock.NewRows([]string{"id", "home_id", "name"}).
			AddRow(recipeID, homeID, "Soup")
		mock.ExpectQuery(`(?i)SELECT \* FROM "recipes" WHERE "recipes"\."id" = \$1 ORDER BY "recipes"\."id" LIMIT \$2`).
			WithArgs(recipeID, 1).
			WillReturnRows(recipeRows)

		mockVerifyHomeAccessForRecipe(mock, homeID, userID, "owner")

		mock.ExpectBegin()
		mock.ExpectExec(`(?i)DELETE FROM "recipes" WHERE "recipes"\."id" = \$1`).
			WithArgs(recipeID).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		req, _ := http.NewRequest("DELETE", "/api/v1/recipes/"+recipeID.String(), nil)
		resp := httptest.NewRecorder()

		router.ServeHTTP(resp, req)

		assert.Equal(t, http.StatusOK, resp.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestCookRecipe(t *testing.T) {
	gormDB, mock := setupTestDBForRecipe(t)
	handler := &RecipeHandler{DB: gormDB}
	userID := uuid.New()
	homeID := uuid.New()
	recipeID := uuid.New()
	itemDefID := uuid.New()
	invItemID := uuid.New()
	router := setupTestRouterForRecipe(handler, userID)

	t.Run("Success Cook FEFO", func(t *testing.T) {
		recipeRows := sqlmock.NewRows([]string{"id", "home_id", "name"}).
			AddRow(recipeID, homeID, "Omelette")
		mock.ExpectQuery(`(?i)SELECT \* FROM "recipes" WHERE "recipes"\."id" = \$1 ORDER BY "recipes"\."id" LIMIT \$2`).
			WithArgs(recipeID, 1).
			WillReturnRows(recipeRows)

		ingRows := sqlmock.NewRows([]string{"id", "recipe_id", "item_definition_id", "quantity_required"}).
			AddRow(uuid.New(), recipeID, itemDefID, 2)
		mock.ExpectQuery(`(?i)SELECT \* FROM "recipe_ingredients" WHERE "recipe_ingredients"\."recipe_id" = \$1`).
			WithArgs(recipeID).
			WillReturnRows(ingRows)

		mockVerifyHomeAccessForRecipe(mock, homeID, userID, "owner")

		mock.ExpectBegin()
		expDate := time.Now().AddDate(0, 0, 2)
		invRows := sqlmock.NewRows([]string{"id", "home_id", "item_definition_id", "quantity", "expiration_date"}).
			AddRow(invItemID, homeID, itemDefID, 5.0, expDate)
		mock.ExpectQuery(`(?i)SELECT \* FROM "inventory_items" WHERE home_id = \$1 AND item_definition_id = \$2 AND \(expiration_date IS NULL OR expiration_date > \$3\) ORDER BY expiration_date ASC NULLS LAST`).
			WithArgs(homeID, itemDefID, sqlmock.AnyArg()).
			WillReturnRows(invRows)

		// Update inventory item quantity
		mock.ExpectExec(`(?i)UPDATE "inventory_items" SET "quantity"=\$1,"updated_at"=\$2 WHERE "id" = \$3`).
			WithArgs(3.0, sqlmock.AnyArg(), invItemID).
			WillReturnResult(sqlmock.NewResult(1, 1))

		// Insert transaction log
		mock.ExpectQuery(`(?i)INSERT INTO "inventory_transactions"`).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))

		// Update shopping list check
		itemDefRows := sqlmock.NewRows([]string{"id", "home_id", "name", "low_stock_threshold", "target_quantity"}).
			AddRow(itemDefID, homeID, "Eggs", nil, nil)
		mock.ExpectQuery(`(?i)SELECT \* FROM "item_definitions" WHERE "item_definitions"\."id" = \$1 ORDER BY "item_definitions"\."id" LIMIT \$2`).
			WithArgs(itemDefID, 1).
			WillReturnRows(itemDefRows)

		shopRows := sqlmock.NewRows([]string{"id", "home_id"})
		mock.ExpectQuery(`(?i)SELECT \* FROM "shopping_list_items" WHERE home_id = \$1 AND item_definition_id = \$2 AND is_auto_generated = \$3 AND is_bought = \$4 ORDER BY "shopping_list_items"\."id" LIMIT \$5`).
			WithArgs(homeID, itemDefID, true, false, 1).
			WillReturnRows(shopRows)

		mock.ExpectCommit()

		req, _ := http.NewRequest("POST", "/api/v1/recipes/"+recipeID.String()+"/cook", nil)
		resp := httptest.NewRecorder()

		router.ServeHTTP(resp, req)

		assert.Equal(t, http.StatusOK, resp.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("Insufficient Stock", func(t *testing.T) {
		recipeRows := sqlmock.NewRows([]string{"id", "home_id", "name"}).
			AddRow(recipeID, homeID, "Omelette")
		mock.ExpectQuery(`(?i)SELECT \* FROM "recipes" WHERE "recipes"\."id" = \$1 ORDER BY "recipes"\."id" LIMIT \$2`).
			WithArgs(recipeID, 1).
			WillReturnRows(recipeRows)

		ingRows := sqlmock.NewRows([]string{"id", "recipe_id", "item_definition_id", "quantity_required"}).
			AddRow(uuid.New(), recipeID, itemDefID, 10)
		mock.ExpectQuery(`(?i)SELECT \* FROM "recipe_ingredients" WHERE "recipe_ingredients"\."recipe_id" = \$1`).
			WithArgs(recipeID).
			WillReturnRows(ingRows)

		mockVerifyHomeAccessForRecipe(mock, homeID, userID, "owner")

		mock.ExpectBegin()
		invRows := sqlmock.NewRows([]string{"id", "home_id", "item_definition_id", "quantity", "expiration_date"}).
			AddRow(invItemID, homeID, itemDefID, 2.0, time.Now().AddDate(0, 0, 1))
		mock.ExpectQuery(`(?i)SELECT \* FROM "inventory_items" WHERE home_id = \$1 AND item_definition_id = \$2 AND \(expiration_date IS NULL OR expiration_date > \$3\) ORDER BY expiration_date ASC NULLS LAST`).
			WithArgs(homeID, itemDefID, sqlmock.AnyArg()).
			WillReturnRows(invRows)

		mock.ExpectRollback()

		req, _ := http.NewRequest("POST", "/api/v1/recipes/"+recipeID.String()+"/cook", nil)
		resp := httptest.NewRecorder()

		router.ServeHTTP(resp, req)

		assert.Equal(t, http.StatusBadRequest, resp.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestGetRecipeSuggestions(t *testing.T) {
	gormDB, mock := setupTestDBForRecipe(t)
	handler := &RecipeHandler{DB: gormDB}
	userID := uuid.New()
	homeID := uuid.New()
	recipeID1 := uuid.New()
	recipeID2 := uuid.New()
	itemDefID1 := uuid.New()
	itemDefID2 := uuid.New()
	router := setupTestRouterForRecipe(handler, userID)

	t.Run("Prioritize Expiring Soon and Can Cook", func(t *testing.T) {
		mockVerifyHomeAccessForRecipe(mock, homeID, userID, "owner")

		recipeRows := sqlmock.NewRows([]string{"id", "home_id", "name", "instructions", "servings"}).
			AddRow(recipeID1, homeID, "Pancakes", "Mix and fry", 2).
			AddRow(recipeID2, homeID, "Milkshake", "Blend", 1)
		mock.ExpectQuery(`(?i)SELECT \* FROM "recipes" WHERE home_id = \$1`).
			WithArgs(homeID).
			WillReturnRows(recipeRows)

		ingRows := sqlmock.NewRows([]string{"id", "recipe_id", "item_definition_id", "quantity_required"}).
			AddRow(uuid.New(), recipeID1, itemDefID1, 1).
			AddRow(uuid.New(), recipeID2, itemDefID2, 1)
		mock.ExpectQuery(`(?i)SELECT \* FROM "recipe_ingredients" WHERE "recipe_ingredients"\."recipe_id" IN \(\$1,\$2\)`).
			WithArgs(recipeID1, recipeID2).
			WillReturnRows(ingRows)

		itemDefRows := sqlmock.NewRows([]string{"id", "home_id", "name"}).
			AddRow(itemDefID1, homeID, "Flour").
			AddRow(itemDefID2, homeID, "Milk")
		mock.ExpectQuery(`(?i)SELECT \* FROM "item_definitions" WHERE "item_definitions"\."id" IN \(\$1,\$2\)`).
			WithArgs(itemDefID1, itemDefID2).
			WillReturnRows(itemDefRows)

		expiringDate := time.Now().AddDate(0, 0, 2)
		invRows := sqlmock.NewRows([]string{"id", "home_id", "item_definition_id", "quantity", "expiration_date"}).
			AddRow(uuid.New(), homeID, itemDefID2, 2.0, expiringDate)
		mock.ExpectQuery(`(?i)SELECT \* FROM "inventory_items" WHERE home_id = \$1 AND \(expiration_date IS NULL OR expiration_date > \$2\)`).
			WithArgs(homeID, sqlmock.AnyArg()).
			WillReturnRows(invRows)

		req, _ := http.NewRequest("GET", "/api/v1/recipes/suggestions", nil)
		req.Header.Set("X-Home-Id", homeID.String())
		resp := httptest.NewRecorder()

		router.ServeHTTP(resp, req)

		assert.Equal(t, http.StatusOK, resp.Code)

		var suggestions []RecipeSuggestion
		err := json.Unmarshal(resp.Body.Bytes(), &suggestions)
		assert.NoError(t, err)
		assert.Len(t, suggestions, 2)
		assert.Equal(t, "Milkshake", suggestions[0].Recipe.Name)
		assert.True(t, suggestions[0].UsesExpiringSoon)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestAddRecipeToShoppingList(t *testing.T) {
	gormDB, mock := setupTestDBForRecipe(t)
	handler := &RecipeHandler{DB: gormDB}
	userID := uuid.New()
	homeID := uuid.New()
	recipeID := uuid.New()
	itemDefID := uuid.New()
	router := setupTestRouterForRecipe(handler, userID)

	t.Run("Success Meal Plan Integration", func(t *testing.T) {
		recipeRows := sqlmock.NewRows([]string{"id", "home_id", "name"}).
			AddRow(recipeID, homeID, "Salad")
		mock.ExpectQuery(`(?i)SELECT \* FROM "recipes" WHERE "recipes"\."id" = \$1 ORDER BY "recipes"\."id" LIMIT \$2`).
			WithArgs(recipeID, 1).
			WillReturnRows(recipeRows)

		ingRows := sqlmock.NewRows([]string{"id", "recipe_id", "item_definition_id", "quantity_required"}).
			AddRow(uuid.New(), recipeID, itemDefID, 3)
		mock.ExpectQuery(`(?i)SELECT \* FROM "recipe_ingredients" WHERE "recipe_ingredients"\."recipe_id" = \$1`).
			WithArgs(recipeID).
			WillReturnRows(ingRows)

		itemDefRows := sqlmock.NewRows([]string{"id", "home_id", "name"}).
			AddRow(itemDefID, homeID, "Tomatoes")
		mock.ExpectQuery(`(?i)SELECT \* FROM "item_definitions" WHERE "item_definitions"\."id" = \$1`).
			WithArgs(itemDefID).
			WillReturnRows(itemDefRows)

		mockVerifyHomeAccessForRecipe(mock, homeID, userID, "owner")

		mock.ExpectBegin()
		// Current stock = 1 (missing = 2)
		sumRows := sqlmock.NewRows([]string{"COALESCE(SUM(quantity), 0)"}).AddRow(1.0)
		mock.ExpectQuery(`(?i)SELECT COALESCE\(SUM\(quantity\), 0\) FROM "inventory_items" WHERE home_id = \$1 AND item_definition_id = \$2 AND \(expiration_date IS NULL OR expiration_date > \$3\)`).
			WithArgs(homeID, itemDefID, sqlmock.AnyArg()).
			WillReturnRows(sumRows)

		// Check existing shopping list item
		mock.ExpectQuery(`(?i)SELECT \* FROM "shopping_list_items" WHERE home_id = \$1 AND item_definition_id = \$2 AND is_bought = \$3 ORDER BY "shopping_list_items"\."id" LIMIT \$4`).
			WithArgs(homeID, itemDefID, false, 1).
			WillReturnError(gorm.ErrRecordNotFound)

		// Create shopping list item
		mock.ExpectQuery(`(?i)INSERT INTO "shopping_list_items"`).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(uuid.New()))

		mock.ExpectCommit()

		req, _ := http.NewRequest("POST", "/api/v1/recipes/"+recipeID.String()+"/meal-plan", nil)
		resp := httptest.NewRecorder()

		router.ServeHTTP(resp, req)

		assert.Equal(t, http.StatusOK, resp.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}
