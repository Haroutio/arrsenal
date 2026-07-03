package wire

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// SeerrArr is one arr as Seerr should see it: container-name URL, its key,
// and the defaults every request needs (quality profile + root folder).
type SeerrArr struct {
	Name        string // "Sonarr" / "Radarr" — display + report label
	Host        string // container name
	Port        int
	APIKey      string
	ProfileID   int
	ProfileName string
	RootFolder  string
}

// SeerrTarget is everything the Seerr lane needs. ServerType selects the
// automation path: "jellyfin" and "emby" sign in with the admin credential
// (Seerr's own init API); anything else — Plex pairing (browser OAuth) or
// no credential — is the manual wizard, as before.
type SeerrTarget struct {
	URL           string // reachable from the wiring process
	HostAccessURL string // shown to the user in the report
	ServerType    string // "jellyfin" | "emby" | ""
	ServerHost    string // container name Seerr reaches the media server on
	ServerPort    int
	AdminUser     string
	AdminPass     string
	Sonarr        *SeerrArr // nil = not selected / key unavailable
	Radarr        *SeerrArr
}

// mediaServerTypes are Seerr's MediaServerType enum values (PLEX=1 is the
// manual path and deliberately absent).
var mediaServerTypes = map[string]int{"jellyfin": 2, "emby": 3}

// EnsureSeerr drives Seerr's first-run setup end to end when the media
// server is Jellyfin or Emby: sign in with the admin credential (this
// creates Seerr's admin and connects the media server — Seerr mints its own
// Jellyfin token), sync + enable the libraries, register Sonarr/Radarr with
// sane defaults, and flip the initialized flag.
//
// An INITIALIZED Seerr is adopted: one ● line, nothing touched. Plex
// pairing stays the manual wizard — its sign-in is browser OAuth, which no
// headless flow can perform. Any step failing degrades to the manual note
// (with what remains), never a blocked run: the wizard resumes exactly
// where the API path stopped.
func EnsureSeerr(ctx context.Context, t SeerrTarget) []Result {
	conn := "Seerr requests"
	probe := NewClient(t.URL, "", "").WithRetry(2, 500*time.Millisecond)

	// The public settings endpoint needs no auth and reports init state.
	var pub struct {
		Initialized bool `json:"initialized"`
	}
	if err := probe.GetJSON(ctx, "/api/v1/settings/public", &pub); err != nil {
		return []Result{{Connection: conn, Outcome: OutcomeManual,
			Detail:      fmt.Sprintf("could not reach Seerr to check its setup (%v) — finish it at its web UI", err),
			FallbackURL: t.HostAccessURL}}
	}
	if pub.Initialized {
		return []Result{{Connection: conn, Outcome: OutcomeExisted,
			Detail: "already set up — left as configured"}}
	}

	serverType, automatable := mediaServerTypes[t.ServerType]
	if !automatable || t.AdminPass == "" {
		detail := "installed and running — finish its 2-minute setup wizard (sign in with your media server account, " +
			"then add Sonarr/Radarr)"
		if t.ServerType == "plex" {
			detail += "; Plex sign-in is browser OAuth and cannot be automated"
		}
		return []Result{{Connection: conn, Outcome: OutcomeManual,
			Detail: detail, FallbackURL: t.HostAccessURL}}
	}

	// The init flow is session-authenticated: the sign-in sets the cookie
	// every later call rides.
	c := NewClient(t.URL, "", "").WithCookies().WithRetry(2, time.Second)
	c.WithRedaction(t.AdminPass)

	manual := func(results []Result, step string, err error) []Result {
		return append(results, Result{Connection: conn, Outcome: OutcomeManual,
			Detail:      fmt.Sprintf("automated setup stopped at %s (%v) — the wizard at its web UI resumes from there", step, err),
			FallbackURL: t.HostAccessURL})
	}

	var results []Result

	// 1. Sign in. On a fresh Seerr this creates the admin, stores the media
	// server connection, and starts its background jobs.
	signIn := map[string]any{
		"username":   t.AdminUser,
		"password":   t.AdminPass,
		"hostname":   t.ServerHost,
		"port":       t.ServerPort,
		"useSsl":     false,
		"urlBase":    "",
		"email":      t.AdminUser + "@arrsenal.local",
		"serverType": serverType,
	}
	if err := c.PostJSON(ctx, "/api/v1/auth/jellyfin", signIn, nil); err != nil {
		return manual(results, "the "+t.ServerType+" sign-in", err)
	}
	results = append(results, Result{
		Connection: fmt.Sprintf("Seerr ← %s sign-in", t.ServerType), Outcome: OutcomeWired})

	// 2. Libraries: sync the list from the media server, enable everything.
	var libs []struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
	}
	if err := c.GetJSON(ctx, "/api/v1/settings/jellyfin/library?sync=true", &libs); err != nil {
		return manual(results, "the library sync", err)
	}
	if len(libs) > 0 {
		ids := make([]string, len(libs))
		for i, l := range libs {
			ids[i] = l.ID
		}
		if err := c.GetJSON(ctx, "/api/v1/settings/jellyfin/library?enable="+strings.Join(ids, ","), &libs); err != nil {
			return manual(results, "enabling the libraries", err)
		}
	}
	results = append(results, Result{
		Connection: "Seerr ← libraries", Outcome: OutcomeWired,
		Detail: fmt.Sprintf("%d enabled", len(libs))})

	// 3. The arrs, with the defaults every request needs.
	for _, arr := range []*SeerrArr{t.Sonarr, t.Radarr} {
		if arr == nil {
			continue
		}
		payload := map[string]any{
			"name":              arr.Name,
			"hostname":          arr.Host,
			"port":              arr.Port,
			"apiKey":            arr.APIKey,
			"useSsl":            false,
			"baseUrl":           "",
			"activeProfileId":   arr.ProfileID,
			"activeProfileName": arr.ProfileName,
			"activeDirectory":   arr.RootFolder,
			"tags":              []int{},
			"is4k":              false,
			"isDefault":         true,
			"syncEnabled":       true,
			"preventSearch":     false,
		}
		c.WithRedaction(arr.APIKey)
		path := "/api/v1/settings/" + strings.ToLower(arr.Name)
		if err := c.PostJSON(ctx, path, payload, nil); err != nil {
			results = append(results, Result{Connection: "Seerr → " + arr.Name, Outcome: OutcomeFailed,
				Detail: fmt.Sprintf("registering the server: %v", err)})
			continue
		}
		results = append(results, Result{Connection: "Seerr → " + arr.Name, Outcome: OutcomeWired})
	}

	// 4. Done: flip the initialized flag so the wizard never appears.
	if err := c.PostJSON(ctx, "/api/v1/settings/initialize", map[string]any{}, nil); err != nil {
		return manual(results, "finalizing", err)
	}
	results = append(results, Result{Connection: "Seerr initialized", Outcome: OutcomeWired})
	return results
}
