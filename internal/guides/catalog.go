// Package guides implements the catalog and durable guide lifecycle shared by
// the web and mobile applications. Clients own their localized walkthrough UI.
package guides

import "time"

type Platform string

const (
	Web    Platform = "web"
	Mobile Platform = "mobile"
)

func (p Platform) Valid() bool { return p == Web || p == Mobile }

// Revision changes ONLY for deliberate reintroduction. ContentVersion can change
// for routine content fixes without discarding any user's previous decision.
type Definition struct {
	ID            string               `json:"id"`
	Revision      int                  `json:"revision"`
	TitleKey      string               `json:"title_key"`
	PublishedAt   time.Time            `json:"published_at"`
	Active        bool                 `json:"-"`
	Prerequisites []string             `json:"prerequisites"`
	RequiresHome  bool                 `json:"requires_home"`
	Roles         []string             `json:"roles"`
	Variants      map[Platform]Variant `json:"-"`
}

type Variant struct {
	ContentVersion   int      `json:"content_version"`
	MinClientVersion int      `json:"min_client_version"`
	EntryPoint       string   `json:"entry_point"`
	Steps            []string `json:"steps"`
}

// Catalog is deployed with the API. Adding a later guide does not depend on when
// an account registered. Clients opt into their supported guide protocol version.
func Catalog() []Definition {
	return []Definition{{
		ID: "introduction", Revision: 1, TitleKey: "guides.introduction.title",
		Active: true, PublishedAt: time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC),
		Prerequisites: []string{}, RequiresHome: true, Roles: []string{"owner", "editor"},
		Variants: map[Platform]Variant{
			Web:    {ContentVersion: 1, MinClientVersion: 1, EntryPoint: "session", Steps: []string{"home", "definition", "stock", "quantity"}},
			Mobile: {ContentVersion: 1, MinClientVersion: 1, EntryPoint: "session", Steps: []string{"home", "definition", "inventory", "quantity"}},
		},
	}}
}
