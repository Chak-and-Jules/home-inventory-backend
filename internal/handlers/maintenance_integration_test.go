package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
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

func maintenanceBody(f *unitFixture, amount float64) map[string]any {
	return map[string]any{"description": "Replace filter", "scheduled_date": time.Now().UTC().Truncate(time.Second), "frequency": "Once", "dependencies": []any{map[string]any{"item_definition_id": f.def.ID, "quantity_required": amount, "quantity_unit_id": f.units["kg"]}}}
}
func maintenanceTask(t *testing.T, f *unitFixture, amount float64) models.MaintenanceTask {
	t.Helper()
	w := f.request(t, "POST", "/maintenance", maintenanceBody(f, amount), 201)
	var task models.MaintenanceTask
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &task))
	return task
}
func maintenanceRaw(f *unitFixture, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Home-Id", f.home.String())
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	return w
}
func maintenanceRoutes(f *unitFixture) {
	h := &MaintenanceTaskHandler{DB: f.db}
	f.router.DELETE("/maintenance/:id", h.DeleteMaintenanceTask)
	f.router.GET("/maintenance/:id", h.GetMaintenanceTask)
}

func TestMaintenanceCompletionHTTP(t *testing.T) {
	f := newUnitFixture(t)
	maintenanceRoutes(f)
	stock := f.stock(t, 1, "kg")
	task := maintenanceTask(t, f, 0.1)
	path := "/maintenance/" + task.ID.String()
	w := f.request(t, "GET", path, nil, 200)
	require.Contains(t, w.Body.String(), `"Version":0`)
	command := map[string]any{"request_id": uuid.New(), "expected_version": 0}
	w = f.request(t, "POST", path+"/complete", command, 200)
	require.Equal(t, "private, no-store", w.Header().Get("Cache-Control"))
	var first maintenance.Result
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &first))
	require.NotNil(t, first.ExecutionID)
	w = f.request(t, "POST", path+"/complete", command, 200)
	var retry maintenance.Result
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &retry))
	require.True(t, retry.Replayed)
	require.Equal(t, first.ExecutionID, retry.ExecutionID)
	w = maintenanceRaw(f, "POST", path+"/complete", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"already_completed":true`)
	// Legacy writers cannot erase a completion or the consumed supplies.
	body := maintenanceBody(f, 0.2)
	f.request(t, "PUT", path, body, 409)
	f.request(t, "DELETE", path, nil, 409)
	var saved models.InventoryItem
	require.NoError(t, f.db.First(&saved, stock.ID).Error)
	require.Equal(t, 900.0, saved.Quantity)
	f.request(t, "POST", "/maintenance/"+uuid.NewString()+"/complete", map[string]any{}, 404)
	// Permissions are checked even for a completed task and an existing receipt.
	require.NoError(t, f.db.Model(&models.UserHome{}).Where("user_id = ? AND home_id = ?", f.user, f.home).Update("role", models.RoleViewer).Error)
	f.request(t, "POST", path+"/complete", command, 403)
	f.request(t, "POST", path+"/complete", map[string]any{}, 403)
}

func TestMaintenanceCommandValidationAndLocalization(t *testing.T) {
	f := newUnitFixture(t)
	task := maintenanceTask(t, f, 0.1)
	path := "/maintenance/" + task.ID.String() + "/complete"
	for _, body := range []string{`null`, `[]`, `{"request_id":null}`, `{"expected_version":null}`, `{"request_id":"bad"}`, `{"request_id":"00000000-0000-0000-0000-000000000000"}`, `{"expected_version":0}`, `{"other":true}`, `{} {}`, `{"request_id":"` + uuid.NewString() + `","expected_version":-1}`, strings.Repeat(" ", 4097)} {
		w := maintenanceRaw(f, "POST", path, body)
		require.Equal(t, 400, w.Code, body)
		require.Contains(t, w.Body.String(), `"code":"invalid_request"`)
	}
	w := maintenanceRaw(f, "POST", "/maintenance/bad/complete", `{}`)
	require.Equal(t, 400, w.Code)
	h := &MaintenanceTaskHandler{DB: f.db}
	router := gin.New()
	router.POST("/maintenance/:id/complete", h.CompleteMaintenanceTask)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("POST", path, nil))
	require.Equal(t, 401, w.Code)
	language := models.Language{ID: uuid.New(), Name: "Türkçe"}
	require.NoError(t, f.db.Create(&language).Error)
	require.NoError(t, f.db.Model(&models.Profile{}).Where("id = ?", f.user).Update("language_id", language.ID).Error)
	i18n.InvalidateUserLanguageCache(f.user)
	w = maintenanceRaw(f, "POST", path, `{"expected_version":0}`)
	require.Contains(t, w.Body.String(), "Geçersiz bakım isteği")
}

func TestMaintenanceEditDeleteVersionsAndDependencies(t *testing.T) {
	f := newUnitFixture(t)
	maintenanceRoutes(f)
	stock := f.stock(t, 1, "kg")
	task := maintenanceTask(t, f, 0.1)
	path := "/maintenance/" + task.ID.String()
	body := maintenanceBody(f, 0.2)
	body["expected_version"] = 0
	f.request(t, "PUT", path, body, 200)
	var saved models.MaintenanceTask
	require.NoError(t, f.db.Preload("Dependencies").First(&saved, task.ID).Error)
	require.EqualValues(t, 1, saved.Version)
	require.Equal(t, 200.0, saved.Dependencies[0].QuantityRequired)
	f.request(t, "PUT", path, body, 409)
	f.request(t, "DELETE", path+"?expected_version=0", nil, 409)
	f.request(t, "POST", path+"/complete", map[string]any{"request_id": uuid.New(), "expected_version": 0}, 409)
	// The legacy command consumes the current dependency set, never the old 100g.
	f.request(t, "POST", path+"/complete", map[string]any{}, 200)
	require.NoError(t, f.db.First(&stock, stock.ID).Error)
	require.Equal(t, 800.0, stock.Quantity)
	other := maintenanceTask(t, f, 0.1)
	otherPath := "/maintenance/" + other.ID.String()
	for _, v := range []string{"", "-1", "abc", "1.5", "0&expected_version=0"} {
		f.request(t, "DELETE", otherPath+"?expected_version="+v, nil, 400)
	}
	body["expected_version"] = -1
	f.request(t, "PUT", otherPath, body, 400)
	// A failure replacing dependencies must roll back the task edit and version.
	require.NoError(t, f.db.Callback().Create().After("gorm:create").Register("maintenance:fail_dependency", func(tx *gorm.DB) {
		if tx.Statement.Table == "task_item_dependencies" {
			tx.AddError(errors.New("dependency insert failed"))
		}
	}))
	body["expected_version"] = 0
	f.request(t, "PUT", otherPath, body, 500)
	require.NoError(t, f.db.Callback().Create().Remove("maintenance:fail_dependency"))
	var unchanged models.MaintenanceTask
	require.NoError(t, f.db.Preload("Dependencies").First(&unchanged, other.ID).Error)
	require.Zero(t, unchanged.Version)
	require.Equal(t, 100.0, unchanged.Dependencies[0].QuantityRequired)
	f.request(t, "DELETE", otherPath+"?expected_version=0", nil, 200)
	f.request(t, "POST", otherPath+"/complete", map[string]any{}, 404)
	var n int64
	require.NoError(t, f.db.Model(&models.TaskItemDependency{}).Where("maintenance_task_id = ?", other.ID).Count(&n).Error)
	require.Zero(t, n)
}

func TestMaintenanceDemoResetClearsAuditOnlyForDemo(t *testing.T) {
	f := newDemoFixture(t)
	demo := f.action(t, "create")
	f.stock(t, 1, "kg")
	personal := maintenanceTask(t, f.unitFixture, 0.1)
	_, err := maintenance.New(f.db).Complete(context.Background(), f.user, personal.ID, maintenance.Command{})
	require.NoError(t, err)
	var sample models.InventoryItem
	require.NoError(t, f.db.Where("home_id = ? AND quantity > 0", demo.Home.ID).First(&sample).Error)
	task := models.MaintenanceTask{ID: uuid.New(), HomeID: demo.Home.ID, Description: "Clean", ScheduledDate: f.now}
	require.NoError(t, f.db.Create(&task).Error)
	require.NoError(t, f.db.Create(&models.TaskItemDependency{ID: uuid.New(), MaintenanceTaskID: task.ID, ItemDefinitionID: sample.ItemDefinitionID, QuantityRequired: 0.001}).Error)
	requestID := uuid.New()
	_, err = maintenance.New(f.db).Complete(context.Background(), f.user, task.ID, maintenance.Command{RequestID: &requestID})
	require.NoError(t, err)
	f.action(t, "reset")
	var executions []models.MaintenanceExecution
	require.NoError(t, f.db.Find(&executions).Error)
	require.Len(t, executions, 1)
	require.Equal(t, f.home, executions[0].HomeID)
	var usages []models.MaintenanceConsumption
	require.NoError(t, f.db.Find(&usages).Error)
	require.Len(t, usages, 1)
	require.Equal(t, executions[0].ID, usages[0].ExecutionID)
	var n int64
	require.NoError(t, f.db.Model(&models.MaintenanceRequest{}).Count(&n).Error)
	require.Zero(t, n)
}
