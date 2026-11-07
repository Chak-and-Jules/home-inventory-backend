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

type guideFixture struct {
	*unitFixture
	handler *GuideHandler
	router  *gin.Engine
	now     time.Time
}

func newGuideFixture(t *testing.T) *guideFixture {
	t.Helper()
	base := newUnitFixture(t)
	require.NoError(t, base.db.AutoMigrate(&models.GuideProgress{}, &models.GuideMutation{}))
	f := &guideFixture{unitFixture: base, now: time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)}
	f.handler = &GuideHandler{DB: base.db, Now: func() time.Time { return f.now }}
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if id, err := uuid.Parse(c.GetHeader("X-Test-User")); err == nil {
			c.Set("userID", id)
		}
	})
	r.GET("/guides", f.handler.List)
	r.POST("/guides/:id/actions", f.handler.Act)
	f.router = r
	return f
}
func (f *guideFixture) send(t *testing.T, user uuid.UUID, home uuid.UUID, method, path string, body any, want int) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	require.NoError(t, err)
	request := httptest.NewRequest(method, path, bytes.NewReader(b))
	request.Header.Set("Content-Type", "application/json")
	if user != uuid.Nil {
		request.Header.Set("X-Test-User", user.String())
	}
	if home != uuid.Nil {
		request.Header.Set("X-Home-Id", home.String())
	}
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, request)
	require.Equal(t, want, w.Code, w.Body.String())
	return w
}
func (f *guideFixture) list(t *testing.T, platform string) ([]GuideView, *string) {
	t.Helper()
	w := f.send(t, f.user, f.home, "GET", "/guides?platform="+platform+"&context=dashboard&capabilities="+guides.Introduction+":1", nil, 200)
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	var result struct {
		Guides     []GuideView `json:"guides"`
		Invitation *string     `json:"invitation_id"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	return result.Guides, result.Invitation
}
func action(platform, name string, version uint64) GuideActionRequest {
	return GuideActionRequest{Platform: platform, PlatformRevision: 1, Revision: 1, Action: name, ExpectedVersion: &version, RequestID: uuid.New()}
}
func (f *guideFixture) act(t *testing.T, r GuideActionRequest, want int) GuideView {
	t.Helper()
	w := f.send(t, f.user, f.home, "POST", "/guides/"+guides.Introduction+"/actions", r, want)
	var v GuideView
	if want == 200 {
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &v))
	}
	return v
}
func (f *guideFixture) start(t *testing.T, platform string) GuideView {
	v := f.act(t, action(platform, "start", 0), 200)
	r := action(platform, "checkpoint", v.Version)
	r.Step = v.Steps[1]
	return f.act(t, r, 200)
}
func (f *guideFixture) selectStock(t *testing.T, platform string, v GuideView) GuideView {
	item := f.stock(t, 1, "kg")
	r := action(platform, "choose_inventory", v.Version)
	r.InventoryID = &item.ID
	return f.act(t, r, 200)
}

func TestGuideCreateResumeCompleteAndRetry(t *testing.T) {
	f := newGuideFixture(t)
	views, invite := f.list(t, "web")
	require.Len(t, views, 1)
	require.Equal(t, guides.Introduction, *invite)
	v := f.start(t, "web")
	kg := f.units["kg"]
	create := action("web", "create_definition", v.Version)
	create.Definition = &ItemDefinitionRequest{Name: "Rice", SizeUnitID: &kg}
	v = f.act(t, create, 200)
	require.Equal(t, "stock", v.Checkpoint.Step)
	require.NotNil(t, v.Checkpoint.DefinitionID)
	// A lost response may be retried after refresh, without another definition.
	again := f.act(t, create, 200)
	require.Equal(t, v.Version, again.Version)
	require.Equal(t, *v.Checkpoint.DefinitionID, *again.Checkpoint.DefinitionID)
	var count int64
	require.NoError(t, f.db.Model(&models.ItemDefinition{}).Where("name = ?", "Rice").Count(&count).Error)
	require.EqualValues(t, 1, count)
	views, _ = f.list(t, "web")
	require.Equal(t, "stock", views[0].Checkpoint.Step)
	require.Equal(t, *v.Checkpoint.DefinitionID, *views[0].Checkpoint.DefinitionID)
	invalid := action("web", "complete", v.Version)
	f.act(t, invalid, 400)
	amount := -1.0
	save := action("web", "create_inventory", v.Version)
	save.Inventory = &CreateInventoryItemRequest{ItemDefinitionID: *v.Checkpoint.DefinitionID, Quantity: &amount, QuantityUnitID: &kg}
	f.act(t, save, 400)
	views, _ = f.list(t, "web")
	require.Equal(t, v.Version, views[0].Version)
	require.Equal(t, v.Checkpoint.DefinitionID, views[0].Checkpoint.DefinitionID)
	amount = 3
	v = f.act(t, save, 200)
	require.Equal(t, "dashboard", v.Checkpoint.Step)
	var item models.InventoryItem
	require.NoError(t, f.db.First(&item, "id = ?", v.Checkpoint.InventoryID).Error)
	require.Equal(t, 3000.0, item.Quantity)
	again = f.act(t, save, 200)
	require.Equal(t, v.Version, again.Version)
	require.NoError(t, f.db.Model(&models.InventoryItem{}).Where("item_definition_id = ?", item.ItemDefinitionID).Count(&count).Error)
	require.EqualValues(t, 1, count)
	v = f.act(t, action("web", "complete", v.Version), 200)
	require.Equal(t, "completed", v.Status)
	mobile, invite := f.list(t, "mobile")
	require.Equal(t, "completed", mobile[0].Status)
	require.Nil(t, invite)
	// A late retry returns the newer completed state; it cannot revive its checkpoint.
	again = f.act(t, create, 200)
	require.Equal(t, "completed", again.Status)
	require.Equal(t, v.Version, again.Version)
}

func TestGuideOutcomesRestoreDeferralAndStaleClients(t *testing.T) {
	for _, platform := range []string{"web", "mobile"} {
		for _, outcome := range []string{"skip", "complete"} {
			t.Run(platform+outcome, func(t *testing.T) {
				f := newGuideFixture(t)
				other := "web"
				if platform == "web" {
					other = "mobile"
				}
				v := f.start(t, platform)
				stale := action(platform, "checkpoint", v.Version)
				stale.Step = v.Steps[2]
				if outcome == "complete" {
					v = f.selectStock(t, platform, v)
				}
				v = f.act(t, action(platform, outcome, v.Version), 200)
				for _, p := range []string{"web", "mobile"} {
					list, inv := f.list(t, p)
					require.Equal(t, v.Status, list[0].Status)
					require.Nil(t, inv)
				}
				v = f.act(t, action(platform, "restore", v.Version), 200)
				require.Equal(t, "in_progress", v.Status)
				require.True(t, v.Checkpoint.Restored)
				list, _ := f.list(t, other)
				require.Equal(t, map[string]string{"skip": "skipped", "complete": "completed"}[outcome], list[0].Status)
				f.act(t, stale, 409)
				// A refresh/login does not overwrite a local restore from the shared outcome.
				list, inv := f.list(t, platform)
				require.Equal(t, "in_progress", list[0].Status)
				require.NotNil(t, inv)
				v = f.act(t, action(platform, "start", v.Version), 200)
				require.True(t, v.Checkpoint.Restored)
				v = f.act(t, action(platform, "skip", v.Version), 200)
				for _, p := range []string{"web", "mobile"} {
					list, inv := f.list(t, p)
					require.Equal(t, "skipped", list[0].Status)
					require.Nil(t, inv)
				}
			})
		}
	}
	f := newGuideFixture(t)
	v := f.start(t, "web")
	v = f.act(t, action("web", "defer", v.Version), 200)
	require.Equal(t, "definition", v.Checkpoint.Step)
	require.Equal(t, "in_progress", v.Status)
	_, invite := f.list(t, "web")
	require.Nil(t, invite)
	mobile, invite := f.list(t, "mobile")
	require.NotNil(t, invite)
	require.Equal(t, "available", mobile[0].Status)
	require.Equal(t, "home-basics", mobile[0].Checkpoint.Step)
	f.now = f.now.Add(25 * time.Hour)
	web, invite := f.list(t, "web")
	require.NotNil(t, invite)
	require.Equal(t, "definition", web[0].Checkpoint.Step)
}

func TestGuideCompletionAfterPlatformRestorePropagatesAgain(t *testing.T) {
	for _, platform := range []string{"web", "mobile"} {
		t.Run(platform, func(t *testing.T) {
			f := newGuideFixture(t)
			v := f.act(t, action(platform, "skip", 0), 200)
			v = f.act(t, action(platform, "restore", v.Version), 200)
			r := action(platform, "checkpoint", v.Version)
			r.Step = v.Steps[1]
			v = f.act(t, r, 200)
			v = f.selectStock(t, platform, v)
			v = f.act(t, action(platform, "complete", v.Version), 200)
			for _, p := range []string{"web", "mobile"} {
				list, invite := f.list(t, p)
				require.Equal(t, "completed", list[0].Status)
				require.False(t, list[0].Checkpoint.Restored)
				require.Nil(t, invite)
			}
		})
	}
}

func TestGuideResourceReferencesRemainInsideSelectedHome(t *testing.T) {
	f := newGuideFixture(t)
	other := uuid.New()
	require.NoError(t, f.db.Create(&models.Home{ID: other, Name: "Other home"}).Error)
	category := models.Category{ID: uuid.New(), HomeID: other, Name: "Other category"}
	require.NoError(t, f.db.Create(&category).Error)
	kg := f.units["kg"]
	def := models.ItemDefinition{ID: uuid.New(), HomeID: other, Name: "Other definition", SizeUnitID: &kg}
	require.NoError(t, f.db.Create(&def).Error)
	item := models.InventoryItem{ID: uuid.New(), HomeID: other, ItemDefinitionID: def.ID, Quantity: 1}
	require.NoError(t, f.db.Create(&item).Error)
	v := f.start(t, "web")
	r := action("web", "choose_definition", v.Version)
	r.DefinitionID = &def.ID
	f.act(t, r, 404)
	r = action("web", "choose_inventory", v.Version)
	r.InventoryID = &item.ID
	f.act(t, r, 404)
	r = action("web", "create_definition", v.Version)
	r.Definition = &ItemDefinitionRequest{Name: "Wrong category", SizeUnitID: &kg, CategoryID: &category.ID}
	f.act(t, r, 404)
	list, _ := f.list(t, "web")
	require.Equal(t, v.Version, list[0].Version)
	require.Nil(t, list[0].Checkpoint.DefinitionID)
}

func TestGuidePublicationPrerequisitesCapabilitiesAndRevisionPolicy(t *testing.T) {
	f := newGuideFixture(t)
	intro := guides.Catalog()[0]
	later := guides.Definition{ID: "later-feature", Revision: 1, Active: true, PublishedAt: f.now.Add(time.Hour), Priority: 20, Contexts: []string{"dashboard", "feature"}, Prerequisites: []string{guides.Introduction}, Variants: map[string]guides.Variant{"web": {Revision: 1, Steps: []string{"learn", "finish"}}}}
	f.handler.Catalog = []guides.Definition{intro, later}
	get := func(context, platform, caps string) struct {
		Guides     []GuideView `json:"guides"`
		Invitation *string     `json:"invitation_id"`
	} {
		w := f.send(t, f.user, f.home, "GET", fmt.Sprintf("/guides?platform=%s&context=%s&capabilities=%s", platform, context, caps), nil, 200)
		var result struct {
			Guides     []GuideView `json:"guides"`
			Invitation *string     `json:"invitation_id"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
		return result
	}
	caps := guides.Introduction + ":1,later-feature:1"
	require.Len(t, get("dashboard", "web", caps).Guides, 1)
	v := f.start(t, "web")
	v = f.selectStock(t, "web", v)
	v = f.act(t, action("web", "complete", v.Version), 200)
	f.now = f.now.Add(2 * time.Hour)
	// A newly published guide reaches an existing account independently of registration.
	result := get("feature", "web", caps)
	require.Len(t, result.Guides, 2)
	require.Equal(t, "later-feature", *result.Invitation)
	require.Len(t, get("feature", "mobile", caps).Guides, 1)
	require.Len(t, get("feature", "web", guides.Introduction+":1").Guides, 1)
	require.Nil(t, get("settings", "web", caps).Invitation)
	// Routine catalog/copy edits keep the revision and disposition.
	f.handler.Catalog[0].Priority = 11
	web, _ := f.list(t, "web")
	require.Equal(t, "completed", web[0].Status)
	f.handler.Catalog[0].Variants["web"] = guides.Variant{Revision: 2, Steps: []string{"new-welcome", "new-end"}}
	result = get("dashboard", "web", guides.Introduction+":2")
	require.Equal(t, "completed", result.Guides[0].Status)
	// A deliberate logical revision makes the introduction eligible again.
	f.handler.Catalog[0].Revision = 2
	result = get("dashboard", "web", guides.Introduction+":2")
	require.Equal(t, "available", result.Guides[0].Status)
	require.NotNil(t, result.Invitation)
	f.act(t, action("web", "start", v.Version), 409)
}

func TestGuideAuthorizationAndViewerPath(t *testing.T) {
	f := newGuideFixture(t)
	f.send(t, uuid.Nil, f.home, "GET", "/guides?platform=web", nil, 401)
	other := uuid.New()
	require.NoError(t, f.db.Create(&models.Profile{ID: other, Email: "other@example.test"}).Error)
	f.send(t, other, f.home, "GET", "/guides?platform=web", nil, 403)
	outsider := uuid.New()
	require.NoError(t, f.db.Create(&models.Home{ID: outsider, Name: "Other"}).Error)
	f.send(t, f.user, outsider, "POST", "/guides/"+guides.Introduction+"/actions", action("web", "start", 0), 403)
	require.NoError(t, f.db.Model(&models.UserHome{}).Where("user_id = ? AND home_id = ?", f.user, f.home).Update("role", models.RoleViewer).Error)
	v := f.start(t, "mobile")
	kg := f.units["kg"]
	r := action("mobile", "create_definition", v.Version)
	r.Definition = &ItemDefinitionRequest{Name: "Forbidden", SizeUnitID: &kg}
	f.act(t, r, 403)
	// Viewers can learn without being made to create records.
	for _, step := range []string{"add-stock", "dashboard-tips"} {
		r = action("mobile", "checkpoint", v.Version)
		r.Step = step
		v = f.act(t, r, 200)
	}
	v = f.act(t, action("mobile", "complete", v.Version), 200)
	require.Equal(t, "completed", v.Status)
}

func TestGuideCheckpointIsolationAndConcurrentActions(t *testing.T) {
	f := newGuideFixture(t)
	v := f.start(t, "web")
	r := action("mobile", "start", v.Version)
	v = f.act(t, r, 200)
	require.Equal(t, "home-basics", v.Checkpoint.Step)
	r = action("mobile", "checkpoint", v.Version)
	r.Step = "definition"
	f.act(t, r, 400)
	r = action("mobile", "checkpoint", v.Version)
	r.Step = "choose-item"
	v = f.act(t, r, 200)
	web, _ := f.list(t, "web")
	require.Equal(t, "definition", web[0].Checkpoint.Step)
	// Two devices using the same version cannot both overwrite progress.
	var group sync.WaitGroup
	codes := make(chan int, 2)
	for _, name := range []string{"skip", "defer"} {
		group.Add(1)
		go func(name string) {
			defer group.Done()
			req := action("web", name, v.Version)
			b, _ := json.Marshal(req)
			request := httptest.NewRequest("POST", "/guides/"+guides.Introduction+"/actions", bytes.NewReader(b))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("X-Test-User", f.user.String())
			request.Header.Set("X-Home-Id", f.home.String())
			w := httptest.NewRecorder()
			f.router.ServeHTTP(w, request)
			codes <- w.Code
		}(name)
	}
	group.Wait()
	close(codes)
	counts := map[int]int{}
	for code := range codes {
		counts[code]++
	}
	require.Equal(t, map[int]int{200: 1, 409: 1}, counts)
}

func TestGuideOtherGuidesAndUsersAreIndependent(t *testing.T) {
	f := newGuideFixture(t)
	otherGuide := guides.Definition{ID: "another-guide", Revision: 1, Active: true, Priority: 20, Variants: map[string]guides.Variant{"web": {Revision: 1, Steps: []string{"learn", "done"}}}}
	f.handler.Catalog = append(guides.Catalog(), otherGuide)
	r := action("web", "skip", 0)
	f.send(t, f.user, f.home, "POST", "/guides/another-guide/actions", r, 200)
	intro := f.act(t, action("web", "skip", 0), 200)
	f.act(t, action("web", "restore", intro.Version), 200)
	var stored models.GuideProgress
	require.NoError(t, f.db.Where("user_id = ? AND guide_id = ?", f.user, "another-guide").First(&stored).Error)
	require.Equal(t, "skipped", stored.Outcome)
	second := uuid.New()
	require.NoError(t, f.db.Create(&models.Profile{ID: second, Email: "second@example.test"}).Error)
	require.NoError(t, f.db.Create(&models.UserHome{UserID: second, HomeID: f.home, Role: models.RoleEditor}).Error)
	w := f.send(t, second, f.home, "GET", "/guides?platform=web&context=dashboard&capabilities="+guides.Introduction+":1", nil, 200)
	var list struct {
		Guides []GuideView `json:"guides"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
	require.Equal(t, "available", list.Guides[0].Status)
	require.Zero(t, list.Guides[0].Version)
	require.Nil(t, list.Guides[0].Checkpoint.DefinitionID)
	// Account deletion cascades only the deleted user's persisted guide state/receipts.
	require.NoError(t, f.db.Where("user_id = ?", f.user).Delete(&models.UserHome{}).Error)
	require.NoError(t, f.db.Where("id = ?", f.user).Delete(&models.Profile{}).Error)
	var count int64
	require.NoError(t, f.db.Model(&models.GuideProgress{}).Where("user_id = ?", f.user).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, f.db.Model(&models.GuideMutation{}).Where("user_id = ?", f.user).Count(&count).Error)
	require.Zero(t, count)
}

func TestGuideStockFailureRollsBackCheckpointAndCanRetry(t *testing.T) {
	f := newGuideFixture(t)
	v := f.start(t, "web")
	r := action("web", "choose_definition", v.Version)
	r.DefinitionID = &f.def.ID
	v = f.act(t, r, 200)
	require.NoError(t, f.db.Callback().Create().Before("gorm:create").Register("test:guide_log_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "inventory_transactions" {
			tx.AddError(fmt.Errorf("log unavailable"))
		}
	}))
	amount := 2.0
	kg := f.units["kg"]
	r = action("web", "create_inventory", v.Version)
	r.Inventory = &CreateInventoryItemRequest{ItemDefinitionID: f.def.ID, Quantity: &amount, QuantityUnitID: &kg}
	f.act(t, r, 500)
	list, _ := f.list(t, "web")
	require.Equal(t, v.Version, list[0].Version)
	require.Equal(t, "stock", list[0].Checkpoint.Step)
	var count int64
	require.NoError(t, f.db.Model(&models.InventoryItem{}).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, f.db.Callback().Create().Remove("test:guide_log_failure"))
	v = f.act(t, r, 200)
	require.Equal(t, "dashboard", v.Checkpoint.Step)
	// Idempotency IDs cannot be reused for a different operation.
	r.Action = "skip"
	f.act(t, r, 409)
}
