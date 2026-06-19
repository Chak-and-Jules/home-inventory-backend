package routes

import (
	"github.com/Chak-and-Jules/home-inventory-backend/internal/handlers"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/logger"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/middleware"
	ginzap "github.com/gin-contrib/zap"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"time"
)

func SetupRouter(db *gorm.DB) *gin.Engine {
	r := gin.New()
	r.Use(ginzap.Ginzap(logger.Log, time.RFC3339, true))
	r.Use(ginzap.RecoveryWithZap(logger.Log, true))

	r.Use(middleware.CORSMiddleware())

	r.Use(middleware.RateLimitMiddleware())

	// CORS middleware can be added here if needed

	// Initialize handlers
	homeHandler := &handlers.HomeHandler{DB: db}
	profileHandler := &handlers.ProfileHandler{DB: db}
	categoryHandler := &handlers.CategoryHandler{DB: db}
	itemDefHandler := &handlers.ItemDefinitionHandler{DB: db}
	sizeUnitHandler := &handlers.SizeUnitHandler{DB: db}
	languageHandler := &handlers.LanguageHandler{DB: db}
	inventoryItemHandler := &handlers.InventoryItemHandler{DB: db}

	// API v1 group
	v1 := r.Group("/api/v1")
	v1.Use(middleware.SupabaseAuthMiddleware())
	{
		// Profiles
		profiles := v1.Group("/profiles")
		{
			profiles.GET("", profileHandler.GetProfile)
			profiles.PUT("", profileHandler.UpdateProfile)
			profiles.POST("/sync", profileHandler.SyncProfile)
		}

		// Homes
		homes := v1.Group("/homes")
		{
			homes.GET("", homeHandler.GetHomes)
			homes.POST("", homeHandler.CreateHome)
			homes.PUT("/:id", homeHandler.UpdateHome)
			homes.DELETE("/:id", homeHandler.DeleteHome)
			homes.POST("/:id/default", homeHandler.SetDefaultHome)
		}

		// Categories
		categories := v1.Group("/categories")
		{
			categories.GET("", categoryHandler.GetCategories)
			categories.POST("", categoryHandler.CreateCategory)
			categories.PUT("/:id", categoryHandler.UpdateCategory)
			categories.DELETE("/:id", categoryHandler.DeleteCategory)
		}

		// Item Definitions
		itemDefs := v1.Group("/item-definitions")
		{
			itemDefs.GET("", itemDefHandler.GetItemDefinitions)
			itemDefs.POST("", itemDefHandler.CreateItemDefinition)
			itemDefs.PUT("/:id", itemDefHandler.UpdateItemDefinition)
			itemDefs.DELETE("/:id", itemDefHandler.DeleteItemDefinition)
		}

		// Size Units
		sizeUnits := v1.Group("/size-units")
		{
			sizeUnits.GET("", sizeUnitHandler.GetSizeUnits)
		}

		// Languages
		languages := v1.Group("/languages")
		{
			languages.GET("", languageHandler.GetLanguages)
		} // Inventory Items
		inventory := v1.Group("/inventory")
		{
			inventory.GET("", inventoryItemHandler.GetInventoryItems)
			inventory.GET("/almost-finished", inventoryItemHandler.GetAlmostFinishedItems)
			inventory.POST("", inventoryItemHandler.CreateInventoryItem)
			inventory.PUT("/:id", inventoryItemHandler.UpdateInventoryItem)
			inventory.PATCH("/:id/quantity", inventoryItemHandler.UpdateInventoryItemQuantity)
			inventory.DELETE("/:id", inventoryItemHandler.DeleteInventoryItem)
		}
	}

	return r
}
