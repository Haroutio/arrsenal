package preflight

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/Haroutio/arrsenal/internal/state"
)

// DataSubdirs is the fixed TRaSH-style layout under the data root
// (DESIGN.md §5.4). Torrent dirs match the download clients' category
// dirs; usenet keeps SABnzbd's complete/incomplete split. The container-
// internal layout is fixed — that opinion is what keeps the wiring
// engine's root-folder calls deterministic.
var DataSubdirs = []string{
	"media/tv",
	"media/movies",
	"media/music",
	"usenet/complete",
	"usenet/incomplete",
	"torrents/tv",
	"torrents/movies",
	"torrents/music",
}

// CreatedDir records one directory EnsureTree touched, for the run report.
type CreatedDir struct {
	Path    string
	Created bool // false = existed, ownership verified/fixed only
}

// EnsureTree creates the data-root layout and the per-app appdata dirs for
// the selection, owned PUID:PGID with mode derived from the state's umask.
// It is idempotent: existing directories are left in place (content is
// NEVER touched — DESIGN §1's iron rule), only ownership is corrected, and
// only when it is wrong.
//
// Callers run the TUI confirmation BEFORE this; EnsureTree does not prompt.
func EnsureTree(s *state.State) ([]CreatedDir, error) {
	if err := s.Validate(); err != nil {
		return nil, fmt.Errorf("refusing to create directories for invalid state: %w", err)
	}

	var out []CreatedDir
	dirMode := dirModeFromUmask(s.Umask)

	var targets []string
	for _, sub := range DataSubdirs {
		targets = append(targets, filepath.Join(s.DataRoot, sub))
	}
	for _, id := range s.Apps {
		targets = append(targets, filepath.Join(s.AppdataRoot, id))
	}
	// Jellyfin's transcode scratch lives on tmpfs and evaporates on reboot;
	// docker recreates bind sources as root, so make it ours instead.
	if selected(s, "jellyfin") {
		targets = append(targets, "/dev/shm/jellyfin")
	}

	for _, dir := range targets {
		created, err := ensureDir(dir, dirMode, s.PUID, s.PGID)
		if err != nil {
			return out, err
		}
		out = append(out, CreatedDir{Path: dir, Created: created})
	}
	return out, nil
}

func selected(s *state.State, id string) bool {
	for _, a := range s.Apps {
		if a == id {
			return true
		}
	}
	return false
}

// ensureDir creates dir (and parents) if missing and makes sure it is owned
// by uid:gid. Returns whether the leaf was created by this call.
func ensureDir(dir string, mode os.FileMode, uid, gid int) (bool, error) {
	info, statErr := os.Stat(dir)
	switch {
	case statErr == nil:
		if !info.IsDir() {
			return false, fmt.Errorf("%s exists but is not a directory — move it aside and re-run", dir)
		}
		return false, chownIfNeeded(dir, uid, gid)
	case os.IsNotExist(statErr):
		if err := os.MkdirAll(dir, mode); err != nil {
			return false, fmt.Errorf("creating %s: %w", dir, err)
		}
		// MkdirAll may have created several levels; chown from the first
		// missing ancestor would be nicer, but correcting the leaf is what
		// the containers need and parents the user pre-made are theirs.
		if err := chownIfNeeded(dir, uid, gid); err != nil {
			return true, err
		}
		return true, nil
	default:
		return false, fmt.Errorf("checking %s: %w", dir, statErr)
	}
}

func chownIfNeeded(dir string, uid, gid int) error {
	if runtime.GOOS == "windows" {
		return nil // dev convenience only; the product targets Linux
	}
	curUID, curGID, ok := statOwner(dir)
	if ok && curUID == uid && curGID == gid {
		return nil
	}
	if err := os.Chown(dir, uid, gid); err != nil {
		return fmt.Errorf("setting ownership of %s to %d:%d: %w (run with sudo?)", dir, uid, gid, err)
	}
	return nil
}

// dirModeFromUmask derives the directory mode the containers themselves
// would use: 0777 &^ umask.
func dirModeFromUmask(umask string) os.FileMode {
	var m uint32
	for _, c := range umask {
		m = m*8 + uint32(c-'0') // state.Validate guarantees 3-4 octal digits
	}
	return os.FileMode(0o777 &^ m)
}
