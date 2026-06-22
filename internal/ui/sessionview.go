package ui

import (
	"fmt"
	"strings"

	"github.com/hassan-alachek/ccpane/internal/export"
	"github.com/hassan-alachek/ccpane/internal/transcript"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// sessionView is the shared header + collapsible tree (and full-text reader)
// for one transcript. Powers both the live pane and the browser detail.
type sessionView struct {
	path      string
	limit     int // 0 = auto-detect window
	title     string
	cwd       string
	gitBranch string
	stats     transcript.Stats
	tree      *tree
	width     int
	height    int

	reading      bool
	reader       viewport.Model
	readerHeader string
	readNode     *transcript.Node
	readRaw      bool
	status       string
}

func newSessionView(path string, limit int) *sessionView {
	sv := &sessionView{path: path, limit: limit}
	sv.reload()
	return sv
}

func (sv *sessionView) reload() {
	recs, err := transcript.ParseFile(sv.path)
	if err != nil {
		return
	}
	sv.stats = transcript.Aggregate(recs)
	sv.title = transcript.SessionTitle(recs)
	sv.cwd = transcript.SessionCwd(recs)
	sv.gitBranch = transcript.SessionGitBranch(recs)

	roots := transcript.FullTree(sv.path)
	if sv.tree == nil {
		sv.tree = newTree(roots)
	} else {
		sv.tree.setRoots(roots)
	}
}

// window returns the context window: explicit -limit, else auto-detected.
func (sv *sessionView) window() int {
	if sv.limit > 0 {
		return sv.limit
	}
	return transcript.AutoWindow(sv.stats.MaxContext)
}

func (sv *sessionView) setSize(w, h int) {
	sv.width, sv.height = w, h
	if sv.reading {
		sv.reader.Width = max(1, w)
		sv.reader.Height = max(1, h-2)
		sv.setReaderContent()
	}
}

func (sv *sessionView) handleKey(msg tea.KeyMsg) {
	if sv.tree == nil {
		return
	}
	sv.status = ""
	k := msg.String()

	if sv.reading {
		switch k {
		case "esc", "q", "enter", "backspace":
			sv.reading = false
		case "t", "r":
			if sv.readNode != nil && sv.readNode.Raw != "" {
				sv.readRaw = !sv.readRaw
				sv.setReaderContent()
			}
		default:
			sv.reader, _ = sv.reader.Update(msg)
		}
		return
	}

	switch k {
	case "up", "k":
		sv.tree.up()
	case "down", "j":
		sv.tree.down()
	case "g", "home":
		sv.tree.top()
	case "G", "end":
		sv.tree.bottom()
	case "left", "h":
		sv.tree.collapse()
	case "right", "l":
		sv.tree.expand()
	case " ", "tab":
		sv.tree.toggle()
	case "enter":
		if n := sv.tree.current(); n != nil && strings.TrimSpace(n.Full) != "" {
			sv.enterRead(n)
		} else {
			sv.tree.toggle()
		}
	case "e":
		sv.export()
	}
}

func (sv *sessionView) enterRead(n *transcript.Node) {
	sv.reading = true
	sv.readNode = n
	sv.readRaw = false
	w, h := max(1, sv.width), max(1, sv.height-2)
	sv.reader = viewport.New(w, h)
	sv.readerHeader = strings.TrimSpace(n.Icon + " " + nodeTitle(n))
	sv.setReaderContent()
}

func (sv *sessionView) setReaderContent() {
	if sv.readNode == nil {
		return
	}
	content := sv.readNode.Full
	if sv.readRaw && sv.readNode.Raw != "" {
		content = sv.readNode.Raw
	}
	sv.reader.SetContent(lipgloss.NewStyle().Width(max(1, sv.width)).Render(content))
}

func (sv *sessionView) export() {
	path, err := export.WriteHTML(sv.path, sv.window(), "")
	if err != nil {
		sv.status = "export failed: " + err.Error()
		return
	}
	sv.status = "exported → " + path
}

func nodeTitle(n *transcript.Node) string {
	if n.Label != "" {
		return n.Label
	}
	if n.Detail != "" {
		return n.Detail
	}
	return n.Kind
}

func (sv *sessionView) header() string {
	w := sv.width
	if w <= 0 {
		w = 80
	}
	win := sv.window()
	pct := 0.0
	if win > 0 {
		pct = float64(sv.stats.ContextNow) / float64(win)
	}

	title := stTitle.Render("❖ " + sv.title)
	model := lipgloss.NewStyle().Foreground(cCyan).Render(shortModelSafe(sv.stats.Model))
	line1 := spread(title, model, w)

	barStr := stGuide.Render("▕") + barStyle(pct).Render(bar(pct, 18)) + stGuide.Render("▏")
	ctxL := stDim.Render("ctx ") + barStr + " " +
		barStyle(pct).Render(pctStr(pct)) + " " +
		stDim.Render(fmtTok(sv.stats.ContextNow)+"/"+fmtTok(win))
	ctxR := stDim.Render("out ") + stFg.Render(fmtTok(sv.stats.OutputTokens)) + "  " +
		lipgloss.NewStyle().Foreground(cYellow).Render("~"+money(sv.stats.EstCost(transcript.PricingFor(sv.stats.Model))))
	line2 := spread(ctxL, ctxR, w)

	meta := []string{}
	if sv.cwd != "" {
		meta = append(meta, sv.cwd)
	}
	if sv.gitBranch != "" {
		meta = append(meta, "⎇ "+sv.gitBranch)
	}
	meta = append(meta, fmt.Sprintf("%d turns", sv.stats.Turns))
	line3 := stDim.Render(strings.Join(meta, "  ·  "))

	sep := stGuide.Render(strings.Repeat("─", w))
	return strings.Join([]string{clip(line1, w), clip(line2, w), clip(line3, w), sep}, "\n")
}

func (sv *sessionView) footer() string {
	if sv.status != "" {
		return clip(lipgloss.NewStyle().Foreground(cGreen).Render(sv.status), sv.width)
	}
	return clip(stDim.Render("↑/↓ move · space fold · enter read · e export · g/G ends · q quit"), sv.width)
}

func (sv *sessionView) view() string {
	if sv.reading {
		mode, hint := "", "↑/↓ scroll · esc back"
		if sv.readNode != nil && sv.readNode.Raw != "" {
			if sv.readRaw {
				mode = lipgloss.NewStyle().Foreground(cYellow).Render("  [raw]")
			} else {
				mode = lipgloss.NewStyle().Foreground(cGreen).Render("  [parsed]")
			}
			hint = "↑/↓ scroll · t raw/parsed · esc back"
		}
		header := clip(stTitle.Render("❖ "+sv.readerHeader)+mode, sv.width)
		help := clip(stDim.Render(hint), sv.width)
		return header + "\n" + sv.reader.View() + "\n" + help
	}
	header := sv.header()
	footer := sv.footer()
	treeH := sv.height - lipgloss.Height(header) - 1
	if treeH < 1 {
		treeH = 1
	}
	body := "no session data"
	if sv.tree != nil {
		body = sv.tree.view(treeH, sv.width)
	}
	return header + "\n" + body + "\n" + footer
}
