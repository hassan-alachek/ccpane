package ui

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

func fmtTok(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1_000:
		return strconv.Itoa(n/1000) + "k"
	default:
		return strconv.Itoa(n)
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

func money(v float64) string { return fmt.Sprintf("$%.2f", v) }

// truncate shortens s to w runes with an ellipsis (plain text only).
func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= w {
		return s
	}
	if w == 1 {
		return "…"
	}
	return string(r[:w-1]) + "…"
}

func padR(s string, w int) string {
	s = truncate(s, w)
	if n := len([]rune(s)); n < w {
		s += strings.Repeat(" ", w-n)
	}
	return s
}

func padL(s string, w int) string {
	s = truncate(s, w)
	if n := len([]rune(s)); n < w {
		s = strings.Repeat(" ", w-n) + s
	}
	return s
}

// clip truncates a possibly-styled string to w display cells (ANSI-aware).
func clip(s string, w int) string {
	if w <= 0 {
		return ""
	}
	return lipgloss.NewStyle().MaxWidth(w).Render(s)
}

// spread places left and right on one line with filler between, fit to width.
func spread(left, right string, width int) string {
	lw, rw := lipgloss.Width(left), lipgloss.Width(right)
	gap := width - lw - rw
	if gap < 1 {
		left = clip(left, max(0, width-rw-1))
		gap = 1
		if width-rw-1 <= 0 {
			return clip(right, width)
		}
	}
	return left + strings.Repeat(" ", gap) + right
}

func withBg(st lipgloss.Style, sel bool) lipgloss.Style {
	if sel {
		return st.Background(cSelBg)
	}
	return st
}

func bar(pct float64, w int) string {
	if pct < 0 {
		pct = 0
	}
	if pct > 1 {
		pct = 1
	}
	fill := int(pct*float64(w) + 0.5)
	if fill > w {
		fill = w
	}
	return strings.Repeat("█", fill) + strings.Repeat("░", w-fill)
}

func barStyle(pct float64) lipgloss.Style {
	switch {
	case pct < 0.6:
		return lipgloss.NewStyle().Foreground(cGreen)
	case pct < 0.85:
		return lipgloss.NewStyle().Foreground(cYellow)
	default:
		return lipgloss.NewStyle().Foreground(cRed)
	}
}

func pctStr(pct float64) string { return fmt.Sprintf("%.0f%%", pct*100) }

func shortModelSafe(m string) string {
	if m == "" {
		return "—"
	}
	return strings.TrimPrefix(m, "claude-")
}

// projectName renders a cwd as its last two path segments.
func projectName(p string) string {
	if p == "" {
		return "—"
	}
	base := filepath.Base(p)
	parent := filepath.Base(filepath.Dir(p))
	if parent != "" && parent != "." && parent != string(filepath.Separator) {
		return parent + "/" + base
	}
	return base
}

func relTime(ts string) string {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
