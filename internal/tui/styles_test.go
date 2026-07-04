package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestWrapTextWrapsToWidth(t *testing.T) {
	msg := "/data is on your OS disk (/, 145.3 GiB free). Media will fill it until the system tips over. Press enter again to accept, or point the data root at real storage."
	out := wrapText(60, 0, msg)
	lines := strings.Split(out, "\n")
	if len(lines) < 2 {
		t.Fatalf("long prose must wrap: %q", out)
	}
	for _, ln := range lines {
		if lipgloss.Width(ln) > 60 {
			t.Fatalf("line exceeds width: %q", ln)
		}
	}
	// Nothing may be lost — the field report was a warning cut mid-sentence.
	joined := strings.Join(strings.Fields(strings.ReplaceAll(out, "\n", " ")), " ")
	if !strings.Contains(joined, "real storage.") {
		t.Fatalf("tail of the message lost: %q", joined)
	}
}

func TestWrapTextIndentsEveryLine(t *testing.T) {
	out := wrapText(40, 8, strings.Repeat("word ", 20))
	for _, ln := range strings.Split(out, "\n") {
		if !strings.HasPrefix(ln, strings.Repeat(" ", 8)) {
			t.Fatalf("line missing indent: %q", ln)
		}
	}
}

func TestWrapTextZeroWidthNoWrap(t *testing.T) {
	msg := strings.Repeat("x ", 100)
	if out := wrapText(0, 2, msg); strings.Contains(out, "\n") {
		t.Fatalf("unknown width must not wrap: %q", out)
	}
}

func TestFitLineTruncatesStyledRows(t *testing.T) {
	long := "▸ ● " + styleHot.Render("Jellyfin") + "     " + styleDim.Render(strings.Repeat("description ", 20))
	out := fitLine(50, long)
	if w := lipgloss.Width(out); w > 50 {
		t.Fatalf("row not truncated: width %d", w)
	}
	if !strings.Contains(out, "…") {
		t.Fatalf("truncation must be visible: %q", out)
	}
	// Short lines and unknown widths pass through untouched.
	if fitLine(0, long) != long || fitLine(500, long) != long {
		t.Fatal("fitLine must be a no-op when it fits or width is unknown")
	}
}
