package preflight

import (
	"fmt"
	"os"
	"path/filepath"
)

// HardlinkCheck is the result of the real-link probe (DESIGN.md §5.3).
type HardlinkCheck struct {
	OK bool
	// Detail is user-facing: why it failed and what that means for imports.
	Detail string
}

// CheckHardlink answers the only question that matters for instant imports:
// can a file in the download tree be hard-linked into the media tree — with a
// real syscall on the REAL paths, not device-ID heuristics (which lie on
// mergerfs, where success depends on branch policy).
//
// Call it after EnsureTree: both directories must exist. Failure is a
// warning, never a block — copy-mode is legitimate on NFS-backed setups.
func CheckHardlink(downloadDir, mediaDir string) HardlinkCheck {
	for _, d := range []string{downloadDir, mediaDir} {
		if info, err := os.Stat(d); err != nil || !info.IsDir() {
			return HardlinkCheck{OK: false, Detail: fmt.Sprintf(
				"%s does not exist — the hardlink check runs after directory creation; this is a bug in the caller", d)}
		}
	}

	src, err := os.CreateTemp(downloadDir, ".arrsenal-hardlink-*")
	if err != nil {
		return HardlinkCheck{OK: false, Detail: fmt.Sprintf(
			"cannot write to %s: %v — fix ownership before continuing", downloadDir, err)}
	}
	srcName := src.Name()
	_ = src.Close()
	defer func() { _ = os.Remove(srcName) }()

	dst := filepath.Join(mediaDir, filepath.Base(srcName)+".lnk")
	if err := os.Link(srcName, dst); err != nil {
		return HardlinkCheck{OK: false, Detail: linkFailureDetail(downloadDir, mediaDir, err)}
	}
	_ = os.Remove(dst)
	return HardlinkCheck{OK: true, Detail: "downloads and media share a filesystem — imports will be instant hardlinks"}
}

func linkFailureDetail(downloadDir, mediaDir string, err error) string {
	base := fmt.Sprintf(
		"hardlink from %s to %s failed (%v).\nImports will COPY then delete: slower, double disk usage during import, no instant availability.",
		downloadDir, mediaDir, err)
	if isCrossDevice(err) {
		base += "\nCause: the two paths are on different filesystems (or different mergerfs branches). Keep downloads and media under ONE mount to fix this."
	}
	return base
}
