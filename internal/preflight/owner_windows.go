//go:build windows

package preflight

// POSIX ownership does not apply on the dev platform; the product targets
// Linux and CI exercises the real path.
func statOwner(string) (uid, gid int, ok bool) {
	return 0, 0, false
}
