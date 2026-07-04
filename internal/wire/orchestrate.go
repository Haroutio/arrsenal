package wire

import (
	"context"
	"errors"
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
	Apps    []registry.App  // selected, registry order
	Adopted map[string]bool // app ID → appdata predated this run
	// Owned marks apps whose appdata Arrsenal itself created on an earlier
	// run (the state's ownership ledger). The settings lanes treat an app
	// as adopted only when it predates the run AND is not ours — otherwise
	// every re-run would strand a lane that failed once (audit finding).
	Owned       map[string]bool
	AppdataRoot string
	// Usenet is the news server to register in SABnzbd (nil = none given).
	// The single setting without which the whole stack downloads nothing.
	Usenet *UsenetProvider
	// Indexers are usenet indexers to register in Prowlarr (generic
	// Newznab), from where they propagate to every arr.
	Indexers []NewznabIndexer
	// PUID/PGID own the tail configs this pass writes: root-owned 0600
	// files are invisible to the container users that must read them
	// (Homepage rendered a red parse error instead of a dashboard — field
	// report).
	PUID, PGID int

	AdminUser string // may be empty → auth + Jellyfin wizard become manual steps
	AdminPass string
	QBitPass  string // the pre-seeded WebUI password (state secret)
	// BazarrAPIKey is the pre-seeded key written into Bazarr's config.yaml
	// (state secret); OrchestrateTail authenticates with it once Bazarr is
	// up (issue #107).
	BazarrAPIKey string
	HWAccel      string // Jellyfin encoder: nvenc/qsv/vaapi/"" per detected GPU

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

	// Progress, when non-nil, receives each step's Result the moment it
	// lands — the wiring narrates itself live (issue #115). The report at
	// the end stays the receipt.
	Progress func(Result)
	// Stage, when non-nil, announces the slow, otherwise-silent stretches
	// (key waits, the Recyclarr sync, Jellyfin's wizard).
	Stage func(string)
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

	// settingsAdopted is the gate the settings lanes use: hands-off only
	// for apps that are genuinely someone else's.
	settingsAdopted := func(id string) bool { return spec.Adopted[id] && !spec.Owned[id] }

	// emit records results AND narrates them live (issue #115).
	emit := func(rs []Result, add ...Result) []Result {
		for _, r := range add {
			if spec.Progress != nil {
				spec.Progress(r)
			}
		}
		return append(rs, add...)
	}
	stage := func(msg string) {
		if spec.Stage != nil {
			spec.Stage(msg)
		}
	}

	// 1. Keys: read what every key-bearing selected app generated.
	stage("collecting API keys (freshly started apps can take a minute to mint them)")
	keys := map[string]string{}
	for _, a := range spec.Apps {
		if a.Key.Format == registry.KeyNone {
			continue
		}
		key, err := ReadKey(ctx, a, spec.AppdataRoot, spec.KeyTimeout, time.Second)
		if err != nil {
			results = emit(results, Result{
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
		// sabReady gates the arrs' download-client wiring and is decided by
		// the plumbing steps ONLY — a failed news-server registration must
		// not silently suppress every Sonarr→SABnzbd connection (audit
		// finding): SAB without a working server is still a valid download
		// client the user finishes later.
		results = emit(results, steps...)
		sabReady = !Failed(steps)
		if spec.Usenet != nil {
			results = emit(results, EnsureSABServer(ctx, sab, *spec.Usenet))
		}
	}
	_, qbitSelected := sel["qbittorrent"]

	// 2.7 qBittorrent categories (issue #107): the arrs tag downloads with a
	// category per PVR; registering each with a save path under
	// /data/torrents keeps torrents on the shared tree so imports hardlink
	// from the very first grab.
	if qbitSelected && spec.QBitPass != "" {
		qc, err := NewQBitSession(ctx, spec.Access("qbittorrent"), "admin", spec.QBitPass)
		if err != nil {
			// An adopted qBittorrent has its own credentials, not our
			// pre-seed — a REJECTED LOGIN there is the adoption contract
			// working, not a failure. A transport error (down, timeout) is
			// a failure on adopted and fresh alike; calling it "own
			// credentials" would hide a dead container (audit finding).
			if settingsAdopted("qbittorrent") && errors.Is(err, ErrQBitCredentials) {
				results = emit(results, Result{
					Connection: "qBittorrent ← category save paths", Outcome: OutcomeExisted,
					Detail: "adopted qBittorrent has its own credentials — categories left as configured"})
			} else {
				results = emit(results, Result{
					Connection: "qBittorrent ← category save paths", Outcome: OutcomeFailed,
					Detail: err.Error()})
			}
		} else {
			for _, a := range spec.Apps {
				if a.Role == registry.RolePVR {
					results = emit(results,
						EnsureQBitCategory(ctx, qc, a.MediaDir, "/data/torrents/"+a.MediaDir))
				}
			}
		}
	}

	// 3. Auth: the single admin credential on every arr-family app that is
	// fresh (adopted auth is the user's).
	if spec.AdminPass != "" {
		for _, a := range spec.Apps {
			// The registry knows each arr's API prefix — v3 and v1 coexist
			// in the family, and guessing v3 404'd Lidarr (field report).
			if a.APIBase == "" || keys[a.ID] == "" {
				continue
			}
			results = emit(results,
				EnsureAuth(ctx, arrClient(a.ID), a.Name, a.APIBase, spec.AdminUser, spec.AdminPass, spec.Adopted[a.ID]))
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
			results = emit(results, EnsureApplication(ctx,
				NewClient(spec.Access("prowlarr"), keys["prowlarr"], "X-Api-Key"),
				ArrTarget{
					Name: a.Name, Implementation: a.Name,
					URL:         fmt.Sprintf("http://%s:%d", a.ID, a.Web.Container),
					APIKey:      keys[a.ID],
					ProwlarrURL: "http://prowlarr:9696",
				}))
		}

		if sabSelected && sabReady {
			results = emit(results, EnsureDownloadClient(ctx, c, DownloadClientTarget{
				ArrName: a.Name, APIBase: a.APIBase, ClientName: "SABnzbd", Implementation: "Sabnzbd",
				Host: "sabnzbd", Port: 8080, Category: a.MediaDir, APIKey: keys["sabnzbd"],
			}))
		}
		if qbitSelected && spec.QBitPass != "" {
			results = emit(results, EnsureDownloadClient(ctx, c, DownloadClientTarget{
				ArrName: a.Name, APIBase: a.APIBase, ClientName: "qBittorrent", Implementation: "QBittorrent",
				Host: spec.QBitHost, Port: spec.QBitContainerPort, Category: a.MediaDir,
				Username: "admin", Password: spec.QBitPass,
			}))
		}

		results = emit(results, EnsureRootFolder(ctx, c, a.APIBase, a.Name, "/data/media/"+a.MediaDir))
	}

	// 4.5 Indexers: registered in Prowlarr, which syncs them everywhere.
	// Afterwards the honesty check: a report that ends "done" over a stack
	// that cannot search anything would be a lie by omission.
	if prowlarrSelected && keys["prowlarr"] != "" {
		prowlarr := NewClient(spec.Access("prowlarr"), keys["prowlarr"], "X-Api-Key")
		for _, ix := range spec.Indexers {
			results = emit(results, EnsureNewznabIndexer(ctx, prowlarr, ix))
		}
		if r := CheckIndexers(ctx, prowlarr, spec.Access("prowlarr")); r != nil {
			results = emit(results, *r)
		}
	}

	// 5. Jellyfin lane. Its minted API key feeds the dashboard widget below.
	jellyfinKey := ""
	if _, ok := sel["jellyfin"]; ok {
		if spec.AdminPass == "" {
			results = emit(results, Result{
				Connection: "Jellyfin setup", Outcome: OutcomeManual,
				Detail:      "no admin credential provided — finish Jellyfin's wizard in its web UI",
				FallbackURL: spec.Access("jellyfin")})
		} else {
			stage("running Jellyfin's setup wizard")
			jfResults, jfKey := EnsureJellyfin(ctx, JellyfinTarget{
				URL: spec.Access("jellyfin"), AdminUser: spec.AdminUser, AdminPass: spec.AdminPass,
				HWAccel: spec.HWAccel, TranscodePath: transcodePathFor(spec.HWAccel),
				Libraries: []JellyfinLibrary{
					{Name: "Movies", CollectionType: "movies", Path: "/media/movies"},
					{Name: "Shows", CollectionType: "tvshows", Path: "/media/tv"},
					{Name: "Music", CollectionType: "music", Path: "/media/music"},
				},
			})
			results = emit(results, jfResults...)
			jellyfinKey = jfKey
		}
	}

	// 5.5 The manual-tier apps (issue #26): installed, GPU-configured where
	// applicable, but their setup lives in their own UIs (Plex claim +
	// libraries; Emby wizard; Overseerr's Plex OAuth). Adopted instances are
	// left as configured.
	manualNotes := []struct{ id, note string }{
		{"plex", "finish setup in Plex's web UI (claim the server if you skipped the token, add libraries pointing at /media)"},
		{"emby", "finish Emby's setup wizard (add libraries pointing at /media; hardware transcoding needs Emby Premiere)"},
	}
	for _, m := range manualNotes {
		id, note := m.id, m.note
		if _, ok := sel[id]; !ok {
			continue
		}
		app := sel[id]
		if spec.Adopted[id] {
			results = emit(results, Result{Connection: app.Name + " setup", Outcome: OutcomeExisted,
				Detail: "left as configured"})
			continue
		}
		results = emit(results, Result{Connection: app.Name + " setup", Outcome: OutcomeManual,
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
		results = emit(results, WriteTailConfig(
			filepath.Join(spec.AppdataRoot, "bazarr", "config", "config.yaml"),
			BazarrConfig(spec.BazarrAPIKey, sonarr, radarr), 0o600, spec.PUID, spec.PGID, "Bazarr ← sonarr/radarr connections"))
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
			key := keys[a.ID]
			if a.ID == "jellyfin" {
				key = jellyfinKey
			}
			inputs = append(inputs, HomepageInput{
				App: a, HostURL: spec.Access(a.ID), Key: key,
				Username: "admin", Password: qbitPassFor(a.ID, spec.QBitPass),
				WidgetPort: widgetPort, WidgetHost: widgetHost,
			})
		}
		results = emit(results, WriteTailConfig(
			filepath.Join(spec.AppdataRoot, "homepage", "services.yaml"),
			HomepageServices(BuildHomepageServices(inputs)), 0o600, spec.PUID, spec.PGID, "Homepage ← service widgets"))
	}

	// 6.5 TRaSH quality sync (issue #60): a CONVERGENT step — Recyclarr
	// pushes the guide profiles into the arrs every pass; ↻ is its verdict.
	if spec.TRaSH != nil && spec.RunRecyclarr != nil {
		stage("syncing TRaSH quality profiles via Recyclarr (takes a minute or so)")
		results = emit(results, runTRaSH(spec, keys)...)
	}

	// 6.6 TRaSH naming + media management (issue #105), riding the same
	// consent as the profiles. Fresh arrs only — adopted naming is the
	// user's, and renaming a curated library out from under someone is the
	// kind of surprise this tool exists to prevent.
	if spec.TRaSH != nil {
		if a, ok := sel["sonarr"]; ok && keys["sonarr"] != "" {
			c := arrClient("sonarr")
			results = emit(results,
				EnsureSonarrNaming(ctx, c, settingsAdopted("sonarr")),
				EnsureMediaManagement(ctx, c, a.APIBase, a.Name, settingsAdopted("sonarr")))
		}
		if a, ok := sel["radarr"]; ok && keys["radarr"] != "" {
			c := arrClient("radarr")
			results = emit(results,
				EnsureRadarrNaming(ctx, c, settingsAdopted("radarr")),
				EnsureMediaManagement(ctx, c, a.APIBase, a.Name, settingsAdopted("radarr")))
		}
	}

	// 7. Seerr, best effort, last — after the Jellyfin lane, whose finished
	// wizard the sign-in authenticates against.
	if _, ok := sel["jellyseerr"]; ok {
		t := SeerrTarget{
			URL: spec.Access("jellyseerr"), HostAccessURL: spec.Access("jellyseerr"),
			AdminUser: spec.AdminUser, AdminPass: spec.AdminPass,
		}
		switch {
		case sel["jellyfin"].ID != "":
			t.ServerType, t.ServerHost, t.ServerPort = "jellyfin", "jellyfin", sel["jellyfin"].Web.Container
		case sel["emby"].ID != "":
			t.ServerType, t.ServerHost, t.ServerPort = "emby", "emby", sel["emby"].Web.Container
		case sel["plex"].ID != "":
			t.ServerType = "plex"
		}
		sonarrProfile, radarrProfile := "", ""
		if spec.TRaSH != nil {
			sonarrProfile, radarrProfile = quality.MainProfileNames(*spec.TRaSH)
		}
		if a, ok := sel["sonarr"]; ok && keys["sonarr"] != "" {
			if id, name, err := fetchQualityProfile(ctx, arrClient("sonarr"), a.APIBase, sonarrProfile); err == nil {
				t.Sonarr = &SeerrArr{Name: "Sonarr", Host: "sonarr", Port: a.Web.Container, APIKey: keys["sonarr"],
					ProfileID: id, ProfileName: name, RootFolder: "/data/media/" + a.MediaDir}
			}
		}
		if a, ok := sel["radarr"]; ok && keys["radarr"] != "" {
			if id, name, err := fetchQualityProfile(ctx, arrClient("radarr"), a.APIBase, radarrProfile); err == nil {
				t.Radarr = &SeerrArr{Name: "Radarr", Host: "radarr", Port: a.Web.Container, APIKey: keys["radarr"],
					ProfileID: id, ProfileName: name, RootFolder: "/data/media/" + a.MediaDir}
			}
		}
		stage("setting up Seerr")
		results = emit(results, EnsureSeerr(ctx, t)...)
	}

	return results
}

// OrchestrateTail is the small second pass for wiring that needs the TAIL
// apps running — Orchestrate runs before they boot (their configs are its
// output). Today that is one lane: Bazarr's language defaults (issue #107).
func OrchestrateTail(ctx context.Context, spec Spec) []Result {
	sel := map[string]bool{}
	for _, a := range spec.Apps {
		sel[a.ID] = true
	}
	var results []Result
	if sel["bazarr"] && spec.BazarrAPIKey != "" {
		if spec.Stage != nil {
			spec.Stage("finishing Bazarr (its first boot can take a moment)")
		}
		// Readiness means the CONTAINER runs; Bazarr's web server takes a
		// while longer on first boot (migrations, config parse) — caught
		// live in CI as connection-refused. Patience, not failure.
		c := NewClient(spec.Access("bazarr"), spec.BazarrAPIKey, "X-API-KEY").
			WithRetry(6, 5*time.Second)
		adopted := spec.Adopted["bazarr"] && !spec.Owned["bazarr"]
		r := EnsureBazarrLanguages(ctx, c, adopted)
		if spec.Progress != nil {
			spec.Progress(r)
		}
		results = append(results, r)
	}
	return results
}

// fetchQualityProfile picks the arr profile Seerr should request with: the
// preferred name when it exists (the TRaSH main profile), the first profile
// otherwise.
func fetchQualityProfile(ctx context.Context, c *Client, apiBase, preferred string) (int, string, error) {
	var profiles []struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	if err := c.GetJSON(ctx, apiBase+"/qualityprofile", &profiles); err != nil {
		return 0, "", err
	}
	if len(profiles) == 0 {
		return 0, "", fmt.Errorf("no quality profiles")
	}
	for _, p := range profiles {
		if p.Name == preferred {
			return p.ID, p.Name, nil
		}
	}
	return profiles[0].ID, profiles[0].Name, nil
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
	if err != nil {
		// On failure dockerx returns empty output and embeds the container's
		// full combined output inside the error itself — so redaction must
		// cover BOTH, or a sync error that quotes the config's api_key lines
		// walks straight into the report (audit finding). The tail is the
		// user's only diagnostic; Recyclarr's fatal line often trails a
		// longer root cause (e.g. a repo-clone error), so keep enough of it.
		detail := fmt.Sprintf("%v\n%s", err, out)
		for _, k := range keys {
			detail = strings.ReplaceAll(detail, k, "[redacted]")
		}
		if len(detail) > 2000 {
			detail = detail[len(detail)-2000:]
		}
		return []Result{{Connection: conn, Outcome: OutcomeFailed,
			Detail: "recyclarr sync failed: " + detail}}
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
