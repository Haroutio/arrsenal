// Package dockerx wraps interactions with the docker and compose CLIs:
// bring-up, readiness polling, and the container/port probes preflight needs.
// Reconciliation itself is delegated to `docker compose up -d --remove-orphans`
// (DESIGN.md §1) — this package drives it, it does not reimplement it.
package dockerx
