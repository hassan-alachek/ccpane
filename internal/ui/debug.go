package ui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// DebugPane prints the pane view at a fixed size with color disabled, so the
// layout/connectors can be inspected without a TTY.
func DebugPane(path string, limit, w, h int) {
	lipgloss.SetColorProfile(termenv.Ascii)
	sv := newSessionView(path, limit)
	sv.setSize(w, h)
	fmt.Println(sv.view())
}

// DebugBrowser prints the browser view at a fixed size with color disabled.
func DebugBrowser(limit, w, h int, query string) {
	lipgloss.SetColorProfile(termenv.Ascii)
	m := newBrowserModel(limit)
	m.width, m.height = w, h
	if query != "" {
		m.search.SetValue(query)
		m.rebuild()
	}
	fmt.Println(m.View())
}
