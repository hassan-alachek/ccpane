package ui

import (
	"fmt"
	"strings"

	"github.com/hassan-alachek/ccpane/internal/memory"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

type memState int

const (
	mProjects memState = iota
	mFiles
	mInspect
)

// memoryModel browses Claude Code auto-memory: projects → memory files → inspect.
type memoryModel struct {
	projects   []*memory.Project
	st         memState
	pc         int // project cursor
	proj       *memory.Project
	fc         int // file cursor
	mem        *memory.Memory
	vp         viewport.Model
	raw        bool
	limit      int
	width      int
	height     int
	standalone bool
}

func newMemoryModel(limit, w, h int, standalone bool) memoryModel {
	return memoryModel{projects: memory.Projects(), limit: limit, width: w, height: h, standalone: standalone}
}

func (m memoryModel) Init() tea.Cmd { return nil }

func (m memoryModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if m.st == mInspect {
			m.vp.Width = max(1, m.width)
			m.vp.Height = max(1, m.height-3)
			(&m).setInspect()
		}
	case tea.KeyMsg:
		k := msg.String()
		if k == "ctrl+c" {
			return m, tea.Quit
		}
		switch m.st {
		case mProjects:
			switch k {
			case "up", "k":
				m.pc = clampi(m.pc-1, 0, len(m.projects)-1)
			case "down", "j":
				m.pc = clampi(m.pc+1, 0, len(m.projects)-1)
			case "g", "home":
				m.pc = 0
			case "G", "end":
				m.pc = len(m.projects) - 1
			case "enter", "right", "l":
				if m.pc >= 0 && m.pc < len(m.projects) {
					m.proj = m.projects[m.pc]
					m.fc = 0
					m.st = mFiles
				}
			case "q", "esc":
				return m.exit()
			}
		case mFiles:
			switch k {
			case "up", "k":
				m.fc = clampi(m.fc-1, 0, len(m.proj.Memories)-1)
			case "down", "j":
				m.fc = clampi(m.fc+1, 0, len(m.proj.Memories)-1)
			case "g", "home":
				m.fc = 0
			case "G", "end":
				m.fc = len(m.proj.Memories) - 1
			case "enter", "right", "l":
				if m.fc >= 0 && m.fc < len(m.proj.Memories) {
					m.mem = m.proj.Memories[m.fc]
					(&m).openInspect()
				}
			case "esc", "left", "h":
				m.st = mProjects
			case "q":
				return m.exit()
			}
		case mInspect:
			switch k {
			case "r", "t":
				m.raw = !m.raw
				(&m).setInspect()
			case "esc", "left", "h", "backspace":
				m.st = mFiles
			case "q":
				return m.exit()
			default:
				var cmd tea.Cmd
				m.vp, cmd = m.vp.Update(msg)
				return m, cmd
			}
		}
	}
	return m, nil
}

func (m memoryModel) exit() (tea.Model, tea.Cmd) {
	if m.standalone {
		return m, tea.Quit
	}
	b := newBrowserModel(m.limit)
	b.width, b.height = m.width, m.height
	return b, nil
}

func (m *memoryModel) openInspect() {
	m.st = mInspect
	m.raw = false
	m.vp = viewport.New(max(1, m.width), max(1, m.height-3))
	m.setInspect()
}

func (m *memoryModel) setInspect() {
	if m.raw {
		m.vp.SetContent(m.mem.Raw)
		return
	}
	w := max(20, m.width-2)
	var b strings.Builder
	if m.mem.Description != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(cDim).Italic(true).Width(w).Render(m.mem.Description))
		b.WriteString("\n\n")
	}
	body := m.mem.Body
	if r, err := glamour.NewTermRenderer(glamour.WithStandardStyle("dark"), glamour.WithWordWrap(w)); err == nil {
		if rendered, e := r.Render(m.mem.Body); e == nil {
			body = rendered
		}
	}
	b.WriteString(body)
	m.vp.SetContent(b.String())
}

func (m memoryModel) View() string {
	switch m.st {
	case mFiles:
		return m.viewFiles()
	case mInspect:
		return m.viewInspect()
	default:
		return m.viewProjects()
	}
}

func (m memoryModel) exitWord() string {
	if m.standalone {
		return "quit"
	}
	return "back"
}

func (m memoryModel) viewProjects() string {
	w, h := m.width, m.height
	if w <= 0 {
		w = 100
	}
	if h <= 0 {
		h = 30
	}
	files := 0
	for _, p := range m.projects {
		files += len(p.Memories)
	}
	top := clip(stTitle.Render(fmt.Sprintf("❖ Project memories — %d projects · %d files", len(m.projects), files)), w) +
		"\n" + stGuide.Render(strings.Repeat("─", w))
	help := clip(stDim.Render("↑/↓ move · enter open · q "+m.exitWord()), w)

	listH := h - 2 - 1
	if listH < 1 {
		listH = 1
	}
	var b strings.Builder
	start := startFor(m.pc, listH, len(m.projects))
	printed := 0
	for i := start; i < len(m.projects) && printed < listH; i++ {
		p := m.projects[i]
		sel := i == m.pc
		name := projectName(p.Label())
		info := fmt.Sprintf("%d memories  %s", len(p.Memories), typeSummary(p))
		row := composeRow([]seg{
			{"  " + name, stFg},
			{"   " + info, stDim},
		}, sel, w)
		if printed > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(row)
		printed++
	}
	for printed < listH {
		b.WriteByte('\n')
		printed++
	}
	if len(m.projects) == 0 {
		return top + "\n" + clip(stDim.Render("  no project memories found"), w) + "\n" + help
	}
	return top + "\n" + b.String() + "\n" + help
}

func (m memoryModel) viewFiles() string {
	w, h := m.width, m.height
	if w <= 0 {
		w = 100
	}
	if h <= 0 {
		h = 30
	}
	top := clip(stTitle.Render("❖ "+projectName(m.proj.Label())), w) +
		"\n" + stGuide.Render(strings.Repeat("─", w))
	help := clip(stDim.Render("↑/↓ move · enter inspect · esc up · q "+m.exitWord()), w)

	listH := h - 2 - 1
	if listH < 1 {
		listH = 1
	}
	var b strings.Builder
	start := startFor(m.fc, listH, len(m.proj.Memories))
	printed := 0
	for i := start; i < len(m.proj.Memories) && printed < listH; i++ {
		mem := m.proj.Memories[i]
		sel := i == m.fc
		icon, st := memTypeStyle(mem.Type)
		row := composeRow([]seg{
			{"  " + icon, st},
			{" " + mem.Name, stFg},
			{"   " + mem.Description, stDim},
		}, sel, w)
		if printed > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(row)
		printed++
	}
	for printed < listH {
		b.WriteByte('\n')
		printed++
	}
	return top + "\n" + b.String() + "\n" + help
}

func (m memoryModel) viewInspect() string {
	w := m.width
	if w <= 0 {
		w = 100
	}
	icon, st := memTypeStyle(m.mem.Type)
	t := m.mem.Type
	if t == "" {
		t = "memory"
	}
	title := clip(stTitle.Render("❖ "+m.mem.Name)+"  "+st.Render(icon+" "+t), w)
	sep := stGuide.Render(strings.Repeat("─", w))
	hint := "↑/↓ scroll · r raw/rendered · esc up · q " + m.exitWord()
	if m.raw {
		hint = "↑/↓ scroll · r rendered · esc up · q " + m.exitWord()
	}
	help := clip(stDim.Render(hint), w)
	return title + "\n" + sep + "\n" + m.vp.View() + "\n" + help
}

func memTypeStyle(t string) (string, lipgloss.Style) {
	switch t {
	case "project":
		return "◆", lipgloss.NewStyle().Foreground(cAccent)
	case "feedback":
		return "✎", lipgloss.NewStyle().Foreground(cYellow)
	case "reference":
		return "◈", lipgloss.NewStyle().Foreground(cMagenta)
	case "user":
		return "●", lipgloss.NewStyle().Foreground(cGreen)
	}
	return "·", stDim
}

func typeSummary(p *memory.Project) string {
	tc := p.TypeCounts()
	var parts []string
	for _, t := range []string{"project", "feedback", "reference", "user", "other"} {
		if n := tc[t]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, t))
		}
	}
	return strings.Join(parts, " · ")
}

func clampi(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// startFor returns a scroll offset keeping cursor visible in a height-line window.
func startFor(cursor, height, n int) int {
	if height <= 0 || n <= height {
		return 0
	}
	start := cursor - height + 1
	if start < 0 {
		start = 0
	}
	if start > n-height {
		start = n - height
	}
	return start
}

// RunMemory launches the memory browser standalone.
func RunMemory(limit int) error {
	_, err := tea.NewProgram(newMemoryModel(limit, 0, 0, true), tea.WithAltScreen()).Run()
	return err
}
