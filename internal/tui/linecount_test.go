package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// The fit gate promises the composition fits exactly — an overflowing
// frame clips from the top and eats the wordmark, so the promise is a test.
func TestSplashHeightPromiseIsExact(t *testing.T) {
	m := bootedSplash(t, 140, 41)
	if got := len(strings.Split(m.View(), "\n")); got > 41 {
		t.Fatalf("ship frame is %d lines; the gate promises ≤41", got)
	}
	small := bootedSplash(t, 80, 24)
	if got := len(strings.Split(small.View(), "\n")); got > 15 {
		t.Fatalf("ship-less frame is %d lines; must stay tiny", got)
	}
	// A mid-boot frame on a big terminal must already occupy the full
	// canvas — the layout never jumps as the ship rises.
	mid := NewSplash("v0.5.1", splashRows())
	next, _ := mid.Update(tea.WindowSizeMsg{Width: 140, Height: 41})
	mid = next.(SplashModel)
	next, _ = mid.Update(splashTick{})
	mid = next.(SplashModel)
	first := len(strings.Split(mid.View(), "\n"))
	done := len(strings.Split(bootedSplash(t, 140, 41).View(), "\n"))
	if first != done {
		t.Fatalf("layout jumps: first frame %d lines, finished %d", first, done)
	}
}
