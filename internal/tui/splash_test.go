package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func splashRows() []SplashRow {
	return []SplashRow{
		{Label: "host", Value: "ubuntu 24.04", OK: true},
		{Label: "docker", Value: "engine + compose plugin", OK: true},
		{Label: "gpu", Value: "nvidia", OK: true},
		{Label: "storage", Value: "/data · 79G free", OK: true},
	}
}

func bootedSplash(t *testing.T, w, h int) SplashModel {
	t.Helper()
	m := NewSplash("v0.5.0", splashRows())
	next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	m = next.(SplashModel)
	// The whole boot fits comfortably in 300 ticks (~18s of clock).
	for i := 0; i < 300; i++ {
		next, _ = m.Update(splashTick{})
		m = next.(SplashModel)
	}
	return m
}

// The user's rule, verbatim: the galleon sails uncut or not at all.
func TestShipAllOrNothing(t *testing.T) {
	big := bootedSplash(t, 140, 50)
	view := big.View()
	if !strings.Contains(view, "_..-''") { // the foremast pennant — art row 2
		t.Fatalf("a 140x50 terminal must show the galleon:\n%s", view)
	}

	small := bootedSplash(t, 80, 24)
	if strings.Contains(small.View(), "_..-''") {
		t.Fatal("an 80x24 terminal cannot fit the uncut galleon — no ship at all")
	}
}

func TestBootFinishesWithTheAnchorLine(t *testing.T) {
	for _, dims := range [][2]int{{140, 50}, {80, 24}} {
		m := bootedSplash(t, dims[0], dims[1])
		view := m.View()
		for _, want := range []string{"SYSTEM CHECK", "weigh anchor", "v0.5.0", "ready",
			"host", "docker", "gpu", "storage", "under one flag"} {
			if !strings.Contains(view, want) {
				t.Fatalf("%dx%d finished frame missing %q:\n%s", dims[0], dims[1], want, view)
			}
		}
	}
}

func TestAnyKeySkipsTheTheater(t *testing.T) {
	m := NewSplash("v0.5.0", splashRows())
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = next.(SplashModel)
	next, _ = m.Update(splashTick{}) // mid-reveal
	m = next.(SplashModel)

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")})
	m = next.(SplashModel)
	if m.Done() || m.Quit() {
		t.Fatal("first key skips to the finished frame, it does not exit")
	}
	if !strings.Contains(m.View(), "weigh anchor") {
		t.Fatalf("skip must land on the finished frame:\n%s", m.View())
	}

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")})
	m = next.(SplashModel)
	if !m.Done() {
		t.Fatal("a key on the finished frame continues the install")
	}
}

func TestCtrlCQuits(t *testing.T) {
	m := NewSplash("v0.5.0", splashRows())
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = next.(SplashModel)
	if !m.Quit() {
		t.Fatal("ctrl+c must abort")
	}
}
