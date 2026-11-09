// Package demos provisions private, editable sample houses and manages their
// lifecycle independently of tutorial completion or restoration.
package demos

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/guides"
	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrUnavailable = errors.New("Demo home is not available")
	ErrExists      = errors.New("Demo home already exists")
	ErrAccess      = errors.New("Access denied to this demo home")
)

type Labels func(string) string

type Service struct {
	DB  *gorm.DB
	Now func() time.Time
}

type View struct {
	Status    string       `json:"status"`
	Version   int64        `json:"version"`
	Home      *models.Home `json:"home"`
	CanCreate bool         `json:"can_create"`
}

type Command struct {
	RequestID       uuid.UUID `json:"request_id"`
	ExpectedVersion *int64    `json:"expected_version"`
	Action          string    `json:"action"`
}

func New(db *gorm.DB) *Service { return &Service{DB: db, Now: time.Now} }

func label(labels Labels, key string) string {
	if labels == nil {
		return key
	}
	return labels(key)
}

func lockUser(tx *gorm.DB, user uuid.UUID) error {
	var profile models.Profile
	return tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").First(&profile, "id = ?", user).Error
}

func registry(db *gorm.DB, user uuid.UUID) (models.UserDemoHome, bool, error) {
	v := models.UserDemoHome{UserID: user}
	err := db.Where("user_id = ?", user).First(&v).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return v, false, nil
	}
	return v, err == nil, err
}

func ownedHome(db *gorm.DB, user uuid.UUID, record models.UserDemoHome) (models.Home, error) {
	db = db.Session(&gorm.Session{}) // Keep independent queries when a locking clause was supplied.
	if record.HomeID == nil {
		return models.Home{}, ErrUnavailable
	}
	var home models.Home
	if err := db.Where("id = ? AND is_demo = ?", record.HomeID, true).First(&home).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return home, ErrAccess
		}
		return home, err
	}
	var member models.UserHome
	if err := db.Where("user_id = ? AND home_id = ? AND role = ?", user, home.ID, models.RoleOwner).First(&member).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return home, ErrAccess
		}
		return home, err
	}
	return home, nil
}

// Status holds a short shared profile lock so version/home cannot be read from
// different lifecycle operations while reset/removal/recreation is committing.
func (s *Service) Status(ctx context.Context, user uuid.UUID) (View, error) {
	var view View
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var profile models.Profile
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Select("id").First(&profile, "id = ?", user).Error; err != nil {
			return err
		}
		record, exists, err := registry(tx, user)
		if err != nil {
			return err
		}
		view = View{Status: "not_created", CanCreate: true}
		if !exists {
			return nil
		}
		view.Version = record.Version
		if record.HomeID == nil {
			view.Status = "removed"
			return nil
		}
		home, err := ownedHome(tx, user, record)
		if err != nil {
			return err
		}
		view.Status, view.CanCreate, view.Home = "available", false, &home
		return nil
	})
	return view, err
}

// EnsureInitialHomes serializes first sign-in on the existing profile row. Real
// and demo creation commit together. Existing users keep their current houses;
// they can explicitly create a demo through the action endpoint.
func (s *Service) EnsureInitialHomes(ctx context.Context, user uuid.UUID, labels Labels) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockUser(tx, user); err != nil {
			return err
		}
		var count int64
		if err := tx.Model(&models.UserHome{}).Where("user_id = ? AND home_id IN (SELECT id FROM homes WHERE is_demo = ?)", user, false).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return nil
		}
		home := models.Home{ID: uuid.New(), Name: label(labels, "My Home")}
		if err := tx.Create(&home).Error; err != nil {
			return err
		}
		if err := tx.Create(&models.UserHome{UserID: user, HomeID: home.ID, Role: models.RoleOwner, IsDefault: true}).Error; err != nil {
			return err
		}
		record, exists, err := registry(tx, user)
		if err != nil {
			return err
		}
		if exists {
			return nil
		} // Including deliberately removed demos.
		if err := tx.Create(&record).Error; err != nil {
			return err
		}
		return s.create(tx, user, &record, labels)
	})
}

func (s *Service) create(tx *gorm.DB, user uuid.UUID, record *models.UserDemoHome, labels Labels) error {
	home := models.Home{ID: uuid.New(), Name: label(labels, "Sample Home"), IsDemo: true}
	if err := tx.Create(&home).Error; err != nil {
		return err
	}
	if err := tx.Create(&models.UserHome{UserID: user, HomeID: home.ID, Role: models.RoleOwner, IsDefault: false}).Error; err != nil {
		return err
	}
	if err := s.seed(tx, home.ID, labels); err != nil {
		return err
	}
	record.HomeID = &home.ID
	return saveRegistry(tx, record, s.Now())
}

func saveRegistry(tx *gorm.DB, record *models.UserDemoHome, now time.Time) error {
	record.Version++
	return tx.Model(&models.UserDemoHome{}).Where("user_id = ?", record.UserID).
		Updates(map[string]any{"home_id": record.HomeID, "version": record.Version, "updated_at": now}).Error
}

func (s *Service) remove(tx *gorm.DB, user uuid.UUID, record *models.UserDemoHome) error {
	home, err := ownedHome(tx.Clauses(clause.Locking{Strength: "UPDATE"}), user, *record)
	if err != nil {
		return err
	}
	if err := clearContent(tx, home.ID); err != nil {
		return err
	}
	if err := tx.Delete(&home).Error; err != nil {
		return err
	}
	record.HomeID = nil
	return saveRegistry(tx, record, s.Now())
}

// RemoveByHome also protects the legacy DELETE /homes/{id} route. It preserves
// the registry tombstone and never selects a different user's demo by ID.
func (s *Service) RemoveByHome(ctx context.Context, user, home uuid.UUID) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockUser(tx, user); err != nil {
			return err
		}
		r, exists, err := registry(tx, user)
		if err != nil {
			return err
		}
		if !exists || r.HomeID == nil || *r.HomeID != home {
			return ErrAccess
		}
		return s.remove(tx, user, &r)
	})
}

func (s *Service) Act(ctx context.Context, user uuid.UUID, c Command, labels Labels) (View, error) {
	if c.RequestID == uuid.Nil || c.ExpectedVersion == nil || *c.ExpectedVersion < 0 || (c.Action != "create" && c.Action != "reset" && c.Action != "remove") {
		return View{}, guides.ErrInvalid
	}
	encoded, _ := json.Marshal(c)
	digest := sha256.Sum256(append([]byte("demo-home\n"), encoded...))
	hash := hex.EncodeToString(digest[:])
	db := s.DB.WithContext(ctx)
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := lockUser(tx, user); err != nil {
			return err
		}
		if replayed, err := guides.CheckReceipt(tx, user, c.RequestID, hash); err != nil || replayed {
			return err
		}
		r, exists, err := registry(tx, user)
		if err != nil {
			return err
		}
		if r.Version != *c.ExpectedVersion {
			return guides.ErrConflict
		}
		if !exists {
			if err := tx.Create(&r).Error; err != nil {
				return err
			}
		}
		switch c.Action {
		case "create":
			if r.HomeID != nil {
				return ErrExists
			}
			if err := s.create(tx, user, &r, labels); err != nil {
				return err
			}
		case "reset":
			home, err := ownedHome(tx.Clauses(clause.Locking{Strength: "UPDATE"}), user, r)
			if err != nil {
				return err
			}
			if err := clearContent(tx, home.ID); err != nil {
				return err
			}
			if err := s.seed(tx, home.ID, labels); err != nil {
				return err
			}
			if err := saveRegistry(tx, &r, s.Now()); err != nil {
				return err
			}
		case "remove":
			if err := s.remove(tx, user, &r); err != nil {
				return err
			}
		}
		return tx.Create(&models.GuideRequest{UserID: user, RequestID: c.RequestID, Fingerprint: hash}).Error
	})
	if err != nil {
		if replayed, replayErr := guides.CheckReceipt(db, user, c.RequestID, hash); replayErr != nil {
			if errors.Is(replayErr, guides.ErrRequestReuse) {
				return View{}, replayErr
			}
		} else if replayed {
			err = nil
		}
	}
	if err != nil {
		return View{}, err
	}
	return s.Status(ctx, user)
}

// Delete children by their owning home, never by a definition reference that
// could accidentally reach records in another house. FK failures roll back.
func clearContent(tx *gorm.DB, home uuid.UUID) error {
	children := []struct {
		model         any
		column, table string
	}{
		{&models.ReceiptJobItem{}, "receipt_job_id", "receipt_jobs"},
		{&models.TaskItemDependency{}, "maintenance_task_id", "maintenance_tasks"},
		{&models.RecipeIngredient{}, "recipe_id", "recipes"},
		{&models.InventoryPrediction{}, "inventory_item_id", "inventory_items"},
	}
	for _, child := range children {
		if err := tx.Where(child.column+" IN (SELECT id FROM "+child.table+" WHERE home_id = ?)", home).Delete(child.model).Error; err != nil {
			return err
		}
	}
	for _, model := range []any{&models.InventoryTransaction{}, &models.ShoppingListItem{}, &models.MaintenanceTask{}, &models.Recipe{}, &models.ReceiptJob{}, &models.InventoryItem{}, &models.ItemDefinition{}, &models.Category{}} {
		if err := tx.Where("home_id = ?", home).Delete(model).Error; err != nil {
			return err
		}
	}
	return nil
}
