package wire

import (
	"context"
	"fmt"
	"strings"
)

// ArrTarget is one arr app as Prowlarr should see it.
type ArrTarget struct {
	Name           string // display + idempotency name, e.g. "Sonarr"
	Implementation string // Prowlarr implementation, e.g. "Sonarr"
	URL            string // http://sonarr:8989 — container-name URL
	APIKey         string
	ProwlarrURL    string // how the arr reaches Prowlarr back
}

// application mirrors Prowlarr's /api/v1/applications resource loosely; only
// the fields Arrsenal touches are typed, everything else rides in Fields.
type application struct {
	Name           string     `json:"name"`
	Implementation string     `json:"implementation"`
	ConfigContract string     `json:"configContract"`
	SyncLevel      string     `json:"syncLevel"`
	Fields         []appField `json:"fields"`
}

type appField struct {
	Name  string `json:"name"`
	Value any    `json:"value"`
}

// EnsureApplication registers an arr in Prowlarr (DESIGN.md §7.4) so every
// indexer added to Prowlarr propagates automatically. The create payload
// starts from Prowlarr's OWN schema template for the implementation —
// category defaults and future fields track the running Prowlarr version
// instead of whatever this binary hardcoded at build time.
func EnsureApplication(ctx context.Context, prowlarr *Client, target ArrTarget) Result {
	conn := fmt.Sprintf("Prowlarr → %s", target.Name)

	return EnsureByName(conn,
		func() ([]application, error) {
			var existing []application
			err := prowlarr.GetJSON(ctx, "/api/v1/applications", &existing)
			return existing, err
		},
		func(a application) string { return a.Name },
		target.Name,
		func() error {
			var schemas []application
			if err := prowlarr.GetJSON(ctx, "/api/v1/applications/schema", &schemas); err != nil {
				return fmt.Errorf("reading application schema: %w", err)
			}
			var tmpl *application
			for i := range schemas {
				if schemas[i].Implementation == target.Implementation {
					tmpl = &schemas[i]
					break
				}
			}
			if tmpl == nil {
				return fmt.Errorf("prowlarr has no %q application template", target.Implementation)
			}

			tmpl.Name = target.Name
			tmpl.SyncLevel = "fullSync"
			for i, f := range tmpl.Fields {
				switch f.Name {
				case "prowlarrUrl":
					tmpl.Fields[i].Value = target.ProwlarrURL
				case "baseUrl":
					tmpl.Fields[i].Value = target.URL
				case "apiKey":
					tmpl.Fields[i].Value = target.APIKey
				}
			}
			return prowlarr.PostJSON(ctx, "/api/v1/applications", tmpl, nil)
		},
	)
}

// NewznabIndexer is one usenet indexer as the user supplies it: a name, its
// URL, and the API key from the indexer's account page. Most commercial
// usenet indexers speak generic Newznab, which is why this one shape covers
// nearly all of them.
type NewznabIndexer struct {
	Name   string
	URL    string
	APIKey string
}

// indexer mirrors Prowlarr's /api/v1/indexer resource loosely.
type indexer struct {
	Name           string     `json:"name"`
	Implementation string     `json:"implementation"`
	ConfigContract string     `json:"configContract"`
	Protocol       string     `json:"protocol,omitempty"`
	Enable         bool       `json:"enable"`
	AppProfileID   int        `json:"appProfileId"`
	Priority       int        `json:"priority,omitempty"`
	Fields         []appField `json:"fields"`
}

// EnsureNewznabIndexer registers a usenet indexer in Prowlarr, from where it
// propagates to every connected arr automatically. The payload starts from
// Prowlarr's own generic-Newznab schema template; Prowlarr VALIDATES the
// indexer on save, so a typo'd key or URL comes back as a failure here
// instead of a silent dead indexer.
func EnsureNewznabIndexer(ctx context.Context, prowlarr *Client, t NewznabIndexer) Result {
	conn := fmt.Sprintf("Prowlarr ← indexer %q", t.Name)
	prowlarr.WithRedaction(t.APIKey)

	return EnsureByName(conn,
		func() ([]indexer, error) {
			var existing []indexer
			err := prowlarr.GetJSON(ctx, "/api/v1/indexer", &existing)
			return existing, err
		},
		func(i indexer) string { return i.Name },
		t.Name,
		func() error {
			var schemas []indexer
			if err := prowlarr.GetJSON(ctx, "/api/v1/indexer/schema", &schemas); err != nil {
				return fmt.Errorf("reading indexer schema: %w", err)
			}
			var tmpl *indexer
			for i := range schemas {
				if schemas[i].Implementation == "Newznab" && strings.EqualFold(schemas[i].Name, "Generic Newznab") {
					tmpl = &schemas[i]
					break
				}
			}
			if tmpl == nil {
				// Fall back to any Newznab-implementation template.
				for i := range schemas {
					if schemas[i].Implementation == "Newznab" {
						tmpl = &schemas[i]
						break
					}
				}
			}
			if tmpl == nil {
				return fmt.Errorf("prowlarr has no Newznab indexer template")
			}

			tmpl.Name = t.Name
			tmpl.Enable = true
			if tmpl.AppProfileID == 0 {
				tmpl.AppProfileID = 1 // Prowlarr's built-in default sync profile
			}
			for i, f := range tmpl.Fields {
				switch f.Name {
				case "baseUrl":
					tmpl.Fields[i].Value = t.URL
				case "apiKey":
					tmpl.Fields[i].Value = t.APIKey
				}
			}
			return prowlarr.PostJSON(ctx, "/api/v1/indexer", tmpl, nil)
		},
	)
}

// CheckIndexers is the honesty line: a stack without indexers cannot search
// anything, and a report that ends "done" while that is true is a lie by
// omission. Zero indexers → a ⚠ pointing at Prowlarr; any other outcome is
// silent (indexers present is the normal, unremarkable state).
func CheckIndexers(ctx context.Context, prowlarr *Client, prowlarrURL string) *Result {
	var existing []indexer
	if err := prowlarr.GetJSON(ctx, "/api/v1/indexer", &existing); err != nil {
		return nil // unreachable Prowlarr already produced failure lines upstream
	}
	if len(existing) > 0 {
		return nil
	}
	return &Result{Connection: "Prowlarr indexers", Outcome: OutcomeManual,
		Detail: "no indexers configured — the stack can't search for anything yet; " +
			"add your indexers in Prowlarr's web UI (Indexers → Add) and they sync to every arr",
		FallbackURL: prowlarrURL}
}
