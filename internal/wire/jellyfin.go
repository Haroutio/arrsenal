package wire

import (
	"context"
	"errors"
	"fmt"
	"net/url"
)

// JellyfinLibrary is one virtual folder to ensure.
type JellyfinLibrary struct {
	Name           string // "Movies", "Shows", "Music"
	CollectionType string // movies / tvshows / music
	Path           string // container path, e.g. /media/movies
}

// JellyfinTarget is everything the Jellyfin lane needs.
type JellyfinTarget struct {
	URL       string // host-side URL, e.g. http://localhost:8096
	AdminUser string
	AdminPass string
	// HWAccel is Jellyfin's HardwareAccelerationType for the detected GPU:
	// "nvenc", "qsv", "vaapi", or "" for CPU (encoder left alone).
	HWAccel string
	// TranscodePath is the scratch mount ("/transcode"); set only together
	// with HWAccel so CPU-only servers keep Jellyfin's default.
	TranscodePath string
	Libraries     []JellyfinLibrary
}

// authHeader identifies Arrsenal to Jellyfin; AuthenticateByName rejects
// requests without a client identity.
const authHeader = `MediaBrowser Client="Arrsenal", Device="Arrsenal", DeviceId="arrsenal-wiring", Version="1.0"`

// EnsureJellyfin drives the whole lane (DESIGN.md §7.4): the /Startup wizard
// with the admin credential, the libraries, and the encoder configuration
// that turns "GPU passed through" into "transcode actually works".
//
// An ADOPTED Jellyfin (wizard already completed) is left entirely alone —
// its users, libraries and encoder settings are the owner's — reported as a
// single Existed line.
func EnsureJellyfin(ctx context.Context, t JellyfinTarget) []Result {
	anon := NewClient(t.URL, "", "").WithHeader("X-Emby-Authorization", authHeader)
	anon.WithRedaction(t.AdminPass)

	// The wizard is destructive (it creates the admin user and can wipe an
	// existing setup). So it runs ONLY on a server we can POSITIVELY confirm
	// is fresh — never on uncertainty. A completed server refuses the
	// anonymous /Startup endpoints (401); a fresh one answers 200. Anything
	// else is ambiguous and must be refused, not guessed.
	switch detectStartup(ctx, anon) {
	case jfAdopted:
		return []Result{{Connection: "Jellyfin setup", Outcome: OutcomeExisted,
			Detail: "wizard already completed — users, libraries and encoding left as configured"}}
	case jfUnknown:
		return []Result{{Connection: "Jellyfin setup", Outcome: OutcomeFailed,
			Detail: "could not confirm whether Jellyfin's setup wizard has run; refusing to touch it rather than risk " +
				"re-running setup on a configured server — complete Jellyfin manually at its web UI. check: docker logs jellyfin"}}
	}

	var results []Result
	r := runStartupWizard(ctx, anon, t)
	results = append(results, r)
	// A race or a misdetection surfaces here as ErrAuth on a wizard step:
	// the server was actually configured. Treat it as adopted, not failed —
	// and having written nothing destructive (POSTs come after the GETs).
	if r.Outcome == OutcomeFailed {
		if r.becameAdopted {
			return []Result{{Connection: "Jellyfin setup", Outcome: OutcomeExisted,
				Detail: "server was already configured — left untouched"}}
		}
		return results
	}

	// From here on we act as the admin we just created.
	token, err := authenticate(ctx, t)
	if err != nil {
		return append(results, Result{Connection: "Jellyfin login", Outcome: OutcomeFailed,
			Detail: fmt.Sprintf("authenticating as the new admin: %v", err)})
	}
	authed := NewClient(t.URL, token, "X-Emby-Token")
	authed.WithRedaction(t.AdminPass)

	for _, lib := range t.Libraries {
		results = append(results, ensureLibrary(ctx, authed, lib))
	}
	if t.HWAccel != "" {
		results = append(results, ensureEncoder(ctx, authed, t.HWAccel, t.TranscodePath))
	}
	return results
}

// jfStartupState is what the anonymous /Startup probe concluded.
type jfStartupState int

const (
	// jfUnknown: could not determine — never run the wizard on this.
	jfUnknown jfStartupState = iota
	// jfFresh: the wizard is available; this server is unconfigured.
	jfFresh
	// jfAdopted: the wizard is closed; this server is already configured.
	jfAdopted
)

// detectStartup reads the server's setup state. Only a clean 200 from the
// anonymous /Startup/User endpoint counts as fresh; a 401/403 is adopted;
// EVERYTHING else — transient 5xx, a network blip, an unexpected body — is
// unknown, and unknown never gets the wizard.
func detectStartup(ctx context.Context, anon *Client) jfStartupState {
	var user struct {
		Name string `json:"Name"`
	}
	err := anon.GetJSON(ctx, "/Startup/User", &user)
	switch {
	case err == nil:
		return jfFresh
	case errors.Is(err, ErrAuth):
		return jfAdopted
	default:
		return jfUnknown
	}
}

// wizardResult carries the becameAdopted signal alongside the report line:
// a wizard step that hits ErrAuth means the server was actually configured
// (a race against detection), which the caller reports as Existed, not a
// failure — and no destructive POST has run at that point.
func runStartupWizard(ctx context.Context, anon *Client, t JellyfinTarget) Result {
	conn := "Jellyfin setup wizard"
	fail := func(what string, err error) Result {
		return Result{Connection: conn, Outcome: OutcomeFailed,
			Detail: fmt.Sprintf("%s: %v", what, err), becameAdopted: errors.Is(err, ErrAuth)}
	}

	// All GETs come before any destructive POST: if the server is actually
	// configured, we learn it here having changed nothing.
	var cfg map[string]any
	if err := anon.GetJSON(ctx, "/Startup/Configuration", &cfg); err != nil {
		return fail("reading startup config", err)
	}
	var seed map[string]any
	if err := anon.GetJSON(ctx, "/Startup/User", &seed); err != nil {
		return fail("initializing first user", err)
	}
	if err := anon.PostJSON(ctx, "/Startup/Configuration", cfg, nil); err != nil {
		return fail("confirming startup config", err)
	}
	if err := anon.PostJSON(ctx, "/Startup/User",
		map[string]string{"Name": t.AdminUser, "Password": t.AdminPass}, nil); err != nil {
		return fail("creating admin user", err)
	}
	if err := anon.PostJSON(ctx, "/Startup/RemoteAccess",
		map[string]bool{"EnableRemoteAccess": true, "EnableAutomaticPortMapping": false}, nil); err != nil {
		return fail("setting remote access", err)
	}
	if err := anon.PostJSON(ctx, "/Startup/Complete", struct{}{}, nil); err != nil {
		return fail("completing wizard", err)
	}
	return Result{Connection: conn, Outcome: OutcomeWired}
}

func authenticate(ctx context.Context, t JellyfinTarget) (string, error) {
	c := NewClient(t.URL, "", "").WithHeader("X-Emby-Authorization", authHeader)
	c.WithRedaction(t.AdminPass)
	var resp struct {
		AccessToken string `json:"AccessToken"`
	}
	err := c.PostJSON(ctx, "/Users/AuthenticateByName",
		map[string]string{"Username": t.AdminUser, "Pw": t.AdminPass}, &resp)
	if err != nil {
		return "", err
	}
	if resp.AccessToken == "" {
		return "", errors.New("no access token in login response")
	}
	return resp.AccessToken, nil
}

func ensureLibrary(ctx context.Context, c *Client, lib JellyfinLibrary) Result {
	conn := fmt.Sprintf("Jellyfin library %q", lib.Name)
	type virtualFolder struct {
		Name string `json:"Name"`
	}
	return EnsureByName(conn,
		func() ([]virtualFolder, error) {
			var existing []virtualFolder
			err := c.GetJSON(ctx, "/Library/VirtualFolders", &existing)
			return existing, err
		},
		func(v virtualFolder) string { return v.Name },
		lib.Name,
		func() error {
			q := url.Values{}
			q.Set("name", lib.Name)
			q.Set("collectionType", lib.CollectionType)
			q.Add("paths", lib.Path)
			q.Set("refreshLibrary", "true")
			return c.PostJSON(ctx, "/Library/VirtualFolders?"+q.Encode(),
				map[string]any{"LibraryOptions": map[string]any{}}, nil)
		},
	)
}

// ensureEncoder is the last mile of DESIGN §8: point Jellyfin's transcoder
// at the hardware the preflight detected. Only runs on servers whose wizard
// Arrsenal itself completed — adopted servers never reach here.
func ensureEncoder(ctx context.Context, c *Client, hwAccel, transcodePath string) Result {
	conn := fmt.Sprintf("Jellyfin hardware transcoding (%s)", hwAccel)

	var enc map[string]any
	if err := c.GetJSON(ctx, "/System/Configuration/encoding", &enc); err != nil {
		return Result{Connection: conn, Outcome: OutcomeFailed, Detail: fmt.Sprintf("reading encoding config: %v", err)}
	}
	if current, _ := enc["HardwareAccelerationType"].(string); current == hwAccel {
		return Result{Connection: conn, Outcome: OutcomeExisted}
	}
	enc["HardwareAccelerationType"] = hwAccel
	enc["EnableHardwareEncoding"] = true
	if transcodePath != "" {
		enc["TranscodingTempPath"] = transcodePath
	}
	if err := c.PostJSON(ctx, "/System/Configuration/encoding", enc, nil); err != nil {
		return Result{Connection: conn, Outcome: OutcomeFailed, Detail: fmt.Sprintf("applying encoding config: %v", err)}
	}
	return Result{Connection: conn, Outcome: OutcomeWired}
}
