package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/demos"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/guides"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/i18n"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/logger"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/middleware"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/utils"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	"gorm.io/gorm"
)

type demoFixture struct {
	*unitFixture
	router  *gin.Engine
	service *demos.Service
	guides  *guides.Service
	now     time.Time
}

func newDemoFixture(t *testing.T) *demoFixture {
	t.Helper()
	base := newUnitFixture(t)
	require.NoError(t, base.db.AutoMigrate(&models.UserDemoHome{}, &models.GuideProgress{}, &models.GuideRequest{}))
	require.NoError(t, base.db.Model(&models.UserHome{}).Where("user_id = ? AND home_id = ?", base.user, base.home).Update("is_default", true).Error)
	f := &demoFixture{unitFixture: base, service: demos.New(base.db), guides: guides.New(base.db), now: time.Now().UTC().Truncate(time.Second)}
	f.service.Now = func() time.Time { return f.now }
	f.guides.Now = f.service.Now
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if id, err := uuid.Parse(c.GetHeader("X-Test-User")); err == nil {
			c.Set("userID", id)
			c.Set("email", id.String()+"@example.com")
			c.Set("firebaseUID", "firebase-"+id.String())
		}
	})
	h := &DemoHandler{Service: f.service}
	r.GET("/demo-home", h.Status)
	r.POST("/demo-home/actions", h.Act)
	gh := &GuideHandler{Service: f.guides}
	r.GET("/guides", gh.List)
	r.POST("/guides/:id/actions", gh.Act)
	r.POST("/guides/:id/setup", gh.Setup)
	ph := &ProfileHandler{DB: base.db}
	r.POST("/profiles/sync", ph.SyncProfile)
	hh := &HomeHandler{DB: base.db}
	homes := r.Group("/homes")
	homes.Use(middleware.DemoHomePolicy(f.service))
	homes.GET("", hh.GetHomes)
	homes.PUT("/:id", hh.UpdateHome)
	homes.DELETE("/:id", hh.DeleteHome)
	homes.POST("/:id/default", hh.SetDefaultHome)
	homes.POST("/:id/users", hh.AddHomeUser)
	homes.PUT("/:id/users/:userId/role", hh.UpdateHomeUserRole)
	homes.DELETE("/:id/users/:userId", hh.RemoveHomeUser)
	f.router = r
	return f
}

func (f *demoFixture) send(t *testing.T, method, path string, body any, want int) *httptest.ResponseRecorder {
	t.Helper()
	encoded, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest(method, path, bytes.NewReader(encoded))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-User", f.user.String())
	req.Header.Set("X-Home-Id", f.home.String())
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	require.Equal(t, want, w.Code, w.Body.String())
	return w
}

func (f *demoFixture) status(t *testing.T) demos.View {
	t.Helper()
	var v demos.View
	w := f.send(t, "GET", "/demo-home", nil, 200)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &v))
	return v
}

func (f *demoFixture) action(t *testing.T, action string) demos.View {
	t.Helper()
	v := f.status(t)
	version := v.Version
	w := f.send(t, "POST", "/demo-home/actions", demos.Command{RequestID: uuid.New(), ExpectedVersion: &version, Action: action}, 200)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &v))
	return v
}

func (f *demoFixture) freshUser(t *testing.T) uuid.UUID {
	t.Helper()
	id := uuid.New()
	uid := "firebase-" + id.String()
	require.NoError(t, f.db.Create(&models.Profile{ID: id, Email: id.String() + "@example.com", FirebaseUID: &uid}).Error)
	return id
}

func (f *demoFixture) syncProfile(t *testing.T, want int) {
	t.Helper()
	f.send(t, "POST", "/profiles/sync", map[string]any{"profile": map[string]string{"id": "firebase-" + f.user.String(), "email": f.user.String() + "@example.com"}}, want)
}

func TestDemoProvisioningSamplesAndPrivateHomes(t *testing.T) {
	f := newDemoFixture(t)
	originalUser, originalHome := f.user, f.home
	initial := f.status(t)
	require.Equal(t, "not_created", initial.Status)
	require.True(t, initial.CanCreate)
	// Existing users keep their current home set until explicitly opting in.
	f.syncProfile(t, 200)
	require.Equal(t, "not_created", f.status(t).Status)
	f.user = f.freshUser(t)
	f.syncProfile(t, 200)
	f.syncProfile(t, 200)
	demo := f.status(t)
	require.Equal(t, "available", demo.Status)
	require.EqualValues(t, 1, demo.Version)
	require.True(t, demo.Home.IsDemo)
	var homes []models.UserHome
	require.NoError(t, f.db.Preload("Home").Where("user_id = ?", f.user).Find(&homes).Error)
	require.Len(t, homes, 2)
	var personal uuid.UUID
	for _, h := range homes {
		if h.Home.IsDemo {
			require.False(t, h.IsDefault)
		} else {
			require.True(t, h.IsDefault)
			personal = h.HomeID
		}
	}
	require.NotEqual(t, uuid.Nil, personal)
	var count int64
	for _, entry := range []struct {
		model any
		want  int64
	}{{&models.Category{}, 2}, {&models.ItemDefinition{}, 5}, {&models.InventoryItem{}, 6}, {&models.InventoryTransaction{}, 6}, {&models.ShoppingListItem{}, 1}} {
		require.NoError(t, f.db.Model(entry.model).Where("home_id = ?", demo.Home.ID).Count(&count).Error)
		require.Equal(t, entry.want, count)
	}
	var rice models.ItemDefinition
	require.NoError(t, f.db.First(&rice, "home_id = ? AND name = ?", demo.Home.ID, "Rice").Error)
	var batches []models.InventoryItem
	require.NoError(t, f.db.Where("item_definition_id = ?", rice.ID).Order("quantity desc").Find(&batches).Error)
	require.Len(t, batches, 2)
	require.Equal(t, 1000.0, batches[0].Quantity)
	require.Equal(t, 500.0, batches[1].Quantity)
	var milk models.InventoryItem
	require.NoError(t, f.db.Joins("JOIN item_definitions d ON d.id = inventory_items.item_definition_id").Where("inventory_items.home_id = ? AND d.name = ?", demo.Home.ID, "Milk").First(&milk).Error)
	require.WithinDuration(t, time.Now().UTC().AddDate(0, 0, 2), *milk.ExpirationDate, time.Minute)
	var shopping models.ShoppingListItem
	require.NoError(t, f.db.First(&shopping, "home_id = ?", demo.Home.ID).Error)
	require.True(t, shopping.IsAutoGenerated)
	require.Equal(t, 900.0, shopping.Quantity)
	var unchanged models.Home
	require.NoError(t, f.db.First(&unchanged, "id = ?", originalHome).Error)
	require.False(t, unchanged.IsDemo)
	// Another user's demo is a different house and cannot be accessed via home routes.
	f.user = originalUser
	other := f.action(t, "create")
	require.NotEqual(t, demo.Home.ID, other.Home.ID)
	f.send(t, "PUT", "/homes/"+demo.Home.ID.String(), map[string]any{"name": "hijacked"}, 403)
	f.send(t, "DELETE", "/homes/"+demo.Home.ID.String(), nil, 403)
}

func TestDemoResetRemovalAndReceiptRecovery(t *testing.T) {
	f := newDemoFixture(t)
	personalStock := f.stock(t, 3, "kg")
	demo := f.action(t, "create")
	home := demo.Home.ID
	var oldMilk models.InventoryItem
	require.NoError(t, f.db.Where("home_id = ?", home).Order("created_at asc").First(&oldMilk).Error)
	// User-edited demo quantities behave like normal inventory.
	require.NoError(t, f.db.Model(&models.InventoryItem{}).Where("home_id = ?", home).Update("quantity", 42).Error)
	f.now = f.now.AddDate(0, 0, 5)
	version := demo.Version
	c := demos.Command{RequestID: uuid.New(), ExpectedVersion: &version, Action: "reset"}
	w := f.send(t, "POST", "/demo-home/actions", c, 200)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &demo))
	require.Equal(t, home, demo.Home.ID)
	require.EqualValues(t, 2, demo.Version)
	var count int64
	require.NoError(t, f.db.Model(&models.InventoryItem{}).Where("id = ?", oldMilk.ID).Count(&count).Error)
	require.Zero(t, count)
	var personal models.InventoryItem
	require.NoError(t, f.db.First(&personal, "id = ?", personalStock.ID).Error)
	require.Equal(t, 3000.0, personal.Quantity)
	var milk models.InventoryItem
	require.NoError(t, f.db.Joins("JOIN item_definitions d ON d.id = inventory_items.item_definition_id").Where("inventory_items.home_id = ? AND d.name = ?", home, "Milk").First(&milk).Error)
	require.WithinDuration(t, f.now.AddDate(0, 0, 2), *milk.ExpirationDate, time.Second)
	// A lost-response retry cannot run reset a second time.
	f.send(t, "POST", "/demo-home/actions", c, 200)
	var same models.InventoryItem
	require.NoError(t, f.db.First(&same, "id = ?", milk.ID).Error)
	demo = f.action(t, "remove")
	require.Equal(t, "removed", demo.Status)
	require.Nil(t, demo.Home)
	f.syncProfile(t, 200)
	require.Equal(t, "removed", f.status(t).Status)
	// Even an older successful reset retry after removal only acknowledges it.
	w = f.send(t, "POST", "/demo-home/actions", c, 200)
	require.Contains(t, w.Body.String(), `"status":"removed"`)
	c.Action = "create"
	f.send(t, "POST", "/demo-home/actions", c, 409)
	recreated := f.action(t, "create")
	require.NotEqual(t, home, recreated.Home.ID)
	var member models.UserHome
	require.NoError(t, f.db.First(&member, "user_id = ? AND home_id = ?", f.user, f.home).Error)
	require.True(t, member.IsDefault)
}

func TestDemoPolicyProtectsDefaultsAndRemovalTombstone(t *testing.T) {
	f := newDemoFixture(t)
	demo := f.action(t, "create")
	id := demo.Home.ID.String()
	f.send(t, "POST", "/homes/"+id+"/default", nil, 403)
	f.send(t, "POST", "/homes/"+id+"/users", map[string]any{"email": "other@example.com", "role": "viewer"}, 403)
	f.send(t, "PUT", "/homes/"+id+"/users/"+f.user.String()+"/role", map[string]any{"role": "viewer"}, 403)
	f.send(t, "DELETE", "/homes/"+id+"/users/"+f.user.String(), nil, 403)
	f.send(t, "DELETE", "/homes/"+f.home.String()+"?approved=true", nil, 400)
	f.send(t, "PUT", "/homes/"+id, map[string]any{"name": "Practice kitchen"}, 200)
	// A newly created normal home is selected as default if the old default is removed.
	second := models.Home{ID: uuid.New(), Name: "Second real home"}
	require.NoError(t, f.db.Create(&second).Error)
	require.NoError(t, f.db.Create(&models.UserHome{UserID: f.user, HomeID: second.ID, Role: models.RoleOwner}).Error)
	f.send(t, "DELETE", "/homes/"+f.home.String()+"?approved=true", nil, 200)
	var member models.UserHome
	require.NoError(t, f.db.First(&member, "user_id = ? AND home_id = ?", f.user, second.ID).Error)
	require.True(t, member.IsDefault)
	f.send(t, "DELETE", "/homes/"+id, nil, 200)
	require.Equal(t, "removed", f.status(t).Status)
	f.syncProfile(t, 200)
	require.Equal(t, "removed", f.status(t).Status)
}

func TestDemoGuideEligibilityAndPlatformLifecycle(t *testing.T) {
	f := newDemoFixture(t)
	demo := f.action(t, "create")
	list := func(platform guides.Platform, client int) []guides.View {
		v, err := f.guides.List(context.Background(), f.user, platform, client, &f.home)
		require.NoError(t, err)
		return v
	}
	require.Len(t, list(guides.Web, 2), 1)
	v := list(guides.Web, 3)
	require.Len(t, v, 2)
	require.False(t, v[1].ShouldOffer)
	require.Contains(t, v[1].UnavailableReasons, "prerequisite_required")
	zero := int64(0)
	intro := guides.Command{RequestID: uuid.New(), Platform: guides.Web, ClientVersion: 3, Revision: 1, ExpectedVersion: &zero, Action: "start", HomeID: &demo.Home.ID}
	_, err := f.guides.Act(context.Background(), f.user, "introduction", intro)
	require.ErrorIs(t, err, guides.ErrUnavailable)
	intro.Action = "skip"
	intro.HomeID = nil
	_, err = f.guides.Act(context.Background(), f.user, "introduction", intro)
	require.NoError(t, err)
	v = list(guides.Web, 3)
	require.True(t, v[1].ShouldOffer)
	require.Equal(t, &demo.Home.ID, v[1].DemoHomeID)
	apply := func(platform guides.Platform, action, step string) guides.View {
		current := list(platform, 3)[1]
		c := guides.Command{RequestID: uuid.New(), Platform: platform, ClientVersion: 3, Revision: 1, ExpectedVersion: &current.Version, Action: action, StepID: step}
		result, err := f.guides.Act(context.Background(), f.user, "demo_house", c)
		require.NoError(t, err)
		return result
	}
	v1 := apply(guides.Web, "start", "")
	require.Equal(t, &demo.Home.ID, v1.State.HomeID)
	apply(guides.Web, "checkpoint", "return_home")
	apply(guides.Web, "complete", "")
	require.Equal(t, guides.Completed, list(guides.Mobile, 3)[1].State.Status)
	apply(guides.Mobile, "restore", "")
	require.Equal(t, guides.Completed, list(guides.Web, 3)[1].State.Status)
	require.Equal(t, guides.NotStarted, list(guides.Mobile, 3)[1].State.Status)
	deadline := f.now.Add(time.Hour)
	current := list(guides.Mobile, 3)[1]
	c := guides.Command{RequestID: uuid.New(), Platform: guides.Mobile, ClientVersion: 3, Revision: 1, ExpectedVersion: &current.Version, Action: "maybe_later", DeferUntil: &deadline}
	_, err = f.guides.Act(context.Background(), f.user, "demo_house", c)
	require.NoError(t, err)
	require.False(t, list(guides.Mobile, 3)[1].ShouldOffer)
	f.action(t, "remove")
	apply(guides.Web, "restore", "")
	require.Equal(t, "removed", f.status(t).Status)
	require.Contains(t, list(guides.Web, 3)[1].UnavailableReasons, "demo_home_required")
	var progress models.GuideProgress
	require.NoError(t, f.db.First(&progress, "user_id = ? AND guide_id = ?", f.user, "introduction").Error)
	require.Equal(t, guides.Skipped, progress.WebStatus)
	require.Equal(t, guides.Skipped, progress.MobileStatus)
}

func TestDemoAtomicRollbackAndExistingDataMigration(t *testing.T) {
	f := newDemoFixture(t)
	require.NoError(t, f.db.Callback().Create().Before("gorm:create").Register("fail_demo_stock", func(tx *gorm.DB) {
		if tx.Statement.Table == "inventory_items" {
			tx.AddError(errors.New("injected stock failure"))
		}
	}))
	zero := int64(0)
	c := demos.Command{RequestID: uuid.New(), ExpectedVersion: &zero, Action: "create"}
	f.send(t, "POST", "/demo-home/actions", c, 500)
	require.Equal(t, "not_created", f.status(t).Status)
	var count int64
	require.NoError(t, f.db.Model(&models.Home{}).Where("is_demo = ?", true).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, f.db.Callback().Create().Remove("fail_demo_stock"))
	f.send(t, "POST", "/demo-home/actions", c, 200)
	demo := f.status(t)
	require.NoError(t, f.db.AutoMigrate(&models.Home{}, &models.UserDemoHome{}))
	require.Equal(t, demo.Home.ID, f.status(t).Home.ID)
	// Receipt failure rolls back destructive reset and preserves the previous stock.
	var old models.InventoryItem
	require.NoError(t, f.db.First(&old, "home_id = ?", demo.Home.ID).Error)
	require.NoError(t, f.db.Callback().Create().Before("gorm:create").Register("fail_demo_receipt", func(tx *gorm.DB) {
		if tx.Statement.Table == "guide_requests" {
			tx.AddError(errors.New("receipt unavailable"))
		}
	}))
	c = demos.Command{RequestID: uuid.New(), ExpectedVersion: &demo.Version, Action: "reset"}
	f.send(t, "POST", "/demo-home/actions", c, 500)
	require.NoError(t, f.db.Callback().Create().Remove("fail_demo_receipt"))
	require.NoError(t, f.db.First(&old, "id = ?", old.ID).Error)
	require.Equal(t, demo.Version, f.status(t).Version)
}

func TestDemoHTTPValidationAndLocalization(t *testing.T) {
	f := newDemoFixture(t)
	for _, body := range []map[string]any{{}, {"action": "reset"}, {"request_id": uuid.New(), "expected_version": 0, "action": "create", "home_id": uuid.New()}, {"request_id": uuid.New(), "expected_version": 0, "action": "unknown"}} {
		f.send(t, "POST", "/demo-home/actions", body, 400)
	}
	zero := int64(0)
	f.send(t, "POST", "/demo-home/actions", demos.Command{RequestID: uuid.New(), ExpectedVersion: &zero, Action: "reset"}, 404)
	lang := models.Language{ID: uuid.New(), Name: "Türkçe"}
	require.NoError(t, f.db.Create(&lang).Error)
	require.NoError(t, f.db.Model(&models.Profile{}).Where("id = ?", f.user).Update("language_id", lang.ID).Error)
	i18n.InvalidateUserLanguageCache(f.user)
	demo := f.action(t, "create")
	require.Equal(t, "Örnek Ev", demo.Home.Name)
	var count int64
	require.NoError(t, f.db.Model(&models.ItemDefinition{}).Where("home_id = ? AND name = ?", demo.Home.ID, "Pirinç").Count(&count).Error)
	require.EqualValues(t, 1, count)
	w := f.send(t, "POST", "/demo-home/actions", map[string]any{}, 400)
	require.Contains(t, w.Body.String(), "Geçersiz örnek ev isteği")
	h := &DemoHandler{Service: f.service}
	r := gin.New()
	r.GET("/demo-home", h.Status)
	r.POST("/demo-home/actions", h.Act)
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		path := "/demo-home"
		if method == http.MethodPost {
			path += "/actions"
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader("{}")))
		require.Equal(t, 401, w.Code)
	}
}

func TestDemoResetClearsOnlyItsDependentContent(t *testing.T) {
	f := newDemoFixture(t)
	demo := f.action(t, "create")
	personal := f.stock(t, 1, "kg")
	var sample models.InventoryItem
	require.NoError(t, f.db.First(&sample, "home_id = ?", demo.Home.ID).Error)
	type records struct{ task, dependency, recipe, ingredient, receipt, line, prediction uuid.UUID }
	create := func(home uuid.UUID, item models.InventoryItem) records {
		r := records{uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()}
		require.NoError(t, f.db.Create(&models.MaintenanceTask{ID: r.task, HomeID: home, InventoryItemID: &item.ID, Description: "Clean", ScheduledDate: f.now, Frequency: "Once"}).Error)
		require.NoError(t, f.db.Create(&models.TaskItemDependency{ID: r.dependency, MaintenanceTaskID: r.task, ItemDefinitionID: item.ItemDefinitionID, QuantityRequired: 1}).Error)
		require.NoError(t, f.db.Create(&models.Recipe{ID: r.recipe, HomeID: home, Name: "Recipe", Servings: 1}).Error)
		require.NoError(t, f.db.Create(&models.RecipeIngredient{ID: r.ingredient, RecipeID: r.recipe, ItemDefinitionID: item.ItemDefinitionID, QuantityRequired: 1}).Error)
		require.NoError(t, f.db.Create(&models.ReceiptJob{ID: r.receipt, HomeID: home, UserID: f.user, Status: "completed"}).Error)
		require.NoError(t, f.db.Create(&models.ReceiptJobItem{ID: r.line, ReceiptJobID: r.receipt, RawName: "Item", Quantity: 1, MatchedItemDefinitionID: &item.ItemDefinitionID}).Error)
		require.NoError(t, f.db.Create(&models.InventoryPrediction{ID: r.prediction, InventoryItemID: item.ID, PredictedConsumedAmount: 1, Status: models.PredictionStatusPredicted}).Error)
		return r
	}
	demoRecords, realRecords := create(demo.Home.ID, sample), create(f.home, personal)
	f.action(t, "reset")
	for _, r := range []struct {
		value records
		count int64
	}{{demoRecords, 0}, {realRecords, 1}} {
		for _, row := range []struct {
			model any
			id    uuid.UUID
		}{{&models.MaintenanceTask{}, r.value.task}, {&models.TaskItemDependency{}, r.value.dependency}, {&models.Recipe{}, r.value.recipe}, {&models.RecipeIngredient{}, r.value.ingredient}, {&models.ReceiptJob{}, r.value.receipt}, {&models.ReceiptJobItem{}, r.value.line}, {&models.InventoryPrediction{}, r.value.prediction}} {
			var count int64
			require.NoError(t, f.db.Model(row.model).Where("id = ?", row.id).Count(&count).Error)
			require.Equal(t, r.count, count)
		}
	}
}

func TestDemoMigrationKeepsExistingHomePersonal(t *testing.T) {
	db := openUnitTestDB(t)
	id := uuid.New()
	require.NoError(t, db.Exec("CREATE TABLE homes (id uuid PRIMARY KEY, name varchar(255) NOT NULL)").Error)
	require.NoError(t, db.Exec("INSERT INTO homes (id,name) VALUES (?,?)", id, "Existing home").Error)
	require.NoError(t, db.AutoMigrate(&models.Home{}, &models.UserDemoHome{}))
	var home models.Home
	require.NoError(t, db.First(&home, "id = ?", id).Error)
	require.False(t, home.IsDemo)
	require.Equal(t, "Existing home", home.Name)
}

func TestDemoBackgroundRefreshKeepsNotificationsPrivate(t *testing.T) {
	f := newDemoFixture(t)
	demo := f.action(t, "create")
	core, logs := observer.New(zap.InfoLevel)
	previous := logger.Log
	logger.Log = zap.New(core)
	defer func() { logger.Log = previous }()
	utils.RefreshAllShoppingLists(f.db)
	require.Zero(t, logs.FilterMessage("Notification Triggered").Len())
	var sample models.ShoppingListItem
	require.NoError(t, f.db.First(&sample, "home_id = ?", demo.Home.ID).Error)
	require.Equal(t, 900.0, sample.Quantity)
	// Ordinary real-home alerts still work through the same entry point.
	threshold, target := 1000.0, 3000.0
	require.NoError(t, f.db.Model(&models.ItemDefinition{}).Where("id = ?", f.def.ID).Updates(map[string]any{"low_stock_threshold": threshold, "target_quantity": target}).Error)
	require.NoError(t, utils.UpdateShoppingListForDefinition(f.db, f.home, f.def.ID))
	require.Equal(t, 1, logs.FilterMessage("Notification Triggered").Len())
}

func TestDemoConcurrentProvisioningAndActionsPostgres(t *testing.T) {
	if os.Getenv("UNIT_TEST_POSTGRES_DSN") == "" {
		t.Skip("requires disposable PostgreSQL for concurrent transactions")
	}
	f := newDemoFixture(t)
	f.user = f.freshUser(t)
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs <- f.service.EnsureInitialHomes(context.Background(), f.user, nil)
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	var count int64
	require.NoError(t, f.db.Model(&models.UserHome{}).Where("user_id = ?", f.user).Count(&count).Error)
	require.EqualValues(t, 2, count)
	for _, duplicate := range []bool{false, true} {
		t.Run(fmt.Sprintf("reset_duplicate=%v", duplicate), func(t *testing.T) {
			before := f.status(t)
			shared := demos.Command{RequestID: uuid.New(), ExpectedVersion: &before.Version, Action: "reset"}
			var wg sync.WaitGroup
			ready := make(chan struct{})
			results := make(chan error, 8)
			for range 8 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					c := shared
					if !duplicate {
						c.RequestID = uuid.New()
					}
					<-ready
					_, err := f.service.Act(context.Background(), f.user, c, nil)
					results <- err
				}()
			}
			close(ready)
			wg.Wait()
			close(results)
			success := 0
			for err := range results {
				if err == nil {
					success++
				} else {
					require.ErrorIs(t, err, guides.ErrConflict)
				}
			}
			if duplicate {
				require.Equal(t, 8, success)
			} else {
				require.Equal(t, 1, success)
			}
			require.Equal(t, before.Version+1, f.status(t).Version)
			require.NoError(t, f.db.Model(&models.InventoryItem{}).Where("home_id = ?", before.Home.ID).Count(&count).Error)
			require.EqualValues(t, 6, count)
		})
	}
}

func TestDemoGuideAndRemovalSerializePostgres(t *testing.T) {
	if os.Getenv("UNIT_TEST_POSTGRES_DSN") == "" {
		t.Skip("requires disposable PostgreSQL for concurrent transactions")
	}
	f := newDemoFixture(t)
	zero := int64(0)
	_, err := f.guides.Act(context.Background(), f.user, "introduction", guides.Command{
		RequestID: uuid.New(), Platform: guides.Web, ClientVersion: 3,
		Revision: 1, ExpectedVersion: &zero, Action: "skip",
	})
	require.NoError(t, err)
	// Exercise both possible lock winners across repeated removal/recreation.
	for range 8 {
		demo := f.action(t, "create")
		views, err := f.guides.List(context.Background(), f.user, guides.Web, 3, &f.home)
		require.NoError(t, err)
		guide, err := f.guides.Act(context.Background(), f.user, "demo_house", guides.Command{
			RequestID: uuid.New(), Platform: guides.Web, ClientVersion: 3,
			Revision: 1, ExpectedVersion: &views[1].Version, Action: "start",
		})
		require.NoError(t, err)
		ready := make(chan struct{})
		guideResult, removeResult := make(chan error, 1), make(chan error, 1)
		go func() {
			<-ready
			_, err := f.guides.Act(context.Background(), f.user, "demo_house", guides.Command{
				RequestID: uuid.New(), Platform: guides.Web, ClientVersion: 3, Revision: 1,
				ExpectedVersion: &guide.Version, Action: "checkpoint", StepID: "explore_demo",
			})
			guideResult <- err
		}()
		go func() {
			<-ready
			_, err := f.service.Act(context.Background(), f.user, demos.Command{
				RequestID: uuid.New(), ExpectedVersion: &demo.Version, Action: "remove",
			}, nil)
			removeResult <- err
		}()
		close(ready)
		require.NoError(t, <-removeResult)
		if err := <-guideResult; err != nil {
			require.ErrorIs(t, err, guides.ErrUnavailable)
		}
		require.Equal(t, "removed", f.status(t).Status)
		var progress models.GuideProgress
		require.NoError(t, f.db.First(&progress, "user_id = ? AND guide_id = ?", f.user, "demo_house").Error)
		require.Nil(t, progress.WebHomeID)
	}
}
