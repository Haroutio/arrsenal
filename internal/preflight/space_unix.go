//go:build !windows

package preflight

import "golang.org/x/sys/unix"

func fsSpace(path string) (free, total uint64) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return 0, 0
	}
	bsize := uint64(st.Bsize)
	return st.Bavail * bsize, st.Blocks * bsize
}
