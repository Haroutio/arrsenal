// Package registry is the catalog of supported apps — the single source of
// truth everything else hangs off (DESIGN.md §3). Each entry declares its
// role, image, port, volumes, wiring tier, boot phase, and TUI warnings.
// No app name or port is hardcoded anywhere outside this package.
package registry
