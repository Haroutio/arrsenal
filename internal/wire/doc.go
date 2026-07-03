// Package wire is the post-boot auto-wiring engine (DESIGN.md §7): reads the
// keys the apps generated, sets auth once, and connects everything —
// idempotent by name, never modifying an entry that already exists.
package wire
