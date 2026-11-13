package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"net/url"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/i18n"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/maintenance"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func maintenanceReadRoutes(f *unitFixture) {
	h := &MaintenanceTaskHandler{DB: f.db}
	f.router.GET("/maintenance-schedules", h.GetMaintenanceSchedules)
	f.router.GET("/maintenance-schedules/:id", h.GetMaintenanceSchedule)
	f.router.GET("/maintenance-occurrences", h.GetMaintenanceOccurrences)
	f.router.GET("/maintenance-occurrences/:id", h.GetMaintenanceOccurrence)
	f.router.GET("/maintenance-occurrences/:id/history", h.GetMaintenanceOccurrenceHistory)
}
func readOccurrence(t *testing.T, f *unitFixture, id uuid.UUID) occurrenceView {
	t.Helper()
	w := f.request(t, "GET", "/maintenance-occurrences/"+id.String(), nil, 200)
	var view occurrenceView
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &view))
	return view
}
func TestMaintenanceOccurrenceLegacyTransitionAndReads(t *testing.T) {
	f := newUnitFixture(t)
	maintenanceReadRoutes(f)
	maintenanceRoutes(f)
	stock := f.stock(t, 1, "kg")
	body := maintenanceBody(f, 0.1)
	body["inventory_item_id"] = stock.ID
	w := f.request(t, "POST", "/maintenance", body, 201)
	var task models.MaintenanceTask
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &task))
	occurrence := readOccurrence(t, f, task.ID)
	require.Equal(t, models.OccurrencePending, occurrence.State)
	require.Equal(t, stock.ID, *occurrence.InventoryItemID)
	require.Len(t, occurrence.Requirements, 1)
	require.Equal(t, 100.0, occurrence.Requirements[0].Quantity)
	f.request(t, "GET", "/maintenance-schedules/"+occurrence.ScheduleID.String(), nil, 200)
	initialDue := *occurrence.DueAt
	body = maintenanceBody(f, 0.2)
	later := initialDue.Add(24 * time.Hour)
	body["scheduled_date"] = later
	body["expected_version"] = 0
	f.request(t, "PUT", "/maintenance/"+task.ID.String(), body, 200)
	occurrence = readOccurrence(t, f, task.ID)
	require.EqualValues(t, 1, occurrence.Version)
	require.True(t, initialDue.Equal(*occurrence.OriginalDueAt))
	require.True(t, later.Equal(*occurrence.DueAt))
	require.Equal(t, 200.0, occurrence.Requirements[0].Quantity)
	command := map[string]any{"request_id": uuid.New(), "expected_version": 1}
	f.request(t, "POST", "/maintenance/"+task.ID.String()+"/complete", command, 200)
	w = f.request(t, "GET", "/maintenance-occurrences/"+task.ID.String()+"/history", nil, 200)
	require.Equal(t, "private, no-store", w.Header().Get("Cache-Control"))
	require.Equal(t, maintenanceReadContract, w.Header().Get("X-Maintenance-Contract"))
	var history struct {
		Occurrence occurrenceView  `json:"occurrence"`
		Executions []executionView `json:"executions"`
		Known      bool            `json:"execution_history_known"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &history))
	require.True(t, history.Known)
	require.Len(t, history.Executions, 1)
	require.Equal(t, models.OccurrenceCompleted, history.Occurrence.State)
	require.EqualValues(t, 2, history.Occurrence.Version)
	require.Equal(t, f.user, history.Executions[0].ActorUserID)
	require.Len(t, history.Executions[0].Consumptions, 1)
	require.Equal(t, 200.0, history.Executions[0].Consumptions[0].Quantity)
	var schedule models.MaintenanceSchedule
	require.NoError(t, f.db.First(&schedule, occurrence.ScheduleID).Error)
	require.Equal(t, models.ScheduleArchived, schedule.State)
	// Future defaults and live names cannot rewrite completed requirements.
	require.NoError(t, f.db.Model(&schedule).Updates(map[string]any{"requirements_json": "[]", "description": "Future work", "version": 10}).Error)
	require.NoError(t, f.db.Model(&f.def).Update("name", "New label").Error)
	same := readOccurrence(t, f, task.ID)
	require.Equal(t, 200.0, same.Requirements[0].Quantity)
	require.Equal(t, "Flour", *same.Requirements[0].DefinitionName)
	f.request(t, "GET", "/maintenance/"+task.ID.String(), nil, 200) // unchanged legacy response shape
	other := maintenanceTask(t, f, 0.1)
	f.request(t, "DELETE", "/maintenance/"+other.ID.String(), nil, 200)
	retired := readOccurrence(t, f, other.ID)
	require.Equal(t, models.OccurrenceCancelled, retired.State)
	require.Equal(t, "legacy_delete", *retired.CancellationReason)
	require.Equal(t, f.user, *retired.CancelledBy)
	f.request(t, "GET", "/maintenance/"+other.ID.String(), nil, 404)
}

func TestMaintenanceReadIsolationPagingAndUnknownHistory(t *testing.T) {
	f := newUnitFixture(t)
	maintenanceReadRoutes(f)
	first := maintenanceTask(t, f, 0.1)
	due := first.ScheduledDate
	second := models.MaintenanceTask{ID: uuid.New(), HomeID: f.home, Description: "Old completed work", ScheduledDate: due, IsCompleted: true}
	require.NoError(t, f.db.Create(&second).Error)
	otherHome := models.Home{ID: uuid.New(), Name: "Other"}
	require.NoError(t, f.db.Create(&otherHome).Error)
	require.NoError(t, f.db.Create(&models.UserHome{UserID: f.user, HomeID: otherHome.ID, Role: models.RoleOwner}).Error)
	foreign := models.MaintenanceTask{ID: uuid.New(), HomeID: otherHome.ID, Description: "Private other home", ScheduledDate: due}
	require.NoError(t, f.db.Create(&foreign).Error)
	require.NoError(t, maintenance.InitializeLegacy(context.Background(), f.db))
	w := f.request(t, "GET", "/maintenance-occurrences?limit=1", nil, 200)
	var page struct {
		Items []occurrenceView `json:"items"`
		Next  *int             `json:"next_offset"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
	require.Len(t, page.Items, 1)
	require.NotNil(t, page.Next)
	seen := page.Items[0].ID
	w = f.request(t, "GET", "/maintenance-occurrences?limit=1&offset=1", nil, 200)
	page.Items = nil
	page.Next = nil
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
	require.Len(t, page.Items, 1)
	require.NotEqual(t, seen, page.Items[0].ID)
	require.Nil(t, page.Next)
	var foreignOccurrence models.MaintenanceOccurrence
	require.NoError(t, f.db.First(&foreignOccurrence, foreign.ID).Error)
	for _, path := range []string{"/maintenance-occurrences/" + foreign.ID.String(), "/maintenance-occurrences/" + foreign.ID.String() + "/history", "/maintenance-schedules/" + foreignOccurrence.ScheduleID.String(), "/maintenance-occurrences?schedule_id=" + foreignOccurrence.ScheduleID.String()} {
		f.request(t, "GET", path, nil, 404)
	}
	w = f.request(t, "GET", "/maintenance-occurrences/"+second.ID.String()+"/history", nil, 200)
	require.Contains(t, w.Body.String(), `"execution_history_known":false`)
	require.Contains(t, w.Body.String(), `"executions":[]`)
	require.Contains(t, w.Body.String(), `"completed_at":null`)
	query := url.Values{"state": {"pending"}, "due_after": {due.Format(time.RFC3339Nano)}, "due_before": {due.Format(time.RFC3339Nano)}}
	w = f.request(t, "GET", "/maintenance-occurrences?"+query.Encode(), nil, 200)
	page.Items = nil
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
	require.Len(t, page.Items, 1)
	require.Equal(t, first.ID, page.Items[0].ID)
	owner := f.user
	viewer := uuid.New()
	require.NoError(t, f.db.Create(&models.Profile{ID: viewer, Email: viewer.String() + "@example.com"}).Error)
	require.NoError(t, f.db.Create(&models.UserHome{UserID: viewer, HomeID: f.home, Role: models.RoleViewer}).Error)
	f.user = viewer
	f.request(t, "GET", "/maintenance-schedules", nil, 200)
	f.request(t, "GET", "/maintenance-occurrences/"+first.ID.String()+"/history", nil, 200)
	f.user = uuid.New()
	f.request(t, "GET", "/maintenance-occurrences/"+first.ID.String()+"/history", nil, 403)
	f.user = owner
}

func TestMaintenanceReadValidationAndLocalization(t *testing.T) {
	f := newUnitFixture(t)
	maintenanceReadRoutes(f)
	task := maintenanceTask(t, f, 0.1)
	for _, q := range []string{"limit=0", "limit=101", "offset=-1", "limit=1&limit=2", "state=rubbish", "state=", "unknown=yes", "due_before=bad", "due_after=2030-01-02T00:00:00Z&due_before=2030-01-01T00:00:00Z", "schedule_id=00000000-0000-0000-0000-000000000000"} {
		f.request(t, "GET", "/maintenance-occurrences?"+q, nil, 400)
	}
	f.request(t, "GET", "/maintenance-schedules?due_before=2030-01-01T00:00:00Z", nil, 400)
	f.request(t, "GET", "/maintenance-occurrences/bad", nil, 400)
	f.request(t, "GET", "/maintenance-occurrences/"+task.ID.String()+"?unknown=true", nil, 400)
	missingHome := httptest.NewRecorder()
	f.router.ServeHTTP(missingHome, httptest.NewRequest("GET", "/maintenance-schedules", nil))
	require.Equal(t, 400, missingHome.Code)
	f.request(t, "GET", "/maintenance-occurrences/"+uuid.NewString(), nil, 404)
	language := models.Language{ID: uuid.New(), Name: "Türkçe"}
	require.NoError(t, f.db.Create(&language).Error)
	require.NoError(t, f.db.Model(&models.Profile{}).Where("id = ?", f.user).Update("language_id", language.ID).Error)
	i18n.InvalidateUserLanguageCache(f.user)
	w := f.request(t, "GET", "/maintenance-occurrences/"+uuid.NewString(), nil, 404)
	require.Contains(t, w.Body.String(), "Bakım işi bulunamadı")
	h := &MaintenanceTaskHandler{DB: f.db}
	r := gin.New()
	r.GET("/maintenance-occurrences/:id", h.GetMaintenanceOccurrence)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/maintenance-occurrences/"+task.ID.String(), nil))
	require.Equal(t, 401, w.Code)
}

func TestMaintenanceCanonicalWritesRollbackWithLegacyChanges(t *testing.T) {
	f := newUnitFixture(t)
	maintenanceReadRoutes(f)
	maintenanceRoutes(f)
	task := maintenanceTask(t, f, 0.1)
	before := readOccurrence(t, f, task.ID)
	failure := errors.New("occurrence storage unavailable")
	require.NoError(t, f.db.Callback().Update().After("gorm:update").Register("test:occurrence_fail", func(tx *gorm.DB) {
		if tx.Statement.Table == "maintenance_occurrences" {
			tx.AddError(failure)
		}
	}))
	body := maintenanceBody(f, 0.2)
	body["expected_version"] = 0
	f.request(t, "PUT", "/maintenance/"+task.ID.String(), body, 500)
	require.NoError(t, f.db.Callback().Update().Remove("test:occurrence_fail"))
	after := readOccurrence(t, f, task.ID)
	require.Equal(t, before.Requirements, after.Requirements)
	require.Zero(t, after.Version)
	var live models.MaintenanceTask
	require.NoError(t, f.db.Preload("Dependencies").First(&live, task.ID).Error)
	require.Zero(t, live.Version)
	require.Equal(t, 100.0, live.Dependencies[0].QuantityRequired)
	require.NoError(t, f.db.Callback().Create().After("gorm:create").Register("test:schedule_fail", func(tx *gorm.DB) {
		if tx.Statement.Table == "maintenance_schedules" {
			tx.AddError(failure)
		}
	}))
	f.request(t, "POST", "/maintenance", maintenanceBody(f, 0.1), 500)
	require.NoError(t, f.db.Callback().Create().Remove("test:schedule_fail"))
	require.NoError(t, f.db.Callback().Delete().After("gorm:delete").Register("test:delete_fail", func(tx *gorm.DB) {
		if tx.Statement.Table == "maintenance_tasks" {
			tx.AddError(failure)
		}
	}))
	f.request(t, "DELETE", "/maintenance/"+task.ID.String(), nil, 500)
	require.NoError(t, f.db.Callback().Delete().Remove("test:delete_fail"))
	retained := readOccurrence(t, f, task.ID)
	require.Equal(t, models.OccurrencePending, retained.State)
	require.Zero(t, retained.Version)
	require.Nil(t, retained.CancelledAt)
	var n int64
	for _, model := range []any{&models.MaintenanceTask{}, &models.MaintenanceSchedule{}, &models.MaintenanceOccurrence{}} {
		require.NoError(t, f.db.Model(model).Count(&n).Error)
		require.EqualValues(t, 1, n)
	}
}

func TestMaintenanceHistoryReadSnapshotPostgres(t *testing.T) {
	if os.Getenv("UNIT_TEST_POSTGRES_DSN") == "" {
		t.Skip("requires disposable PostgreSQL")
	}
	f := newUnitFixture(t)
	maintenanceReadRoutes(f)
	f.stock(t, 1, "kg")
	task := maintenanceTask(t, f, 0.1)
	observed, release := make(chan struct{}), make(chan struct{})
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	var held atomic.Bool
	require.NoError(t, f.db.Callback().Query().After("gorm:query").Register("test:read_snapshot", func(tx *gorm.DB) {
		if tx.Statement.Table == "maintenance_occurrences" && held.CompareAndSwap(false, true) {
			close(observed)
			<-release
		}
	}))
	result := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		result <- maintenanceRaw(f, "GET", "/maintenance-occurrences/"+task.ID.String()+"/history", "")
	}()
	select {
	case <-observed:
	case <-time.After(10 * time.Second):
		t.Fatal("history did not read occurrence")
	}
	f.request(t, "POST", "/maintenance/"+task.ID.String()+"/complete", map[string]any{}, 200)
	close(release)
	released = true
	w := <-result
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"state":"pending"`)
	require.Contains(t, w.Body.String(), `"executions":[]`)
	require.NoError(t, f.db.Callback().Query().Remove("test:read_snapshot"))
	w = f.request(t, "GET", "/maintenance-occurrences/"+task.ID.String()+"/history", nil, 200)
	require.Contains(t, w.Body.String(), `"state":"completed"`)
	require.Contains(t, w.Body.String(), `"execution_history_known":true`)
}

func TestMaintenanceLegacyWritesRejectAdvancedCanonicalState(t *testing.T) {
	for _, mode := range []string{"schedule revision", "occurrence revision", "confirmed recurrence"} {
		t.Run(mode, func(t *testing.T) {
			f := newUnitFixture(t)
			maintenanceReadRoutes(f)
			maintenanceRoutes(f)
			stock := f.stock(t, 1, "kg")
			task := maintenanceTask(t, f, 0.1)
			view := readOccurrence(t, f, task.ID)
			switch mode {
			case "schedule revision":
				require.NoError(t, f.db.Model(&models.MaintenanceSchedule{}).Where("id = ?", view.ScheduleID).Update("version", 10).Error)
			case "occurrence revision":
				require.NoError(t, f.db.Model(&models.MaintenanceOccurrence{}).Where("id = ?", view.ID).Update("version", 1).Error)
			case "confirmed recurrence":
				require.NoError(t, f.db.Model(&models.MaintenanceSchedule{}).Where("id = ?", view.ScheduleID).Updates(map[string]any{"recurrence_mode": "after_completion", "time_zone": "Europe/Istanbul"}).Error)
			}
			path := "/maintenance/" + task.ID.String()
			f.request(t, "PUT", path, maintenanceBody(f, 0.2), 409)
			f.request(t, "POST", path+"/complete", map[string]any{}, 409)
			f.request(t, "DELETE", path, nil, 409)
			var current models.MaintenanceTask
			require.NoError(t, f.db.First(&current, task.ID).Error)
			require.Zero(t, current.Version)
			require.False(t, current.IsCompleted)
			require.NoError(t, f.db.First(&stock, stock.ID).Error)
			require.Equal(t, 1000.0, stock.Quantity)
			var n int64
			require.NoError(t, f.db.Model(&models.MaintenanceExecution{}).Count(&n).Error)
			require.Zero(t, n)
		})
	}
}
