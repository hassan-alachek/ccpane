package ui

import (
	"fmt"
	"strings"

	"github.com/hassan-alachek/ccpane/internal/export"
	"github.com/hassan-alachek/ccpane/internal/transcript"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type seg struct {
	text  string
	style lipgloss.Style
}

func composeRow(segs []seg, selected bool, w int) string {
	var b strings.Builder
	for _, s := range segs {
		b.WriteString(withBg(s.style, selected).Render(s.text))
	}
	line := clip(b.String(), w)
	if selected {
		if pad := w - lipgloss.Width(line); pad > 0 {
			line += lipgloss.NewStyle().Background(cSelBg).Render(strings.Repeat(" ", pad))
		}
	}
	return line
}

type browseRow struct {
	header    bool
	group     transcript.Group
	collapsed bool
	session   *transcript.Session
}

// browserModel lists every session, grouped by directory, with search + resume.
type browserModel struct {
	all           []*transcript.Session
	rows          []browseRow
	collapsed     map[string]bool
	cursor        int
	offset        int
	search        textinput.Model
	searching     bool
	detail        *sessionView
	detailSession *transcript.Session
	resume        *transcript.Session
	limit         int
	width         int
	height        int
	status        string
}

func newBrowserModel(limit int) browserModel {
	ti := textinput.New()
	ti.Placeholder = "filter by title or path…"
	ti.Prompt = "/ "
	m := browserModel{
		all:       transcript.IndexSessions(),
		collapsed: map[string]bool{},
		search:    ti,
		limit:     limit,
	}
	m.rebuild()
	return m
}

func (m *browserModel) rebuild() {
	q := strings.ToLower(strings.TrimSpace(m.search.Value()))
	var filtered []*transcript.Session
	for _, s := range m.all {
		if q == "" ||
			strings.Contains(strings.ToLower(s.Title), q) ||
			strings.Contains(strings.ToLower(s.Project), q) ||
			strings.Contains(strings.ToLower(s.Path), q) {
			filtered = append(filtered, s)
		}
	}
	m.rows = m.rows[:0]
	for _, g := range transcript.GroupByProject(filtered) {
		collapsed := m.collapsed[g.Project] && q == ""
		m.rows = append(m.rows, browseRow{header: true, group: g, collapsed: collapsed})
		if !collapsed {
			for _, s := range g.Sessions {
				m.rows = append(m.rows, browseRow{session: s})
			}
		}
	}
	if m.cursor > len(m.rows)-1 {
		m.cursor = len(m.rows) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

func (m browserModel) cur() browseRow {
	if m.cursor >= 0 && m.cursor < len(m.rows) {
		return m.rows[m.cursor]
	}
	return browseRow{}
}

func (m *browserModel) moveCursor(d int) {
	n := m.cursor + d
	if n < 0 {
		n = 0
	}
	if n > len(m.rows)-1 {
		n = len(m.rows) - 1
	}
	m.cursor = n
}

func (m browserModel) Init() tea.Cmd { return nil }

func (m browserModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.search.Width = msg.Width - 6
		if m.detail != nil {
			m.detail.setSize(msg.Width, msg.Height)
		}
	case tea.KeyMsg:
		// detail view
		if m.detail != nil {
			if m.detail.reading {
				m.detail.handleKey(msg)
				if msg.String() == "ctrl+c" {
					return m, tea.Quit
				}
				return m, nil
			}
			switch msg.String() {
			case "ctrl+c":
				return m, tea.Quit
			case "esc", "q":
				m.detail, m.detailSession = nil, nil
			case "r":
				if m.detailSession != nil {
					m.resume = m.detailSession
					return m, tea.Quit
				}
			default:
				m.detail.handleKey(msg)
			}
			return m, nil
		}
		// search input
		if m.searching {
			switch msg.String() {
			case "enter":
				m.searching = false
				m.search.Blur()
			case "esc":
				m.searching = false
				m.search.Blur()
				m.search.SetValue("")
				m.rebuild()
			default:
				var cmd tea.Cmd
				m.search, cmd = m.search.Update(msg)
				m.rebuild()
				return m, cmd
			}
			return m, nil
		}
		// list
		m.status = ""
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "e":
			if r := m.cur(); r.session != nil {
				if out, err := export.WriteHTML(r.session.Path, m.limit, ""); err != nil {
					m.status = "export failed: " + err.Error()
				} else {
					m.status = "exported → " + out
				}
			}
		case "/":
			m.searching = true
			m.search.Focus()
			return m, textinput.Blink
		case "esc":
			if m.search.Value() != "" {
				m.search.SetValue("")
				m.rebuild()
			}
		case "up", "k":
			m.moveCursor(-1)
		case "down", "j":
			m.moveCursor(1)
		case "g", "home":
			m.cursor, m.offset = 0, 0
		case "G", "end":
			m.cursor = len(m.rows) - 1
		case "enter":
			r := m.cur()
			if r.header {
				m.collapsed[r.group.Project] = !m.collapsed[r.group.Project]
				m.rebuild()
			} else if r.session != nil {
				sv := newSessionView(r.session.Path, m.limit)
				sv.setSize(m.width, m.height)
				m.detail, m.detailSession = sv, r.session
			}
		case "r":
			if r := m.cur(); r.session != nil {
				m.resume = r.session
				return m, tea.Quit
			}
		}
	}
	return m, nil
}

func (m *browserModel) clamp(listH int) {
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor > len(m.rows)-1 {
		m.cursor = len(m.rows) - 1
	}
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+listH {
		m.offset = m.cursor - listH + 1
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

func (m browserModel) View() string {
	if m.detail != nil {
		return m.detail.view()
	}
	w := m.width
	if w <= 0 {
		w = 100
	}
	h := m.height
	if h <= 0 {
		h = 30
	}
	top := m.topBar(w)
	help := clip(stDim.Render("↑/↓ move · enter open · r resume · e export · / search · q quit"), w)
	if m.status != "" {
		help = clip(lipgloss.NewStyle().Foreground(cGreen).Render(m.status), w)
	}
	listH := h - lipgloss.Height(top) - 1
	if listH < 1 {
		listH = 1
	}
	return top + "\n" + m.renderList(listH, w) + "\n" + help
}

func (m browserModel) topBar(w int) string {
	var line string
	if m.searching || m.search.Value() != "" {
		right := stDim.Render(fmt.Sprintf("%d/%d", m.countSessions(), len(m.all)))
		line = spread(m.search.View(), right, w)
	} else {
		right := stDim.Render(fmt.Sprintf("%d sessions · %d projects", len(m.all), m.countProjects()))
		line = spread(stTitle.Render("❖ Claude Code sessions"), right, w)
	}
	return clip(line, w) + "\n" + stGuide.Render(strings.Repeat("─", w))
}

func (m browserModel) renderList(listH, w int) string {
	(&m).clamp(listH)
	var b strings.Builder
	printed := 0
	end := m.offset + listH
	if end > len(m.rows) {
		end = len(m.rows)
	}
	for i := m.offset; i < end; i++ {
		if printed > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(m.renderRow(m.rows[i], i == m.cursor, w))
		printed++
	}
	for printed < listH {
		if printed > 0 {
			b.WriteByte('\n')
		}
		printed++
	}
	if len(m.rows) == 0 {
		return clip(stDim.Render("  no sessions match"), w)
	}
	return b.String()
}

func (m browserModel) renderRow(r browseRow, selected bool, w int) string {
	if r.header {
		caret := "▾ "
		if r.collapsed {
			caret = "▸ "
		}
		proj := r.group.Project
		if proj == "" {
			proj = "(unknown directory)"
		}
		right := fmt.Sprintf("  %d · ~%s", len(r.group.Sessions), money(r.group.Cost()))
		gap := w - len([]rune(caret)) - len([]rune(proj)) - 1 - len([]rune(right))
		if gap < 1 {
			gap = 1
		}
		return composeRow([]seg{
			{caret, stGuide},
			{proj, lipgloss.NewStyle().Foreground(cCyan).Bold(true)},
			{strings.Repeat(" ", gap), lipgloss.NewStyle()},
			{right, stDim},
		}, selected, w)
	}

	s := r.session
	costW, tokW, msgW, modW := 8, 7, 6, 10
	titleW := w - 2 - modW - msgW - tokW - costW - 4
	if titleW < 12 {
		titleW = 12
	}
	return composeRow([]seg{
		{"  ", lipgloss.NewStyle()},
		{padR(s.Title, titleW), stFg},
		{" ", lipgloss.NewStyle()},
		{padL(relTime(s.LastTS), modW), stDim},
		{" ", lipgloss.NewStyle()},
		{padL(itoa(s.Messages), msgW), stDim},
		{" ", lipgloss.NewStyle()},
		{padL(fmtTok(s.TotalTokens()), tokW), lipgloss.NewStyle().Foreground(cCyan)},
		{" ", lipgloss.NewStyle()},
		{padL(money(s.Cost()), costW), lipgloss.NewStyle().Foreground(cYellow)},
	}, selected, w)
}

func (m browserModel) countSessions() int {
	n := 0
	for _, r := range m.rows {
		if !r.header {
			n++
		}
	}
	return n
}

func (m browserModel) countProjects() int {
	set := map[string]bool{}
	for _, s := range m.all {
		set[s.Project] = true
	}
	return len(set)
}

// RunBrowser launches the all-sessions browser and returns a session to resume
// (or nil if the user just quit).
func RunBrowser(limit int) (*transcript.Session, error) {
	m, err := tea.NewProgram(newBrowserModel(limit), tea.WithAltScreen()).Run()
	if err != nil {
		return nil, err
	}
	if bm, ok := m.(browserModel); ok {
		return bm.resume, nil
	}
	return nil, nil
}
