package preflight

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// DetectIdentity returns the uid/gid the containers should run as. Arrsenal
// itself usually runs under sudo, so the *invoking* user (SUDO_UID/SUDO_GID)
// is the right default — media files should belong to the human, not root.
// Falls back to the process identity, then to 1000:1000 (Windows dev).
func DetectIdentity() (uid, gid int) {
	if u, err := strconv.Atoi(os.Getenv("SUDO_UID")); err == nil && u > 0 {
		g := u
		if v, err := strconv.Atoi(os.Getenv("SUDO_GID")); err == nil {
			g = v
		}
		return u, g
	}
	if u := os.Getuid(); u >= 0 {
		return u, os.Getgid()
	}
	return 1000, 1000
}

// DetectTZ returns the host's IANA timezone: /etc/timezone (Debian family)
// first, the /etc/localtime symlink target second, UTC when neither answers.
func DetectTZ() string {
	return detectTZAt("/etc/timezone", "/etc/localtime")
}

func detectTZAt(tzFile, localtime string) string {
	if b, err := os.ReadFile(tzFile); err == nil {
		if tz := strings.TrimSpace(string(b)); tz != "" {
			return tz
		}
	}
	if target, err := os.Readlink(localtime); err == nil {
		// .../zoneinfo/America/Los_Angeles → America/Los_Angeles
		if i := strings.Index(target, "zoneinfo/"); i >= 0 {
			return target[i+len("zoneinfo/"):]
		}
		// Some layouts nest: take the last two path elements as a guess.
		parts := strings.Split(filepath.ToSlash(target), "/")
		if len(parts) >= 2 {
			return parts[len(parts)-2] + "/" + parts[len(parts)-1]
		}
	}
	return "Etc/UTC"
}
