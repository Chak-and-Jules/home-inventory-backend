// Package maintenance coordinates task mutations and preserves execution facts
// independently of the live task/batch rows. Occurrence scheduling builds on it.
package maintenance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/quantity"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/utils"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrInvalid      = errors.New("invalid maintenance command")
	ErrNotFound     = errors.New("maintenance task not found")
	ErrForbidden    = errors.New("maintenance write access denied")
	ErrConflict     = errors.New("maintenance task state changed")
	ErrRequestReuse = errors.New("maintenance request ID reused")
	ErrCompleted    = errors.New("completed maintenance task is immutable")
	ErrInsufficient = errors.New("insufficient maintenance supplies")
	ErrDependency   = errors.New("invalid maintenance dependency")
)

type Command struct {
	RequestID       *uuid.UUID `json:"request_id,omitempty"`
	ExpectedVersion *int64     `json:"expected_version,omitempty"`
}

type Result struct {
	TaskID           uuid.UUID  `json:"task_id"`
	ExecutionID      *uuid.UUID `json:"execution_id"`
	CompletedAt      *time.Time `json:"completed_at"`
	Version          int64      `json:"version"`
	AlreadyCompleted bool       `json:"already_completed"`
	Replayed         bool       `json:"replayed"`
}

type Service struct {
	DB  *gorm.DB
	Now func() time.Time
}

func New(db *gorm.DB) *Service { return &Service{DB: db, Now: time.Now} }

// Home KEY SHARE precedes the task lock: reset/home deletion take the home lock
// before cascading into tasks. It also leaves different tasks free to proceed.
func AuthorizeHome(tx *gorm.DB, user, homeID uuid.UUID) error {
	var home models.Home
	if err := tx.Clauses(clause.Locking{Strength: "KEY SHARE"}).Select("id").First(&home, "id = ?", homeID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		return err
	}
	var member models.UserHome
	if err := tx.Where("user_id = ? AND home_id = ?", user, homeID).First(&member).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrForbidden
		}
		return err
	}
	if member.Role != models.RoleOwner && member.Role != models.RoleEditor {
		return ErrForbidden
	}
	return nil
}

// LoadForWrite is shared by completion, edit and deletion. The first read only
// routes to the home lock; the second read is the authoritative task snapshot.
func LoadForWrite(tx *gorm.DB, user, taskID uuid.UUID) (models.MaintenanceTask, error) {
	var route models.MaintenanceTask
	if err := tx.Select("id", "home_id").First(&route, "id = ?", taskID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return route, ErrNotFound
		}
		return route, err
	}
	if err := AuthorizeHome(tx, user, route.HomeID); err != nil {
		return route, err
	}
	var task models.MaintenanceTask
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&task, "id = ? AND home_id = ?", taskID, route.HomeID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return task, ErrNotFound
		}
		return task, err
	}
	return task, nil
}

func CheckVersion(task models.MaintenanceTask, expected *int64) error {
	if expected != nil && *expected != task.Version {
		return ErrConflict
	}
	return nil
}

func utcTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	utc := value.UTC()
	return &utc
}

func fingerprint(taskID uuid.UUID, c Command) string {
	data, _ := json.Marshal(struct {
		Action          string    `json:"action"`
		TaskID          uuid.UUID `json:"task_id"`
		ExpectedVersion *int64    `json:"expected_version"`
	}{"complete", taskID, c.ExpectedVersion})
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func replay(tx *gorm.DB, user uuid.UUID, requestID *uuid.UUID, hash string) (Result, bool, error) {
	if requestID == nil {
		return Result{}, false, nil
	}
	var receipt models.MaintenanceRequest
	err := tx.First(&receipt, "user_id = ? AND request_id = ?", user, requestID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Result{}, false, nil
	}
	if err != nil {
		return Result{}, false, err
	}
	if receipt.Fingerprint != hash {
		return Result{}, false, ErrRequestReuse
	}
	// Replays remain private even after a role/membership change.
	if err := AuthorizeHome(tx, user, receipt.HomeID); err != nil {
		return Result{}, false, err
	}
	return Result{TaskID: receipt.TaskID, ExecutionID: receipt.ExecutionID,
		CompletedAt: utcTime(receipt.CompletedAt), Version: receipt.TaskVersion,
		AlreadyCompleted: receipt.AlreadyCompleted, Replayed: true}, true, nil
}

func (s *Service) Complete(ctx context.Context, user, taskID uuid.UUID, c Command) (Result, error) {
	if user == uuid.Nil || taskID == uuid.Nil || (c.RequestID != nil && *c.RequestID == uuid.Nil) ||
		(c.ExpectedVersion != nil && (*c.ExpectedVersion < 0 || c.RequestID == nil)) {
		return Result{}, ErrInvalid
	}
	db, hash := s.DB.WithContext(ctx), fingerprint(taskID, c)
	var result Result
	err := db.Transaction(func(tx *gorm.DB) error {
		if r, found, err := replay(tx, user, c.RequestID, hash); err != nil || found {
			result = r
			return err
		}
		task, err := LoadForWrite(tx, user, taskID)
		if err != nil {
			return err
		}
		// An identical command may have committed while we waited on the task.
		if r, found, err := replay(tx, user, c.RequestID, hash); err != nil || found {
			result = r
			return err
		}
		if err := CheckVersion(task, c.ExpectedVersion); err != nil {
			return err
		}
		result = Result{TaskID: task.ID, CompletedAt: utcTime(task.CompletedAt), Version: task.Version, AlreadyCompleted: task.IsCompleted}
		if task.IsCompleted {
			var execution models.MaintenanceExecution
			if err := tx.First(&execution, "task_id = ?", task.ID).Error; err == nil {
				result.ExecutionID = &execution.ID
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			// Legacy completed rows without an execution stay unknown: no backfill.
		} else {
			var prior int64
			if err := tx.Model(&models.MaintenanceExecution{}).Where("task_id = ?", task.ID).Count(&prior).Error; err != nil {
				return err
			}
			if prior != 0 {
				return ErrCompleted // An unsupported raw reopen must not consume again.
			}
			if err := s.execute(tx, user, &task, c.RequestID, &result); err != nil {
				return err
			}
		}
		if c.RequestID != nil {
			return tx.Create(&models.MaintenanceRequest{UserID: user, RequestID: *c.RequestID,
				HomeID: task.HomeID, TaskID: task.ID, Fingerprint: hash,
				ExecutionID: result.ExecutionID, CompletedAt: result.CompletedAt,
				TaskVersion: result.Version, AlreadyCompleted: result.AlreadyCompleted,
				CreatedAt: s.Now().UTC()}).Error
		}
		return nil
	})
	if err != nil && c.RequestID != nil {
		// Different tasks lock different rows, but command IDs share one per-user
		// namespace. A unique-key loser must roll back, then acknowledge/conflict.
		var recovered Result
		var found bool
		replayErr := db.Transaction(func(tx *gorm.DB) error {
			var e error
			recovered, found, e = replay(tx, user, c.RequestID, hash)
			return e
		})
		if replayErr == nil && found {
			return recovered, nil
		}
		if errors.Is(replayErr, ErrRequestReuse) || errors.Is(replayErr, ErrForbidden) {
			return Result{}, replayErr
		}
	}
	return result, err
}

func (s *Service) execute(tx *gorm.DB, user uuid.UUID, task *models.MaintenanceTask, requestID *uuid.UUID, result *Result) error {
	// The task lock protects this dependency set from supported edits/deletion.
	if err := tx.Where("maintenance_task_id = ?", task.ID).Order("item_definition_id, id").Find(&task.Dependencies).Error; err != nil {
		return err
	}
	wants := map[uuid.UUID]float64{}
	for _, dep := range task.Dependencies {
		if dep.QuantityRequired <= 0 || quantity.Validate(dep.QuantityRequired) != nil {
			return ErrDependency
		}
		wants[dep.ItemDefinitionID] = quantity.Add(wants[dep.ItemDefinitionID], dep.QuantityRequired)
		if quantity.Validate(wants[dep.ItemDefinitionID]) != nil {
			return ErrDependency
		}
	}
	ids := make([]uuid.UUID, 0, len(wants))
	for id := range wants {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
	snapshot, err := json.Marshal(task)
	if err != nil {
		return err
	}
	// PostgreSQL persists microseconds: first responses and replays must agree.
	now := s.Now().UTC().Truncate(time.Microsecond)
	execution := models.MaintenanceExecution{ID: uuid.New(), HomeID: task.HomeID, TaskID: task.ID,
		ActorUserID: user, RequestID: requestID, TaskVersion: task.Version + 1,
		CompletedAt: now, CreatedAt: now, SnapshotJSON: string(snapshot)}
	if err := tx.Create(&execution).Error; err != nil {
		return err
	}
	for _, id := range ids {
		if err := s.consume(tx, task.HomeID, execution.ID, id, wants[id], now); err != nil {
			return err
		}
	}
	update := tx.Model(&models.MaintenanceTask{}).Where("id = ? AND version = ?", task.ID, task.Version).
		Updates(map[string]any{"is_completed": true, "completed_at": now, "version": task.Version + 1, "updated_at": now})
	if update.Error != nil {
		return update.Error
	}
	if update.RowsAffected != 1 {
		return ErrConflict
	}
	for _, id := range ids {
		if err := utils.UpdateShoppingListForDefinition(tx, task.HomeID, id); err != nil {
			return err
		}
	}
	result.ExecutionID, result.CompletedAt, result.Version = &execution.ID, &now, task.Version+1
	return nil
}

func (s *Service) consume(tx *gorm.DB, home, execution, definition uuid.UUID, wanted float64, now time.Time) error {
	var def models.ItemDefinition
	if err := tx.Where("id = ? AND home_id = ?", definition, home).First(&def).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrDependency
		}
		return err
	}
	catalog, err := models.Catalog(tx)
	if err != nil {
		return err
	}
	baseID := catalog.BaseID(def.SizeUnitID)
	if baseID == nil || *baseID == uuid.Nil {
		return ErrDependency
	}
	base := catalog.Units[*baseID]
	var items []models.InventoryItem
	// Eligibility/expiry changes are intentionally isolated in follow-up #153.
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("home_id = ? AND item_definition_id = ? AND quantity > 0", home, definition).
		Order("expiration_date ASC NULLS LAST, id ASC").Find(&items).Error; err != nil {
		return err
	}
	remaining := wanted
	for _, item := range items {
		if remaining <= 0 {
			break
		}
		deduction := min(item.Quantity, remaining)
		if deduction <= 0 || quantity.Validate(deduction) != nil {
			return ErrDependency
		}
		if err := tx.Model(&item).Update("quantity", quantity.Sub(item.Quantity, deduction)).Error; err != nil {
			return err
		}
		source := models.InventorySourceMaintenance
		log := models.InventoryTransaction{ID: uuid.New(), HomeID: home, ItemDefinitionID: definition,
			InventoryItemID: item.ID, QuantityChange: -deduction, CreatedAt: now,
			Source: &source, MaintenanceExecutionID: &execution}
		if err := tx.Create(&log).Error; err != nil {
			return err
		}
		usage := models.MaintenanceConsumption{ID: uuid.New(), ExecutionID: execution,
			InventoryTransactionID: log.ID, ItemDefinitionID: definition, InventoryItemID: item.ID,
			DefinitionName: def.Name, BaseSizeUnitID: *baseID, BaseUnitName: base.Name,
			UnitFamily: base.Family, Quantity: deduction, ExpirationDate: item.ExpirationDate, CreatedAt: now}
		if err := tx.Create(&usage).Error; err != nil {
			return err
		}
		remaining = quantity.Sub(remaining, deduction)
	}
	if remaining > 0 {
		return ErrInsufficient
	}
	return nil
}
