package ui

import (
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type tickMsg struct{}

func tick() tea.Cmd {
	return tea.Tick(750*time.Millisecond, func(time.Time) tea.Msg { return tickMsg{} })
}

// paneModel is the live, single-session pane; it polls the transcript mtime
// and reloads on change.
type paneModel struct {
	sv      *sessionView
	lastMod int64
}

func newPaneModel(path string, limit int) paneModel {
	m := paneModel{sv: newSessionView(path, limit)}
	if fi, err := os.Stat(path); err == nil {
		m.lastMod = fi.ModTime().UnixNano()
	}
	return m
}

func (m paneModel) Init() tea.Cmd { return tick() }

func (m paneModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.sv.setSize(msg.Width, msg.Height)
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		if m.sv.reading {
			m.sv.handleKey(msg)
			return m, nil
		}
		switch msg.String() {
		case "q":
			return m, tea.Quit
		case "r":
			m.sv.reload()
		default:
			m.sv.handleKey(msg)
		}
	case tickMsg:
		if fi, err := os.Stat(m.sv.path); err == nil {
			if mod := fi.ModTime().UnixNano(); mod != m.lastMod {
				m.lastMod = mod
				m.sv.reload()
			}
		}
		return m, tick()
	}
	return m, nil
}

func (m paneModel) View() string { return m.sv.view() }

// RunPane launches the live pane for a transcript.
func RunPane(path string, limit int) error {
	_, err := tea.NewProgram(newPaneModel(path, limit), tea.WithAltScreen()).Run()
	return err
}
