# Contributing to Arrsenal

Thanks for considering it. This project runs issue-driven: every change traces to an
issue, and the design has a written contract.

## Before you write code

1. **Read [docs/DESIGN.md](docs/DESIGN.md).** It records every load-bearing decision and
   the reasoning. PRs that cross one of those lines (e.g. "let Arrsenal edit the user's
   override file", "add a vault for API keys", "install NVIDIA drivers") will be asked to
   argue the line in an issue first — that's cheaper for everyone than arguing on a
   finished PR.
2. **Find or file an issue.** Small fixes can go straight to PR; anything with surface
   area should have an issue so scope is agreed before effort is spent. Issues labeled
   `good-first-issue` are genuinely self-contained.

## Development setup

- Go 1.24+ (the binary targets Linux; development works anywhere Go does)
- Docker + compose plugin, for integration tests
- `golangci-lint` for linting

```bash
git clone https://github.com/Haroutio/arrsenal
cd arrsenal
go build ./...
go test ./...
```

## Workflow

- Branch per issue: `<issue-number>-short-slug` (e.g. `14-hardlink-preflight`)
- PRs reference their issue (`Closes #14`) and get CI green before review
- Keep commits scoped; a reviewer should be able to read the diff top to bottom

## Testing expectations

- **Generation changes** must update the golden files (`go test ./... -update` will be
  provided) — a compose-output change with no golden diff means the test didn't cover it.
- **Preflight and wiring logic** get unit tests with fakes. For wiring, idempotency is a
  test case, not a comment: "entry already exists → no create call issued."
- **Integration tests** run the real stack in CI. If your change affects wiring, extend
  the live assertions.

## Style

- Standard Go. `gofmt`, `golangci-lint` clean.
- No YAML/JSON built by string concatenation — marshal structs.
- Secrets never reach stdout, logs, or error messages. If a value could be a secret,
  treat it as one.
- User-facing messages: say what happened, what it means, and the one command to run
  next. No stack traces at users.

## Releases

Maintainer-driven via goreleaser on tags. `install.sh` always points at pinned release
artifacts with checksum verification — never at `main`.
