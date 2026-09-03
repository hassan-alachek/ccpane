package transcript

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Lifetime is Claude Code's own cumulative usage record, read from
// ~/.claude/stats-cache.json.
//
// It is the only surviving account of sessions whose transcripts Claude Code
// has already deleted: the cache accumulates forever and is never recomputed
// for past days, while transcripts age out after cleanupPeriodDays. That also
// means its token counts come from the undeduplicated scanner — each API
// response counted once per transcript row — so they need correcting before
// they can be compared with anything ccpane derives itself. Deflate does that.
type Lifetime struct {
	// Raw is per-model usage exactly as Claude Code recorded it. Requests is
	// always zero: the cache stores no response count.
	Raw ModelTokens
	// Sessions is every session Claude Code has ever seen, including deleted ones.
	Sessions int
	// FirstDay is the earliest day it has a record of (YYYY-MM-DD).
	FirstDay string
	// Through is the last day it computed (YYYY-MM-DD).
	Through string
}

// ccStatsCache mirrors the fields of stats-cache.json that matter here. The
// file is Claude Code's private format, so every field is optional and a
// mismatch degrades to "no lifetime data" rather than an error.
type ccStatsCache struct {
	ModelUsage map[string]struct {
		InputTokens              int `json:"inputTokens"`
		OutputTokens             int `json:"outputTokens"`
		CacheReadInputTokens     int `json:"cacheReadInputTokens"`
		CacheCreationInputTokens int `json:"cacheCreationInputTokens"`
	} `json:"modelUsage"`
	TotalSessions    int    `json:"totalSessions"`
	FirstSessionDate string `json:"firstSessionDate"`
	LastComputedDate string `json:"lastComputedDate"`
}

// LoadLifetime reads Claude Code's cumulative stats cache. It returns nil when
// the file is absent or unreadable, which is not an error: the cache is an
// implementation detail of another program and ccpane works without it.
func LoadLifetime() *Lifetime {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	b, err := os.ReadFile(filepath.Join(home, ".claude", "stats-cache.json"))
	if err != nil {
		return nil
	}
	var c ccStatsCache
	if json.Unmarshal(b, &c) != nil || len(c.ModelUsage) == 0 {
		return nil
	}
	lt := &Lifetime{
		Raw:      ModelTokens{},
		Sessions: c.TotalSessions,
		Through:  c.LastComputedDate,
	}
	if len(c.FirstSessionDate) >= 10 {
		lt.FirstDay = c.FirstSessionDate[:10]
	}
	for model, u := range c.ModelUsage {
		if model == "" || model == syntheticModel {
			continue
		}
		lt.Raw.Add(model, Tokens{
			Input:      u.InputTokens,
			Output:     u.OutputTokens,
			CacheRead:  u.CacheReadInputTokens,
			CacheWrite: u.CacheCreationInputTokens,
		})
	}
	if lt.Raw.Total().Total() == 0 {
		return nil
	}
	return lt
}

// Deflate estimates the deduplicated equivalent of the lifetime record.
//
// raw and deduped are the same usage measured both ways over the transcripts
// still on disk; their ratio is the inflation Claude Code's cache carries.
// Correction is per model and per token kind, because the factor depends on how
// many content blocks a turn produced — output tokens inflate about a third
// more than cache reads, and a tool-heavy model more than a conversational one.
//
// A model the surviving transcripts say nothing about falls back to the overall
// ratio; with no usable ratio at all the raw figures are returned unchanged.
// The result is an estimate, and only ever as good as the assumption that
// deleted sessions resembled the surviving ones.
func (lt *Lifetime) Deflate(raw, deduped ModelTokens) ModelTokens {
	out := ModelTokens{}
	if lt == nil {
		return out
	}
	allRaw, allDed := raw.Total(), deduped.Total()
	for model, lr := range lt.Raw {
		r, okR := raw[model]
		d, okD := deduped[model]
		if !okR || !okD {
			r, d = allRaw, allDed
		}
		out.Add(model, Tokens{
			Input:      scale(lr.Input, r.Input, d.Input, allRaw.Input, allDed.Input),
			Output:     scale(lr.Output, r.Output, d.Output, allRaw.Output, allDed.Output),
			CacheWrite: scale(lr.CacheWrite, r.CacheWrite, d.CacheWrite, allRaw.CacheWrite, allDed.CacheWrite),
			CacheRead:  scale(lr.CacheRead, r.CacheRead, d.CacheRead, allRaw.CacheRead, allDed.CacheRead),
		})
	}
	return out
}

// scale maps v through the measured deduped/raw ratio, falling back to the
// overall ratio and then to v unchanged.
func scale(v, raw, ded, allRaw, allDed int) int {
	if raw <= 0 || ded <= 0 {
		raw, ded = allRaw, allDed
	}
	if raw <= 0 || ded <= 0 {
		return v
	}
	return int(float64(v) * float64(ded) / float64(raw))
}
