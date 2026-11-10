package handlers

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestMaintenancePostgresConcurrentEditDelete(t *testing.T) {
	if os.Getenv("UNIT_TEST_POSTGRES_DSN") == "" {
		t.Skip("requires disposable PostgreSQL via UNIT_TEST_POSTGRES_DSN")
	}
	for _, method := range []string{"PUT", "DELETE"} {
		t.Run(method, func(t *testing.T) {
			f := newUnitFixture(t)
			maintenanceRoutes(f)
			stock := f.stock(t, 1, "kg")
			task := maintenanceTask(t, f, 0.1)
			path := "/maintenance/" + task.ID.String()
			body := maintenanceBody(f, 0.2)
			body["expected_version"] = 0
			edit, _ := json.Marshal(body)
			command, _ := json.Marshal(map[string]any{"request_id": uuid.New(), "expected_version": 0})
			start := make(chan struct{})
			completed, mutated := make(chan int, 1), make(chan int, 1)
			go func() { <-start; completed <- maintenanceRaw(f, "POST", path+"/complete", string(command)).Code }()
			go func() { <-start; mutated <- maintenanceRaw(f, method, path+"?expected_version=0", string(edit)).Code }()
			close(start)
			completeStatus, mutationStatus := <-completed, <-mutated
			var saved models.InventoryItem
			require.NoError(t, f.db.First(&saved, stock.ID).Error)
			var n int64
			require.NoError(t, f.db.Model(&models.MaintenanceExecution{}).Count(&n).Error)
			if completeStatus == 200 {
				require.Equal(t, 409, mutationStatus)
				require.Equal(t, 900.0, saved.Quantity)
				require.EqualValues(t, 1, n)
			} else {
				require.Equal(t, 200, mutationStatus)
				expected := 409
				if method == "DELETE" {
					expected = 404
				}
				require.Equal(t, expected, completeStatus)
				require.Equal(t, 1000.0, saved.Quantity)
				require.Zero(t, n)
			}
		})
	}
}
