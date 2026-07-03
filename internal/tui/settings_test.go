package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Haroutio/arrsenal/internal/preflight"
	"github.com/Haroutio/arrsenal/internal/state"
)

func settingsKey(m SettingsModel, s *state.State, k string) SettingsModel {
	var msg tea.Msg
	switch k {
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "tab":
		msg = tea.KeyMsg{Type: tea.KeyTab}
	case "backspace":
		msg = tea.KeyMsg{Type: tea.KeyBackspace}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
	}
	next, _ := m.UpdateWith(msg, s)
	return next
}

func TestSettingsPrefillsAndApplies(t *testing.T) {
	s := state.New()
	s.Apps = []string{"sonarr"}
	s.PUID, s.PGID, s.TZ = 1234, 1234, "America/Los_Angeles"

	m := NewSettings(s)
	view := m.View()
	for _, want := range []string{"1234", "America/Los_Angeles", "002"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing prefilled %q", want)
		}
	}

	m = settingsKey(m, s, "enter")
	if !m.Done() {
		t.Fatal("valid prefill must confirm")
	}
	m.Apply(s)
	if s.PUID != 1234 || s.TZ != "America/Los_Angeles" {
		t.Fatalf("apply mangled state: %+v", s)
	}
}

func TestSettingsRejectsInvalidValues(t *testing.T) {
	s := state.New()
	s.Apps = []string{"sonarr"}
	m := NewSettings(s)

	// PUID field focused: type garbage.
	m = settingsKey(m, s, "x")
	m = settingsKey(m, s, "enter")
	if m.Done() {
		t.Fatal("non-numeric PUID must not confirm")
	}
	if m.err == "" || !strings.Contains(m.View(), "✗") {
		t.Fatal("error must render inline")
	}
}

func TestSettingsUmaskValidationComesFromState(t *testing.T) {
	s := state.New()
	s.Apps = []string{"sonarr"}
	m := NewSettings(s)
	// Move focus to umask (3 tabs) and corrupt it.
	m = settingsKey(m, s, "tab")
	m = settingsKey(m, s, "tab")
	m = settingsKey(m, s, "tab")
	m = settingsKey(m, s, "9")
	m = settingsKey(m, s, "enter")
	if m.Done() {
		t.Fatal("umask 0029 must fail state validation")
	}
}

func TestRemapResolvesPortConflicts(t *testing.T) {
	s := state.New()
	s.Apps = []string{"sonarr", "sabnzbd"}
	conflicts := []preflight.Conflict{
		{App: "sonarr", Kind: preflight.KindPort, Port: 8989, ContainerPort: 8989, Protocol: "tcp",
			Detail: "host port 8989/tcp (web UI) is already in use"},
	}
	m := NewRemap(conflicts, s)
	if !m.NeedsInput() {
		t.Fatal("a port conflict needs input")
	}
	if !strings.Contains(m.View(), "9989") {
		t.Fatalf("suggestion should be busy+1000: %s", m.View())
	}
	next, _ := m.UpdateWith(tea.KeyMsg{Type: tea.KeyEnter}, s)
	m = next
	if !m.Done() {
		t.Fatalf("valid suggestion must confirm: %s", m.err)
	}
	m.Apply(s)
	if s.PortRemaps["sonarr"][8989] != 9989 {
		t.Fatalf("remap not applied: %v", s.PortRemaps)
	}
}

func TestRemapRejectsCollidingResolution(t *testing.T) {
	s := state.New()
	s.Apps = []string{"sonarr", "sabnzbd"}
	conflicts := []preflight.Conflict{
		{App: "sonarr", Kind: preflight.KindPort, Port: 8989, ContainerPort: 8989, Protocol: "tcp"},
	}
	m := NewRemap(conflicts, s)
	// Type sabnzbd's port over the suggestion.
	m.inputs[0].SetValue("8080")
	next, _ := m.UpdateWith(tea.KeyMsg{Type: tea.KeyEnter}, s)
	m = next
	if m.Done() {
		t.Fatal("resolving onto another selected app's port must be rejected")
	}
	if !strings.Contains(m.err, "8080") {
		t.Fatalf("error should name the collision: %s", m.err)
	}
}

func TestRemapNameConflictsForceBack(t *testing.T) {
	s := state.New()
	s.Apps = []string{"sonarr"}
	conflicts := []preflight.Conflict{
		{App: "sonarr", Kind: preflight.KindContainerName, Detail: "a container named \"sonarr\" already exists"},
	}
	m := NewRemap(conflicts, s)
	next, _ := m.UpdateWith(tea.KeyMsg{Type: tea.KeyEnter}, s)
	m = next
	if m.Done() {
		t.Fatal("name conflicts cannot be resolved by enter")
	}
	m = func() RemapModel {
		n, _ := m.UpdateWith(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b")}, s)
		return n
	}()
	if !m.Back() {
		t.Fatal("b must go back to selection")
	}
}

func TestIdentityDetectionHonorsSudo(t *testing.T) {
	t.Setenv("SUDO_UID", "1000")
	t.Setenv("SUDO_GID", "1001")
	uid, gid := preflight.DetectIdentity()
	if uid != 1000 || gid != 1001 {
		t.Fatalf("sudo identity = %d:%d", uid, gid)
	}
}
