package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Haroutio/arrsenal/internal/preflight"
	"github.com/Haroutio/arrsenal/internal/registry"
	"github.com/Haroutio/arrsenal/internal/state"
)

// RemapModel appears ONLY when preflight found blocking conflicts
// (DESIGN.md §4): port conflicts get an input pre-filled with a suggestion;
// container-name conflicts cannot be remapped and send the user back to the
// selection screen instead.
type RemapModel struct {
	ports  []preflight.Conflict
	names  []preflight.Conflict
	inputs []textinput.Model
	focus  int
	err    string
	done   bool
	back   bool
	quit   bool
}

// NewRemap builds the screen from the scan findings; non-blocking notices
// are not its business (the caller prints those).
func NewRemap(conflicts []preflight.Conflict, s *state.State) RemapModel {
	m := RemapModel{}
	for _, c := range conflicts {
		switch c.Kind {
		case preflight.KindPort:
			m.ports = append(m.ports, c)
		case preflight.KindContainerName:
			m.names = append(m.names, c)
		}
	}
	m.inputs = make([]textinput.Model, len(m.ports))
	for i, c := range m.ports {
		in := textinput.New()
		in.CharLimit = 5
		in.SetValue(strconv.Itoa(suggestPort(c.Port, s)))
		m.inputs[i] = in
	}
	if len(m.inputs) > 0 {
		m.inputs[0].Focus()
	}
	return m
}

// suggestPort proposes the busy port + 1000 (a very visible offset), walking
// upward past ports the current state already claims.
func suggestPort(busy int, s *state.State) int {
	claimed := map[int]bool{}
	for _, id := range s.Apps {
		app, ok := registry.ByID(id)
		if !ok {
			continue
		}
		claimed[s.WebHostPort(app)] = true
		for _, p := range app.ExtraPorts {
			claimed[s.HostPort(app, p)] = true
		}
	}
	candidate := busy + 1000
	for claimed[candidate] || candidate > 65535 {
		candidate++
		if candidate > 65535 {
			candidate = 1024
		}
	}
	return candidate
}

// NeedsInput reports whether the screen has anything to resolve at all.
func (m RemapModel) NeedsInput() bool { return len(m.ports) > 0 || len(m.names) > 0 }

// Done reports all port conflicts were resolved (only reachable when there
// are no name conflicts — those force Back).
func (m RemapModel) Done() bool { return m.done }

// Back reports the user chose to return to app selection.
func (m RemapModel) Back() bool { return m.back }

// Quit reports the user aborted.
func (m RemapModel) Quit() bool { return m.quit }

// Apply writes the resolved remaps into s (keyed by container port, per
// state.PortRemaps' contract). Call only after Done.
func (m RemapModel) Apply(s *state.State) {
	if s.PortRemaps == nil {
		s.PortRemaps = map[string]map[int]int{}
	}
	for i, c := range m.ports {
		port, _ := strconv.Atoi(strings.TrimSpace(m.inputs[i].Value()))
		if s.PortRemaps[c.App] == nil {
			s.PortRemaps[c.App] = map[int]int{}
		}
		s.PortRemaps[c.App][c.ContainerPort] = port
	}
}

// validate checks every input against a candidate state so cross-field
// collisions surface too (two conflicts resolved onto the same port).
func (m RemapModel) validate(s *state.State) error {
	for i, c := range m.ports {
		v := strings.TrimSpace(m.inputs[i].Value())
		port, err := strconv.Atoi(v)
		if err != nil || port < 1 || port > 65535 {
			return fmt.Errorf("%s: %q is not a port (1-65535)", c.App, v)
		}
	}
	candidate := *s
	candidate.PortRemaps = map[string]map[int]int{}
	for app, remaps := range s.PortRemaps {
		candidate.PortRemaps[app] = map[int]int{}
		for k, v := range remaps {
			candidate.PortRemaps[app][k] = v
		}
	}
	m.Apply(&candidate)
	return candidate.Validate()
}

// UpdateWith drives the screen.
func (m RemapModel) UpdateWith(msg tea.Msg, s *state.State) (RemapModel, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "ctrl+c", "esc":
			m.quit = true
			return m, tea.Quit
		case "b":
			m.back = true
			return m, nil
		case "tab", "down":
			if len(m.inputs) > 0 {
				m.focus = (m.focus + 1) % len(m.inputs)
				m.refocus()
			}
			return m, nil
		case "enter":
			if len(m.names) > 0 {
				m.err = "container-name conflicts cannot be remapped — press b to change your selection"
				return m, nil
			}
			if err := m.validate(s); err != nil {
				m.err = err.Error()
				return m, nil
			}
			m.err = ""
			m.done = true
			return m, nil
		}
	}
	if len(m.inputs) > 0 {
		var cmd tea.Cmd
		m.inputs[m.focus], cmd = m.inputs[m.focus].Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *RemapModel) refocus() {
	for i := range m.inputs {
		if i == m.focus {
			m.inputs[i].Focus()
		} else {
			m.inputs[i].Blur()
		}
	}
}

// View implements the render.
func (m RemapModel) View() string {
	var b strings.Builder
	b.WriteString(header("Conflicts found") + "\n")
	for _, c := range m.names {
		b.WriteString(styleWarn.Render("✗ "+c.Detail) + "\n")
	}
	for i, c := range m.ports {
		fmt.Fprintf(&b, "%s\n  new host port for %s: %s\n",
			styleWarn.Render("✗ "+c.Detail), c.App, m.inputs[i].View())
	}
	if m.err != "" {
		b.WriteString("\n" + styleWarn.Render("✗ "+m.err) + "\n")
	}
	pairs := []string{"enter", "apply", "b", "back to selection", "esc", "quit"}
	if len(m.inputs) > 1 {
		pairs = append([]string{"tab", "next"}, pairs...)
	}
	b.WriteString(helpBar(pairs...))
	return b.String()
}
