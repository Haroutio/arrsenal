package wire

import (
	"fmt"

	"github.com/Haroutio/arrsenal/internal/registry"
)

// homepageMeta maps a registry app to its Homepage presentation: which
// dashboard group it belongs in, its icon, and the widget type Homepage
// knows. Apps absent from this table get a tile with no stats widget.
var homepageMeta = map[string]struct {
	group, icon, widget string
}{
	"jellyfin":    {"Media", "jellyfin.png", "jellyfin"},
	"jellyseerr":  {"Media", "jellyseerr.png", "jellyseerr"},
	"prowlarr":    {"Management", "prowlarr.png", "prowlarr"},
	"sonarr":      {"Management", "sonarr.png", "sonarr"},
	"radarr":      {"Management", "radarr.png", "radarr"},
	"lidarr":      {"Management", "lidarr.png", "lidarr"},
	"bazarr":      {"Management", "bazarr.png", "bazarr"},
	"sabnzbd":     {"Downloads", "sabnzbd.png", "sabnzbd"},
	"qbittorrent": {"Downloads", "qbittorrent.png", "qbittorrent"},
}

// HomepageInput is one selected app's data for the dashboard.
type HomepageInput struct {
	App      registry.App
	HostURL  string // user-clickable, e.g. http://192.168.1.10:8989
	Key      string // API key (empty for keyless apps)
	Username string // qBittorrent
	Password string // qBittorrent
	// WidgetPort overrides the container port for the widget URL (0 = the
	// registry default). Needed for apps whose container port follows a
	// host remap (qBittorrent's WEBUI_PORT).
	WidgetPort int
}

// BuildHomepageServices turns selected apps into dashboard tiles. Homepage
// itself is skipped (it is the dashboard, not a tile on it).
func BuildHomepageServices(inputs []HomepageInput) []HomepageService {
	var out []HomepageService
	for _, in := range inputs {
		if in.App.ID == "homepage" {
			continue
		}
		meta, known := homepageMeta[in.App.ID]
		group := meta.group
		if !known {
			group = "Media"
		}
		svc := HomepageService{
			Group: group,
			Name:  in.App.Name,
			Icon:  meta.icon,
			Href:  in.HostURL,
		}
		if known && meta.widget != "" {
			port := in.App.Web.Container
			if in.WidgetPort != 0 {
				port = in.WidgetPort
			}
			w := &HomepageWidget{
				Type: meta.widget,
				// Container-to-container URL; Homepage reaches the app on the
				// bridge by name at its container port.
				URL: fmt.Sprintf("http://%s:%d", in.App.ID, port),
			}
			// qBittorrent authenticates with username+password; everyone
			// else with an API key.
			if in.App.ID == "qbittorrent" {
				w.Username, w.Password = in.Username, in.Password
			} else {
				w.Key = in.Key
			}
			svc.Widget = w
		}
		out = append(out, svc)
	}
	return out
}
