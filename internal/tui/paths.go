package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Haroutio/arrsenal/internal/preflight"
	"github.com/Haroutio/arrsenal/internal/state"
)

// PathsModel is the storage screen (DESIGN.md §5.2): data root + appdata
// root as pre-filled editable fields above a table of the machine's real
// filesystems, so nobody has to remember where the big disk is mounted.
// Choosing the OS disk is allowed — after an explicit, numbered warning.
type PathsModel struct {
	mounts    []preflight.MountInfo
	inputs    []textinput.Model // 0 = data root, 1 = appdata root
	focus     int
	width     int // terminal width from WindowSizeMsg; 0 = unknown, no wrap
	err       string
	osWarn    string
	confirmed bool // OS-disk warning acknowledged once shown
	done      bool
	quit      bool
}

// NewPaths builds the screen; mounts come from preflight.ListMounts (or a
// fixture in tests).
func NewPaths(s *state.State, mounts []preflight.MountInfo) PathsModel {
	m := PathsModel{
		mounts: mounts,
		inputs: make([]textinput.Model, 3),
	}
	for i, v := range []string{s.DataRoot, s.AppdataRoot, s.DownloadsRoot} {
		in := textinput.New()
		in.SetValue(v)
		in.CharLimit = 200
		m.inputs[i] = in
	}
	m.inputs[2].Placeholder = "blank = same disk as media (hardlink imports)"
	m.inputs[0].Focus()
	return m
}

// Apply writes the chosen roots into s.
func (m PathsModel) Apply(s *state.State) {
	s.DataRoot = strings.TrimSpace(m.inputs[0].Value())
	s.AppdataRoot = strings.TrimSpace(m.inputs[1].Value())
	s.DownloadsRoot = strings.TrimSpace(m.inputs[2].Value())
}

// Done reports confirmation with valid paths (and an acknowledged warning
// when one applied).
func (m PathsModel) Done() bool { return m.done }

// Quit reports abort.
func (m PathsModel) Quit() bool { return m.quit }

// UpdateWith drives the screen.
func (m PathsModel) UpdateWith(msg tea.Msg, s *state.State) (PathsModel, tea.Cmd) {
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = size.Width
		return m, nil
	}
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "ctrl+c", "esc":
			m.quit = true
			return m, tea.Quit
		case "tab", "down":
			m.focus = (m.focus + 1) % len(m.inputs)
			m.refocus()
			return m, nil
		case "shift+tab", "up":
			m.focus = (m.focus + len(m.inputs) - 1) % len(m.inputs)
			m.refocus()
			return m, nil
		case "enter":
			candidate := *s
			m.Apply(&candidate)
			if err := candidate.Validate(); err != nil {
				m.err = err.Error()
				return m, nil
			}
			m.err = ""
			if warn := m.osDiskWarning(candidate.DataRoot); warn != "" && !m.confirmed {
				m.osWarn = warn
				m.confirmed = true // next enter proceeds
				return m, nil
			}
			m.done = true
			return m, nil
		default:
			// Any edit resets an acknowledged warning: new path, new decision.
			m.confirmed = false
			m.osWarn = ""
		}
	}
	var cmd tea.Cmd
	m.inputs[m.focus], cmd = m.inputs[m.focus].Update(msg)
	return m, cmd
}

func (m *PathsModel) refocus() {
	for i := range m.inputs {
		if i == m.focus {
			m.inputs[i].Focus()
		} else {
			m.inputs[i].Blur()
		}
	}
}

// osDiskWarning speaks up when the data root lands on the root filesystem.
func (m PathsModel) osDiskWarning(dataRoot string) string {
	mount, ok := preflight.MountFor(dataRoot, m.mounts)
	if !ok || !mount.IsOSDisk() {
		return ""
	}
	return fmt.Sprintf(
		"%s is on your OS disk (/, %s free). Media will fill it until the system tips over. Press enter again to accept, or point the data root at real storage.",
		dataRoot, preflight.HumanBytes(mount.FreeBytes))
}

// View implements the render.
func (m PathsModel) View() string {
	var b strings.Builder
	b.WriteString(header("Storage"))
	b.WriteString(styleDim.Render("One data root keeps imports as instant hardlinks; appdata is the backup surface.") + "\n\n")
	fmt.Fprintf(&b, "  %-15s %s\n", "Media root", m.inputs[0].View())
	fmt.Fprintf(&b, "  %-15s %s\n", "Appdata root", m.inputs[1].View())
	fmt.Fprintf(&b, "  %-15s %s\n", "Downloads root", m.inputs[2].View())
	if strings.TrimSpace(m.inputs[2].Value()) != "" &&
		strings.TrimSpace(m.inputs[2].Value()) != strings.TrimSpace(m.inputs[0].Value()) {
		b.WriteString(styleDim.Render(wrapText(m.width, 2, "split storage: downloads on their own disk — imports copy instead of hardlink (fine for an SSD-scratch setup)")) + "\n")
	}

	if len(m.mounts) > 0 {
		b.WriteString("\n" + groupRule("Detected storage") + "\n")
		for _, mt := range m.mounts {
			line := fmt.Sprintf("  %-24s %-10s %s free", mt.Target, mt.FSType, preflight.HumanBytes(mt.FreeBytes))
			if mt.IsOSDisk() {
				line += styleWarn.Render("  (OS disk)")
			}
			b.WriteString(line + "\n")
		}
	}

	if m.err != "" {
		b.WriteString("\n" + styleWarn.Render(wrapText(m.width, 0, "✗ "+m.err)) + "\n")
	}
	if m.osWarn != "" {
		b.WriteString("\n" + styleWarn.Render(wrapText(m.width, 0, "⚠ "+m.osWarn)) + "\n")
	}
	b.WriteString(helpBar("tab", "switch field", "enter", "continue", "esc", "quit"))
	return b.String()
}
