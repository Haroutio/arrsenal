// Package generate renders docker-compose.yml and .env deterministically
// from state + registry (DESIGN.md §1): structs marshalled to YAML — never
// string templating — with byte-identical output for identical state.
package generate
