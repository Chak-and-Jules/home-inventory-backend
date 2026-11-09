package middleware

import (
	"errors"
	"net/http"
	"strings"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/demos"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/i18n"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/utils"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// DemoHomePolicy applies to the existing home routes too, so a user cannot share
// a private demo, set it as default, or bypass removal bookkeeping via old URLs.
func DemoHomePolicy(service *demos.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		homeID, err := uuid.Parse(c.Param("id"))
		if err != nil {
			c.Next()
			return
		}
		user, ok := utils.GetAuthUserID(c, service.DB)
		if !ok {
			c.Abort()
			return
		}
		fail := func(status int, key string) {
			c.AbortWithStatusJSON(status, gin.H{"error": i18n.TranslateDB(service.DB, c, key)})
		}
		var home models.Home
		if err := service.DB.WithContext(c.Request.Context()).First(&home, "id = ?", homeID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				c.Next()
				return
			}
			fail(500, "Failed to verify demo home access")
			return
		}
		deleting := c.Request.Method == http.MethodDelete && strings.HasSuffix(c.FullPath(), "/:id")
		if home.IsDemo {
			var owner models.UserDemoHome
			if err := service.DB.Where("user_id = ? AND home_id = ?", user, homeID).First(&owner).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					fail(403, demos.ErrAccess.Error())
				} else {
					fail(500, "Failed to verify demo home access")
				}
				return
			}
			if deleting {
				if err := service.RemoveByHome(c.Request.Context(), user, homeID); err != nil {
					if errors.Is(err, demos.ErrAccess) {
						fail(403, demos.ErrAccess.Error())
						return
					}
					if errors.Is(err, demos.ErrUnavailable) {
						fail(404, demos.ErrUnavailable.Error())
						return
					}
					fail(500, "Failed to process demo home")
					return
				}
				c.AbortWithStatusJSON(200, gin.H{"message": i18n.TranslateDB(service.DB, c, "Home deleted successfully")})
				return
			}
			if c.Request.Method != http.MethodGet && (strings.Contains(c.FullPath(), "/users") || strings.HasSuffix(c.FullPath(), "/default")) {
				fail(403, "Demo homes are private and cannot be shared or set as default")
				return
			}
		} else if deleting {
			var member models.UserHome
			if err := service.DB.Where("user_id = ? AND home_id = ?", user, homeID).First(&member).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					c.Next()
					return
				}
				fail(500, "Failed to verify demo home access")
				return
			}
			if member.Role == models.RoleOwner {
				var count int64
				if err := service.DB.Model(&models.UserHome{}).Where("user_id = ? AND home_id IN (SELECT id FROM homes WHERE is_demo = ?)", user, false).Count(&count).Error; err != nil {
					fail(500, "Failed to count user homes")
					return
				}
				if count <= 1 {
					fail(400, "Cannot delete your only home")
					return
				}
			}
		}
		c.Next()
	}
}
