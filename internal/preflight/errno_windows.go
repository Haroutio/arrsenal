//go:build windows

package preflight

// Cross-device detection is informational only and the product targets
// Linux; on the dev platform we simply skip the refinement.
func isCrossDevice(error) bool {
	return false
}
