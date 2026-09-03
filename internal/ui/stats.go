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

// rangeAgg is everything the view needs for the selected date range. Each
// session contributes only the days that fall inside the range, so a session
// spanning weeks lands its tokens on the days they were actually spent.
type rangeAgg struct {
	tok      transcript.Tokens
	cost     float64
	models   transcript.ModelTokens
	proj     map[string]*statAgg
	attr     map[string]transcript.ModelTokens // attribution key -> per-model usage
	raw      transcript.ModelTokens            // undeduplicated (what /usage shows)
	perDay   map[string]int                    // deduplicated tokens per UTC day
	rawTotal int                               // undeduplicated total
	sessions int
	files    int    // transcripts on disk, including ones with no billable usage
	firstDay string // earliest UTC day any surviving transcript covers
}

// cutoff is the first UTC day included in the range. Claude Code uses
// today-N+1, so "7d" means today plus the previous six days.
func (m statsModel) cutoff() string {
	if m.rangeDays <= 0 {
		return ""
	}
	return time.Now().UTC().AddDate(0, 0, -(m.rangeDays - 1)).Format("2006-01-02")
}

// inRange compares YYYY-MM-DD day keys, for which string order is date order.
func (m statsModel) inRange(day string) bool {
	c := m.cutoff()
	return c == "" || day >= c
}

func (m statsModel) aggregate() rangeAgg {
	a := rangeAgg{
		models: transcript.ModelTokens{},
		proj:   map[string]*statAgg{},
		attr:   map[string]transcript.ModelTokens{},
		raw:    transcript.ModelTokens{},
		perDay: map[string]int{},
	}
	for _, s := range m.sessions {
		a.files++
		var st transcript.Tokens
		sm := transcript.ModelTokens{}
		for d := range s.Daily {
			if a.firstDay == "" || d < a.firstDay {
				a.firstDay = d
			}
		}
		for d, mt := range s.Daily {
			if !m.inRange(d) {
				continue
			}
			for model, t := range mt {
				st.Add(t)
				sm.Add(model, t)
				a.perDay[d] += t.Total()
			}
		}
		if st.Requests == 0 {
			continue // no activity inside the range
		}
		a.sessions++
		a.tok.Add(st)
		a.models.Merge(sm)
		cost := sm.Cost()
		a.cost += cost

		pa := a.proj[s.Project]
		if pa == nil {
			pa = &statAgg{}
			a.proj[s.Project] = pa
		}
		pa.tok += st.Total()
		pa.cost += cost
		pa.n++

		for d, mt := range s.Rows {
			if !m.inRange(d) {
				continue
			}
			a.raw.Merge(mt)
			a.rawTotal += mt.Total().Total()
		}
		for d, byKey := range s.Attr {
			if !m.inRange(d) {
				continue
			}
			for key, mt := range byKey {
				if a.attr[key] == nil {
					a.attr[key] = transcript.ModelTokens{}
				}
				a.attr[key].Merge(mt)
			}
		}
	}
	return a
}

func (m statsModel) content(w int) string {
	a := m.aggregate()
	var b strings.Builder
	b.WriteString(rangeTabs(m.rangeDays) + "\n\n")

	b.WriteString(sectionTitle("Overview") + "\n")
	b.WriteString("  " + stDim.Render("sessions ") + stFg.Render(itoa(a.sessions)) +
		stDim.Render("    projects ") + stFg.Render(itoa(len(a.proj))) +
		stDim.Render("    requests ") + stFg.Render(count(a.tok.Requests)) + "\n")
	b.WriteString("  " + stDim.Render("tokens   ") + accent(cCyan, fmtTok(a.tok.Total())) +
		stDim.Render("  ("+fmtTok(a.tok.Input)+" in · "+fmtTok(a.tok.Output)+" out · "+fmtTok(a.tok.Cache())+" cache)") + "\n")
	b.WriteString("  " + stDim.Render("est cost ") + accent(cYellow, "~"+money(a.cost)) +
		stDim.Render("  "+priceNote()) + "\n")
	b.WriteString(usageNote(a, w))
	b.WriteString(m.coverageNote(a))
	b.WriteString("\n")

	b.WriteString(sectionTitle("Token composition") + "\n  ")
	b.WriteString(compositionBar(a.tok, max(10, w-4)) + "\n")
	b.WriteString(compLegend(a.tok) + "\n\n")

	sparkDays := m.rangeDays
	if sparkDays == 0 {
		sparkDays = 90
	}
	b.WriteString(sectionTitle(fmt.Sprintf("Tokens per day, last %d days", sparkDays)) + "\n  ")
	b.WriteString(spark(a.perDay, sparkDays) + "\n\n")

	b.WriteString(sectionTitle("Where the tokens go") + "\n")
	b.WriteString(attrBars(a.attr, transcript.AttrSurface, w, 4, a.tok.Total()))
	b.WriteString("\n")

	for _, sec := range []struct{ dim, title string }{
		{transcript.AttrAgent, "Subagents"},
		{transcript.AttrSkill, "Skills"},
		{transcript.AttrMCP, "MCP servers"},
		{transcript.AttrTool, "MCP tools"},
	} {
		rows := attrBars(a.attr, sec.dim, w, 6, a.tok.Total())
		if rows == "" {
			continue
		}
		b.WriteString(sectionTitle(sec.title) + "\n" + rows + "\n")
	}
	if hasAnyAttr(a.attr) {
		b.WriteString("  " + stDim.Render("overlapping characteristics, not a partition — a skill that calls an") + "\n")
		b.WriteString("  " + stDim.Render("MCP tool counts under both") + "\n\n")
	}

	b.WriteString(sectionTitle("Top projects by tokens") + "\n")
	b.WriteString(topProjects(a.proj, w))
	b.WriteString("\n")

	b.WriteString(sectionTitle("Models by tokens") + "\n")
	b.WriteString(modelBars(a.models, w))

	if m.rangeDays == 0 {
		if lt := transcript.LoadLifetime(); lt != nil {
			b.WriteString("\n" + lifetimeSection(lt, a, w))
		}
	}
	return b.String()
}

// lifetimeSection reports Claude Code's cumulative record, which still counts
// the sessions whose transcripts it has deleted. Its figures are
// undeduplicated, so a corrected estimate is shown beside them.
func lifetimeSection(lt *transcript.Lifetime, a rangeAgg, w int) string {
	est := lt.Deflate(a.raw, a.models)
	rawTot, estTot := lt.Raw.Total(), est.Total()

	var b strings.Builder
	b.WriteString(sectionTitle("Lifetime — includes deleted sessions") + "\n")
	line := "  " + stDim.Render("sessions ") + stFg.Render(count(lt.Sessions))
	if lt.FirstDay != "" {
		line += stDim.Render("    since ") + stFg.Render(lt.FirstDay)
	}
	if lt.Through != "" {
		line += stDim.Render("    through ") + stFg.Render(lt.Through)
	}
	b.WriteString(line + "\n")
	b.WriteString("  " + stDim.Render("tokens   ") + accent(cCyan, fmtTok(estTot.Total())) +
		stDim.Render(" estimated  ·  ") + fmtTok(rawTot.Total()) + stDim.Render(" as Claude Code counted it") + "\n")
	b.WriteString("  " + stDim.Render("list px  ") + accent(cYellow, "~"+money(est.Cost())) +
		stDim.Render("  ·  ~") + money(lt.Raw.Cost()) + stDim.Render(" at its own counts") + "\n")
	b.WriteString("  " + stDim.Render("ⓘ read from Claude Code's cumulative cache, the only record left of") + "\n")
	b.WriteString("  " + stDim.Render("  deleted sessions. It counts each API response once per transcript") + "\n")
	b.WriteString("  " + stDim.Render("  row, so the estimate rescales it per model by the ratio measured") + "\n")
	b.WriteString("  " + stDim.Render("  on surviving transcripts — good only if deleted sessions looked") + "\n")
	b.WriteString("  " + stDim.Render("  like the ones still here.") + "\n\n")
	b.WriteString("  " + stDim.Render("estimated lifetime by model") + "\n")
	b.WriteString(modelBars(est, w))
	return b.String()
}

// usageNote reconciles ccpane's figure with Claude Code's /usage Stats tab,
// which adds each API response once per transcript row rather than once.
func usageNote(a rangeAgg, w int) string {
	if a.rawTotal <= 0 || a.tok.Total() <= 0 || a.rawTotal <= a.tok.Total() {
		return ""
	}
	ratio := float64(a.rawTotal) / float64(a.tok.Total())
	return "  " + stDim.Render(fmt.Sprintf("ⓘ /usage → Stats reads %s here (%.2f×): it counts each API response",
		fmtTok(a.rawTotal), ratio)) + "\n" +
		"  " + stDim.Render("  once per transcript row. ccpane counts it once, matching /cost.") + "\n"
}

// coverageNote states how far back the surviving transcripts actually reach.
// Claude Code deletes transcripts after cleanupPeriodDays (30 by default) while
// its own counters keep counting the deleted ones, so "all time" here is not
// the same all time /usage reports, and a range reaching past the oldest
// surviving transcript is necessarily incomplete.
func (m statsModel) coverageNote(a rangeAgg) string {
	if a.firstDay == "" {
		return ""
	}
	cut := m.cutoff()
	if cut != "" && a.firstDay <= cut {
		return "" // range is fully covered by transcripts still on disk
	}
	days := 1
	if t, err := time.Parse("2006-01-02", a.firstDay); err == nil {
		days = int(time.Since(t).Hours()/24) + 1
	}
	return "  " + stDim.Render(fmt.Sprintf("ⓘ covers %d transcripts on disk, back to %s (%d days). Claude Code",
		a.files, a.firstDay, days)) + "\n" +
		"  " + stDim.Render("  deletes transcripts after cleanupPeriodDays (default 30); its own") + "\n" +
		"  " + stDim.Render("  session and token counters keep counting the deleted ones.") + "\n"
}

func hasAnyAttr(attr map[string]transcript.ModelTokens) bool {
	for k := range attr {
		if dim, _ := transcript.SplitAttrKey(k); dim != transcript.AttrSurface {
			return true
		}
	}
	return false
}

// attrBars renders the top n entries of one attribution dimension, as a share
// of the range's total tokens.
func attrBars(attr map[string]transcript.ModelTokens, dim string, w, n, total int) string {
	type row struct {
		name string
		tok  int
		cost float64
	}
	var rows []row
	for k, mt := range attr {
		d, name := transcript.SplitAttrKey(k)
		if d != dim {
			continue
		}
		t := mt.Total()
		rows = append(rows, row{name, t.Total(), mt.Cost()})
	}
	if len(rows) == 0 {
		return ""
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].tok > rows[j].tok })
	if len(rows) > n {
		rows = rows[:n]
	}
	if total <= 0 {
		total = 1
	}
	nameW := 24
	barW := max(10, w-nameW-30) // leave room for pct, tokens and cost
	var b strings.Builder
	for _, r := range rows {
		share := float64(r.tok) / float64(total)
		b.WriteString("  " + stFg.Render(padR(truncate(r.name, nameW), nameW)) + " " +
			accent(cAccent, bar(share, barW)) + " " +
			stDim.Render(fmt.Sprintf("%4.0f%%", share*100)+"  "+padL(fmtTok(r.tok), 6)+"  "+money(r.cost)) + "\n")
	}
	return b.String()
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

func compositionBar(t transcript.Tokens, width int) string {
	total := t.Total()
	if total <= 0 || width <= 0 {
		return stDim.Render("(no data)")
	}
	vals := []int{t.Input, t.Output, t.Cache()}
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

func compLegend(t transcript.Tokens) string {
	total := t.Total()
	if total == 0 {
		total = 1
	}
	pct := func(v int) string { return fmt.Sprintf("%.1f%%", float64(v)/float64(total)*100) }
	return "  " +
		accent(cAccent, "█") + stDim.Render(" input "+fmtTok(t.Input)+" "+pct(t.Input)+"   ") +
		accent(cGreen, "█") + stDim.Render(" output "+fmtTok(t.Output)+" "+pct(t.Output)+"   ") +
		accent(cDim, "█") + stDim.Render(" cache "+fmtTok(t.Cache())+" "+pct(t.Cache()))
}

// priceNote labels the cost figure. It is list price for the tokens used, per
// model — on a Claude subscription that is emphatically not what you paid, so
// the label says so rather than implying a bill.
func priceNote() string {
	if transcript.PricingLoaded() {
		return "(list price, per model — not your bill)"
	}
	return "(list-price estimate — not your bill)"
}

// spark plots tokens per day over the trailing window.
func spark(day map[string]int, days int) string {
	if days < 1 {
		days = 1
	}
	runes := []rune(" ▁▂▃▄▅▆▇█")
	vals := make([]int, days)
	maxv := 1
	now := time.Now().UTC()
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
	return accent(cGreen, b.String()) + stDim.Render("  peak "+fmtTok(maxv)+"/day")
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

func modelBars(model transcript.ModelTokens, w int) string {
	type kv struct {
		name string
		tok  int
		cost float64
	}
	var arr []kv
	total := 0
	for k, t := range model {
		arr = append(arr, kv{k, t.Total(), t.Cost(transcript.PricingFor(k))})
		total += t.Total()
	}
	if total == 0 {
		return "  " + stDim.Render("(no model data)") + "\n"
	}
	sort.Slice(arr, func(i, j int) bool { return arr[i].tok > arr[j].tok })
	maxTok := arr[0].tok
	nameW := 22
	barW := max(10, w-nameW-30) // leave room for pct, tokens and cost
	var b strings.Builder
	for _, x := range arr {
		pct := float64(x.tok) / float64(total) * 100
		b.WriteString("  " + stFg.Render(padR(shortModelSafe(x.name), nameW)) + " " +
			accent(cGreen, bar(float64(x.tok)/float64(maxTok), barW)) + " " +
			stDim.Render(fmt.Sprintf("%4.0f%%", pct)+"  "+padL(fmtTok(x.tok), 6)+"  "+money(x.cost)) + "\n")
	}
	return b.String()
}

// RunStats launches the stats view standalone.
func RunStats(limit int) error {
	_, err := tea.NewProgram(newStatsModel(limit, 0, 0, true), tea.WithAltScreen()).Run()
	return err
}
