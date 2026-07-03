package wire

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Haroutio/arrsenal/internal/quality"
	"github.com/Haroutio/arrsenal/internal/registry"
)

// Spec is everything one wiring pass needs. URLs come in two spaces and must
// not be confused: Access URLs are how THIS PROCESS reaches an app (the
// host side, localhost:<published port>); wire URLs are how apps reach EACH
// OTHER (container-name:container-port on the bridge, DESIGN.md §6).
type Spec struct {
	Apps        []registry.App  // selected, registry order
	Adopted     map[string]bool // app ID → appdata predated this run
	AppdataRoot string

	AdminUser string // may be empty → auth + Jellyfin wizard become manual steps
	AdminPass string
	QBitPass  string // the pre-seeded WebUI password (state secret)
	HWAccel   string // Jellyfin encoder: nvenc/qsv/vaapi/"" per detected GPU

	// Access returns the host-side URL this process reaches app id on.
	Access func(id string) string
	// QBitContainerPort is the effective in-container web port (it follows
	// host remaps — state.WebPorts).
	QBitContainerPort int
	// QBitHost is qBittorrent's name on the bridge: "qbittorrent" normally,
	// "gluetun" when the VPN owns its network namespace (issue #27).
	QBitHost string

	// TRaSH enables the Recyclarr quality sync (issue #60): Orchestrate
	// writes the config (it holds the keys) and calls RunRecyclarr, which
	// the cmd layer wires to a one-shot container run.
	TRaSH        *quality.Answers
	RecyclarrDir string // where recyclarr.yml lands (0600)
	RunRecyclarr func() (output string, err error)

	KeyTimeout time.Duration // how long to wait for each app's key
}

// Orchestrate runs the whole wiring pass (DESIGN.md §7) and returns the
// report lines. It never aborts mid-pass: each failure is a line, the rest
// continues — partial failure is not rollback (§7.6).
func Orchestrate(ctx context.Context, spec Spec) []Result {
	if spec.KeyTimeout == 0 {
		spec.KeyTimeout = 2 * time.Minute
	}
	if spec.QBitHost == "" {
		spec.QBitHost = "qbittorrent"
	}
	sel := map[string]registry.App{}
	for _, a := range spec.Apps {
		sel[a.ID] = a
	}
	var results []Result

	// 1. Keys: read what every key-bearing selected app generated.
	keys := map[string]string{}
	for _, a := range spec.Apps {
		if a.Key.Format == registry.KeyNone {
			continue
		}
		key, err := ReadKey(ctx, a, spec.AppdataRoot, spec.KeyTimeout, time.Second)
		if err != nil {
			results = append(results, Result{
				Connection: fmt.Sprintf("%s API key", a.Name), Outcome: OutcomeFailed,
				Detail: err.Error()})
			continue
		}
		keys[a.ID] = key
	}

	arrClient := func(id string) *Client {
		return NewClient(spec.Access(id), keys[id], "X-Api-Key")
	}

	// 2. SABnzbd preparation: reachable by container name, folders on the
	// data tree, one category per selected PVR.
	_, sabSelected := sel["sabnzbd"]
	sabReady := false
	if sabSelected && keys["sabnzbd"] != "" {
		sab := NewSABClient(spec.Access("sabnzbd"), keys["sabnzbd"])
		steps := []Result{
			EnsureSABWhitelist(ctx, sab, "sabnzbd"),
			EnsureSABFolders(ctx, sab),
		}
		for _, a := range spec.Apps {
			if a.Role == registry.RolePVR {
				steps = append(steps, EnsureSABCategory(ctx, sab, a.MediaDir))
			}
		}
		results = append(results, steps...)
		sabReady = !Failed(steps)
	}
	_, qbitSelected := sel["qbittorrent"]

	// 3. Auth: the single admin credential on every arr-family app that is
	// fresh (adopted auth is the user's).
	if spec.AdminPass != "" {
		for _, a := range spec.Apps {
			apiBase := ""
			switch {
			case a.Role == registry.RolePVR:
				apiBase = "/api/v3"
			case a.ID == "prowlarr":
				apiBase = "/api/v1"
			}
			if apiBase == "" || keys[a.ID] == "" {
				continue
			}
			results = append(results,
				EnsureAuth(ctx, arrClient(a.ID), a.Name, apiBase, spec.AdminUser, spec.AdminPass, spec.Adopted[a.ID]))
		}
	}

	// 4. Per PVR: Prowlarr application, download clients, root folder.
	_, prowlarrSelected := sel["prowlarr"]
	for _, a := range spec.Apps {
		if a.Role != registry.RolePVR || keys[a.ID] == "" {
			continue
		}
		c := arrClient(a.ID)

		if prowlarrSelected && keys["prowlarr"] != "" {
			results = append(results, EnsureApplication(ctx,
				NewClient(spec.Access("prowlarr"), keys["prowlarr"], "X-Api-Key"),
				ArrTarget{
					Name: a.Name, Implementation: a.Name,
					URL:         fmt.Sprintf("http://%s:%d", a.ID, a.Web.Container),
					APIKey:      keys[a.ID],
					ProwlarrURL: "http://prowlarr:9696",
				}))
		}

		if sabSelected && sabReady {
			results = append(results, EnsureDownloadClient(ctx, c, DownloadClientTarget{
				ArrName: a.Name, ClientName: "SABnzbd", Implementation: "Sabnzbd",
				Host: "sabnzbd", Port: 8080, Category: a.MediaDir, APIKey: keys["sabnzbd"],
			}))
		}
		if qbitSelected && spec.QBitPass != "" {
			results = append(results, EnsureDownloadClient(ctx, c, DownloadClientTarget{
				ArrName: a.Name, ClientName: "qBittorrent", Implementation: "QBittorrent",
				Host: spec.QBitHost, Port: spec.QBitContainerPort, Category: a.MediaDir,
				Username: "admin", Password: spec.QBitPass,
			}))
		}

		results = append(results, EnsureRootFolder(ctx, c, a.Name, "/data/media/"+a.MediaDir))
	}

	// 5. Jellyfin lane.
	if _, ok := sel["jellyfin"]; ok {
		if spec.AdminPass == "" {
			results = append(results, Result{
				Connection: "Jellyfin setup", Outcome: OutcomeManual,
				Detail:      "no admin credential provided — finish Jellyfin's wizard in its web UI",
				FallbackURL: spec.Access("jellyfin")})
		} else {
			results = append(results, EnsureJellyfin(ctx, JellyfinTarget{
				URL: spec.Access("jellyfin"), AdminUser: spec.AdminUser, AdminPass: spec.AdminPass,
				HWAccel: spec.HWAccel, TranscodePath: transcodePathFor(spec.HWAccel),
				Libraries: []JellyfinLibrary{
					{Name: "Movies", CollectionType: "movies", Path: "/media/movies"},
					{Name: "Shows", CollectionType: "tvshows", Path: "/media/tv"},
					{Name: "Music", CollectionType: "music", Path: "/media/music"},
				},
			})...)
		}
	}

	// 5.5 The manual-tier apps (issue #26): installed, GPU-configured where
	// applicable, but their setup lives in their own UIs (Plex claim +
	// libraries; Emby wizard; Overseerr's Plex OAuth). Adopted instances are
	// left as configured.
	manualNotes := []struct{ id, note string }{
		{"plex", "finish setup in Plex's web UI (claim the server if you skipped the token, add libraries pointing at /media)"},
		{"emby", "finish Emby's setup wizard (add libraries pointing at /media; hardware transcoding needs Emby Premiere)"},
		{"overseerr", "finish Overseerr's wizard — sign in with your Plex account and connect your arrs"},
	}
	for _, m := range manualNotes {
		id, note := m.id, m.note
		if _, ok := sel[id]; !ok {
			continue
		}
		app := sel[id]
		if spec.Adopted[id] {
			results = append(results, Result{Connection: app.Name + " setup", Outcome: OutcomeExisted,
				Detail: "left as configured"})
			continue
		}
		results = append(results, Result{Connection: app.Name + " setup", Outcome: OutcomeManual,
			Detail: note, FallbackURL: spec.Access(id)})
	}

	// 6. Tail configs (written before the tail apps first start — the cmd
	// layer sequences that; these calls only generate).
	if _, ok := sel["bazarr"]; ok {
		var sonarr, radarr *ArrConn
		if _, s := sel["sonarr"]; s && keys["sonarr"] != "" {
			sonarr = &ArrConn{Host: "sonarr", Port: 8989, APIKey: keys["sonarr"]}
		}
		if _, r := sel["radarr"]; r && keys["radarr"] != "" {
			radarr = &ArrConn{Host: "radarr", Port: 7878, APIKey: keys["radarr"]}
		}
		results = append(results, WriteTailConfig(
			filepath.Join(spec.AppdataRoot, "bazarr", "config", "config.yaml"),
			BazarrConfig(sonarr, radarr), 0o600, "Bazarr ← sonarr/radarr connections"))
	}
	if _, ok := sel["homepage"]; ok {
		var inputs []HomepageInput
		for _, a := range spec.Apps {
			widgetPort := 0
			widgetHost := ""
			if a.ID == "qbittorrent" {
				widgetPort = spec.QBitContainerPort
				if spec.QBitHost != "qbittorrent" {
					widgetHost = spec.QBitHost
				}
			}
			inputs = append(inputs, HomepageInput{
				App: a, HostURL: spec.Access(a.ID), Key: keys[a.ID],
				Username: "admin", Password: qbitPassFor(a.ID, spec.QBitPass),
				WidgetPort: widgetPort, WidgetHost: widgetHost,
			})
		}
		results = append(results, WriteTailConfig(
			filepath.Join(spec.AppdataRoot, "homepage", "services.yaml"),
			HomepageServices(BuildHomepageServices(inputs)), 0o600, "Homepage ← service widgets"))
	}

	// 6.5 TRaSH quality sync (issue #60): a CONVERGENT step — Recyclarr
	// pushes the guide profiles into the arrs every pass; ↻ is its verdict.
	if spec.TRaSH != nil && spec.RunRecyclarr != nil {
		results = append(results, runTRaSH(spec, keys)...)
	}

	// 7. Jellyseerr, best effort, last.
	if _, ok := sel["jellyseerr"]; ok {
		results = append(results, EnsureJellyseerr(ctx, spec.Access("jellyseerr"), spec.Access("jellyseerr")))
	}

	return results
}

func runTRaSH(spec Spec, keys map[string]string) []Result {
	conn := "TRaSH quality profiles (Recyclarr)"
	var sonarr, radarr *quality.Instance
	if k := keys["sonarr"]; k != "" {
		sonarr = &quality.Instance{BaseURL: "http://sonarr:8989", APIKey: k}
	}
	if k := keys["radarr"]; k != "" {
		radarr = &quality.Instance{BaseURL: "http://radarr:7878", APIKey: k}
	}

	cfg, err := quality.RecyclarrConfig(*spec.TRaSH, sonarr, radarr)
	if err != nil {
		return []Result{{Connection: conn, Outcome: OutcomeFailed,
			Detail: fmt.Sprintf("building recyclarr config: %v", err)}}
	}
	// Arrsenal-owned and key-bearing: regenerated every run, 0600.
	path := filepath.Join(spec.RecyclarrDir, "recyclarr.yml")
	if err := os.MkdirAll(spec.RecyclarrDir, 0o755); err != nil {
		return []Result{{Connection: conn, Outcome: OutcomeFailed, Detail: err.Error()}}
	}
	if err := os.WriteFile(path, cfg, 0o600); err != nil {
		return []Result{{Connection: conn, Outcome: OutcomeFailed,
			Detail: fmt.Sprintf("writing %s: %v", path, err)}}
	}

	out, err := spec.RunRecyclarr()
	// Never let an echoed key reach the report.
	for _, k := range keys {
		out = strings.ReplaceAll(out, k, "[redacted]")
	}
	if err != nil {
		tail := out
		if len(tail) > 600 {
			tail = tail[len(tail)-600:]
		}
		return []Result{{Connection: conn, Outcome: OutcomeFailed,
			Detail: fmt.Sprintf("recyclarr sync failed: %v\n%s", err, tail)}}
	}
	var results []Result
	if sonarr != nil {
		results = append(results, Result{Connection: "Sonarr ← TRaSH quality profiles", Outcome: OutcomeSynced})
	}
	if radarr != nil {
		results = append(results, Result{Connection: "Radarr ← TRaSH quality profiles", Outcome: OutcomeSynced})
	}
	return results
}

func transcodePathFor(hwAccel string) string {
	if hwAccel == "" {
		return ""
	}
	return "/transcode"
}

func qbitPassFor(id, pass string) string {
	if id == "qbittorrent" {
		return pass
	}
	return ""
}
