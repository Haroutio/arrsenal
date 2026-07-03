package preflight

import (
	"fmt"
	"os"
	"path/filepath"
)

// PathCheck reports whether a root is usable by the containers.
type PathCheck struct {
	Path     string
	Writable bool // this process could create and remove a file there
	OwnerOK  bool // owned by the expected PUID:PGID (always true on Windows dev)
	Detail   string
}

// CheckOwnership verifies a directory is writable and owned by the identity
// the containers will run as. Informational: the TUI decides what to do
// with a failure (usually offer the chown EnsureTree would apply anyway).
func CheckOwnership(path string, wantUID, wantGID int) PathCheck {
	c := PathCheck{Path: path}

	info, err := os.Stat(path)
	if err != nil {
		c.Detail = fmt.Sprintf("%s does not exist yet — it will be created owned %d:%d", path, wantUID, wantGID)
		return c
	}
	if !info.IsDir() {
		c.Detail = fmt.Sprintf("%s exists but is not a directory", path)
		return c
	}

	probe, err := os.CreateTemp(path, ".arrsenal-write-*")
	if err == nil {
		name := probe.Name()
		_ = probe.Close()
		_ = os.Remove(name)
		c.Writable = true
	}

	uid, gid, ok := statOwner(path)
	switch {
	case !ok:
		c.OwnerOK = true // no POSIX ownership on this platform (Windows dev)
	case uid == wantUID && gid == wantGID:
		c.OwnerOK = true
	default:
		c.Detail = fmt.Sprintf(
			"%s is owned by %d:%d but the containers run as %d:%d — apps will fail to write; fix: chown -R %d:%d %s",
			path, uid, gid, wantUID, wantGID, wantUID, wantGID, filepath.Clean(path))
	}
	if !c.Writable && c.Detail == "" {
		c.Detail = fmt.Sprintf("%s is not writable by this process", path)
	}
	return c
}
