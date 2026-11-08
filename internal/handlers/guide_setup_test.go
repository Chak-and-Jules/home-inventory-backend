package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/guides"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type setupFixture struct {
	*unitFixture
	router  *gin.Engine
	service *guides.Service
}

func newSetupFixture(t *testing.T) *setupFixture {
	t.Helper()
	base := newUnitFixture(t)
	require.NoError(t, base.db.AutoMigrate(&models.GuideProgress{}, &models.GuideRequest{}))
	service := guides.New(base.db)
	service.Now = func() time.Time { return time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC) }
	h := &GuideHandler{Service: service}
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if id, err := uuid.Parse(c.GetHeader("X-Test-User")); err == nil {
			c.Set("userID", id)
		}
	})
	r.GET("/guides", h.List)
	r.POST("/guides/:id/actions", h.Act)
	r.POST("/guides/:id/setup", h.Setup)
	return &setupFixture{base, r, service}
}
func setupCommand(platform, action string, version int64) map[string]any {
	return map[string]any{"platform": platform, "client_version": 2, "revision": 1, "expected_version": version, "request_id": uuid.New(), "action": action}
}
func (f *setupFixture) send(t *testing.T, endpoint string, body map[string]any, want int) guides.View {
	t.Helper()
	b, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest("POST", "/guides/introduction/"+endpoint, bytes.NewReader(b))
	req.Header.Set("X-Test-User", f.user.String())
	req.Header.Set("X-Home-Id", f.home.String())
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	require.Equal(t, want, w.Code, w.Body.String())
	var view guides.View
	if want == 200 {
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &view))
	}
	return view
}
func (f *setupFixture) start(t *testing.T, platform string) guides.View {
	c := setupCommand(platform, "start", 0)
	c["home_id"] = f.home
	v := f.send(t, "actions", c, 200)
	c = setupCommand(platform, "checkpoint", v.Version)
	c["home_id"] = f.home
	c["step_id"] = "definition"
	return f.send(t, "actions", c, 200)
}

func TestGuideSetupUsesMergedLifecycleAndReceipts(t *testing.T) {
	for _, platform := range []string{"web", "mobile"} {
		t.Run(platform, func(t *testing.T) {
			f := newSetupFixture(t)
			v := f.start(t, platform)
			create := setupCommand(platform, "create_definition", v.Version)
			create["definition"] = map[string]any{"name": "Rice", "size_unit_id": f.units["kg"]}
			v = f.send(t, "setup", create, 200)
			require.NotNil(t, v.State.DefinitionID)
			require.Equal(t, map[string]string{"web": "stock", "mobile": "inventory"}[platform], v.State.StepID)
			again := f.send(t, "setup", create, 200)
			require.Equal(t, v.Version, again.Version)
			require.Equal(t, v.State.DefinitionID, again.State.DefinitionID)
			var count int64
			require.NoError(t, f.db.Model(&models.ItemDefinition{}).Where("name=?", "Rice").Count(&count).Error)
			require.EqualValues(t, 1, count)
			list, err := f.service.List(t.Context(), f.user, guides.Platform(platform), 1, &f.home)
			require.NoError(t, err)
			// Version-1 discovery sees exactly the aggregate changed by the setup API.
			require.Equal(t, v.Version, list[0].Version)
			require.Equal(t, v.State.DefinitionID, list[0].State.DefinitionID)
			save := setupCommand(platform, "create_inventory", v.Version)
			save["inventory"] = map[string]any{"item_definition_id": v.State.DefinitionID, "quantity": -1, "quantity_unit_id": f.units["kg"]}
			f.send(t, "setup", save, 400)
			save["inventory"].(map[string]any)["quantity"] = 3
			v = f.send(t, "setup", save, 200)
			require.Equal(t, "quantity", v.State.StepID)
			var item models.InventoryItem
			require.NoError(t, f.db.First(&item, "id=?", v.State.InventoryID).Error)
			require.Equal(t, 3000.0, item.Quantity)
			again = f.send(t, "setup", save, 200)
			require.Equal(t, v.Version, again.Version)
			c := setupCommand(platform, "complete", v.Version)
			c["home_id"] = f.home
			v = f.send(t, "actions", c, 200)
			require.Equal(t, guides.Completed, v.State.Status)
			other := guides.Mobile
			if platform == "mobile" {
				other = guides.Web
			}
			list, err = f.service.List(t.Context(), f.user, other, 2, &f.home)
			require.NoError(t, err)
			require.Equal(t, guides.Completed, list[0].State.Status)
			require.False(t, list[0].ShouldOffer)
			// Replaying an older successful create cannot revive progress or duplicate it.
			again = f.send(t, "setup", create, 200)
			require.Equal(t, guides.Completed, again.State.Status)
			restored := setupCommand(string(other), "restore", v.Version)
			v = f.send(t, "actions", restored, 200)
			require.Equal(t, guides.NotStarted, v.State.Status)
			require.Nil(t, v.State.DefinitionID)
			require.Nil(t, v.State.InventoryID)
			list, err = f.service.List(t.Context(), f.user, guides.Platform(platform), 2, &f.home)
			require.NoError(t, err)
			require.Equal(t, guides.Completed, list[0].State.Status)
			require.NoError(t, f.db.Model(&models.InventoryItem{}).Where("item_definition_id=?", item.ItemDefinitionID).Count(&count).Error)
			require.EqualValues(t, 1, count)
		})
	}
}

func TestGuideSetupRollbackReuseAndContext(t *testing.T) {
	f := newSetupFixture(t)
	v := f.start(t, "web")
	c := setupCommand("web", "choose_definition", v.Version)
	c["definition_id"] = f.def.ID
	v = f.send(t, "setup", c, 200)
	require.NoError(t, f.db.Callback().Create().Before("gorm:create").Register("test:setup_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "inventory_transactions" {
			tx.AddError(fmt.Errorf("injected log failure"))
		}
	}))
	save := setupCommand("web", "create_inventory", v.Version)
	save["inventory"] = map[string]any{"item_definition_id": f.def.ID, "quantity": 2, "quantity_unit_id": f.units["kg"]}
	f.send(t, "setup", save, 500)
	list, err := f.service.List(t.Context(), f.user, guides.Web, 2, &f.home)
	require.NoError(t, err)
	require.Equal(t, v.Version, list[0].Version)
	require.Equal(t, v.State.DefinitionID, list[0].State.DefinitionID)
	var count int64
	require.NoError(t, f.db.Model(&models.InventoryItem{}).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, f.db.Callback().Create().Remove("test:setup_failure"))
	v = f.send(t, "setup", save, 200)
	reused := setupCommand("web", "skip", v.Version)
	reused["request_id"] = save["request_id"]
	f.send(t, "actions", reused, 409)
	// Starting in another authorized home clears references but retains inventory.
	other := models.Home{ID: uuid.New(), Name: "Other"}
	require.NoError(t, f.db.Create(&other).Error)
	require.NoError(t, f.db.Create(&models.UserHome{UserID: f.user, HomeID: other.ID, Role: models.RoleOwner}).Error)
	c = setupCommand("web", "start", v.Version)
	c["home_id"] = other.ID
	v = f.send(t, "actions", c, 200)
	require.Equal(t, "home", v.State.StepID)
	require.Nil(t, v.State.DefinitionID)
	require.Nil(t, v.State.InventoryID)
	require.NoError(t, f.db.Model(&models.InventoryItem{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestGuideSetupReferencesAndViewerCompatibility(t *testing.T) {
	f := newSetupFixture(t)
	other := models.Home{ID: uuid.New(), Name: "Other"}
	require.NoError(t, f.db.Create(&other).Error)
	category := models.Category{ID: uuid.New(), HomeID: other.ID, Name: "Other"}
	require.NoError(t, f.db.Create(&category).Error)
	kg := f.units["kg"]
	def := models.ItemDefinition{ID: uuid.New(), HomeID: other.ID, Name: "Other", SizeUnitID: &kg}
	require.NoError(t, f.db.Create(&def).Error)
	item := models.InventoryItem{ID: uuid.New(), HomeID: other.ID, ItemDefinitionID: def.ID, Quantity: 1}
	require.NoError(t, f.db.Create(&item).Error)
	v := f.start(t, "web")
	for _, entry := range []struct {
		action, key string
		value       any
	}{{"choose_definition", "definition_id", def.ID}, {"choose_inventory", "inventory_id", item.ID}, {"create_definition", "definition", map[string]any{"name": "Wrong", "size_unit_id": kg, "category_id": category.ID}}} {
		c := setupCommand("web", entry.action, v.Version)
		c[entry.key] = entry.value
		f.send(t, "setup", c, 404)
	}
	c := setupCommand("web", "checkpoint", v.Version)
	c["step_id"] = "quantity"
	c["home_id"] = f.home
	v = f.send(t, "actions", c, 200)
	c = setupCommand("web", "complete", v.Version)
	c["home_id"] = f.home
	f.send(t, "actions", c, 409) // v2 writers need saved stock
	c = setupCommand("web", "restore", v.Version)
	v = f.send(t, "actions", c, 200)
	require.NoError(t, f.db.Model(&models.UserHome{}).Where("user_id=? AND home_id=?", f.user, f.home).Update("role", models.RoleViewer).Error)
	old, err := f.service.List(t.Context(), f.user, guides.Web, 1, &f.home)
	require.NoError(t, err)
	require.False(t, old[0].Available)
	list, err := f.service.List(t.Context(), f.user, guides.Web, 2, &f.home)
	require.NoError(t, err)
	require.True(t, list[0].Available)
	require.False(t, list[0].CanWrite)
	c = setupCommand("web", "start", v.Version)
	c["home_id"] = f.home
	v = f.send(t, "actions", c, 200)
	c = setupCommand("web", "checkpoint", v.Version)
	c["step_id"] = "definition"
	v = f.send(t, "actions", c, 200)
	c = setupCommand("web", "create_definition", v.Version)
	c["definition"] = map[string]any{"name": "Forbidden", "size_unit_id": kg}
	f.send(t, "setup", c, 403)
	for _, step := range []string{"stock", "quantity"} {
		c = setupCommand("web", "checkpoint", v.Version)
		c["step_id"] = step
		v = f.send(t, "actions", c, 200)
	}
	c = setupCommand("web", "complete", v.Version)
	c["home_id"] = f.home
	v = f.send(t, "actions", c, 200)
	require.Equal(t, guides.Completed, v.State.Status)
}

func TestGuideSetupConcurrentRetriesCreateStockOnce(t *testing.T) {
	f := newSetupFixture(t)
	v := f.start(t, "web")
	c := setupCommand("web", "choose_definition", v.Version)
	c["definition_id"] = f.def.ID
	v = f.send(t, "setup", c, 200)
	c = setupCommand("web", "create_inventory", v.Version)
	c["inventory"] = map[string]any{"item_definition_id": f.def.ID, "quantity": 1, "quantity_unit_id": f.units["kg"]}
	b, err := json.Marshal(c)
	require.NoError(t, err)
	var group sync.WaitGroup
	codes := make(chan int, 2)
	for i := 0; i < 2; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			req := httptest.NewRequest("POST", "/guides/introduction/setup", bytes.NewReader(b))
			req.Header.Set("X-Test-User", f.user.String())
			req.Header.Set("X-Home-Id", f.home.String())
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			f.router.ServeHTTP(w, req)
			codes <- w.Code
		}()
	}
	group.Wait()
	close(codes)
	for code := range codes {
		require.Equal(t, 200, code)
	}
	var count int64
	require.NoError(t, f.db.Model(&models.InventoryItem{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestGuideSetupReusesStockAndRestoresWithoutChangingData(t *testing.T) {
	f := newSetupFixture(t)
	item := f.stock(t, 1, "kg")
	v := f.start(t, "mobile")
	c := setupCommand("mobile", "choose_inventory", v.Version)
	c["inventory_id"] = item.ID
	v = f.send(t, "setup", c, 200)
	require.Equal(t, &item.ID, v.State.InventoryID)
	c = setupCommand("mobile", "complete", v.Version)
	c["home_id"] = f.home
	v = f.send(t, "actions", c, 200)
	c = setupCommand("mobile", "restore", v.Version)
	c["client_version"] = 1
	v = f.send(t, "actions", c, 200)
	require.Equal(t, guides.NotStarted, v.State.Status)
	require.Nil(t, v.State.InventoryID)
	other, err := f.service.List(t.Context(), f.user, guides.Web, 2, &f.home)
	require.NoError(t, err)
	require.Equal(t, guides.Completed, other[0].State.Status)
	var count int64
	require.NoError(t, f.db.Model(&models.InventoryItem{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.NoError(t, f.db.First(&item, "id=?", item.ID).Error)
	require.Equal(t, 1000.0, item.Quantity)
}

func TestGuideSetupReceiptKeepsExactQuantityText(t *testing.T) {
	f := newSetupFixture(t)
	v := f.start(t, "web")
	c := setupCommand("web", "choose_definition", v.Version)
	c["definition_id"] = f.def.ID
	v = f.send(t, "setup", c, 200)
	c = setupCommand("web", "create_inventory", v.Version)
	c["inventory"] = map[string]any{"item_definition_id": f.def.ID, "quantity": 0.3, "quantity_unit_id": f.units["kg"]}
	f.send(t, "setup", c, 200)
	c["inventory"].(map[string]any)["quantity"] = json.RawMessage("0.3000000000000000001")
	f.send(t, "setup", c, 409)
	var count int64
	require.NoError(t, f.db.Model(&models.InventoryItem{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
}
