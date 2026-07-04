package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"

	"github.com/Haroutio/arrsenal/internal/dockerx"
	"github.com/Haroutio/arrsenal/internal/wire"
)

// Arrsenal draws its own progress (issue #115) — docker's output never
// reaches the terminal. Palette matches the TUI: cyan for activity, green
// for done, dim for chrome.
var (
	progActive = lipgloss.NewStyle().Foreground(lipgloss.Color("45"))
	progDone   = lipgloss.NewStyle().Foreground(lipgloss.Color("84"))
	progDim    = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
)

// shortImage trims an image ref to the name a human scans for:
// lscr.io/linuxserver/sonarr:latest → sonarr.
func shortImage(ref string) string {
	name := ref
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		name = name[i+1:]
	}
	if i := strings.IndexByte(name, ':'); i >= 0 {
		name = name[:i]
	}
	return name
}

// pullImages downloads refs one at a time, each on its own line: a live
// layer-progress bar on a terminal, start/finish lines otherwise (CI).
func pullImages(d *dockerx.Docker, images []string) error {
	tty := term.IsTerminal(int(os.Stdout.Fd()))
	for i, ref := range images {
		count := progDim.Render(fmt.Sprintf("(%d/%d)", i+1, len(images)))
		name := shortImage(ref)
		if !tty {
			fmt.Printf("  ↓ pulling %s %s\n", name, count)
		}
		lastWidth := 0
		err := d.PullProgress(ref, func(done, total int) {
			if !tty {
				return
			}
			line := fmt.Sprintf("  %s %-12s %s %s",
				progActive.Render("↓"), name, renderBar(done, total), count)
			pad := ""
			if w := lipgloss.Width(line); w < lastWidth {
				pad = strings.Repeat(" ", lastWidth-w)
			} else {
				lastWidth = lipgloss.Width(line)
			}
			fmt.Print("\r" + line + pad)
		})
		if err != nil {
			if tty {
				fmt.Println()
			}
			return err
		}
		doneLine := fmt.Sprintf("  %s %-12s %s", progDone.Render("✓"), name, count)
		if tty {
			pad := ""
			if w := lipgloss.Width(doneLine); w < lastWidth {
				pad = strings.Repeat(" ", lastWidth-w)
			}
			fmt.Print("\r" + doneLine + pad + "\n")
		} else {
			fmt.Println(doneLine)
		}
	}
	return nil
}

// renderBar draws the layer-progress bar. Docker's pipe output has no byte
// counts, so layers are the unit — chunky but honest.
func renderBar(done, total int) string {
	const width = 22
	if total <= 0 {
		total = 1
	}
	filled := done * width / total
	bar := progDone.Render(strings.Repeat("▰", filled)) +
		progDim.Render(strings.Repeat("▱", width-filled))
	return fmt.Sprintf("%s %s", bar, progDim.Render(fmt.Sprintf("%d/%d layers", done, total)))
}

// wireProgress prints one wiring result the moment it lands — the report
// at the end stays the receipt.
func wireProgress(r wire.Result) {
	fmt.Println(wire.RenderLine(r))
}

// wireStage announces the slow stretches so the terminal never goes quiet.
func wireStage(msg string) {
	fmt.Println(progDim.Render("  … " + msg))
}
