package state

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/goccy/go-yaml"

	"github.com/Haroutio/arrsenal/internal/registry"
)

// CurrentVersion is the schema version this build reads and writes.
//
// Bump it for ANY field addition or change, however optional: older binaries
// tolerate unknown fields on load but drop them on save, so an unbumped
// addition is silently destroyed by an old binary's load→save cycle. The
// version gate is what turns that data loss into a clear "upgrade arrsenal"
// error (DESIGN.md §1).
const CurrentVersion = 1

// DefaultPath is where Arrsenal keeps everything it owns (DESIGN.md §1).
const DefaultPath = "/opt/arrsenal/arrsenal.yaml"

// GPUMode is the transcode hardware the user confirmed (DESIGN.md §8).
type GPUMode string

// GPU modes. Detection proposes, the user disposes; this is the disposed value.
const (
	GPUNone   GPUMode = "none"
	GPUNvidia GPUMode = "nvidia"
	GPUIntel  GPUMode = "intel" // QSV via /dev/dri
	GPUAMD    GPUMode = "amd"   // VAAPI via /dev/dri
)

// Secrets holds the values that must persist across runs. Keep this struct
// small on purpose: everything in it is a liability, and the wiring engine's
// admin credential is deliberately NOT here (used, not kept — DESIGN.md §9).
type Secrets struct {
	// QBittorrentPassword is the single pre-seed exception (DESIGN.md §7):
	// qBittorrent's own generated password lands only in container logs, so
	// Arrsenal generates one, persists it, and pre-seeds it.
	QBittorrentPassword string `yaml:"qbittorrent_webui_password,omitempty"`
}

// State is the user's answers — the single source every artifact is
// regenerated from. Unknown fields in the file are tolerated on load so newer
// files degrade gracefully in older binaries; the version gate catches real
// incompatibility.
type State struct {
	Version int `yaml:"version"`

	// Apps holds registry IDs, in no particular order.
	Apps []string `yaml:"apps"`

	PUID  int    `yaml:"puid"`
	PGID  int    `yaml:"pgid"`
	TZ    string `yaml:"tz"`
	Umask string `yaml:"umask"` // string, not int: "002" must keep its leading zero

	DataRoot    string `yaml:"data_root"`
	AppdataRoot string `yaml:"appdata_root"`

	GPU GPUMode `yaml:"gpu"`

	// PortRemaps overrides default host ports: app ID → container port →
	// host port. Keyed by container port so every published port is
	// remappable (DESIGN.md §6), not just the web UI; a port published on
	// both tcp and udp (qBittorrent's 6881) remaps as one unit. Defaults
	// stay in the registry — only overrides live here (DESIGN.md §4).
	PortRemaps map[string]map[int]int `yaml:"port_remaps,omitempty"`

	// JellyfinHostNetwork switches Jellyfin to host networking for DLNA and
	// client auto-discovery (DESIGN.md §6). Bridge is the default.
	JellyfinHostNetwork bool `yaml:"jellyfin_host_network,omitempty"`

	Secrets Secrets `yaml:"secrets,omitempty"`
}

// New returns a state with the documented defaults. Environment-derived
// defaults (current user's UID, host timezone) are the TUI's concern; these
// are the static ones.
func New() *State {
	return &State{
		Version:     CurrentVersion,
		PUID:        1000,
		PGID:        1000,
		TZ:          "Etc/UTC",
		Umask:       "002",
		DataRoot:    "/data",
		AppdataRoot: "/opt/appdata",
		GPU:         GPUNone,
	}
}

// HostPort resolves the effective host port for one of an app's published
// ports: the remap if present, the registry default otherwise. This is the
// only implementation of that rule — generate, preflight and the TUI all
// resolve through here so they cannot drift.
func (s *State) HostPort(app registry.App, p registry.PortMap) int {
	if m, ok := s.PortRemaps[app.ID]; ok {
		if h, ok := m[p.Container]; ok {
			return h
		}
	}
	return p.Host
}

// WebHostPort is HostPort for the app's web UI.
func (s *State) WebHostPort(app registry.App) int {
	return s.HostPort(app, app.Web)
}

// WebPorts resolves both sides of an app's web mapping. For apps whose web
// port must look the same inside and outside the container (WebPortEnv —
// qBittorrent's CSRF validation), a remap moves the container side too;
// everyone else keeps their registry container port. Remaps are always keyed
// by the REGISTRY container port, even when the effective one moves.
func (s *State) WebPorts(app registry.App) (host, container int) {
	host = s.WebHostPort(app)
	container = app.Web.Container
	if app.WebPortEnv != "" {
		container = host
	}
	return host, container
}

// HostNetworked reports whether an app runs on host networking instead of
// the bridge. The one home for the rule — generation and validation must
// never disagree about it. Jellyfin-only until the registry grows a
// host-network capability for Plex/Emby in v0.3 (DESIGN.md §6).
func (s *State) HostNetworked(id string) bool {
	return id == "jellyfin" && s.JellyfinHostNetwork
}

// ErrNotExist reports that no state file exists yet — a fresh install, not a
// failure. Callers test with errors.Is.
var ErrNotExist = errors.New("state file does not exist")

// Load reads and validates a state file. As a hardening side effect it
// tightens the file back to 0600 if hand-editing left it wider (POSIX only).
func Load(path string) (*State, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w: %s", ErrNotExist, path)
	}
	if err != nil {
		return nil, fmt.Errorf("reading state file %s: %w", path, err)
	}
	tightenPermissions(path)

	var s State
	if err := yaml.Unmarshal(raw, &s); err != nil {
		// yaml.FormatError without source: goccy's default annotated errors
		// quote the offending file region, which can include the secrets
		// block — never let file content into an error message (DESIGN §9).
		return nil, fmt.Errorf(
			"state file %s is not valid YAML: %s\nfix it by hand, or delete it to start fresh (your compose stack is not affected)",
			path, yaml.FormatError(err, false, false))
	}
	switch {
	case s.Version == 0:
		return nil, fmt.Errorf(
			"state file %s has no schema version — it does not look like an arrsenal state file; refusing to touch it", path)
	case s.Version > CurrentVersion:
		return nil, fmt.Errorf(
			"state file %s is schema v%d but this arrsenal only understands v%d — upgrade arrsenal and re-run",
			path, s.Version, CurrentVersion)
	}
	if err := s.Validate(); err != nil {
		return nil, fmt.Errorf("state file %s: %w", path, err)
	}
	return &s, nil
}

func tightenPermissions(path string) {
	if runtime.GOOS == "windows" {
		return
	}
	if info, err := os.Stat(path); err == nil && info.Mode().Perm()&0o077 != 0 {
		_ = os.Chmod(path, 0o600)
	}
}

// Save writes the state atomically (temp file + rename, parent directory
// fsynced) with 0600 permissions, creating the parent directory 0700 if
// needed. Marshalling is deterministic: identical state, identical bytes.
func (s *State) Save(path string) error {
	if err := s.Validate(); err != nil {
		return fmt.Errorf("refusing to save invalid state: %w", err)
	}
	out, err := yaml.Marshal(s)
	if err != nil {
		return fmt.Errorf("marshalling state: %w", err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".arrsenal-state-*")
	if err != nil {
		return fmt.Errorf("creating temp state file in %s: %w", dir, err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }() // no-op after successful rename
	// 0600 before content: the file must never be readable while it has secrets.
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("restricting temp state file permissions: %w", err)
	}
	if _, err := tmp.Write(out); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing state: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("syncing state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temp state file: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("replacing %s: %w", path, err)
	}
	syncDir(dir) // without this, a crash can silently revert to the old file
	return nil
}

func syncDir(dir string) {
	if runtime.GOOS == "windows" {
		return
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
}

var umaskRe = regexp.MustCompile(`^0?[0-7]{3}$`)

// Validate checks internal consistency, including that the selected apps'
// effective host ports (defaults + remaps) cannot collide — a state that
// deterministically produces a compose file that cannot start is invalid
// here, not a preflight matter (preflight checks the machine, Validate
// checks the state).
func (s *State) Validate() error {
	if s.Version <= 0 || s.Version > CurrentVersion {
		return fmt.Errorf("schema version %d out of range [1,%d]", s.Version, CurrentVersion)
	}
	seen := map[string]bool{}
	for _, id := range s.Apps {
		if _, ok := registry.ByID(id); !ok {
			return fmt.Errorf("unknown app %q (registry knows: %s)", id, strings.Join(registryIDs(), ", "))
		}
		if seen[id] {
			return fmt.Errorf("app %q selected twice", id)
		}
		seen[id] = true
	}
	if s.PUID < 0 || s.PGID < 0 {
		return fmt.Errorf("puid/pgid must be non-negative, got %d/%d", s.PUID, s.PGID)
	}
	if s.TZ == "" {
		return errors.New("tz must be set")
	}
	if !umaskRe.MatchString(s.Umask) {
		return fmt.Errorf("umask %q is not a 3-digit octal string like \"002\"", s.Umask)
	}
	// TZ and the roots land verbatim in .env and volume specs; constrain the
	// character set here so generation can stay escape-free.
	if strings.ContainsAny(s.TZ, "#\n\r") || strings.TrimSpace(s.TZ) != s.TZ {
		return fmt.Errorf("tz %q contains characters that would corrupt the generated .env", s.TZ)
	}
	for name, p := range map[string]string{"data_root": s.DataRoot, "appdata_root": s.AppdataRoot} {
		if !strings.HasPrefix(p, "/") {
			return fmt.Errorf("%s %q must be an absolute path", name, p)
		}
		if strings.ContainsAny(p, ":#\n\r\t ") {
			return fmt.Errorf("%s %q contains characters that would corrupt volume specs or .env (no colons, hashes, or whitespace)", name, p)
		}
	}
	switch s.GPU {
	case GPUNone, GPUNvidia, GPUIntel, GPUAMD:
	default:
		return fmt.Errorf("unknown gpu mode %q (valid: none, nvidia, intel, amd)", s.GPU)
	}

	// Remaps must reference real apps and ports they actually publish.
	for id, remaps := range s.PortRemaps {
		app, ok := registry.ByID(id)
		if !ok {
			return fmt.Errorf("port remap for unknown app %q", id)
		}
		published := map[int]bool{app.Web.Container: true}
		for _, p := range app.ExtraPorts {
			published[p.Container] = true
		}
		for cport, hport := range remaps {
			if !published[cport] {
				return fmt.Errorf("port remap for %q: it does not publish container port %d", id, cport)
			}
			if hport < 1 || hport > 65535 {
				return fmt.Errorf("port remap for %q: %d is not a valid port", id, hport)
			}
		}
	}

	// Effective host ports across the selection must be collision-free. A
	// host-networked app publishes nothing on the bridge but binds its
	// CONTAINER ports directly on the host — those are claims too, and
	// remaps cannot move them.
	type claim struct{ app, purpose string }
	taken := map[string]claim{} // "host/protocol"
	for _, id := range s.Apps {
		app, _ := registry.ByID(id)
		hostNet := s.HostNetworked(id)
		for _, p := range append([]registry.PortMap{app.Web}, app.ExtraPorts...) {
			hostPort := s.HostPort(app, p)
			purpose := p.Purpose
			if hostNet {
				hostPort = p.Container
				purpose += " (host networking — not remappable)"
			}
			key := fmt.Sprintf("%d/%s", hostPort, p.Protocol)
			if prev, dup := taken[key]; dup {
				return fmt.Errorf("host port %s claimed by both %s (%s) and %s (%s) — remap one of them",
					key, prev.app, prev.purpose, id, purpose)
			}
			taken[key] = claim{app: id, purpose: purpose}
		}
	}
	return nil
}

func registryIDs() []string {
	var ids []string
	for _, a := range registry.All() {
		ids = append(ids, a.ID)
	}
	return ids
}
