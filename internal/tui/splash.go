package tui

import (
	"fmt"
	"math"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// splashTickInterval is the single animation clock (~60ms), one ticker
// driving every effect.
const splashTickInterval = 60 * time.Millisecond

// SplashRow is one line of the boot readout — a real probe result, not
// theater: the values come from the same detection the installer uses.
type SplashRow struct {
	Label string
	Value string
	OK    bool
}

// SplashModel is the boot sequence: ambient strip → wordmark reveals
// column-by-column → the galleon rises
// from the waterline (when it fits — see shipFits) → version and tagline
// type in → the system check streams → "press any key to weigh anchor".
// Any key skips straight to the finished frame; a key on the finished
// frame continues.
type SplashModel struct {
	version string
	rows    []SplashRow

	width, height int
	stage         int // stageReveal → stageShip → stageType → stageRows → stageReady
	cols          int // wordmark columns revealed
	shipN         int // ship rows risen (bottom-up), incl. wave rows
	typed         int // characters of version+tagline typed
	shownRows     int
	rowTick       int
	phase         int // free-running animation clock (sails, waves, spinner, blink)
	done          bool
	quit          bool
}

const (
	stageReveal = iota
	stageShip
	stageType
	stageRows
	stageReady
)

// Half-block wordmark, from the design.
const (
	wordmark1 = "█▀█ █▀█ █▀█ █▀ █▀▀ █▄ █ █▀█ █"
	wordmark2 = "█▀█ █▀▄ █▀▄ ▄█ ██▄ █ ▀█ █▀█ █▄▄"
	tagline   = "the arr stack, under one flag."
	ambient   = "░ ▒ ▓   f l e e t   ·   o n l i n e   ▓ ▒ ░"
)

var spinFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧"}

const waveRows = 3

// splashTick is the single animation clock (~60ms).
type splashTick struct{}

// NewSplash builds the boot screen. rows are the live probe results.
func NewSplash(version string, rows []SplashRow) SplashModel {
	return SplashModel{version: version, rows: rows}
}

// Done reports the user pressed a key on the finished frame.
func (m SplashModel) Done() bool { return m.done }

// Quit reports ctrl+c.
func (m SplashModel) Quit() bool { return m.quit }

// shipFits: the galleon sails uncut or not at all (91 cols of art plus
// margin, and the full composition needs the rows). No trimming — cutting
// to 64 cols would amputate the stern rigging. The row count is exact:
// with the ship, View renders 41 lines (an overflowing frame clips from
// the TOP, which would eat the wordmark — the select menu learned that
// the hard way).
func (m SplashModel) shipFits() bool {
	return m.width >= 95 && m.height >= 41
}

func (m SplashModel) shipTotal() int { return len(shipArt) + waveRows }

func (m SplashModel) typeTarget() string { return m.version + tagline }

// Init implements tea.Model.
func (m SplashModel) Init() tea.Cmd { return splashTickCmd() }

func splashTickCmd() tea.Cmd {
	return tea.Tick(splashTickInterval, func(time.Time) tea.Msg { return splashTick{} })
}

// Update implements tea.Model.
func (m SplashModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			m.quit = true
			return m, tea.Quit
		}
		if m.stage == stageReady {
			m.done = true
			return m, tea.Quit
		}
		// Skip the theater: jump to the finished frame.
		m.cols = len([]rune(wordmark2))
		m.shipN = m.shipTotal()
		m.typed = len([]rune(m.typeTarget()))
		m.shownRows = len(m.rows)
		m.stage = stageReady
		return m, splashTickCmd()
	case splashTick:
		m.phase++
		m.advance()
		return m, splashTickCmd()
	}
	return m, nil
}

// advance moves the boot sequence one clock step, mirroring the design's
// cadence (reveal ~26ms/col, ship 70ms/row, rows 300ms apart) on a 60ms
// clock.
func (m *SplashModel) advance() {
	switch m.stage {
	case stageReveal:
		m.cols += 2
		if m.cols >= len([]rune(wordmark2)) {
			m.cols = len([]rune(wordmark2))
			if m.shipFits() {
				m.stage = stageShip
			} else {
				m.stage = stageType
			}
		}
	case stageShip:
		m.shipN++
		if m.shipN >= m.shipTotal() {
			m.stage = stageType
		}
	case stageType:
		m.typed += 2
		if m.typed >= len([]rune(m.typeTarget())) {
			m.typed = len([]rune(m.typeTarget()))
			m.stage = stageRows
		}
	case stageRows:
		m.rowTick++
		if m.rowTick%5 == 0 {
			m.shownRows++
		}
		if m.shownRows >= len(m.rows) {
			m.shownRows = len(m.rows)
			m.stage = stageReady
		}
	}
}

// View implements tea.Model.
func (m SplashModel) View() string {
	var b strings.Builder

	b.WriteString(styleFaint.Render(ambient) + "\n")

	// Wordmark, revealed left-to-right, with a light band sweeping across
	// once fully revealed (the design's sweep, done with color instead of
	// mix-blend-mode).
	b.WriteString(m.renderWordmark(wordmark1) + "\n")
	b.WriteString(m.renderWordmark(wordmark2) + "\n")
	div := int(float64(m.cols) / float64(len([]rune(wordmark2))) * 48)
	b.WriteString(styleShipSea.Render(strings.Repeat("─", div)) + "\n")

	// Version · tagline, typed.
	typed := string([]rune(m.typeTarget())[:m.typed])
	ver := typed
	tag := ""
	if len(typed) > len(m.version) {
		ver, tag = m.version, typed[len(m.version):]
	}
	line := styleKey.Render(ver)
	if m.stage == stageReady {
		line += " " + styleSelected.Render("● ready")
	}
	if tag != "" {
		line += styleFaint.Render(" · ") + styleDim.Render(tag)
	}
	b.WriteString(line + "\n")

	// The ship canvas is reserved from the first frame (hidden rows render
	// blank) so the layout never jumps as she rises.
	if m.shipFits() {
		b.WriteString(m.renderShip())
	} else {
		b.WriteString("\n")
	}

	// System check.
	b.WriteString(styleKey.Render("§ SYSTEM CHECK ") + styleFaint.Render(strings.Repeat("─", 33)) + "\n")
	for i := 0; i < len(m.rows); i++ {
		if i >= m.shownRows {
			b.WriteString("\n") // reserved: rows stream into a fixed canvas
			continue
		}
		r := m.rows[i]
		row := styleKey.Render("▸ ") + styleShipHull.Render(fmt.Sprintf("%-9s", r.Label)) + styleDim.Render(r.Value)
		if r.OK {
			row += styleSelected.Render(" ●")
		}
		b.WriteString(row + "\n")
	}

	// Bottom bar: progress while booting, anchor line when ready.
	b.WriteString("\n")
	if m.stage == stageReady {
		cursor := " "
		if m.phase%16 < 8 {
			cursor = "█"
		}
		b.WriteString(styleKey.Render("▸ ") + styleDim.Render("press any key to weigh anchor ") + styleKey.Render(cursor))
	} else {
		total := m.progressTotal()
		done := m.progressDone()
		filled := 0
		if total > 0 {
			filled = done * 18 / total
		}
		b.WriteString(styleKey.Render(spinFrames[m.phase%len(spinFrames)]) + styleDim.Render("  booting fleet   ") +
			styleKey.Render(strings.Repeat("▓", filled)) + styleFaint.Render(strings.Repeat("░", 18-filled)) +
			styleDim.Render(fmt.Sprintf("  %3d%%", done*100/max(total, 1))))
	}
	return b.String()
}

func (m SplashModel) progressTotal() int {
	t := len([]rune(wordmark2)) + len([]rune(m.typeTarget())) + len(m.rows)*5
	if m.shipFits() {
		t += m.shipTotal()
	}
	return t
}

func (m SplashModel) progressDone() int {
	return m.cols + m.shipN + m.typed + m.rowTick
}

func (m SplashModel) renderWordmark(w string) string {
	runes := []rune(w)
	shown := m.cols
	if shown > len(runes) {
		shown = len(runes)
	}
	visible := runes[:shown]
	if m.stage == stageReveal || len(visible) == 0 {
		return styleTitle.Render(string(visible))
	}
	// Sweep: a 5-column bright band gliding across, then wrapping.
	pos := (m.phase * 2) % (len(runes) + 24)
	var b strings.Builder
	for i, r := range visible {
		if i >= pos && i < pos+5 {
			b.WriteString(styleShipBright.Render(string(r)))
		} else {
			b.WriteString(styleTitle.Render(string(r)))
		}
	}
	return b.String()
}

// renderShip draws the galleon rising bottom-up, with fluttering sail rows,
// drifting sea, and animated wave crests beneath the hull — the design's
// renderVals math, ported.
func (m SplashModel) renderShip() string {
	const indent = 3
	const sailRows = 18
	n := len(shipArt)
	total := m.shipTotal()
	lastRow := n - 1
	hybridRow := -1
	for i, l := range shipArt {
		if strings.HasPrefix(strings.TrimSpace(l), "`-.") {
			hybridRow = i
			break
		}
	}
	maxLen := 0
	for _, l := range shipArt {
		if len(l) > maxLen {
			maxLen = len(l)
		}
	}
	drift := int(math.Round(1.5 * math.Sin(float64(m.phase)*0.7)))
	seaShift := func(s string) string {
		if drift > 0 {
			return strings.Repeat(" ", drift) + s
		}
		if drift < 0 && -drift < len(s) {
			return s[-drift:]
		}
		return s
	}

	var b strings.Builder
	for i := 0; i < n; i++ {
		shown := i >= total-m.shipN
		if !shown {
			b.WriteString("\n")
			continue
		}
		raw := shipArt[i]
		switch {
		case i == hybridRow:
			// Leading wake token stays still; the rest drifts as sea.
			lead := len(raw) - len(strings.TrimLeft(raw, " "))
			tok := strings.Fields(raw)
			cut := lead
			if len(tok) > 0 {
				cut += len(tok[0])
			}
			b.WriteString(styleShipHull.Render(strings.Repeat(" ", indent)+raw[:cut]) +
				styleShipSea.Render(seaShift(raw[cut:])) + "\n")
		case i == lastRow:
			b.WriteString(styleShipSea.Render(strings.Repeat(" ", indent)+seaShift(raw)) + "\n")
		case i < sailRows:
			// Sail tops flutter: a sub-column ripple easing off toward the hull.
			lift := math.Min(1, float64(sailRows-i)/4)
			shift := indent + int(math.Round(0.7*lift*math.Sin(float64(m.phase)*0.28+float64(i)*0.5)))
			if shift < 0 {
				shift = 0
			}
			b.WriteString(styleShipHull.Render(strings.Repeat(" ", shift)+raw) + "\n")
		default:
			b.WriteString(styleShipHull.Render(strings.Repeat(" ", indent)+raw) + "\n")
		}
	}
	// Splashing water below the hull: crests rise and fall in place.
	waveW := maxLen + indent
	for w := 0; w < waveRows; w++ {
		gi := n + w
		if gi < total-m.shipN {
			b.WriteString("\n")
			continue
		}
		var s strings.Builder
		for c := 0; c < waveW; c++ {
			y := math.Sin(float64(c)*0.5 + float64(m.phase)*0.6 + float64(w)*1.25)
			switch {
			case y > 0.4:
				s.WriteString("≈")
			case y > -0.2:
				s.WriteString("~")
			default:
				s.WriteString(" ")
			}
		}
		b.WriteString(styleShipWave.Render(s.String()) + "\n")
	}
	return b.String()
}
