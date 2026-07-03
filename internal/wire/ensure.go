package wire

import "fmt"

// Outcome is one connection's verdict, feeding the wiring report
// (DESIGN.md §7.6).
type Outcome string

// Outcomes. Existed is a first-class success: it is what makes re-runs and
// brownfield adoption safe — an entry that is already there is left exactly
// as the user (or a previous run) configured it. Manual is the honest
// outcome of the semi-auto tier: installed and reachable, but a step only a
// human can do remains (an OAuth login, say) — never a failure, always with
// a fallback URL (DESIGN.md §7.6).
const (
	OutcomeWired   Outcome = "wired"
	OutcomeExisted Outcome = "existed"
	// OutcomeSynced marks a CONVERGENT step (Recyclarr): it runs every pass
	// by design, so it is neither "newly wired" nor "left untouched".
	OutcomeSynced Outcome = "synced"
	OutcomeManual Outcome = "manual"
	OutcomeFailed Outcome = "failed"
)

// Result is one line of the final report.
type Result struct {
	Connection  string // human label: "Prowlarr → Sonarr"
	Outcome     Outcome
	Detail      string // failure reason, never secrets
	FallbackURL string // where to click when Outcome is Failed

	// becameAdopted marks a Failed result that actually means "the server
	// turned out to be already configured" — the caller may reclassify it
	// as Existed. Internal to the wiring engine; not shown in the report.
	becameAdopted bool
}

// EnsureByName is THE idempotency primitive (DESIGN.md §7.4): list what
// exists, create only when the name is absent, never modify an existing
// entry. Every wiring step goes through here — the contract is structural,
// not a convention each step re-implements.
func EnsureByName[T any](
	connection string,
	list func() ([]T, error),
	nameOf func(T) string,
	wantName string,
	create func() error,
) Result {
	existing, err := list()
	if err != nil {
		return Result{Connection: connection, Outcome: OutcomeFailed,
			Detail: fmt.Sprintf("listing existing entries: %v", err)}
	}
	for _, e := range existing {
		if nameOf(e) == wantName {
			return Result{Connection: connection, Outcome: OutcomeExisted}
		}
	}
	if err := create(); err != nil {
		return Result{Connection: connection, Outcome: OutcomeFailed,
			Detail: fmt.Sprintf("creating %q: %v", wantName, err)}
	}
	return Result{Connection: connection, Outcome: OutcomeWired}
}
