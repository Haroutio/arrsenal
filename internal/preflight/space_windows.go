//go:build windows

package preflight

// Free-space probing is Linux-only; the dev platform renders zeros.
func fsSpace(string) (free, total uint64) {
	return 0, 0
}
