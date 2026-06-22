package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/hassan-alachek/ccpane/internal/transcript"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type statAgg struct {
	tok  int
	cost float64
	n    int
}

type statsModel struct {
	sessions   []*transcript.Session
	vp         viewport.Model
	ready      bool
	rangeDays  int // 0 = all time
	limit      int
	width      int
	height     int
	standalone bool
}

func newStatsModel(limit, w, h int, standalone bool) statsModel {
	m := statsModel{sessions: transcript.IndexSessions(), limit: limit, width: w, height: h, standalone: standalone}
	if w > 0 && h > 0 {
		m.layout()
	}
	return m
}

func (m *statsModel) layout() {
	top := m.vp.YOffset
	m.vp = viewport.New(max(1, m.width), max(1, m.height-2))
	m.vp.SetContent(m.content(max(40, m.width)))
	m.vp.SetYOffset(top)
	m.ready = true
}

func (m statsModel) Init() tea.Cmd { return nil }

func (m statsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		(&m).layout()
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "q", "esc":
			if m.standalone {
				return m, tea.Quit
			}
			b := newBrowserModel(m.limit)
			b.width, b.height = m.width, m.height
			return b, nil
		case "1":
			m.rangeDays = 7
			(&m).vp.SetYOffset(0)
			(&m).layout()
		case "2":
			m.rangeDays = 30
			(&m).vp.SetYOffset(0)
			(&m).layout()
		case "3":
			m.rangeDays = 60
			(&m).vp.SetYOffset(0)
			(&m).layout()
		case "4", "0":
			m.rangeDays = 0
			(&m).vp.SetYOffset(0)
			(&m).layout()
		case "tab":
			m.rangeDays = nextRange(m.rangeDays)
			(&m).vp.SetYOffset(0)
			(&m).layout()
		default:
			var cmd tea.Cmd
			m.vp, cmd = m.vp.Update(msg)
			return m, cmd
		}
	}
	return m, nil
}

func nextRange(d int) int {
	switch d {
	case 7:
		return 30
	case 30:
		return 60
	case 60:
		return 0
	default:
		return 7
	}
}

func (m statsModel) View() string {
	if !m.ready {
		return "loading…"
	}
	title := clip(stTitle.Render("❖ ccpane — usage stats & graphs"), m.width)
	word := "back"
	if m.standalone {
		word = "quit"
	}
	help := clip(stDim.Render("1 7d · 2 30d · 3 60d · 4 all · tab cycle · ↑/↓ scroll · q "+word), m.width)
	return title + "\n" + m.vp.View() + "\n" + help
}

func (m statsModel) content(w int) string {
	var in, out, cache, n int
	var cost float64
	proj := map[string]*statAgg{}
	model := map[string]int{}
	day := map[string]int{}

	for _, s := range m.sessions {
		if !m.inRange(s.LastTS) {
			continue
		}
		n++
		in += s.InputTokens
		out += s.OutputTokens
		cache += s.CacheCreation + s.CacheRead
		cost += s.Cost()
		a := proj[s.Project]
		if a == nil {
			a = &statAgg{}
			proj[s.Project] = a
		}
		a.tok += s.TotalTokens()
		a.cost += s.Cost()
		a.n++
		if s.Model != "" {
			model[s.Model] += s.TotalTokens()
		}
		if d := dayKey(s.LastTS); d != "" {
			day[d]++
		}
	}
	total := in + out + cache

	var b strings.Builder
	b.WriteString(rangeTabs(m.rangeDays) + "\n\n")

	b.WriteString(sectionTitle("Overview") + "\n")
	b.WriteString("  " + stDim.Render("sessions ") + stFg.Render(itoa(n)) +
		stDim.Render("    projects ") + stFg.Render(itoa(len(proj))) + "\n")
	b.WriteString("  " + stDim.Render("tokens   ") + accent(cCyan, fmtTok(total)) +
		stDim.Render("  ("+fmtTok(in)+" in · "+fmtTok(out)+" out · "+fmtTok(cache)+" cache)") + "\n")
	b.WriteString("  " + stDim.Render("est cost ") + accent(cYellow, "~"+money(cost)) +
		stDim.Render("  "+priceNote()) + "\n\n")

	b.WriteString(sectionTitle("Token composition") + "\n  ")
	b.WriteString(compositionBar(in, out, cache, max(10, w-4)) + "\n")
	b.WriteString(compLegend(in, out, cache) + "\n\n")

	sparkDays := m.rangeDays
	if sparkDays == 0 {
		sparkDays = 90
	}
	b.WriteString(sectionTitle(fmt.Sprintf("Activity — sessions/day, last %d days", sparkDays)) + "\n  ")
	b.WriteString(spark(day, sparkDays) + "\n\n")

	b.WriteString(sectionTitle("Top projects by tokens") + "\n")
	b.WriteString(topProjects(proj, w))
	b.WriteString("\n")

	b.WriteString(sectionTitle("Models by tokens") + "\n")
	b.WriteString(modelBars(model, w))
	return b.String()
}

func (m statsModel) inRange(ts string) bool {
	if m.rangeDays <= 0 {
		return true
	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return false
	}
	return t.After(time.Now().AddDate(0, 0, -m.rangeDays))
}

func rangeTabs(active int) string {
	items := []struct {
		label string
		days  int
	}{{"7d", 7}, {"30d", 30}, {"60d", 60}, {"all time", 0}}
	parts := make([]string, 0, len(items))
	for _, it := range items {
		if it.days == active {
			parts = append(parts, lipgloss.NewStyle().Foreground(cFg).Background(cSelBg).Bold(true).Render(" "+it.label+" "))
		} else {
			parts = append(parts, stDim.Render(" "+it.label+" "))
		}
	}
	return stDim.Render("range  ") + strings.Join(parts, " ")
}

func sectionTitle(s string) string {
	return lipgloss.NewStyle().Foreground(cMagenta).Bold(true).Render("▎ " + s)
}

func accent(c lipgloss.Color, s string) string { return lipgloss.NewStyle().Foreground(c).Render(s) }

func compositionBar(in, out, cache, width int) string {
	total := in + out + cache
	if total <= 0 || width <= 0 {
		return stDim.Render("(no data)")
	}
	vals := []int{in, out, cache}
	cols := []lipgloss.Color{cAccent, cGreen, cDim}
	w := make([]int, 3)
	used := 0
	for i, v := range vals {
		w[i] = v * width / total
		if v > 0 && w[i] == 0 {
			w[i] = 1 // keep tiny segments visible
		}
		used += w[i]
	}
	for used > width { // trim from the largest segment
		bi := 0
		for i := range w {
			if w[i] > w[bi] {
				bi = i
			}
		}
		if w[bi] <= 1 {
			break
		}
		w[bi]--
		used--
	}
	for used < width { // pad the largest segment
		bi := 0
		for i := range w {
			if w[i] > w[bi] {
				bi = i
			}
		}
		w[bi]++
		used++
	}
	var b strings.Builder
	for i := range vals {
		b.WriteString(accent(cols[i], strings.Repeat("█", w[i])))
	}
	return b.String()
}

func compLegend(in, out, cache int) string {
	total := in + out + cache
	if total == 0 {
		total = 1
	}
	pct := func(v int) string { return fmt.Sprintf("%.1f%%", float64(v)/float64(total)*100) }
	return "  " +
		accent(cAccent, "█") + stDim.Render(" input "+fmtTok(in)+" "+pct(in)+"   ") +
		accent(cGreen, "█") + stDim.Render(" output "+fmtTok(out)+" "+pct(out)+"   ") +
		accent(cDim, "█") + stDim.Render(" cache "+fmtTok(cache)+" "+pct(cache))
}

func priceNote() string {
	if transcript.PricingLoaded() {
		return "(LiteLLM pricing)"
	}
	return "(estimate)"
}

func spark(day map[string]int, days int) string {
	if days < 1 {
		days = 1
	}
	runes := []rune(" ▁▂▃▄▅▆▇█")
	vals := make([]int, days)
	maxv := 1
	now := time.Now()
	for i := 0; i < days; i++ {
		d := now.AddDate(0, 0, -(days - 1 - i)).Format("2006-01-02")
		vals[i] = day[d]
		if vals[i] > maxv {
			maxv = vals[i]
		}
	}
	var b strings.Builder
	for _, v := range vals {
		idx := 0
		if v > 0 {
			idx = 1 + v*(len(runes)-2)/maxv
			if idx > len(runes)-1 {
				idx = len(runes) - 1
			}
		}
		b.WriteRune(runes[idx])
	}
	return accent(cGreen, b.String()) + stDim.Render(fmt.Sprintf("  peak %d/day", maxv))
}

func topProjects(proj map[string]*statAgg, w int) string {
	type kv struct {
		name string
		a    *statAgg
	}
	arr := make([]kv, 0, len(proj))
	for k, v := range proj {
		arr = append(arr, kv{k, v})
	}
	sort.Slice(arr, func(i, j int) bool { return arr[i].a.tok > arr[j].a.tok })
	if len(arr) > 8 {
		arr = arr[:8]
	}
	if len(arr) == 0 {
		return "  " + stDim.Render("(no sessions in range)") + "\n"
	}
	maxTok := 1
	for _, x := range arr {
		if x.a.tok > maxTok {
			maxTok = x.a.tok
		}
	}
	nameW := 22
	barW := max(10, w-nameW-22)
	var b strings.Builder
	for _, x := range arr {
		b.WriteString("  " + stFg.Render(padR(projectName(x.name), nameW)) + " " +
			accent(cAccent, bar(float64(x.a.tok)/float64(maxTok), barW)) + " " +
			stDim.Render(padL(fmtTok(x.a.tok), 6)+"  "+money(x.a.cost)) + "\n")
	}
	return b.String()
}

func modelBars(model map[string]int, w int) string {
	total := 0
	for _, v := range model {
		total += v
	}
	if total == 0 {
		return "  " + stDim.Render("(no model data)") + "\n"
	}
	type kv struct {
		name string
		tok  int
	}
	arr := make([]kv, 0, len(model))
	for k, v := range model {
		arr = append(arr, kv{k, v})
	}
	sort.Slice(arr, func(i, j int) bool { return arr[i].tok > arr[j].tok })
	maxTok := arr[0].tok
	nameW := 22
	barW := max(10, w-nameW-14)
	var b strings.Builder
	for _, x := range arr {
		pct := float64(x.tok) / float64(total) * 100
		b.WriteString("  " + stFg.Render(padR(shortModelSafe(x.name), nameW)) + " " +
			accent(cGreen, bar(float64(x.tok)/float64(maxTok), barW)) + " " +
			stDim.Render(fmt.Sprintf("%4.0f%%", pct)) + "\n")
	}
	return b.String()
}

func dayKey(ts string) string {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ""
	}
	return t.Format("2006-01-02")
}

// RunStats launches the stats view standalone.
func RunStats(limit int) error {
	_, err := tea.NewProgram(newStatsModel(limit, 0, 0, true), tea.WithAltScreen()).Run()
	return err
}
