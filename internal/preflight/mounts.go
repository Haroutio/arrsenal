package preflight

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

// MountInfo is one real filesystem the paths screen offers as a candidate.
type MountInfo struct {
	Target     string // mount point
	FSType     string
	FreeBytes  uint64
	TotalBytes uint64
}

// IsOSDisk reports whether this is the root filesystem — media on it gets
// the explicit are-you-sure treatment (DESIGN.md §5.2).
func (m MountInfo) IsOSDisk() bool { return m.Target == "/" }

// pseudoFS are filesystem types that can never hold a media library.
var pseudoFS = map[string]bool{
	"proc": true, "sysfs": true, "devtmpfs": true, "devpts": true,
	"tmpfs": true, "ramfs": true, "cgroup": true, "cgroup2": true,
	"securityfs": true, "debugfs": true, "tracefs": true, "pstore": true,
	"bpf": true, "configfs": true, "fusectl": true, "mqueue": true,
	"hugetlbfs": true, "binfmt_misc": true, "autofs": true,
	"squashfs": true, "overlay": true, "nsfs": true, "efivarfs": true,
	"fuse.snapfuse": true, "fuse.gvfsd-fuse": true, "fuse.portal": true,
}

// ListMounts returns the plausible media-holding filesystems, largest free
// space first, with the root filesystem always included (so the OS-disk
// warning has its numbers even on single-disk boxes).
func ListMounts() ([]MountInfo, error) {
	f, err := os.Open("/proc/mounts")
	if err != nil {
		return nil, fmt.Errorf("reading mount table: %w", err)
	}
	defer func() { _ = f.Close() }()
	mounts := parseMounts(f)
	// Bind-mounted FILES (docker's /etc/resolv.conf and friends) parse as
	// mounts but can never hold a library — the splash once proudly offered
	// resolv.conf as the storage recommendation inside a container.
	kept := mounts[:0]
	for _, m := range mounts {
		if info, err := os.Stat(m.Target); err == nil && info.IsDir() {
			kept = append(kept, m)
		}
	}
	mounts = kept
	for i := range mounts {
		mounts[i].FreeBytes, mounts[i].TotalBytes = fsSpace(mounts[i].Target)
	}
	sort.SliceStable(mounts, func(a, b int) bool {
		// Root last: it is the fallback, not the recommendation.
		if mounts[a].IsOSDisk() != mounts[b].IsOSDisk() {
			return !mounts[a].IsOSDisk()
		}
		return mounts[a].FreeBytes > mounts[b].FreeBytes
	})
	return mounts, nil
}

// parseMounts filters /proc/mounts-format content down to real filesystems.
func parseMounts(r io.Reader) []MountInfo {
	var out []MountInfo
	seen := map[string]bool{}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 3 {
			continue
		}
		target, fstype := unescapeMount(fields[1]), fields[2]
		if pseudoFS[fstype] || seen[target] {
			continue
		}
		if strings.HasPrefix(target, "/proc") || strings.HasPrefix(target, "/sys") ||
			strings.HasPrefix(target, "/dev") || strings.HasPrefix(target, "/run") ||
			strings.HasPrefix(target, "/boot") || strings.HasPrefix(target, "/snap") {
			continue
		}
		seen[target] = true
		out = append(out, MountInfo{Target: target, FSType: fstype})
	}
	return out
}

// unescapeMount decodes the octal escapes /proc/mounts uses (\040 = space).
func unescapeMount(s string) string {
	replacer := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)
	return replacer.Replace(s)
}

// MountFor returns the mount a path lives on: longest matching prefix wins.
func MountFor(path string, mounts []MountInfo) (MountInfo, bool) {
	best := MountInfo{}
	found := false
	for _, m := range mounts {
		if path == m.Target || strings.HasPrefix(path, strings.TrimSuffix(m.Target, "/")+"/") {
			if !found || len(m.Target) > len(best.Target) {
				best, found = m, true
			}
		}
	}
	return best, found
}

// HumanBytes renders sizes the way the paths screen shows them.
func HumanBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}
