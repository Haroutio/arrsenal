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
	return m, nil
}

// View implements tea.Model.
func (m SelectModel) View() string {
	var b strings.Builder
	b.WriteString(styleTitle.Render("Arrsenal — pick your stack") + "\n")

	lastRole := registry.Role("")
	for i, r := range m.rows {
		if r.app.Role != lastRole {
			lastRole = r.app.Role
			b.WriteString(styleGroup.Render(roleTitle(lastRole)) + "\n")
		}

		cursor := "  "
		if i == m.cursor {
			cursor = styleCursor.Render("> ")
		}
		box := "[ ]"
		if r.selected {
			box = styleSelected.Render("[x]")
		}
		line := fmt.Sprintf("%s%s %-12s %s", cursor, box, r.app.Name, styleDim.Render(r.app.Description))
		b.WriteString(line + "\n")

		for _, w := range r.app.Warnings {
			b.WriteString("        " + styleWarn.Render("⚠ "+w) + "\n")
		}
		if r.wasInstalled && !r.selected {
			b.WriteString("        " + styleWarn.Render(
				"will be removed — its config in appdata is preserved and reselecting brings it back intact") + "\n")
		}
	}

	if len(m.Selected()) == 0 {
		b.WriteString("\n" + styleWarn.Render("select at least one app to continue") + "\n")
	}
	b.WriteString(styleHelp.Render("↑/↓ move · space toggle · a all · enter continue · q quit"))
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
