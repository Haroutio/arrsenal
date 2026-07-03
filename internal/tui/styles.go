package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// One small palette, defined once. Screens compose these; nobody invents
// colors inline. 256-color palette on purpose — it survives any ssh/tmux
// TERM combination that a media box is realistically driven through.
var (
	colAccent = lipgloss.Color("43")  // teal: brand, titles, focus
	colCursor = lipgloss.Color("212") // pink: the cursor, nothing else
	colOK     = lipgloss.Color("42")  // green: selected/on
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
