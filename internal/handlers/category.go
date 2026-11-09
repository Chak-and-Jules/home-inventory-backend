package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/i18n"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/utils"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type CategoryHandler struct {
	DB *gorm.DB
}

type CategoryRequest struct {
	Name     string     `json:"name" binding:"required"`
	ParentID *uuid.UUID `json:"parent_id"` // Optional
}

func (h *CategoryHandler) GetCategories(c *gin.Context) {
	// ⚡ Bolt: Pass Canonical MIME header key (X-Home-Id instead of x-home-id) to avoid runtime string allocations during normalization
	homeID, ok := utils.ParseUUIDHeader(c, h.DB, "X-Home-Id", "Invalid home_id")
	if !ok {
		return
	}

	if !utils.VerifyHomeAccess(c, h.DB, homeID) {
		c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Access denied to this home")})
		return
	}

	var categories []models.Category
	// ⚡ Bolt: Removed Preload("Parent") to avoid a second query. Since we fetch all categories for the home,
	// all parents are guaranteed to be in the result set. We can stitch them in memory in O(N) time.
	if err := h.DB.Where("home_id = ?", homeID).Find(&categories).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": i18n.TranslateDB(h.DB, c, "Failed to fetch categories")})
		return
	}

	categoryMap := make(map[uuid.UUID]*models.Category, len(categories))
	for i := range categories {
		categoryMap[categories[i].ID] = &categories[i]
	}
	for i := range categories {
		if categories[i].ParentID != nil {
			if parent, ok := categoryMap[*categories[i].ParentID]; ok {
				// Embed only the immediate parent. Legacy deep trees or cycles
				// must not create recursive pointers during JSON serialization.
				copy := *parent
				copy.Parent = nil
				categories[i].Parent = &copy
			}
		}
	}

	c.JSON(http.StatusOK, categories)
}

func (h *CategoryHandler) CreateCategory(c *gin.Context) {
	// ⚡ Bolt: Pass Canonical MIME header key (X-Home-Id instead of x-home-id) to avoid runtime string allocations during normalization
	homeID, ok := utils.ParseUUIDHeader(c, h.DB, "X-Home-Id", "Invalid home_id")
	if !ok {
		return
	}

	if !utils.VerifyHomeWriteAccess(c, h.DB, homeID) {
		c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Write access denied to this home")})
		return
	}

	var req CategoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(h.DB, c, "Invalid request payload")})
		return
	}

	category := models.Category{
		HomeID:   homeID,
		Name:     req.Name,
		ParentID: req.ParentID,
	}

	err := h.DB.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := lockCategoryHome(tx, homeID); err != nil {
			return err
		}
		if err := validateCategoryParent(tx, homeID, uuid.Nil, req.ParentID); err != nil {
			return err
		}
		if err := validateCategoryName(tx, homeID, uuid.Nil, req); err != nil {
			return err
		}
		return tx.Create(&category).Error
	})
	if err != nil {
		h.categoryFailure(c, err, "Failed to create category")
		return
	}

	c.JSON(http.StatusCreated, category)
}

func (h *CategoryHandler) UpdateCategory(c *gin.Context) {
	id, ok := utils.ParseUUIDParam(c, h.DB, "id", "Invalid category ID")
	if !ok {
		return
	}

	var category models.Category
	if err := h.DB.First(&category, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": i18n.TranslateDB(h.DB, c, "Category not found")})
		return
	}

	if !utils.VerifyHomeWriteAccess(c, h.DB, category.HomeID) {
		c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Write access denied to this home")})
		return
	}

	var req CategoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": i18n.TranslateDB(h.DB, c, "Invalid request payload")})
		return
	}

	err := h.DB.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := lockCategoryHome(tx, category.HomeID); err != nil {
			return err
		}
		if err := validateCategoryParent(tx, category.HomeID, id, req.ParentID); err != nil {
			return err
		}
		if err := validateCategoryName(tx, category.HomeID, id, req); err != nil {
			return err
		}
		result := tx.Model(&category).Updates(map[string]interface{}{
			"name": req.Name, "parent_id": req.ParentID,
		})
		if result.Error == nil && result.RowsAffected == 0 {
			return &categoryResponseError{404, "Category not found"}
		}
		return result.Error
	})
	if err != nil {
		h.categoryFailure(c, err, "Failed to update category")
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": i18n.TranslateDB(h.DB, c, "Category updated successfully")})
}

func (h *CategoryHandler) DeleteCategory(c *gin.Context) {
	id, ok := utils.ParseUUIDParam(c, h.DB, "id", "Invalid category ID")
	if !ok {
		return
	}

	var category models.Category
	if err := h.DB.First(&category, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": i18n.TranslateDB(h.DB, c, "Category not found")})
		return
	}

	if !utils.VerifyHomeWriteAccess(c, h.DB, category.HomeID) {
		c.JSON(http.StatusForbidden, gin.H{"error": i18n.TranslateDB(h.DB, c, "Write access denied to this home")})
		return
	}

	err := h.DB.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := lockCategoryHome(tx, category.HomeID); err != nil {
			return err
		}
		result := tx.Delete(&models.Category{}, id)
		if result.Error == nil && result.RowsAffected == 0 {
			return &categoryResponseError{404, "Category not found"}
		}
		return result.Error
	})
	if err != nil {
		h.categoryFailure(c, err, "Failed to delete category")
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": i18n.TranslateDB(h.DB, c, "Category deleted successfully")})
}

type categoryResponseError struct {
	status int
	key    string
}

func (e *categoryResponseError) Error() string { return e.key }

func (h *CategoryHandler) categoryFailure(c *gin.Context, err error, fallback string) {
	status, key := http.StatusInternalServerError, fallback
	var response *categoryResponseError
	if errors.As(err, &response) {
		status, key = response.status, response.key
	}
	// Translate after rollback, including with single-connection database pools.
	c.JSON(status, gin.H{"error": i18n.TranslateDB(h.DB, c, key)})
}

// Every category CRUD write takes this lock before checking or modifying the
// tree. A common home row also serializes creation when no categories exist yet.
func lockCategoryHome(tx *gorm.DB, homeID uuid.UUID) error {
	var home models.Home
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").First(&home, "id = ?", homeID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &categoryResponseError{404, "Home not found"}
		}
		return err
	}
	return nil
}

func validateCategoryParent(tx *gorm.DB, homeID, categoryID uuid.UUID, parentID *uuid.UUID) error {
	if parentID == nil {
		return nil
	}
	if categoryID != uuid.Nil && *parentID == categoryID {
		return &categoryResponseError{400, "A category cannot be its own parent"}
	}
	var parent models.Category
	if err := tx.Select("id", "home_id", "parent_id").Where("id = ?", parentID).First(&parent).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &categoryResponseError{404, "Parent category not found"}
		}
		return &categoryResponseError{500, "Failed to verify parent category"}
	}
	if parent.HomeID != homeID {
		return &categoryResponseError{400, "Parent category must belong to the same home"}
	}
	if parent.ParentID != nil {
		return &categoryResponseError{400, "Parent category must be a top-level category"}
	}
	if categoryID != uuid.Nil {
		var count int64
		if err := tx.Model(&models.Category{}).Where("parent_id = ?", categoryID).Count(&count).Error; err != nil {
			return &categoryResponseError{500, "Failed to verify category children"}
		}
		if count > 0 {
			return &categoryResponseError{400, "A category with children cannot become a child category"}
		}
	}
	return nil
}

func validateCategoryName(tx *gorm.DB, homeID, categoryID uuid.UUID, req CategoryRequest) error {
	normalizedName := strings.ToLower(strings.ReplaceAll(req.Name, " ", ""))
	query := tx.Model(&models.Category{})
	if categoryID == uuid.Nil {
		query = query.Where("home_id = ? AND LOWER(REPLACE(name, ' ', '')) = ?", homeID, normalizedName)
	} else {
		query = query.Where("home_id = ? AND LOWER(REPLACE(name, ' ', '')) = ? AND id != ?", homeID, normalizedName, categoryID)
	}
	if req.ParentID == nil {
		query = query.Where("parent_id IS NULL")
	} else {
		query = query.Where("parent_id = ?", req.ParentID)
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return &categoryResponseError{500, "Failed to validate category uniqueness"}
	}
	if count > 0 {
		return &categoryResponseError{409, "A category with this name already exists at this level"}
	}
	return nil
}
