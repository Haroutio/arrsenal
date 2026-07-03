// Package preflight runs every check that happens before anything is written
// or started (DESIGN.md §5): conflict scans, the real hardlink test, path and
// ownership validation, and the mount table the TUI paths screen shows.
package preflight
