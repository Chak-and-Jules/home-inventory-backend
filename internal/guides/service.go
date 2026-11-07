package guides

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrInvalid      = errors.New("invalid guide request")
	ErrNotFound     = errors.New("guide not available for this client")
	ErrConflict     = errors.New("guide state changed")
	ErrUnavailable  = errors.New("guide prerequisites are not satisfied")
	ErrTransition   = errors.New("invalid guide transition")
	ErrHome         = errors.New("guide home access denied")
	ErrRequestReuse = errors.New("guide request ID reused")
)

const (
	NotStarted = "not_started"
	InProgress = "in_progress"
	Completed  = "completed"
	Skipped    = "skipped"
)

type State struct {
	Status        string     `json:"status"`
	StepID        string     `json:"step_id"`
	HomeID        *uuid.UUID `json:"home_id"`
	DeferredUntil *time.Time `json:"deferred_until"`
}

type View struct {
	Definition
	Platform           Platform `json:"platform"`
	Variant            Variant  `json:"variant"`
	Version            int64    `json:"version"`
	State              State    `json:"state"`
	Available          bool     `json:"available"`
	UnavailableReasons []string `json:"unavailable_reasons"`
	ShouldOffer        bool     `json:"should_offer"`
}

type Command struct {
	RequestID       uuid.UUID  `json:"request_id"`
	Platform        Platform   `json:"platform"`
	ClientVersion   int        `json:"client_version"`
	Revision        int        `json:"revision"`
	ExpectedVersion *int64     `json:"expected_version"`
	Action          string     `json:"action"`
	StepID          string     `json:"step_id,omitempty"`
	HomeID          *uuid.UUID `json:"home_id,omitempty"`
	DeferUntil      *time.Time `json:"defer_until,omitempty"`
}

type Service struct {
	DB          *gorm.DB
	Definitions []Definition
	Now         func() time.Time
}

func New(db *gorm.DB) *Service { return &Service{DB: db, Definitions: Catalog(), Now: time.Now} }

func (s *Service) definition(id string, p Platform, client int) (Definition, bool) {
	for _, d := range s.Definitions {
		variant, exists := d.Variants[p]
		if d.ID == id && exists && d.Active && !d.PublishedAt.After(s.Now()) && client >= variant.MinClientVersion {
			return d, true
		}
	}
	return Definition{}, false
}

func progress(db *gorm.DB, user uuid.UUID, d Definition) (models.GuideProgress, error) {
	p := models.GuideProgress{UserID: user, GuideID: d.ID, Revision: d.Revision, WebStatus: NotStarted, MobileStatus: NotStarted}
	err := db.Where("user_id = ? AND guide_id = ? AND revision = ?", user, d.ID, d.Revision).First(&p).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = nil
	}
	return p, err
}

func state(p models.GuideProgress, platform Platform) State {
	if platform == Web {
		return State{p.WebStatus, p.WebStepID, p.WebHomeID, p.WebDeferredUntil}
	}
	return State{p.MobileStatus, p.MobileStepID, p.MobileHomeID, p.MobileDeferredUntil}
}

func setState(p *models.GuideProgress, platform Platform, v State) {
	if platform == Web {
		p.WebStatus, p.WebStepID, p.WebHomeID, p.WebDeferredUntil = v.Status, v.StepID, v.HomeID, v.DeferredUntil
	} else {
		p.MobileStatus, p.MobileStepID, p.MobileHomeID, p.MobileDeferredUntil = v.Status, v.StepID, v.HomeID, v.DeferredUntil
	}
}

func membership(db *gorm.DB, user, home uuid.UUID) (string, error) {
	var h models.UserHome
	err := db.Where("user_id = ? AND home_id = ?", user, home).First(&h).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	return h.Role, err
}

func (s *Service) view(db *gorm.DB, user uuid.UUID, d Definition, platform Platform, p models.GuideProgress, home *uuid.UUID) (View, error) {
	if d.Prerequisites == nil {
		d.Prerequisites = []string{}
	}
	if d.Roles == nil {
		d.Roles = []string{}
	}
	v := View{Definition: d, Platform: platform, Variant: d.Variants[platform], Version: p.Version, State: state(p, platform), UnavailableReasons: []string{}}
	// Revoked home access must not leak saved home context back to a client.
	if v.State.HomeID != nil {
		role, err := membership(db, user, *v.State.HomeID)
		if err != nil {
			return View{}, err
		}
		if role == "" {
			v.State.HomeID = nil
		}
	}
	if home == nil {
		home = v.State.HomeID
	}
	if d.RequiresHome {
		if home == nil {
			v.UnavailableReasons = append(v.UnavailableReasons, "home_required")
		} else {
			role, err := membership(db, user, *home)
			if err != nil {
				return View{}, err
			}
			if role == "" {
				v.UnavailableReasons = append(v.UnavailableReasons, "home_access_required")
			} else if len(d.Roles) > 0 && !slices.Contains(d.Roles, role) {
				v.UnavailableReasons = append(v.UnavailableReasons, "role_required")
			}
		}
	}
	for _, id := range d.Prerequisites {
		pre, found := s.definition(id, platform, int(^uint(0)>>1))
		if !found {
			v.UnavailableReasons = append(v.UnavailableReasons, "prerequisite_required")
			break
		}
		previous, err := progress(db, user, pre)
		if err != nil {
			return View{}, err
		}
		if state(previous, platform).Status != Completed {
			v.UnavailableReasons = append(v.UnavailableReasons, "prerequisite_required")
			break
		}
	}
	v.Available = len(v.UnavailableReasons) == 0
	v.ShouldOffer = v.Available && (v.State.Status == NotStarted || v.State.Status == InProgress) && (v.State.DeferredUntil == nil || !v.State.DeferredUntil.After(s.Now()))
	return v, nil
}

func (s *Service) List(ctx context.Context, user uuid.UUID, platform Platform, client int, home *uuid.UUID) ([]View, error) {
	if !platform.Valid() || client < 1 {
		return nil, ErrInvalid
	}
	db := s.DB.WithContext(ctx)
	views := []View{}
	for _, candidate := range s.Definitions {
		d, exists := s.definition(candidate.ID, platform, client)
		if !exists {
			continue
		}
		p, err := progress(db, user, d)
		if err != nil {
			return nil, err
		}
		v, err := s.view(db, user, d, platform, p, home)
		if err != nil {
			return nil, err
		}
		views = append(views, v)
	}
	return views, nil
}

func validate(c Command) error {
	if c.RequestID == uuid.Nil || !c.Platform.Valid() || c.ClientVersion < 1 || c.Revision < 1 || c.ExpectedVersion == nil || *c.ExpectedVersion < 0 {
		return ErrInvalid
	}
	if !slices.Contains([]string{"start", "checkpoint", "maybe_later", "complete", "skip", "restore"}, c.Action) {
		return ErrInvalid
	}
	if (c.Action == "checkpoint") != (c.StepID != "") {
		return ErrInvalid
	}
	if (c.Action == "maybe_later") != (c.DeferUntil != nil) {
		return ErrInvalid
	}
	if c.HomeID != nil && (*c.HomeID == uuid.Nil || !slices.Contains([]string{"start", "checkpoint", "complete"}, c.Action)) {
		return ErrInvalid
	}
	return nil
}

func fingerprint(id string, c Command) string {
	b, _ := json.Marshal(c) // Command contains only JSON-safe scalars.
	h := sha256.Sum256(append([]byte(id+"\n"), b...))
	return hex.EncodeToString(h[:])
}

func receipt(db *gorm.DB, user uuid.UUID, request uuid.UUID, hash string) (bool, error) {
	var r models.GuideRequest
	err := db.Where("user_id = ? AND request_id = ?", user, request).First(&r).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if r.Fingerprint != hash {
		return false, ErrRequestReuse
	}
	return true, nil
}

// Act uses a shared aggregate version even for platform-local changes. Conflicting
// clients must reload and consciously retry; last-writer-wins would lose restores.
func (s *Service) Act(ctx context.Context, user uuid.UUID, id string, c Command) (View, error) {
	if err := validate(c); err != nil {
		return View{}, err
	}
	d, found := s.definition(id, c.Platform, c.ClientVersion)
	if !found {
		return View{}, ErrNotFound
	}
	if c.Revision != d.Revision {
		return View{}, ErrConflict
	}
	db := s.DB.WithContext(ctx)
	hash := fingerprint(id, c)
	err := db.Transaction(func(tx *gorm.DB) error {
		replayed, err := receipt(tx, user, c.RequestID, hash)
		if err != nil || replayed {
			return err
		}
		seed := models.GuideProgress{UserID: user, GuideID: id, Revision: d.Revision, WebStatus: NotStarted, MobileStatus: NotStarted}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&seed).Error; err != nil {
			return err
		}
		p, err := progress(tx, user, d)
		if err != nil {
			return err
		}
		if p.Version != *c.ExpectedVersion {
			return ErrConflict
		}
		v := state(p, c.Platform)
		contextChanged := c.HomeID != nil && (v.HomeID == nil || *c.HomeID != *v.HomeID)
		if contextChanged && v.Status == InProgress && (c.Action == "checkpoint" || c.Action == "complete") {
			return ErrTransition // Start explicitly in the new home before resuming.
		}
		if c.HomeID != nil {
			v.HomeID = c.HomeID
		}
		if slices.Contains([]string{"start", "checkpoint", "complete"}, c.Action) {
			if v.HomeID != nil {
				role, err := membership(tx, user, *v.HomeID)
				if err != nil {
					return err
				}
				if role == "" {
					return ErrHome
				}
			}
			availability, err := s.view(tx, user, d, c.Platform, p, v.HomeID)
			if err != nil {
				return err
			}
			if !availability.Available {
				return ErrUnavailable
			}
		}
		terminal := v.Status == Completed || v.Status == Skipped
		switch c.Action {
		case "start":
			if terminal {
				return ErrTransition
			}
			if v.Status == NotStarted || contextChanged {
				v.StepID = d.Variants[c.Platform].Steps[0]
			}
			v.Status, v.DeferredUntil = InProgress, nil
		case "checkpoint":
			if v.Status != InProgress || !slices.Contains(d.Variants[c.Platform].Steps, c.StepID) {
				return ErrTransition
			}
			v.StepID = c.StepID
		case "maybe_later":
			if terminal || !c.DeferUntil.After(s.Now()) {
				return ErrTransition
			}
			v.DeferredUntil = c.DeferUntil
		case "restore":
			v = State{Status: NotStarted}
		case "complete", "skip":
			if c.Action == "complete" {
				steps := d.Variants[c.Platform].Steps
				if v.Status != InProgress || v.StepID != steps[len(steps)-1] {
					return ErrTransition
				}
				v.Status = Completed
			} else {
				v.Status = Skipped
			}
			otherPlatform := Web
			if c.Platform == Web {
				otherPlatform = Mobile
			}
			other := state(p, otherPlatform)
			other.Status, other.DeferredUntil = v.Status, nil
			setState(&p, otherPlatform, other)
			v.DeferredUntil = nil
		}
		setState(&p, c.Platform, v)
		p.Version++
		p.UpdatedAt = s.Now()
		updated := tx.Model(&models.GuideProgress{}).
			Where("user_id = ? AND guide_id = ? AND revision = ? AND version = ?", user, id, d.Revision, *c.ExpectedVersion).
			Select("version", "web_status", "web_step_id", "web_home_id", "web_deferred_until", "mobile_status", "mobile_step_id", "mobile_home_id", "mobile_deferred_until", "updated_at").Updates(&p)
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrConflict
		}
		return tx.Create(&models.GuideRequest{UserID: user, RequestID: c.RequestID, Fingerprint: hash}).Error
	})
	if err != nil {
		// Another copy of this same command may have committed while we waited
		// on the aggregate or receipt unique key. This also handles request IDs
		// concurrently reused across different guides, which lock different rows.
		if replayed, replayErr := receipt(db, user, c.RequestID, hash); replayErr != nil {
			if errors.Is(replayErr, ErrRequestReuse) {
				return View{}, replayErr
			}
		} else if replayed {
			err = nil
		}
	}
	if err != nil {
		return View{}, err
	}
	p, err := progress(db, user, d)
	if err != nil {
		return View{}, err
	}
	return s.view(db, user, d, c.Platform, p, nil)
}

// ValidateCatalog catches deployment mistakes instead of panicking on a missing
// first/last step, silently dropping a platform, or introducing cyclic guides.
func ValidateCatalog(catalog []Definition) error {
	byID := map[string]Definition{}
	for _, d := range catalog {
		if d.ID == "" || len(d.ID) > 100 || d.Revision < 1 || len(d.Variants) == 0 {
			return fmt.Errorf("invalid guide definition: %s", d.ID)
		}
		if _, duplicate := byID[d.ID]; duplicate {
			return fmt.Errorf("duplicate guide: %s", d.ID)
		}
		for p, v := range d.Variants {
			if !p.Valid() || v.MinClientVersion < 1 || v.ContentVersion < 1 || len(v.Steps) == 0 {
				return fmt.Errorf("invalid guide variant: %s", d.ID)
			}
			seen := map[string]bool{}
			for _, step := range v.Steps {
				if step == "" || len(step) > 100 || seen[step] {
					return fmt.Errorf("invalid guide step: %s", d.ID)
				}
				seen[step] = true
			}
		}
		byID[d.ID] = d
	}
	visiting, visited := map[string]bool{}, map[string]bool{}
	var visit func(string) error
	visit = func(id string) error {
		d, exists := byID[id]
		if !exists || visiting[id] {
			return fmt.Errorf("missing or cyclic guide prerequisite: %s", id)
		}
		if visited[id] {
			return nil
		}
		visiting[id] = true
		for _, pre := range d.Prerequisites {
			if err := visit(pre); err != nil {
				return err
			}
		}
		visiting[id], visited[id] = false, true
		return nil
	}
	for id := range byID {
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}
