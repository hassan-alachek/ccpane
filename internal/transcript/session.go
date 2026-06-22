package transcript

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

// Session is summary metadata for one transcript, used by the browser.
type Session struct {
	Path          string `json:"path"`
	SessionID     string `json:"sessionId"`
	Project       string `json:"project"` // real cwd
	Title         string `json:"title"`
	GitBranch     string `json:"gitBranch"`
	FirstTS       string `json:"firstTs"`
	LastTS        string `json:"lastTs"`
	Messages      int    `json:"messages"`
	InputTokens   int    `json:"inputTokens"`
	OutputTokens  int    `json:"outputTokens"`
	CacheCreation int    `json:"cacheCreation"`
	CacheRead     int    `json:"cacheRead"`
	Model         string `json:"model"`
	Mtime         int64  `json:"mtime"`
	Size          int64  `json:"size"`
}

// TotalTokens is all token activity for the session.
func (s *Session) TotalTokens() int {
	return s.InputTokens + s.OutputTokens + s.CacheCreation + s.CacheRead
}

// Cost is the estimated USD cost using DefaultPricing.
func (s *Session) Cost() float64 {
	return Stats{
		InputTokens:   s.InputTokens,
		OutputTokens:  s.OutputTokens,
		CacheCreation: s.CacheCreation,
		CacheRead:     s.CacheRead,
	}.EstCost(DefaultPricing)
}

// Group is a set of sessions sharing a project directory.
type Group struct {
	Project  string
	Sessions []*Session
}

// Cost is the total estimated cost across the group.
func (g Group) Cost() float64 {
	var c float64
	for _, s := range g.Sessions {
		c += s.Cost()
	}
	return c
}

// GroupByProject buckets sessions by their project directory, preserving the
// input order so groups appear by most-recent activity when the input is
// sorted newest-first.
func GroupByProject(sessions []*Session) []Group {
	order := []string{}
	byProj := map[string][]*Session{}
	for _, s := range sessions {
		if _, ok := byProj[s.Project]; !ok {
			order = append(order, s.Project)
		}
		byProj[s.Project] = append(byProj[s.Project], s)
	}
	groups := make([]Group, 0, len(order))
	for _, p := range order {
		groups = append(groups, Group{Project: p, Sessions: byProj[p]})
	}
	return groups
}

// ScanSession parses a transcript and derives its summary metadata.
func ScanSession(path string) *Session {
	recs, err := ParseFile(path)
	if err != nil || len(recs) == 0 {
		return nil
	}
	st := Aggregate(recs)
	s := &Session{
		Path:          path,
		InputTokens:   st.InputTokens,
		OutputTokens:  st.OutputTokens,
		CacheCreation: st.CacheCreation,
		CacheRead:     st.CacheRead,
		Model:         st.Model,
	}
	for _, r := range recs {
		if r.SessionID != "" && s.SessionID == "" {
			s.SessionID = r.SessionID
		}
		if r.Cwd != "" && s.Project == "" {
			s.Project = r.Cwd
		}
		if r.GitBranch != "" && s.GitBranch == "" {
			s.GitBranch = r.GitBranch
		}
		if r.Timestamp != "" {
			if s.FirstTS == "" {
				s.FirstTS = r.Timestamp
			}
			s.LastTS = r.Timestamp
		}
		if r.Message != nil && (r.Type == "user" || r.Type == "assistant") {
			s.Messages++
		}
	}
	s.Title = SessionTitle(recs)
	return s
}

// IndexSessions returns metadata for every session on the machine, using an
// on-disk cache keyed by (mtime,size). Sorted newest-first.
func IndexSessions() []*Session {
	paths := AllTranscripts()
	cache := loadCache()
	out := make([]*Session, 0, len(paths))
	live := make(map[string]bool, len(paths))
	changed := false

	for _, p := range paths {
		live[p] = true
		fi, err := os.Stat(p)
		if err != nil {
			continue
		}
		if c, ok := cache[p]; ok && c.Mtime == fi.ModTime().UnixNano() && c.Size == fi.Size() {
			out = append(out, c)
			continue
		}
		s := ScanSession(p)
		if s == nil {
			continue
		}
		s.Mtime = fi.ModTime().UnixNano()
		s.Size = fi.Size()
		cache[p] = s
		out = append(out, s)
		changed = true
	}
	for p := range cache {
		if !live[p] {
			delete(cache, p)
			changed = true
		}
	}
	if changed {
		saveCache(cache)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Mtime > out[j].Mtime })
	return out
}

// cachePath is versioned so schema changes invalidate stale caches cleanly.
func cachePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "ccpane-index.v3.json")
}

func loadCache() map[string]*Session {
	m := map[string]*Session{}
	if b, err := os.ReadFile(cachePath()); err == nil {
		json.Unmarshal(b, &m)
	}
	return m
}

func saveCache(m map[string]*Session) {
	if b, err := json.Marshal(m); err == nil {
		os.WriteFile(cachePath(), b, 0o644)
	}
}
