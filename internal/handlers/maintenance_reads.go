package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/i18n"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/maintenance"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/utils"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const maintenanceReadContract = "occurrences-v1"

var errMaintenanceReadQuery = errors.New("invalid maintenance read query")

type scheduleView struct {
	models.MaintenanceSchedule
	Requirements []maintenance.Requirement `json:"requirements"`
}
type occurrenceView struct {
	models.MaintenanceOccurrence
	Requirements []maintenance.Requirement `json:"requirements"`
}
type consumptionView struct {
	ID                     uuid.UUID  `json:"id"`
	ExecutionID            uuid.UUID  `json:"execution_id"`
	InventoryTransactionID uuid.UUID  `json:"inventory_transaction_id"`
	ItemDefinitionID       uuid.UUID  `json:"item_definition_id"`
	InventoryItemID        uuid.UUID  `json:"inventory_item_id"`
	DefinitionName         string     `json:"definition_name"`
	BaseSizeUnitID         uuid.UUID  `json:"base_size_unit_id"`
	BaseUnitName           string     `json:"base_unit_name"`
	UnitFamily             string     `json:"unit_family"`
	Quantity               float64    `json:"quantity"`
	ExpirationDate         *time.Time `json:"expiry_date"`
	CreatedAt              time.Time  `json:"created_at"`
}
type executionView struct {
	ID           uuid.UUID         `json:"id"`
	OccurrenceID *uuid.UUID        `json:"occurrence_id"`
	LegacyTaskID uuid.UUID         `json:"legacy_task_id"`
	ActorUserID  uuid.UUID         `json:"actor_user_id"`
	RequestID    *uuid.UUID        `json:"request_id"`
	Version      int64             `json:"version"`
	PerformedAt  time.Time         `json:"performed_at"`
	RecordedAt   time.Time         `json:"recorded_at"`
	Consumptions []consumptionView `json:"consumptions"`
}

type maintenanceReadQuery struct {
	Limit, Offset int
	State         string
	ScheduleID    *uuid.UUID
	After, Before *time.Time
}

func readMaintenanceQuery(c *gin.Context, occurrences bool) (maintenanceReadQuery, error) {
	q := maintenanceReadQuery{Limit: 50}
	for key, values := range c.Request.URL.Query() {
		if len(values) != 1 || values[0] == "" {
			return q, errMaintenanceReadQuery
		}
		value := values[0]
		switch key {
		case "limit", "offset":
			n, err := strconv.ParseInt(value, 10, 32)
			if err != nil || n < 0 {
				return q, errMaintenanceReadQuery
			}
			if key == "limit" {
				if n < 1 || n > 100 {
					return q, errMaintenanceReadQuery
				}
				q.Limit = int(n)
			} else {
				q.Offset = int(n)
			}
		case "state":
			q.State = value
		case "schedule_id":
			id, err := uuid.Parse(value)
			if !occurrences || err != nil || id == uuid.Nil {
				return q, errMaintenanceReadQuery
			}
			q.ScheduleID = &id
		case "due_after", "due_before":
			at, err := time.Parse(time.RFC3339Nano, value)
			if !occurrences || err != nil || at.Year() < 1 {
				return q, errMaintenanceReadQuery
			}
			at = at.UTC()
			if key == "due_after" {
				q.After = &at
			} else {
				q.Before = &at
			}
		default:
			return q, errMaintenanceReadQuery
		}
	}
	if q.After != nil && q.Before != nil && q.After.After(*q.Before) {
		return q, errMaintenanceReadQuery
	}
	if q.State != "" {
		valid := q.State == models.OccurrencePending || q.State == models.OccurrenceCompleted || q.State == models.OccurrenceSkipped || q.State == models.OccurrenceCancelled
		if !occurrences {
			valid = q.State == models.ScheduleActive || q.State == models.SchedulePaused || q.State == models.ScheduleNeedsReview || q.State == models.ScheduleArchived
		}
		if !valid {
			return q, errMaintenanceReadQuery
		}
	}
	return q, nil
}

// Authorize and assemble each multi-query read from one consistent snapshot.
// Translate after releasing the read transaction (also works with small pools).
func (h *MaintenanceTaskHandler) maintenanceRead(c *gin.Context, run func(*gorm.DB, uuid.UUID) (any, error), missing string) {
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Maintenance-Contract", maintenanceReadContract)
	user, ok := utils.GetAuthUserID(c, h.DB)
	if !ok {
		return
	}
	home, err := uuid.Parse(c.GetHeader("X-Home-Id"))
	if err != nil || home == uuid.Nil {
		c.JSON(400, gin.H{"code": "invalid_request", "error": i18n.TranslateDB(h.DB, c, "Invalid home_id")})
		return
	}
	var result any
	err = h.DB.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var member models.UserHome
		if err := tx.First(&member, "user_id = ? AND home_id = ?", user, home).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return maintenance.ErrForbidden
			}
			return err
		}
		if member.Role != models.RoleOwner && member.Role != models.RoleEditor && member.Role != models.RoleViewer {
			return maintenance.ErrForbidden
		}
		var err error
		result, err = run(tx, home)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err == nil {
		c.JSON(http.StatusOK, result)
		return
	}
	status, code, key := 500, "maintenance_storage_error", "Failed to fetch maintenance history"
	switch {
	case errors.Is(err, errMaintenanceReadQuery):
		status, code, key = 400, "invalid_request", "Invalid maintenance query"
	case errors.Is(err, maintenance.ErrForbidden):
		status, code, key = 403, "read_access_denied", "Access denied to this home"
	case errors.Is(err, gorm.ErrRecordNotFound):
		status, code, key = 404, "resource_not_found", missing
	}
	c.JSON(status, gin.H{"code": code, "error": i18n.TranslateDB(h.DB, c, key)})
}

func (h *MaintenanceTaskHandler) GetMaintenanceSchedules(c *gin.Context) {
	h.maintenanceRead(c, func(tx *gorm.DB, home uuid.UUID) (any, error) {
		q, err := readMaintenanceQuery(c, false)
		if err != nil {
			return nil, err
		}
		query := tx.Where("home_id = ?", home)
		if q.State != "" {
			query = query.Where("state = ?", q.State)
		}
		var rows []models.MaintenanceSchedule
		if err := query.Order("created_at, id").Limit(q.Limit + 1).Offset(q.Offset).Find(&rows).Error; err != nil {
			return nil, err
		}
		var next *int
		if len(rows) > q.Limit {
			value := q.Offset + q.Limit
			next = &value
			rows = rows[:q.Limit]
		}
		views := make([]scheduleView, 0, len(rows))
		for _, row := range rows {
			view := scheduleView{MaintenanceSchedule: row}
			if err := json.Unmarshal([]byte(row.RequirementsJSON), &view.Requirements); err != nil {
				return nil, err
			}
			views = append(views, view)
		}
		return gin.H{"contract": maintenanceReadContract, "items": views, "next_offset": next}, nil
	}, "Maintenance schedule not found")
}
func (h *MaintenanceTaskHandler) GetMaintenanceSchedule(c *gin.Context) {
	h.maintenanceRead(c, func(tx *gorm.DB, home uuid.UUID) (any, error) {
		if len(c.Request.URL.Query()) > 0 {
			return nil, errMaintenanceReadQuery
		}
		id, err := uuid.Parse(c.Param("id"))
		if err != nil || id == uuid.Nil {
			return nil, errMaintenanceReadQuery
		}
		var row models.MaintenanceSchedule
		if err := tx.First(&row, "id = ? AND home_id = ?", id, home).Error; err != nil {
			return nil, err
		}
		view := scheduleView{MaintenanceSchedule: row}
		if err := json.Unmarshal([]byte(row.RequirementsJSON), &view.Requirements); err != nil {
			return nil, err
		}
		return view, nil
	}, "Maintenance schedule not found")
}
func (h *MaintenanceTaskHandler) GetMaintenanceOccurrences(c *gin.Context) {
	h.maintenanceRead(c, func(tx *gorm.DB, home uuid.UUID) (any, error) {
		q, err := readMaintenanceQuery(c, true)
		if err != nil {
			return nil, err
		}
		query := tx.Where("home_id = ?", home)
		if q.State != "" {
			query = query.Where("state = ?", q.State)
		}
		if q.ScheduleID != nil {
			var schedule models.MaintenanceSchedule
			if err := tx.Select("id").First(&schedule, "id = ? AND home_id = ?", q.ScheduleID, home).Error; err != nil {
				return nil, err
			}
			query = query.Where("schedule_id = ?", q.ScheduleID)
		}
		if q.After != nil {
			query = query.Where("due_at >= ?", q.After)
		}
		if q.Before != nil {
			query = query.Where("due_at <= ?", q.Before)
		}
		var rows []models.MaintenanceOccurrence
		if err := query.Order("due_at ASC NULLS LAST, id ASC").Limit(q.Limit + 1).Offset(q.Offset).Find(&rows).Error; err != nil {
			return nil, err
		}
		var next *int
		if len(rows) > q.Limit {
			value := q.Offset + q.Limit
			next = &value
			rows = rows[:q.Limit]
		}
		views := make([]occurrenceView, 0, len(rows))
		for _, row := range rows {
			view := occurrenceView{MaintenanceOccurrence: row}
			if err := json.Unmarshal([]byte(row.RequirementsJSON), &view.Requirements); err != nil {
				return nil, err
			}
			views = append(views, view)
		}
		return gin.H{"contract": maintenanceReadContract, "items": views, "next_offset": next}, nil
	}, "Maintenance schedule not found")
}
func loadOccurrence(tx *gorm.DB, home uuid.UUID, idText string) (occurrenceView, error) {
	id, err := uuid.Parse(idText)
	if err != nil || id == uuid.Nil {
		return occurrenceView{}, errMaintenanceReadQuery
	}
	var view occurrenceView
	if err := tx.First(&view.MaintenanceOccurrence, "id = ? AND home_id = ?", id, home).Error; err != nil {
		return view, err
	}
	err = json.Unmarshal([]byte(view.RequirementsJSON), &view.Requirements)
	return view, err
}
func (h *MaintenanceTaskHandler) GetMaintenanceOccurrence(c *gin.Context) {
	h.maintenanceRead(c, func(tx *gorm.DB, home uuid.UUID) (any, error) {
		if len(c.Request.URL.Query()) > 0 {
			return nil, errMaintenanceReadQuery
		}
		return loadOccurrence(tx, home, c.Param("id"))
	}, "Maintenance occurrence not found")
}
func (h *MaintenanceTaskHandler) GetMaintenanceOccurrenceHistory(c *gin.Context) {
	h.maintenanceRead(c, func(tx *gorm.DB, home uuid.UUID) (any, error) {
		if len(c.Request.URL.Query()) > 0 {
			return nil, errMaintenanceReadQuery
		}
		view, err := loadOccurrence(tx, home, c.Param("id"))
		if err != nil {
			return nil, err
		}
		var rows []models.MaintenanceExecution
		if err := tx.Where("home_id = ? AND occurrence_id = ?", home, view.ID).Order("completed_at, id").Find(&rows).Error; err != nil {
			return nil, err
		}
		executions := make([]executionView, 0, len(rows))
		for _, row := range rows {
			execution := executionView{ID: row.ID, OccurrenceID: row.OccurrenceID, LegacyTaskID: row.TaskID, ActorUserID: row.ActorUserID, RequestID: row.RequestID, Version: row.TaskVersion, PerformedAt: row.CompletedAt.UTC(), RecordedAt: row.CreatedAt.UTC(), Consumptions: []consumptionView{}}
			var used []models.MaintenanceConsumption
			if err := tx.Where("execution_id = ?", row.ID).Order("created_at, id").Find(&used).Error; err != nil {
				return nil, err
			}
			for _, usage := range used {
				execution.Consumptions = append(execution.Consumptions, consumptionView{usage.ID, usage.ExecutionID, usage.InventoryTransactionID, usage.ItemDefinitionID, usage.InventoryItemID, usage.DefinitionName, usage.BaseSizeUnitID, usage.BaseUnitName, usage.UnitFamily, usage.Quantity, usage.ExpirationDate, usage.CreatedAt})
			}
			executions = append(executions, execution)
		}
		return gin.H{"contract": maintenanceReadContract, "occurrence": view, "execution_history_known": len(executions) > 0, "executions": executions}, nil
	}, "Maintenance occurrence not found")
}
