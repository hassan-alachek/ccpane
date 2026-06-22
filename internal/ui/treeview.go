package ui

import (
	"strings"

	"github.com/hassan-alachek/ccpane/internal/transcript"

	"github.com/charmbracelet/lipgloss"
)

type row struct {
	node   *transcript.Node
	prefix string // ancestor guide columns (│ / spaces)
	branch string // this node's connector (├─ / └─)
}

// tree is a scrollable, collapsible view over a node forest with a cursor and
// box-drawing connectors.
type tree struct {
	roots  []*transcript.Node
	rows   []row
	cursor int
	offset int
	width  int
	height int
}

func newTree(roots []*transcript.Node) *tree {
	t := &tree{roots: roots}
	t.reflow()
	return t
}

func (t *tree) setRoots(roots []*transcript.Node) {
	t.roots = roots
	t.reflow()
	if t.cursor >= len(t.rows) {
		t.cursor = len(t.rows) - 1
	}
	if t.cursor < 0 {
		t.cursor = 0
	}
}

func (t *tree) reflow() {
	t.rows = t.rows[:0]
	var walk func(ns []*transcript.Node, prefix string, depth int)
	walk = func(ns []*transcript.Node, prefix string, depth int) {
		for i, n := range ns {
			last := i == len(ns)-1
			branch := ""
			if depth > 0 {
				if last {
					branch = "└─ "
				} else {
					branch = "├─ "
				}
			}
			t.rows = append(t.rows, row{node: n, prefix: prefix, branch: branch})
			if n.Expanded && len(n.Children) > 0 {
				childPrefix := prefix
				if depth > 0 {
					if last {
						childPrefix = prefix + "   "
					} else {
						childPrefix = prefix + "│  "
					}
				}
				walk(n.Children, childPrefix, depth+1)
			}
		}
	}
	walk(t.roots, "", 0)
}

func (t *tree) up() {
	if t.cursor > 0 {
		t.cursor--
	}
}

func (t *tree) down() {
	if t.cursor < len(t.rows)-1 {
		t.cursor++
	}
}

func (t *tree) top()    { t.cursor = 0 }
func (t *tree) bottom() { t.cursor = len(t.rows) - 1 }

func (t *tree) current() *transcript.Node {
	if t.cursor < 0 || t.cursor >= len(t.rows) {
		return nil
	}
	return t.rows[t.cursor].node
}

func (t *tree) toggle() {
	if n := t.current(); n != nil && len(n.Children) > 0 {
		n.Expanded = !n.Expanded
		t.reflow()
	}
}

func (t *tree) expand() {
	if n := t.current(); n != nil && len(n.Children) > 0 && !n.Expanded {
		n.Expanded = true
		t.reflow()
	}
}

func (t *tree) collapse() {
	if n := t.current(); n != nil && len(n.Children) > 0 && n.Expanded {
		n.Expanded = false
		t.reflow()
	}
}

func (t *tree) clampOffset() {
	if len(t.rows) == 0 {
		t.cursor, t.offset = 0, 0
		return
	}
	if t.cursor < 0 {
		t.cursor = 0
	}
	if t.cursor >= len(t.rows) {
		t.cursor = len(t.rows) - 1
	}
	if t.height <= 0 {
		return
	}
	if t.cursor < t.offset {
		t.offset = t.cursor
	}
	if t.cursor >= t.offset+t.height {
		t.offset = t.cursor - t.height + 1
	}
	if t.offset < 0 {
		t.offset = 0
	}
}

// view renders exactly height lines (padding with blanks).
func (t *tree) view(height, width int) string {
	t.height, t.width = height, width
	t.clampOffset()

	var b strings.Builder
	printed := 0
	end := t.offset + height
	if end > len(t.rows) {
		end = len(t.rows)
	}
	for i := t.offset; i < end; i++ {
		if printed > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(t.renderRow(t.rows[i], i == t.cursor))
		printed++
	}
	for printed < height {
		if printed > 0 {
			b.WriteByte('\n')
		}
		printed++
	}
	return b.String()
}

func (t *tree) renderRow(r row, selected bool) string {
	n := r.node
	caret := "  "
	if len(n.Children) > 0 {
		if n.Expanded {
			caret = "▾ "
		} else {
			caret = "▸ "
		}
	}
	icon := n.Icon
	if icon == "" {
		icon = " "
	}

	var b strings.Builder
	b.WriteString(withBg(stGuide, selected).Render(r.prefix + r.branch + caret))
	b.WriteString(withBg(iconStyle(n), selected).Render(icon))
	if n.Label != "" {
		b.WriteString(withBg(labelStyle(n), selected).Render(" " + n.Label))
	}
	if n.Detail != "" {
		b.WriteString(withBg(stDim, selected).Render("  " + n.Detail))
	}
	if n.Tokens > 0 {
		b.WriteString(withBg(lipgloss.NewStyle().Foreground(cCyan), selected).Render("  +" + fmtTok(n.Tokens)))
	}

	line := b.String()
	if t.width > 0 {
		line = clip(line, t.width)
		if selected {
			if pad := t.width - lipgloss.Width(line); pad > 0 {
				line += lipgloss.NewStyle().Background(cSelBg).Render(strings.Repeat(" ", pad))
			}
		}
	}
	return line
}

func iconStyle(n *transcript.Node) lipgloss.Style {
	c := cFg
	switch n.Kind {
	case "user":
		c = cGreen
	case "assistant":
		c = cAccent
	case "subagent":
		c = cMagenta
	case "text":
		c = cDim
	case "tool":
		switch n.Icon {
		case transcript.IconToolErr:
			c = cRed
		case transcript.IconToolNone:
			c = cDim
		default:
			c = cGreen
		}
	}
	return lipgloss.NewStyle().Foreground(c)
}

func labelStyle(n *transcript.Node) lipgloss.Style {
	switch n.Kind {
	case "user":
		return lipgloss.NewStyle().Foreground(cFg)
	case "assistant":
		return lipgloss.NewStyle().Foreground(cDim)
	case "subagent":
		return lipgloss.NewStyle().Foreground(cMagenta).Bold(true)
	case "tool":
		return lipgloss.NewStyle().Foreground(cFg).Bold(true)
	case "text":
		return lipgloss.NewStyle().Foreground(cDim).Italic(true)
	}
	return lipgloss.NewStyle().Foreground(cFg)
}
