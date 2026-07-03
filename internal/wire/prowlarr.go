package wire

import (
	"context"
	"fmt"
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
