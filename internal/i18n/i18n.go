package i18n

import (
	"strings"
	"sync"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var userLangCache sync.Map

var Translations = map[string]map[string]string{
	"English": {},
	"Türkçe": {
		"A category with this name already exists at this level": "Bu düzeyde aynı isme sahip bir kategori zaten mevcut",
		"Access denied to this home":                             "Bu eve erişim reddedildi",
		"Authorization header is missing":                        "Yetkilendirme başlığı eksik",
		"Too many requests":                                      "Çok fazla istek",
		"Category deleted successfully":                          "Kategori başarıyla silindi",
		"Category not found":                                     "Kategori bulunamadı",
		"Category updated successfully":                          "Kategori başarıyla güncellendi",
		"Cannot delete your only home":                           "Tek evinizi silemezsiniz",
		"Default home updated successfully":                      "Varsayılan ev başarıyla güncellendi",
		"A category cannot be its own parent":                    "Bir kategori kendisinin üst kategorisi olamaz",
		"Email missing from token claims":                        "E-posta token claims içinde eksik",
		"Failed to check homes":                                  "Evleri kontrol etme başarısız oldu",
		"Failed to create category":                              "Kategori oluşturulamadı",
		"Failed to create default home":                          "Varsayılan ev oluşturulamadı",
		"Failed to create home":                                  "Ev oluşturulamadı",
		"Failed to create inventory item":                        "Envanter öğesi oluşturulamadı",
		"Failed to create item definition":                       "Öğe tanımı oluşturulamadı",
		"Failed to create shopping list item":                    "Alışveriş listesi öğesi oluşturulamadı",
		"Failed to delete category":                              "Kategori silinemedi",
		"Failed to delete home":                                  "Ev silinemedi",
		"Failed to delete inventory item":                        "Envanter öğesi silinemedi",
		"Failed to delete item definition (it might be in use)":  "Öğe tanımı silinemedi (kullanımda olabilir)",
		"Failed to delete shopping list item":                    "Alışveriş listesi öğesi silinemedi",
		"Failed to fetch categories":                             "Kategoriler getirilemedi",
		"Failed to fetch homes":                                  "Evler getirilemedi",
		"Failed to fetch inventory items":                        "Envanter öğeleri getirilemedi",
		"Failed to fetch item definitions":                       "Öğe tanımları getirilemedi",
		"Failed to fetch shopping list items":                    "Alışveriş listesi öğeleri getirilemedi",
		"Failed to fetch languages":                              "Diller getirilemedi",
		"Failed to fetch profile":                                "Profil getirilemedi",
		"Failed to fetch size units":                             "Boyut birimleri getirilemedi",
		"Failed to parse token claims":                           "Token claims ayrıştırılamadı",
		"Failed to set default home":                             "Varsayılan ev ayarlanamadı",
		"Failed to sync profile":                                 "Profil senkronizasyonu başarısız oldu",
		"Failed to update category":                              "Kategori güncellenemedi",
		"Failed to update home":                                  "Ev güncellenemedi",
		"Failed to update inventory item":                        "Envanter öğesi güncellenemedi",
		"Failed to update item definition":                       "Öğe tanımı güncellenemedi",
		"Failed to update profile":                               "Profil güncellenemedi",
		"Failed to verify parent category":                       "Üst kategori doğrulanamadı",
		"Failed to update quantity":                              "Miktar güncellenemedi",
		"Failed to update shopping list item":                    "Alışveriş listesi öğesi güncellenemedi",
		"Failed to toggle bought status":                         "Satın alma durumu değiştirilemedi",
		"Failed to validate category uniqueness":                 "Kategori benzersizliğini doğrulama başarısız oldu",
		"Home deleted successfully":                              "Ev başarıyla silindi",
		"Home not found or access denied":                        "Ev bulunamadı veya erişim reddedildi",
		"Home updated successfully":                              "Ev başarıyla güncellendi",
		"This is your default home. Please confirm deletion.":    "Bu sizin varsayılan evinizdir. Lütfen silme işlemini onaylayın.",
		"Insufficient permissions to update home":                "Evi güncellemek için yetersiz izinler",
		"Invalid ID":                                        "Geçersiz ID",
		"Invalid authorization header format":               "Geçersiz yetkilendirme başlığı formatı",
		"Invalid category ID":                               "Geçersiz kategori ID",
		"Invalid home ID":                                   "Geçersiz ev ID",
		"Invalid home_id":                                   "Geçersiz ev_id",
		"Invalid inventory item ID":                         "Geçersiz envanter öğesi ID",
		"Invalid item definition ID":                        "Geçersiz öğe tanımı ID",
		"Invalid shopping list item ID":                     "Geçersiz alışveriş listesi öğesi ID",
		"Item definition does not belong to this home":      "Öğe tanımı bu eve ait değil",
		"Invalid user ID in token":                          "Token içinde geçersiz kullanıcı ID'si",
		"Invalid request payload":                           "Geçersiz istek yükü",
		"Invalid token":                                     "Geçersiz belirteç",
		"Inventory item deleted successfully":               "Envanter öğesi başarıyla silindi",
		"Inventory item not found":                          "Envanter öğesi bulunamadı",
		"Inventory item updated successfully":               "Envanter öğesi başarıyla güncellendi",
		"Parent category must belong to the same home":      "Üst kategori aynı eve ait olmalıdır",
		"Parent category not found":                         "Üst kategori bulunamadı",
		"Item definition deleted successfully":              "Öğe tanımı başarıyla silindi",
		"Shopping list item deleted successfully":           "Alışveriş listesi öğesi başarıyla silindi",
		"Shopping list item updated successfully":           "Alışveriş listesi öğesi başarıyla güncellendi",
		"Shopping list item status toggled successfully":    "Alışveriş listesi öğesi durumu başarıyla değiştirildi",
		"Item definition or name is required":               "Öğe tanımı veya isim gereklidir",
		"Shopping list item not found":                      "Alışveriş listesi öğesi bulunamadı",
		"Item definition not found":                         "Öğe tanımı bulunamadı",
		"Item definition updated successfully":              "Öğe tanımı başarıyla güncellendi",
		"JWT secret is not configured":                      "JWT sırrı yapılandırılmamış",
		"No valid fields to update":                         "Güncellenecek geçerli alan yok",
		"Only owners can delete homes":                      "Sadece sahipler evleri silebilir",
		"Profile email does not match authenticated user":   "Profil e-postası doğrulanmış kullanıcıyla eşleşmiyor",
		"Profile not found":                                 "Profil bulunamadı",
		"Profile updated successfully":                      "Profil başarıyla güncellendi",
		"Profile user ID does not match authenticated user": "Profil kullanıcı ID'si doğrulanmış kullanıcıyla eşleşmiyor",
		"Quantity updated successfully":                     "Miktar başarıyla güncellendi",
		"SUPABASE_URL is not configured":                    "SUPABASE_URL yapılandırılmamış",
		"Subject missing from token claims":                 "Konu token claims içinde eksik",
		"Write access denied to this home":                  "Bu eve yazma erişimi reddedildi",
		"id header is required":                             "id başlığı gereklidir",
		"id query parameter is required":                    "id sorgu parametresi gereklidir",
		"x-home-id header is required":                      "x-home-id başlığı gereklidir",
	},
}

// InvalidateUserLanguageCache removes a user from the language cache
func InvalidateUserLanguageCache(userID uuid.UUID) {
	userLangCache.Delete(userID)
}

// TranslateDB gets the user language from context/DB and translates the string
func TranslateDB(db *gorm.DB, c *gin.Context, key string) string {
	if c == nil {
		return key
	}

	userIDVal, exists := c.Get("userID")
	if !exists {
		return key
	}

	userID, ok := userIDVal.(uuid.UUID)
	if !ok {
		return key
	}

	langName := "English"
	if val, ok := userLangCache.Load(userID); ok {
		langName = val.(string)
	} else if db != nil {
		var profile models.Profile
		// Only hit DB if DB is valid
		if err := db.Preload("Language").Select("id", "language_id").Where("id = ?", userID).First(&profile).Error; err == nil {
			if profile.Language != nil && profile.Language.Name != "" {
				langName = profile.Language.Name
			}
		}
		userLangCache.Store(userID, langName)
	}

	// ⚡ Bolt: Avoid strings.EqualFold in hot paths for small set of enum-like values to improve performance (exact match is orders of magnitude faster)
	langKey := "English"
	if langName == "Türkçe" || langName == "Turkish" || langName == "tr" || langName == "türkçe" || langName == "turkish" || langName == "TR" || langName == "Tr" {
		langKey = "Türkçe"
	}

	if langMap, ok := Translations[langKey]; ok {
		// ⚡ Bolt: Fast path exact key match to avoid expensive string operations
		if val, ok := langMap[key]; ok {
			return val
		}

		// Check for templated keys
		var lookupKey string
		var suffix string

		if strings.HasSuffix(key, " query parameter is required") {
			lookupKey = "id query parameter is required"
			suffix = key[:len(key)-len(" query parameter is required")]
		} else if strings.HasSuffix(key, " header is required") {
			lookupKey = "id header is required"
			suffix = key[:len(key)-len(" header is required")]
		}

		if lookupKey != "" {
			if val, ok := langMap[lookupKey]; ok {
				// ⚡ Bolt: Fast string slice replace to avoid strings.Contains double-scan and strings.Replace allocation
				if idx := strings.Index(val, "id"); idx != -1 {
					return val[:idx] + suffix + val[idx+2:]
				}
				return val
			}
		}
	}

	return key
}
