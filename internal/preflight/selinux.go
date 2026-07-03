package preflight

import (
	"bytes"
	"os"
)

// SELinuxStatus reports whether the host enforces SELinux, and what to tell
// the user about it. Arrsenal does not manage SELinux policy (Tier 1 distros
// don't ship it enforcing); this exists so Tier 2 users on Fedora/RHEL get a
// plain warning instead of a mystery failure (DESIGN.md §10).
type SELinuxStatus struct {
	Enforcing bool
	Warning   string
}

const selinuxEnforceFile = "/sys/fs/selinux/enforce"

// CheckSELinux reads the kernel's own answer. Absent selinuxfs (Debian,
// Ubuntu, anything without SELinux) means not enforcing.
func CheckSELinux() SELinuxStatus {
	return checkSELinuxAt(selinuxEnforceFile)
}

func checkSELinuxAt(path string) SELinuxStatus {
	b, err := os.ReadFile(path)
	if err != nil || !bytes.HasPrefix(bytes.TrimSpace(b), []byte("1")) {
		return SELinuxStatus{}
	}
	return SELinuxStatus{
		Enforcing: true,
		Warning: "SELinux is enforcing on this host. Container writes to the config and data " +
			"directories may fail with 'permission denied' even though ownership is correct, " +
			"because bind mounts need SELinux labels Arrsenal does not manage. If apps cannot " +
			"write after bring-up, label the trees (e.g. `chcon -Rt container_file_t <dir>`) or " +
			"add `:z` to the volume entries via docker-compose.override.yml. Arrsenal supports " +
			"this setup best-effort (Tier 2).",
	}
}
