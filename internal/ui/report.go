package ui

import (
	"fmt"

	"github.com/hassan-alachek/ccpane/internal/transcript"
)

// PrintSessionStat prints a session summary and tree as plain text (no TUI).
func PrintSessionStat(path string, limit int) {
	recs, err := transcript.ParseFile(path)
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	st := transcript.Aggregate(recs)
	win := limit
	if win <= 0 {
		win = transcript.AutoWindow(st.MaxContext)
	}
	pct := 0.0
	if win > 0 {
		pct = float64(st.ContextNow) / float64(win) * 100
	}
	fmt.Printf("Session : %s\n", transcript.SessionTitle(recs))
	fmt.Printf("Path    : %s\n", path)
	fmt.Printf("Cwd     : %s\n", transcript.SessionCwd(recs))
	fmt.Printf("Branch  : %s\n", transcript.SessionGitBranch(recs))
	fmt.Printf("Model   : %s\n", st.Model)
	fmt.Printf("Context : %d / %d tokens (%.0f%%)  [window auto]\n", st.ContextNow, win, pct)
	fmt.Printf("Tokens  : in %d  out %d  cacheW %d  cacheR %d\n", st.InputTokens, st.OutputTokens, st.CacheCreation, st.CacheRead)
	fmt.Printf("Turns   : %d   est cost ~$%.2f\n\n", st.Turns, st.EstCost(transcript.PricingFor(st.Model)))

	for _, line := range transcript.RenderTreeLines(transcript.FullTree(path)) {
		fmt.Println(line)
	}
}

// PrintBrowserStat prints the all-sessions index grouped by directory.
func PrintBrowserStat(limit int) {
	sessions := transcript.IndexSessions()
	groups := transcript.GroupByProject(sessions)
	fmt.Printf("%d sessions across %d projects\n", len(sessions), len(groups))
	for _, g := range groups {
		proj := g.Project
		if proj == "" {
			proj = "(unknown)"
		}
		fmt.Printf("\n▾ %s   (%d · ~$%.2f)\n", proj, len(g.Sessions), g.Cost())
		for _, s := range g.Sessions {
			fmt.Printf("    %-46s %5d msg  %8s  $%6.2f  %s\n",
				truncate(s.Title, 46), s.Messages, fmtTok(s.TotalTokens()), s.Cost(), relTime(s.LastTS))
		}
	}
}
