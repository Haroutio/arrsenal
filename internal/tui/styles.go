package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// One small palette, defined once. Screens compose these; nobody invents
// colors inline. 256-color palette on purpose — it survives any ssh/tmux
// TERM combination that a media box is realistically driven through.
// Bright cyan is the app's glow — terminal-native and fits the sea the
// galleon sails on; magenta is the Jolly Roger energy, reserved for the
// cursor. Greens confirm, amber warns. 256-color throughout.
var (
	colAccent = lipgloss.Color("45")  // cyan: brand, titles, focus
	colCursor = lipgloss.Color("198") // magenta: the cursor, nothing else
	colOK     = lipgloss.Color("84")  // green: selected/on/ok
	colWarn   = lipgloss.Color("214") // amber: warnings
	colDim    = lipgloss.Color("241")
	colFaint  = lipgloss.Color("238")

	styleBrand    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("231")).Background(colAccent).Padding(0, 1)
	styleTitle    = lipgloss.NewStyle().Bold(true).Foreground(colAccent)
	styleGroup    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("245")).MarginTop(1)
	styleCursor   = lipgloss.NewStyle().Bold(true).Foreground(colCursor)
	styleHot      = lipgloss.NewStyle().Bold(true)
	styleSelected = lipgloss.NewStyle().Foreground(colOK)
	styleDim      = lipgloss.NewStyle().Foreground(colDim)
	styleFaint    = lipgloss.NewStyle().Foreground(colFaint)
	styleWarn     = lipgloss.NewStyle().Foreground(colWarn)
	styleKey      = lipgloss.NewStyle().Foreground(colAccent)

	// Splash-only tiers: the galleon's hull rides bright, the sea mid-cyan,
	// the waves deep, and the wordmark sweep near-white.
	styleShipHull   = lipgloss.NewStyle().Foreground(lipgloss.Color("253"))
	styleShipSea    = lipgloss.NewStyle().Foreground(lipgloss.Color("38"))
	styleShipWave   = lipgloss.NewStyle().Foreground(lipgloss.Color("31"))
	styleShipBright = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("231"))
)

// ruleWidth is the screens' shared visual width. Fixed rather than
// terminal-tracked: every screen reads fine at 64 and nothing jumps around
// on resize.
const ruleWidth = 64

// header is the shared screen chrome: brand chip, screen title, thin rule.
// Two physical lines — the height budgets count on that.
func header(title string) string {
	return styleBrand.Render("ARRSENAL") + " " + styleTitle.Render(title) + "\n" +
		styleFaint.Render(strings.Repeat("─", ruleWidth)) + "\n"
}

// groupRule renders a section label embedded in a rule:  ── Label ────────
func groupRule(label string) string {
	fill := ruleWidth - lipgloss.Width(label) - 4
	if fill < 2 {
		fill = 2
	}
	return styleGroup.Render("── " + label + " " + strings.Repeat("─", fill))
}

// helpBar renders the footer key legend: keys accented, descriptions dim,
// preceded by a blank line. Pass key/description pairs.
func helpBar(pairs ...string) string {
	var parts []string
	for i := 0; i+1 < len(pairs); i += 2 {
		parts = append(parts, styleKey.Render(pairs[i])+" "+styleDim.Render(pairs[i+1]))
	}
	return "\n" + strings.Join(parts, styleFaint.Render("  ·  "))
}

// wrapText wraps prose to the terminal width with every line indented by
// indent spaces. Bubble Tea clips overlong lines at the terminal edge —
// the OS-disk storage warning reached a user cut off mid-sentence (field
// report). Zero width means the terminal never reported a size: no wrap.
func wrapText(width, indent int, s string) string {
	pad := strings.Repeat(" ", indent)
	if width <= 0 {
		return pad + s
	}
	w := width - indent
	if w < 20 {
		w = 20
	}
	wrapped := lipgloss.NewStyle().Width(w).Render(s)
	return pad + strings.ReplaceAll(wrapped, "\n", "\n"+pad)
}

// fitLine truncates one ROW to the terminal width with an ellipsis.
// Menu rows truncate rather than wrap: a wrapped cursor row would break
// the height-window line accounting.
func fitLine(width int, s string) string {
	if width <= 0 || lipgloss.Width(s) <= width {
		return s
	}
	return ansi.Truncate(s, width-1, "…")
}
