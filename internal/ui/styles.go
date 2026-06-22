package ui

import "github.com/charmbracelet/lipgloss"

// Catppuccin Mocha palette (VS Code dark).
var (
	cFg      = lipgloss.Color("#cdd6f4") // text
	cAccent  = lipgloss.Color("#89b4fa") // blue
	cDim     = lipgloss.Color("#7f849c") // overlay1
	cGuide   = lipgloss.Color("#585b70") // surface2 (tree connectors)
	cGreen   = lipgloss.Color("#a6e3a1")
	cYellow  = lipgloss.Color("#f9e2af")
	cRed     = lipgloss.Color("#f38ba8")
	cMagenta = lipgloss.Color("#cba6f7") // mauve
	cCyan    = lipgloss.Color("#94e2d5") // teal
	cSelBg   = lipgloss.Color("#313244") // surface0 (selection)
)

var (
	stTitle = lipgloss.NewStyle().Bold(true).Foreground(cAccent)
	stDim   = lipgloss.NewStyle().Foreground(cDim)
	stFg    = lipgloss.NewStyle().Foreground(cFg)
	stGuide = lipgloss.NewStyle().Foreground(cGuide)
)
