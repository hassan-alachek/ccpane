package ui

import (
	"fmt"
	"strings"

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

// DebugStats prints the stats view at a fixed size with color disabled.
// rangeDays selects the time filter (0 = all time).
func DebugStats(limit, w, h, rangeDays int) {
	lipgloss.SetColorProfile(termenv.Ascii)
	m := newStatsModel(limit, w, h, true)
	m.rangeDays = rangeDays
	(&m).layout()
	fmt.Println(m.View())
}

// DebugMemory prints the memory browser at a fixed size with color disabled.
// If query is set, it drills into the first matching project's first memory's
// inspect view (to verify wrapping).
func DebugMemory(limit, w, h int, query string) {
	lipgloss.SetColorProfile(termenv.Ascii)
	m := newMemoryModel(limit, w, h, true)
	if query != "" {
		for i, p := range m.projects {
			if strings.Contains(strings.ToLower(p.Label()), strings.ToLower(query)) {
				m.pc, m.proj = i, p
				if len(p.Memories) > 0 {
					m.mem = p.Memories[0]
					(&m).openInspect()
				}
				break
			}
		}
	}
	fmt.Println(m.View())
}
