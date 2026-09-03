package transcript

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

// Session is summary metadata for one transcript, used by the browser. Its
// token counts cover the main transcript plus every subagent transcript the
// session spawned, with each API response counted once.
type Session struct {
	Path      string `json:"path"`
	SessionID string `json:"sessionId"`
	Project   string `json:"project"` // real cwd
	Title     string `json:"title"`
	GitBranch string `json:"gitBranch"`
	FirstTS   string `json:"firstTs"`
	LastTS    string `json:"lastTs"`
	Messages  int    `json:"messages"`
	Tokens           // deduplicated session totals

	// Daily is deduplicated usage bucketed by UTC day and model. Range views
	// filter on it, so a long session's tokens land on the days they were
	// actually spent instead of all on its last day.
	Daily map[string]ModelTokens `json:"daily"`
	// Rows is the undeduplicated per-day total: what Claude Code's /usage
	// Stats tab reports. Kept so the pane can show that difference.
	Rows map[string]int `json:"rows"`
	// Attr is deduplicated usage by UTC day, attribution key
	// ("agent:Explore", "skill:artifact-design", "mcp:datadog", ...) and model.
	Attr map[string]map[string]ModelTokens `json:"attr"`

	// RespIDs are the message ids of every API response counted here, used to
	// spot responses replayed into a forked or resumed session.
	RespIDs []string `json:"respIds"`
	// Excluded is how many ids an earlier session had already claimed when
	// this session was last scanned, so the index can tell a stale correction.
	Excluded int `json:"excluded"`

	Model string `json:"model"`
	Mtime int64  `json:"mtime"`
	Size  int64  `json:"size"`
}

// TotalTokens is all token activity for the session.
func (s *Session) TotalTokens() int { return s.Total() }

// ByModel folds Daily into per-model totals.
func (s *Session) ByModel() ModelTokens {
	m := ModelTokens{}
	for _, mt := range s.Daily {
		m.Merge(mt)
	}
	return m
}

// Cost is the estimated USD cost, priced per model.
func (s *Session) Cost() float64 { return s.ByModel().Cost() }

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

// ScanSession parses a transcript and derives its summary metadata. exclude
// may name responses already counted by an earlier session (replayed history in
// a fork or resume); pass nil to count everything the transcript holds.
func ScanSession(path string, exclude map[string]bool) *Session {
	recs, err := ParseFile(path)
	if err != nil || len(recs) == 0 {
		return nil
	}
	sink := NewSink()
	st := AggregateSession(path, recs, sink, exclude)
	s := &Session{
		Path:     path,
		Tokens:   st.Tokens,
		Daily:    sink.Daily,
		Rows:     sink.Rows,
		Attr:     sink.Attr,
		RespIDs:  sink.IDs,
		Excluded: len(exclude),
		Model:    st.Model,
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
		mtime, size := sessionStamp(p)
		if mtime == 0 {
			continue
		}
		if c, ok := cache[p]; ok && c.Mtime == mtime && c.Size == size {
			out = append(out, c)
			continue
		}
		s := ScanSession(p, nil)
		if s == nil {
			continue
		}
		s.Mtime = mtime
		s.Size = size
		cache[p] = s
		out = append(out, s)
		changed = true
	}
	if dedupeForks(out, cache) {
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

// dedupeForks strips responses that appear in more than one session. Resuming
// or branching a session copies the earlier conversation into the new
// transcript verbatim — same message ids, same uuids — but those API calls were
// only ever billed once. The first session in a stable order keeps each
// response; any later session holding it is rescanned without it.
//
// Ordering is by first activity then path: which of two forked siblings keeps a
// shared response is arbitrary, but the machine-wide total is right either way.
func dedupeForks(sessions []*Session, cache map[string]*Session) bool {
	order := append([]*Session(nil), sessions...)
	sort.Slice(order, func(i, j int) bool {
		if order[i].FirstTS != order[j].FirstTS {
			return order[i].FirstTS < order[j].FirstTS
		}
		return order[i].Path < order[j].Path
	})

	claimed := make(map[string]bool, 4096)
	changed := false
	for _, s := range order {
		var dup map[string]bool
		for _, id := range s.RespIDs {
			if claimed[id] {
				if dup == nil {
					dup = map[string]bool{}
				}
				dup[id] = true
			}
		}
		for _, id := range s.RespIDs {
			claimed[id] = true
		}
		if len(dup) == s.Excluded {
			continue // already scanned against this many duplicates
		}
		re := ScanSession(s.Path, dup)
		if re == nil {
			continue
		}
		re.Mtime, re.Size = s.Mtime, s.Size
		re.SessionID, re.Project, re.Title = s.SessionID, s.Project, s.Title
		re.GitBranch, re.FirstTS, re.LastTS, re.Messages = s.GitBranch, s.FirstTS, s.LastTS, s.Messages
		*s = *re
		cache[s.Path] = s
		changed = true
	}
	return changed
}

// sessionStamp fingerprints a transcript together with the subagent
// transcripts counted alongside it, so the index cache invalidates when either
// changes. Returns a zero mtime if the main transcript is unreadable.
func sessionStamp(path string) (mtime, size int64) {
	fi, err := os.Stat(path)
	if err != nil {
		return 0, 0
	}
	mtime, size = fi.ModTime().UnixNano(), fi.Size()
	for _, p := range SubagentTranscripts(path) {
		si, err := os.Stat(p)
		if err != nil {
			continue
		}
		if m := si.ModTime().UnixNano(); m > mtime {
			mtime = m
		}
		size += si.Size()
	}
	return mtime, size
}

// cachePath is versioned so schema changes invalidate stale caches cleanly.
// v4: per-day/per-model buckets, deduplicated by message id.
func cachePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "ccpane-index.v4.json")
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
