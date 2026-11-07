// Package guides defines the published catalog and revision policy. Wording,
// translation and layout edits keep these revisions. Deliberate reintroduction
// increments the logical revision; changed checkpoint structures increment only
// the affected platform revision and never erase a completed/skipped outcome.
package guides

import (
	"github.com/Chak-and-Jules/home-inventory-backend/internal/models"
	"time"
)

const Introduction = "inventory-introduction"

type Variant struct {
	Revision int      `json:"revision"`
	Steps    []string `json:"steps"`
}
type Definition struct {
	ID            string
	Revision      int
	Active        bool
	PublishedAt   time.Time
	Priority      int
	Contexts      []string
	Prerequisites []string
	RequiresWrite bool
	Variants      map[string]Variant
}

func Catalog() []Definition {
	return []Definition{{ID: Introduction, Revision: 1, Active: true,
		PublishedAt: time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC), Priority: 10,
		Contexts: []string{"dashboard", "inventory", "item-definitions"},
		Variants: map[string]Variant{
			"web":    {Revision: 1, Steps: []string{"welcome", "definition", "stock", "dashboard"}},
			"mobile": {Revision: 1, Steps: []string{"home-basics", "choose-item", "add-stock", "dashboard-tips"}},
		},
	}}
}

func Platform(p *models.GuideProgress, platform string) *models.GuidePlatformState {
	if platform == "web" {
		return &p.Web
	}
	return &p.Mobile
}
func Normalize(p *models.GuideProgress, d Definition) {
	if p.Revision != d.Revision {
		p.Revision = d.Revision
		p.Outcome = ""
		p.Web = models.GuidePlatformState{}
		p.Mobile = models.GuidePlatformState{}
	}
	for name, variant := range d.Variants {
		s := Platform(p, name)
		if s.Revision != variant.Revision {
			s.Revision = variant.Revision
			s.Step = variant.Steps[0]
			// Preserve disposition and explicit restore exceptions across content updates.
		}
		if s.Status == "" {
			s.Status = "available"
		}
	}
}
func Status(p *models.GuideProgress, platform string) string {
	s := Platform(p, platform)
	if p.Outcome != "" && !s.Restored {
		return p.Outcome
	}
	return s.Status
}
func Supported(d Definition, platform string, now time.Time) bool {
	_, ok := d.Variants[platform]
	return ok && d.Active && !d.PublishedAt.After(now)
}
func Contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}
