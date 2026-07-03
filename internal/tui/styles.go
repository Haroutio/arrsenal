package tui

import "github.com/charmbracelet/lipgloss"

// One small palette, defined once. Screens compose these; nobody invents
// colors inline.
var (
	styleTitle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	styleGroup    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("245")).MarginTop(1)
	styleCursor   = lipgloss.NewStyle().Foreground(lipgloss.Color("212"))
	styleSelected = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	styleDim      = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	styleWarn     = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	styleHelp     = lipgloss.NewStyle().Foreground(lipgloss.Color("241")).MarginTop(1)
)
