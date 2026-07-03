package wire

import (
	"context"
	"fmt"
	"strings"
)

// DownloadClientTarget is one download client as an arr should see it.
type DownloadClientTarget struct {
	ArrName        string // for the report label
	APIBase        string // the arr's API prefix (registry.App.APIBase): /api/v3 or /api/v1
	ClientName     string // display + idempotency name, e.g. "SABnzbd"
	Implementation string // "Sabnzbd" or "QBittorrent"
	Host           string // container name (DESIGN.md §6)
	Port           int    // container-side port
	Category       string // the arr's MediaDir: tv / movies / music

	APIKey   string // SABnzbd
	Username string // qBittorrent
	Password string // qBittorrent (the pre-seeded WebUI password)
}

// downloadClient mirrors the arr downloadclient resource loosely.
type downloadClient struct {
	Name           string     `json:"name"`
	Implementation string     `json:"implementation"`
	ConfigContract string     `json:"configContract"`
	Protocol       string     `json:"protocol,omitempty"`
	Enable         bool       `json:"enable"`
	Priority       int        `json:"priority,omitempty"`
	Fields         []appField `json:"fields"`
}

// EnsureDownloadClient registers a download client in one arr
// (DESIGN.md §7.4). Payload from the arr's own schema template; Arrsenal
// fills connection details and the category. Field names vary per arr
// (tvCategory / movieCategory / musicCategory) — matched by suffix, with
// qBittorrent's *ImportedCategory sibling deliberately left alone.
func EnsureDownloadClient(ctx context.Context, arr *Client, t DownloadClientTarget) Result {
	conn := fmt.Sprintf("%s → %s", t.ArrName, t.ClientName)
	if t.Password != "" {
		arr.WithRedaction(t.Password)
	}

	return EnsureByName(conn,
		func() ([]downloadClient, error) {
			var existing []downloadClient
			err := arr.GetJSON(ctx, t.APIBase+"/downloadclient", &existing)
			return existing, err
		},
		func(d downloadClient) string { return d.Name },
		t.ClientName,
		func() error {
			var schemas []downloadClient
			if err := arr.GetJSON(ctx, t.APIBase+"/downloadclient/schema", &schemas); err != nil {
				return fmt.Errorf("reading download client schema: %w", err)
			}
			var tmpl *downloadClient
			for i := range schemas {
				if schemas[i].Implementation == t.Implementation {
					tmpl = &schemas[i]
					break
				}
			}
			if tmpl == nil {
				return fmt.Errorf("no %q download client template", t.Implementation)
			}

			tmpl.Name = t.ClientName
			tmpl.Enable = true
			for i, f := range tmpl.Fields {
				switch {
				case f.Name == "host":
					tmpl.Fields[i].Value = t.Host
				case f.Name == "port":
					tmpl.Fields[i].Value = t.Port
				case f.Name == "apiKey" && t.APIKey != "":
					tmpl.Fields[i].Value = t.APIKey
				case f.Name == "username" && t.Username != "":
					tmpl.Fields[i].Value = t.Username
				case f.Name == "password" && t.Password != "":
					tmpl.Fields[i].Value = t.Password
				case strings.HasSuffix(f.Name, "Category") && !strings.HasSuffix(f.Name, "ImportedCategory"):
					tmpl.Fields[i].Value = t.Category
				}
			}
			return arr.PostJSON(ctx, t.APIBase+"/downloadclient", tmpl, nil)
		},
	)
}

// rootFolder mirrors the arr rootfolder resource.
type rootFolder struct {
	Path string `json:"path"`
}

// EnsureRootFolder points an arr at its slice of the fixed media tree
// (DESIGN.md §5.4): /data/media/<MediaDir>, idempotent by path. apiBase is
// the arr's prefix from the registry (/api/v3 or /api/v1 — not uniform).
func EnsureRootFolder(ctx context.Context, arr *Client, apiBase, arrName, path string) Result {
	conn := fmt.Sprintf("%s root folder %s", arrName, path)
	return EnsureByName(conn,
		func() ([]rootFolder, error) {
			var existing []rootFolder
			err := arr.GetJSON(ctx, apiBase+"/rootfolder", &existing)
			return existing, err
		},
		func(r rootFolder) string { return r.Path },
		path,
		func() error {
			return arr.PostJSON(ctx, apiBase+"/rootfolder", rootFolder{Path: path}, nil)
		},
	)
}
