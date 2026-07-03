package wire

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// symbols per outcome — the report's whole vocabulary (DESIGN.md §7.6).
var outcomeSymbol = map[Outcome]string{
	OutcomeWired:   "✓",
	OutcomeExisted: "●",
	OutcomeSynced:  "↻",
	OutcomeManual:  "⚠",
	OutcomeFailed:  "✗",
}

// RenderReport is the closing table: one line per attempted connection,
// aligned, with the reason and the manual fallback where one exists, and a
// one-line summary. Partial failure is not rollback — the report tells the
// user the one thing left to click (DESIGN.md §7.6). Secrets never appear:
// every Detail was redacted at the client layer before it got here.
func RenderReport(results []Result) string {
	if len(results) == 0 {
		return "nothing to wire\n"
	}

	// Pad by rune count, not bytes: connection labels carry arrows (→) and
	// byte-padding would visually misalign every line containing one.
	width := 0
	for _, r := range results {
		if n := utf8.RuneCountInString(r.Connection); n > width {
			width = n
		}
	}

	var b strings.Builder
	b.WriteString("\nWiring report:\n")
	counts := map[Outcome]int{}
	for _, r := range results {
		counts[r.Outcome]++
		pad := strings.Repeat(" ", width-utf8.RuneCountInString(r.Connection))
		fmt.Fprintf(&b, "  %s %s%s", outcomeSymbol[r.Outcome], r.Connection, pad)
		switch {
		case r.Outcome == OutcomeWired:
			// the symbol says it all
		case r.Detail != "" && r.FallbackURL != "":
			fmt.Fprintf(&b, "  %s → %s", r.Detail, r.FallbackURL)
		case r.Detail != "":
			fmt.Fprintf(&b, "  %s", r.Detail)
		case r.FallbackURL != "":
			fmt.Fprintf(&b, "  → %s", r.FallbackURL)
		}
		b.WriteString("\n")
	}

	var parts []string
	for _, o := range []Outcome{OutcomeWired, OutcomeExisted, OutcomeSynced, OutcomeManual, OutcomeFailed} {
		if counts[o] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", counts[o], o))
		}
	}
	fmt.Fprintf(&b, "\n  %s\n", strings.Join(parts, " · "))

	switch {
	case counts[OutcomeFailed] > 0:
		b.WriteString("  Containers stay up — fix the ✗ items above by hand; everything else is done.\n")
	case counts[OutcomeManual] > 0:
		b.WriteString("  Finish the ⚠ items in their web UIs — everything else is done.\n")
	}
	return b.String()
}

// Failed reports whether any result is a hard failure (drives exit codes).
func Failed(results []Result) bool {
	for _, r := range results {
		if r.Outcome == OutcomeFailed {
			return true
		}
	}
	return false
}
