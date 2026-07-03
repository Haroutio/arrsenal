package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Haroutio/arrsenal/internal/registry"
)

// SelectModel is the app-selection screen: the registry menu grouped by
// role, pre-populated from the state file so re-runs open where the user
// left off (DESIGN.md §1).
type SelectModel struct {
	rows   []selectRow
	cursor int
	top    int // first visible row when the menu is taller than the terminal
	height int // terminal height from WindowSizeMsg; 0 = unknown, render all
	done   bool
	quit   bool
}

type selectRow struct {
	app          registry.App
	selected     bool
	wasInstalled bool // selected in the loaded state → deselection is a removal
}

// roleTitles in menu order, matching the registry's ordering contract.
var roleOrder = []struct {
	role  registry.Role
	title string
}{
	{registry.RoleMediaServer, "Media server"},
	{registry.RoleRequests, "Requests"},
	{registry.RoleIndexer, "Indexers"},
	{registry.RolePVR, "Collection managers"},
	{registry.RoleSubtitles, "Subtitles"},
	{registry.RoleDownloadClient, "Download clients"},
	{registry.RoleDashboard, "Dashboard"},
}

// NewSelect builds the screen. installed is the previously-saved selection
// (empty on first run); it both pre-selects and marks rows whose
// deselection means "remove this container".
func NewSelect(installed []string) SelectModel {
	was := map[string]bool{}
	for _, id := range installed {
		was[id] = true
	}
	var rows []selectRow
	for _, group := range roleOrder {
		for _, app := range registry.ByRole(group.role) {
			rows = append(rows, selectRow{
				app:          app,
				selected:     was[app.ID],
				wasInstalled: was[app.ID],
			})
		}
	}
	return SelectModel{rows: rows}
}

// Selected returns the chosen app IDs in registry menu order.
func (m SelectModel) Selected() []string {
	var out []string
	for _, r := range m.rows {
		if r.selected {
			out = append(out, r.app.ID)
		}
	}
	return out
}

// Removals returns apps that were installed and are now deselected — the
// TUI shows the §1 promise for each (container removed, config preserved).
func (m SelectModel) Removals() []string {
	var out []string
	for _, r := range m.rows {
		if r.wasInstalled && !r.selected {
			out = append(out, r.app.ID)
		}
	}
	return out
}

// Done reports the user confirmed the selection.
func (m SelectModel) Done() bool { return m.done }

// Quit reports the user aborted.
func (m SelectModel) Quit() bool { return m.quit }

// Init implements tea.Model.
func (m SelectModel) Init() tea.Cmd { return nil }

// Update implements tea.Model.
func (m SelectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		m.height = size.Height
		m.scrollToCursor()
		return m, nil
	}
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.rows)-1 {
			m.cursor++
		}
	case " ":
		m.rows[m.cursor].selected = !m.rows[m.cursor].selected
	case "a":
		for i := range m.rows {
			m.rows[i].selected = true
		}
	case "enter":
		if len(m.Selected()) > 0 {
			m.done = true
			return m, nil
		}
	case "q", "ctrl+c":
		m.quit = true
		return m, tea.Quit
	}
	m.scrollToCursor()
	return m, nil
}

// rowLines renders one row as physical lines: its group header when it opens
// a group (the header travels with the row so scrolling never orphans it),
// the row itself, and any warning/removal notes beneath.
func (m SelectModel) rowLines(i int) []string {
	r := m.rows[i]
	var lines []string
	if i == 0 || m.rows[i-1].app.Role != r.app.Role {
		lines = append(lines, strings.Split(groupRule(roleTitle(r.app.Role)), "\n")...)
	}

	cursor := "  "
	name := r.app.Name
	if i == m.cursor {
		cursor = styleCursor.Render("▸ ")
		name = styleHot.Render(name)
	}
	box := styleFaint.Render("○")
	if r.selected {
		box = styleSelected.Render("●")
	}
	// The name field pads to 12 BEFORE styling so ANSI codes don't skew
	// the column alignment.
	lines = append(lines, fmt.Sprintf("%s%s %s %s", cursor, box,
		strings.Replace(fmt.Sprintf("%-12s", r.app.Name), r.app.Name, name, 1),
		styleDim.Render(fmt.Sprintf("%s · port %d", r.app.Description, r.app.Web.Host))))

	for _, w := range r.app.Warnings {
		lines = append(lines, "        "+styleWarn.Render("⚠ "+w))
	}
	if r.wasInstalled && !r.selected {
		lines = append(lines, "        "+styleWarn.Render(
			"will be removed — its config in appdata is preserved and reselecting brings it back intact"))
	}
	return lines
}

// budget is how many menu lines fit between the title and the footer, with
// two lines reserved for the ↑/↓ overflow markers. Zero height means the
// terminal never reported a size: render everything (also the test path).
func (m SelectModel) budget() int {
	if m.height == 0 {
		return 1 << 30
	}
	chrome := 2 + 2 // header (brand+rule) + help bar (blank+legend)
	if len(m.Selected()) == 0 {
		chrome += 2 // the "select at least one" nudge + its leading blank
	}
	b := m.height - chrome - 2
	if b < 3 {
		b = 3 // degenerate terminal: show at least the cursor's block
	}
	return b
}

// scrollToCursor moves the window so the cursor's block is fully visible.
func (m *SelectModel) scrollToCursor() {
	if m.cursor < m.top {
		m.top = m.cursor
	}
	for m.top < m.cursor {
		used := 0
		for i := m.top; i <= m.cursor; i++ {
			used += len(m.rowLines(i))
		}
		if used <= m.budget() {
			break
		}
		m.top++
	}
}

// View implements tea.Model. The menu is taller than a default terminal
// (13 apps + group headers + warnings), and an overflowing Bubble Tea view
// is clipped from the TOP — which silently ate the first rows (Jellyfin,
// field-reported). Render a window around the cursor instead, with explicit
// markers for what scrolled out.
func (m SelectModel) View() string {
	var b strings.Builder
	b.WriteString(header("Pick your stack"))

	budget := m.budget()
	if m.top > 0 {
		b.WriteString(styleDim.Render(fmt.Sprintf("  ↑ %d more above", m.top)) + "\n")
	}
	used, last := 0, m.top-1
	for i := m.top; i < len(m.rows); i++ {
		lines := m.rowLines(i)
		if used+len(lines) > budget && i > m.top {
			break
		}
		b.WriteString(strings.Join(lines, "\n") + "\n")
		used += len(lines)
		last = i
	}
	if hidden := len(m.rows) - 1 - last; hidden > 0 {
		b.WriteString(styleDim.Render(fmt.Sprintf("  ↓ %d more below", hidden)) + "\n")
	}

	if len(m.Selected()) == 0 {
		b.WriteString("\n" + styleWarn.Render("select at least one app to continue") + "\n")
	}
	b.WriteString(helpBar("↑/↓", "move", "space", "toggle", "a", "all", "enter", "continue", "q", "quit") +
		styleFaint.Render(fmt.Sprintf("   %d selected", len(m.Selected()))))
	return b.String()
}

func roleTitle(r registry.Role) string {
	for _, g := range roleOrder {
		if g.role == r {
			return g.title
		}
	}
	return string(r)
}
