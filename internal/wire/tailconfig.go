package wire

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// The tail apps (Bazarr, Homepage) are configured by FILE, not API: their
// configs are generated once every core app's key is known, then the
// containers start (DESIGN.md §7.5). Validated live — Bazarr boots from a
// minimal pre-written config.yaml, merges its own defaults, and connects.

// ArrConn is one arr Bazarr should talk to (container-name URL, §6).
type ArrConn struct {
	Host   string // container name, e.g. "sonarr"
	Port   int    // container port
	APIKey string
}

// BazarrConfig renders a minimal config.yaml. Only the sections for selected
// arrs are written; Bazarr fills every other default on first boot. Sonarr
// handles TV subtitles, Radarr movies — Lidarr is not a Bazarr concern.
func BazarrConfig(sonarr, radarr *ArrConn) []byte {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("general:\n")
	fmt.Fprintf(&b, "  use_sonarr: %t\n", sonarr != nil)
	fmt.Fprintf(&b, "  use_radarr: %t\n", radarr != nil)
	writeBazarrArr(&b, "sonarr", sonarr)
	writeBazarrArr(&b, "radarr", radarr)
	return []byte(b.String())
}

func writeBazarrArr(b *strings.Builder, name string, conn *ArrConn) {
	if conn == nil {
		return
	}
	fmt.Fprintf(b, "%s:\n", name)
	fmt.Fprintf(b, "  apikey: %s\n", conn.APIKey)
	fmt.Fprintf(b, "  base_url: /\n")
	fmt.Fprintf(b, "  ip: %s\n", conn.Host)
	fmt.Fprintf(b, "  port: %d\n", conn.Port)
	fmt.Fprintf(b, "  ssl: false\n")
}

// HomepageService is one dashboard tile. Href is the user-clickable URL
// (host IP + published port); the widget URL is container-to-container.
type HomepageService struct {
	Group  string
	Name   string
	Icon   string
	Href   string
	Widget *HomepageWidget
}

// HomepageWidget is the stats integration for a service.
type HomepageWidget struct {
	Type     string
	URL      string // http://sonarr:8989
	Key      string // API-key apps
	Username string // qBittorrent
	Password string // qBittorrent
}

// HomepageServices renders services.yaml — grouped tiles with widgets.
// Widgets carry secrets, so the file lands 0600 (see WriteTailConfig callers).
func HomepageServices(services []HomepageService) []byte {
	// Stable group order for deterministic output.
	order := []string{"Media", "Management", "Downloads", "Dashboard"}
	byGroup := map[string][]HomepageService{}
	for _, s := range services {
		byGroup[s.Group] = append(byGroup[s.Group], s)
	}

	var b strings.Builder
	b.WriteString("---\n")
	for _, group := range order {
		items := byGroup[group]
		if len(items) == 0 {
			continue
		}
		fmt.Fprintf(&b, "- %s:\n", group)
		for _, s := range items {
			fmt.Fprintf(&b, "    - %s:\n", s.Name)
			fmt.Fprintf(&b, "        href: %s\n", s.Href)
			fmt.Fprintf(&b, "        icon: %s\n", s.Icon)
			if s.Widget != nil {
				w := s.Widget
				b.WriteString("        widget:\n")
				fmt.Fprintf(&b, "          type: %s\n", w.Type)
				fmt.Fprintf(&b, "          url: %s\n", w.URL)
				if w.Key != "" {
					fmt.Fprintf(&b, "          key: %s\n", w.Key)
				}
				if w.Username != "" {
					fmt.Fprintf(&b, "          username: %s\n", w.Username)
				}
				if w.Password != "" {
					fmt.Fprintf(&b, "          password: %s\n", w.Password)
				}
			}
		}
	}
	return []byte(b.String())
}

// WriteTailConfig writes content to path only when the file is ABSENT —
// never clobbering an existing (adopted, or previously-generated) config
// (DESIGN.md §1 iron rule). mode restricts secret-bearing files, and the
// file is handed to uid:gid — a 0600 file left owned by root (arrsenal runs
// under sudo) is unreadable to the container that needs it (field report:
// Homepage showed a parse error instead of a dashboard). On the existed
// path, ownership is REPAIRED but content never touched — that heals
// installs made before this fix.
func WriteTailConfig(path string, content []byte, mode os.FileMode, uid, gid int, label string) Result {
	conn := label
	if _, err := os.Stat(path); err == nil {
		chownFile(path, uid, gid)
		return Result{Connection: conn, Outcome: OutcomeExisted,
			Detail: "existing config left untouched"}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Result{Connection: conn, Outcome: OutcomeFailed, Detail: fmt.Sprintf("creating dir: %v", err)}
	}
	if err := os.WriteFile(path, content, mode); err != nil {
		return Result{Connection: conn, Outcome: OutcomeFailed, Detail: fmt.Sprintf("writing %s: %v", path, err)}
	}
	chownFile(path, uid, gid)
	return Result{Connection: conn, Outcome: OutcomeWired}
}

// chownFile is best-effort (non-root runs cannot chown and do not need to:
// their files are already owned by the invoking user, which is the PUID in
// every non-root setup that works at all).
func chownFile(path string, uid, gid int) {
	if runtime.GOOS == "windows" || uid < 0 {
		return
	}
	_ = os.Chown(path, uid, gid)
}
