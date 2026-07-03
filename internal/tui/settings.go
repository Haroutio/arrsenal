package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Haroutio/arrsenal/internal/state"
)

// SettingsModel edits the identity block of the state: PUID, PGID, TZ,
// UMASK. Defaults come pre-filled (detected by the caller); validation is
// the state package's rules, surfaced inline. The wiring admin credential
// deliberately has no field here until v0.2 exists to consume it — nothing
// collects a secret it cannot use (DESIGN.md §9).
type SettingsModel struct {
	inputs []textinput.Model
	labels []string
	focus  int
	err    string
	done   bool
	quit   bool
}

const (
	fieldPUID = iota
	fieldPGID
	fieldTZ
	fieldUmask
	fieldCount
)

// NewSettings builds the screen pre-filled from s.
func NewSettings(s *state.State) SettingsModel {
	m := SettingsModel{
		labels: []string{"PUID", "PGID", "Timezone", "Umask"},
		inputs: make([]textinput.Model, fieldCount),
	}
	values := []string{
		strconv.Itoa(s.PUID), strconv.Itoa(s.PGID), s.TZ, s.Umask,
	}
	for i := range m.inputs {
		in := textinput.New()
		in.SetValue(values[i])
		in.CharLimit = 64
		m.inputs[i] = in
	}
	m.inputs[0].Focus()
	return m
}

// Apply writes the fields into s. Only valid states get out (Done implies
// a passing validation).
func (m SettingsModel) Apply(s *state.State) {
	s.PUID, _ = strconv.Atoi(strings.TrimSpace(m.inputs[fieldPUID].Value()))
	s.PGID, _ = strconv.Atoi(strings.TrimSpace(m.inputs[fieldPGID].Value()))
	s.TZ = strings.TrimSpace(m.inputs[fieldTZ].Value())
	s.Umask = strings.TrimSpace(m.inputs[fieldUmask].Value())
}

// validate runs the candidate values through the state rules on a copy.
func (m SettingsModel) validate(s *state.State) error {
	candidate := *s
	m.Apply(&candidate)
	if _, err := strconv.Atoi(strings.TrimSpace(m.inputs[fieldPUID].Value())); err != nil {
		return fmt.Errorf("PUID must be a number")
	}
	if _, err := strconv.Atoi(strings.TrimSpace(m.inputs[fieldPGID].Value())); err != nil {
		return fmt.Errorf("PGID must be a number")
	}
	return candidate.Validate()
}

// Done reports the screen was confirmed with valid values.
func (m SettingsModel) Done() bool { return m.done }

// Quit reports the user aborted.
func (m SettingsModel) Quit() bool { return m.quit }

// Init implements tea.Model.
func (m SettingsModel) Init() tea.Cmd { return textinput.Blink }

// UpdateWith drives the screen; the state is needed for full validation.
func (m SettingsModel) UpdateWith(msg tea.Msg, s *state.State) (SettingsModel, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "ctrl+c", "esc":
			m.quit = true
			return m, tea.Quit
		case "tab", "down":
			m.focus = (m.focus + 1) % fieldCount
			m.refocus()
			return m, nil
		case "shift+tab", "up":
			m.focus = (m.focus + fieldCount - 1) % fieldCount
			m.refocus()
			return m, nil
		case "enter":
			if err := m.validate(s); err != nil {
				m.err = err.Error()
				return m, nil
			}
			m.err = ""
			m.done = true
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.inputs[m.focus], cmd = m.inputs[m.focus].Update(msg)
	return m, cmd
}

func (m *SettingsModel) refocus() {
	for i := range m.inputs {
		if i == m.focus {
			m.inputs[i].Focus()
		} else {
			m.inputs[i].Blur()
		}
	}
}

// View implements tea.Model.
func (m SettingsModel) View() string {
	var b strings.Builder
	b.WriteString(styleTitle.Render("Identity & environment") + "\n")
	b.WriteString(styleDim.Render("The containers run as this user; media files will belong to it.") + "\n\n")
	for i, in := range m.inputs {
		fmt.Fprintf(&b, "  %-9s %s\n", m.labels[i], in.View())
	}
	if m.err != "" {
		b.WriteString("\n" + styleWarn.Render("✗ "+m.err) + "\n")
	}
	b.WriteString(styleHelp.Render("tab next field · enter continue · esc quit"))
	return b.String()
}
