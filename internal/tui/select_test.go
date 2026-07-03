package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Haroutio/arrsenal/internal/registry"
)

func key(m SelectModel, k string) SelectModel {
	var msg tea.KeyMsg
	switch k {
	case " ":
		msg = tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "up":
		msg = tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		msg = tea.KeyMsg{Type: tea.KeyDown}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
	}
	next, _ := m.Update(msg)
	return next.(SelectModel)
}

func TestSelectShowsEveryRegistryAppGrouped(t *testing.T) {
	m := NewSelect(nil)
	if len(m.rows) != len(registry.All()) {
		t.Fatalf("menu shows %d rows for %d registry apps", len(m.rows), len(registry.All()))
	}
	view := m.View()
	for _, app := range registry.All() {
		if !strings.Contains(view, app.Name) {
			t.Errorf("view is missing %s", app.Name)
		}
	}
	for _, group := range []string{"Media server", "Download clients", "Dashboard"} {
		if !strings.Contains(view, group) {
			t.Errorf("view is missing group header %s", group)
		}
	}
}

func TestSelectPrePopulatesFromState(t *testing.T) {
	m := NewSelect([]string{"sonarr", "jellyfin"})
	got := m.Selected()
	if len(got) != 2 {
		t.Fatalf("pre-selection = %v", got)
	}
	// Registry menu order: jellyfin (media server) before sonarr (pvr).
	if got[0] != "jellyfin" || got[1] != "sonarr" {
		t.Fatalf("selection order should follow the menu, got %v", got)
	}
}

func TestSelectToggleAndConfirm(t *testing.T) {
	m := NewSelect(nil)
	if m.Selected() != nil {
		t.Fatal("fresh run starts empty")
	}
	m = key(m, "enter")
	if m.Done() {
		t.Fatal("enter with zero selection must not advance")
	}
	m = key(m, " ") // toggle first row (jellyfin)
	if len(m.Selected()) != 1 {
		t.Fatalf("toggle failed: %v", m.Selected())
	}
	m = key(m, "enter")
	if !m.Done() {
		t.Fatal("enter with a selection must advance")
	}
}

func TestSelectDeselectionShowsRemovalPromise(t *testing.T) {
	m := NewSelect([]string{"jellyfin"})
	m = key(m, " ") // cursor starts on jellyfin: deselect it
	if got := m.Removals(); len(got) != 1 || got[0] != "jellyfin" {
		t.Fatalf("removals = %v", got)
	}
	view := m.View()
	if !strings.Contains(view, "will be removed") || !strings.Contains(view, "preserved") {
		t.Fatal("deselecting an installed app must show the removal-with-preservation promise")
	}
	// Reselecting clears it.
	m = key(m, " ")
	if len(m.Removals()) != 0 {
		t.Fatal("reselect must clear the removal")
	}
}

func TestSelectSelectAllAndQuit(t *testing.T) {
	m := NewSelect(nil)
	m = key(m, "a")
	if len(m.Selected()) != len(registry.All()) {
		t.Fatalf("select-all got %d", len(m.Selected()))
	}
	m = key(m, "q")
	if !m.Quit() {
		t.Fatal("q must abort")
	}
}

func TestSelectCursorStaysInBounds(t *testing.T) {
	m := NewSelect(nil)
	m = key(m, "up") // at top already
	if m.cursor != 0 {
		t.Fatal("cursor escaped the top")
	}
	for i := 0; i < len(m.rows)+5; i++ {
		m = key(m, "down")
	}
	if m.cursor != len(m.rows)-1 {
		t.Fatalf("cursor escaped the bottom: %d", m.cursor)
	}
}
