package registry

import "fmt"

// Role classifies what an app does in the stack. Roles are first-class so that
// adding a media server in v0.3 (Plex, Emby) is a registry entry plus a pairing
// rule — never a refactor (DESIGN.md §3).
type Role string

// Roles in menu-group order (DESIGN.md §3).
const (
	RoleMediaServer    Role = "media-server"
	RoleRequests       Role = "requests"
	RoleIndexer        Role = "indexer"
	RolePVR            Role = "pvr"
	RoleSubtitles      Role = "subtitles"
	RoleDownloadClient Role = "download-client"
	RoleDashboard      Role = "dashboard"
)

// WiringTier states how far the wiring engine can take an app, so the TUI and
// the wiring report can be honest about it (DESIGN.md §3, §7).
type WiringTier string

const (
	// WiringFullAuto is wired end-to-end with no user action.
	WiringFullAuto WiringTier = "full-auto"
	// WiringSemiAuto is wired best-effort; failure falls back to a report line
	// pointing at the app's own wizard, never blocking the run.
	WiringSemiAuto WiringTier = "semi-auto"
	// WiringManual is installed and configured at the container level only.
	WiringManual WiringTier = "manual"
)

// BootPhase splits bring-up in two (DESIGN.md §7): core apps start first and
// generate their own API keys; tail apps are file-driven and get their configs
// written only after the core keys are known.
type BootPhase string

// Boot phases (DESIGN.md §7).
const (
	BootCore BootPhase = "core"
	BootTail BootPhase = "tail"
)

// Identity is how a container is told which user to run as.
type Identity string

const (
	// IdentityEnvPUIDGID is the LinuxServer.io convention — PUID/PGID/UMASK env vars.
	IdentityEnvPUIDGID Identity = "env-puid-pgid"
	// IdentityUserDirective is the compose `user: uid:gid` (official Jellyfin image).
	IdentityUserDirective Identity = "user-directive"
	// IdentityNone means the image manages its own user; only TZ applies.
	IdentityNone Identity = "none"
)

// SourceKind names the host-side root a mount resolves against. Actual paths
// live in the state file; the registry only speaks in these symbols so that
// generation stays the single place paths are resolved.
type SourceKind string

const (
	// SourceAppdata resolves to <appdata root>/<app ID>.
	SourceAppdata SourceKind = "appdata"
	// SourceData resolves to the shared data root (optionally a subpath of it).
	// Everything under one root is what keeps imports hardlinks (DESIGN.md §5).
	SourceData SourceKind = "data"
	// SourceHost is a literal host path (e.g. /dev/shm for transcode scratch).
	SourceHost SourceKind = "host"
)

// KeyFormat says how an app's self-generated API key is stored on disk.
type KeyFormat string

// Key formats. Apps own their keys; Arrsenal reads them post-boot
// (DESIGN.md §7.2) — the same code path serves fresh installs and
// brownfield-adopted configs.
const (
	// KeyNone means no readable key (the app is wired differently or not at all).
	KeyNone KeyFormat = ""
	// KeyXMLApiKey: <ApiKey> element in a config.xml (the arr family).
	KeyXMLApiKey KeyFormat = "xml-apikey"
	// KeyINIApiKey: api_key entry in an ini file (SABnzbd).
	KeyINIApiKey KeyFormat = "ini-apikey"
)

// KeySource locates an app's self-generated key inside its appdata dir.
type KeySource struct {
	File   string // relative to <appdata>/<app ID>
	Format KeyFormat
}

// Mount is one bind mount, symbolic on the host side.
type Mount struct {
	Kind     SourceKind
	Sub      string // subpath under the kind's root ("media"), or the literal path for SourceHost
	Target   string // absolute path inside the container
	ReadOnly bool
}

// PortMap is one published port. Host is the *default* host port — the state
// file can remap it (DESIGN.md §4); Container never changes.
type PortMap struct {
	Container int
	Host      int
	Protocol  string // "tcp" or "udp"
	Purpose   string // human label for the TUI and conflict messages
}

// App is one registry entry: everything Arrsenal knows about a supported app.
// ID triples as the compose service name, the container name, and the appdata
// directory name.
type App struct {
	ID          string
	Name        string
	Description string
	Role        Role
	Image       string // full image reference, no tag
	Tag         string
	Identity    Identity
	Web         PortMap // primary web UI
	// WebPortEnv names an env var that must always equal the web port as seen
	// INSIDE the container (qBittorrent's WEBUI_PORT: the LSIO image rejects
	// asymmetric mappings via CSRF/host-header validation). When set, a web
	// remap moves both sides of the mapping and this env var follows.
	WebPortEnv string
	ExtraPorts []PortMap // anything beyond the web UI (torrent inbound, discovery)
	Env        map[string]string
	Mounts     []Mount
	Key        KeySource // where the app's self-generated API key lands
	// MediaDir names a PVR's slice of the media tree ("tv", "movies",
	// "music"): its root folder is /data/media/<MediaDir> and its download
	// category is <MediaDir> in both clients. One word keeps the whole
	// hardlink pipeline aligned (DESIGN.md §5.4, §7.4).
	MediaDir   string
	WiringTier WiringTier
	BootPhase  BootPhase
	GPU        bool     // can take the transcode device (DESIGN.md §8)
	Warnings   []string // shown in the TUI at selection time, before commitment
	// APIBase is the arr-family API prefix for the wiring engine. NOT
	// uniform across the family: Sonarr and Radarr speak /api/v3, Lidarr
	// and Prowlarr /api/v1 — assuming v3 everywhere handed Lidarr three
	// 404s in a field report. Empty for apps the arr lanes don't wire.
	APIBase string
}

// ImageRef is the full pullable reference including tag.
func (a App) ImageRef() string {
	return a.Image + ":" + a.Tag
}

// clone deep-copies the fields consumers are likely to mutate (generate merges
// env; preflight rewrites host ports), so lookups never alias the registry.
// Env is always non-nil in clones, letting consumers merge without a nil check.
func (a App) clone() App {
	env := make(map[string]string, len(a.Env))
	for k, v := range a.Env {
		env[k] = v
	}
	a.Env = env
	a.Mounts = append([]Mount(nil), a.Mounts...)
	a.ExtraPorts = append([]PortMap(nil), a.ExtraPorts...)
	a.Warnings = append([]string(nil), a.Warnings...)
	return a
}

// All returns every supported app in stable menu order (grouped by role,
// flagship first within a role). The result is safe to mutate.
func All() []App {
	out := make([]App, len(apps))
	for i, a := range apps {
		out[i] = a.clone()
	}
	return out
}

// ByID looks an app up by its ID. The result is safe to mutate.
func ByID(id string) (App, bool) {
	for _, a := range apps {
		if a.ID == id {
			return a.clone(), true
		}
	}
	return App{}, false
}

// ByRole returns all apps with the given role, in menu order.
func ByRole(r Role) []App {
	var out []App
	for _, a := range apps {
		if a.Role == r {
			out = append(out, a.clone())
		}
	}
	return out
}

// ByPhase returns all apps in the given boot phase, in menu order.
func ByPhase(p BootPhase) []App {
	var out []App
	for _, a := range apps {
		if a.BootPhase == p {
			out = append(out, a.clone())
		}
	}
	return out
}

// Validate checks the registry's internal consistency. It is run by tests;
// a registry that fails validation is a bug, not a runtime condition.
func Validate() error {
	validRole := map[Role]bool{
		RoleMediaServer: true, RoleRequests: true, RoleIndexer: true, RolePVR: true,
		RoleSubtitles: true, RoleDownloadClient: true, RoleDashboard: true,
	}
	validIdentity := map[Identity]bool{IdentityEnvPUIDGID: true, IdentityUserDirective: true, IdentityNone: true}
	validTier := map[WiringTier]bool{WiringFullAuto: true, WiringSemiAuto: true, WiringManual: true}
	validPhase := map[BootPhase]bool{BootCore: true, BootTail: true}
	validKind := map[SourceKind]bool{SourceAppdata: true, SourceData: true, SourceHost: true}

	seenID := map[string]bool{}
	seenHostPort := map[string]string{} // "port/protocol" → app ID
	for _, a := range apps {
		if a.ID == "" || a.Name == "" || a.Description == "" {
			return fmt.Errorf("app %q: ID, Name and Description are required", a.ID)
		}
		if seenID[a.ID] {
			return fmt.Errorf("duplicate app ID %q", a.ID)
		}
		seenID[a.ID] = true
		if a.Image == "" || a.Tag == "" {
			return fmt.Errorf("app %q: image and tag are required", a.ID)
		}
		if !validRole[a.Role] {
			return fmt.Errorf("app %q: unknown role %q", a.ID, a.Role)
		}
		if !validIdentity[a.Identity] {
			return fmt.Errorf("app %q: unknown identity %q", a.ID, a.Identity)
		}
		if !validTier[a.WiringTier] {
			return fmt.Errorf("app %q: unknown wiring tier %q", a.ID, a.WiringTier)
		}
		if !validPhase[a.BootPhase] {
			return fmt.Errorf("app %q: unknown boot phase %q", a.ID, a.BootPhase)
		}
		if a.WebPortEnv != "" && a.Web.Container != a.Web.Host {
			return fmt.Errorf("app %q: WebPortEnv requires a symmetric default web mapping, got %d:%d",
				a.ID, a.Web.Host, a.Web.Container)
		}
		if (a.Role == RolePVR) != (a.MediaDir != "") {
			return fmt.Errorf("app %q: MediaDir is required for PVRs and forbidden elsewhere", a.ID)
		}
		for _, p := range append([]PortMap{a.Web}, a.ExtraPorts...) {
			if p.Container <= 0 || p.Host <= 0 {
				return fmt.Errorf("app %q: port %+v not fully specified", a.ID, p)
			}
			if p.Protocol != "tcp" && p.Protocol != "udp" {
				return fmt.Errorf("app %q: port %d has invalid protocol %q", a.ID, p.Container, p.Protocol)
			}
			// TCP and UDP are separate namespaces; default host ports must be
			// unique per protocol across the whole menu so preflight conflicts
			// are only ever about the user's machine, never about our defaults.
			key := fmt.Sprintf("%d/%s", p.Host, p.Protocol)
			if other, dup := seenHostPort[key]; dup {
				return fmt.Errorf("default host port %s used by both %q and %q", key, other, a.ID)
			}
			seenHostPort[key] = a.ID
		}
		hasConfig := false
		for _, m := range a.Mounts {
			if !validKind[m.Kind] {
				return fmt.Errorf("app %q: unknown mount kind %q", a.ID, m.Kind)
			}
			if m.Kind == SourceAppdata {
				hasConfig = true
			}
			if m.Target == "" {
				return fmt.Errorf("app %q: mount %+v has no target", a.ID, m)
			}
		}
		if !hasConfig {
			return fmt.Errorf("app %q: no appdata (config) mount", a.ID)
		}
	}
	return nil
}
