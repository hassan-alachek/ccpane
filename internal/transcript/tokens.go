package transcript

import "strings"

// syntheticModel marks records Claude Code fabricates locally (cancellations,
// injected notices). They carry a zeroed usage object and no API call.
const syntheticModel = "<synthetic>"

// Usage mirrors message.usage in the transcript.
//
// Claude Code splits one assistant turn into one record per content block
// (thinking / text / each tool_use) and copies the whole response's usage onto
// every one of them, so records sharing message.id describe a single API call
// and must be counted once. See Anthropic's cost-tracking guide: "multiple
// messages share the same id with identical usage data. Track which IDs you've
// already counted and skip duplicates to avoid inflated totals."
type Usage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

// ContextTokens is the prompt size for this turn = context-window fill.
func (u *Usage) ContextTokens() int {
	if u == nil {
		return 0
	}
	return u.InputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens
}

// Tokens is deduplicated usage: one count per API response.
type Tokens struct {
	Input      int `json:"i"`
	Output     int `json:"o"`
	CacheWrite int `json:"w"`
	CacheRead  int `json:"r"`
	Requests   int `json:"n"` // distinct API responses
}

// Total is all token activity in the bucket.
func (t Tokens) Total() int { return t.Input + t.Output + t.CacheWrite + t.CacheRead }

// Cache is cache-write plus cache-read tokens.
func (t Tokens) Cache() int { return t.CacheWrite + t.CacheRead }

// Add folds another bucket into t.
func (t *Tokens) Add(o Tokens) {
	t.Input += o.Input
	t.Output += o.Output
	t.CacheWrite += o.CacheWrite
	t.CacheRead += o.CacheRead
	t.Requests += o.Requests
}

// addResponse folds in one API response's usage.
func (t *Tokens) addResponse(u *Usage) {
	t.Input += u.InputTokens
	t.Output += u.OutputTokens
	t.CacheWrite += u.CacheCreationInputTokens
	t.CacheRead += u.CacheReadInputTokens
	t.Requests++
}

// Cost is the estimated USD cost of the bucket at the given rates.
func (t Tokens) Cost(p Pricing) float64 {
	return float64(t.Input)/1e6*p.InputPerM +
		float64(t.Output)/1e6*p.OutputPerM +
		float64(t.CacheWrite)/1e6*p.CacheWritePerM +
		float64(t.CacheRead)/1e6*p.CacheReadPerM
}

// ModelTokens is deduplicated usage keyed by model id.
type ModelTokens map[string]Tokens

// Add folds a bucket into the entry for model.
func (m ModelTokens) Add(model string, t Tokens) {
	e := m[model]
	e.Add(t)
	m[model] = e
}

// Merge folds every entry of o into m.
func (m ModelTokens) Merge(o ModelTokens) {
	for model, t := range o {
		m.Add(model, t)
	}
}

// Total sums every model's usage.
func (m ModelTokens) Total() Tokens {
	var t Tokens
	for _, v := range m {
		t.Add(v)
	}
	return t
}

// Cost prices each model at its own rates, which a single blended rate cannot
// do: one session often mixes Opus, Haiku and subagent models.
func (m ModelTokens) Cost() float64 {
	var c float64
	for model, t := range m {
		c += t.Cost(PricingFor(model))
	}
	return c
}

// Attribution key prefixes. A turn can match several at once (a skill that
// calls an MCP tool), so only the Surface* keys form a true partition.
const (
	AttrSurface = "surface" // main loop vs subagent
	AttrAgent   = "agent"   // subagent type, e.g. Explore
	AttrSkill   = "skill"   // active skill
	AttrMCP     = "mcp"     // MCP server
	AttrTool    = "tool"    // MCP tool
)

// AttrKey builds the "<dimension>:<name>" key used in Session.Attr.
func AttrKey(dim, name string) string { return dim + ":" + name }

// SplitAttrKey splits a key back into its dimension and name.
func SplitAttrKey(k string) (dim, name string) {
	if i := strings.IndexByte(k, ':'); i >= 0 {
		return k[:i], k[i+1:]
	}
	return "", k
}

// Stats aggregates usage across a set of records.
type Stats struct {
	Tokens                 // deduplicated totals across all models
	ByModel    ModelTokens // per-model breakdown
	ContextNow int         // context fill at the latest assistant turn
	MaxContext int         // peak context fill observed
	Model      string      // model of the latest assistant turn
}

// Turns is the number of API responses, i.e. assistant turns.
func (s Stats) Turns() int { return s.Requests }

// Cost is the estimated USD cost, priced per model.
func (s Stats) Cost() float64 { return s.ByModel.Cost() }

// Aggregate sums usage over one main transcript's records, counting each API
// response once. Sidechain records are skipped: those turns are also written to
// the session's subagents/ transcripts, where AggregateSession counts them.
func Aggregate(records []*Record) Stats {
	s := Stats{ByModel: ModelTokens{}}
	accumulate(records, true, map[string]bool{}, nil, &s, nil)
	return s
}

// AggregateSession sums a whole session: its main transcript plus every
// subagent transcript it spawned. sink may be nil; when given it collects the
// per-day, per-model and per-attribution breakdowns.
// exclude, when non-nil, pre-claims message ids owned by an earlier session, so
// history replayed into a forked or resumed session is not billed twice.
func AggregateSession(path string, recs []*Record, sink *Sink, exclude map[string]bool) Stats {
	s := Stats{ByModel: ModelTokens{}}
	seen := map[string]bool{}
	accumulate(recs, true, seen, exclude, &s, sink)
	for _, p := range SubagentTranscripts(path) {
		sub, err := ParseFile(p)
		if err != nil {
			continue
		}
		// Subagent records are themselves flagged isSidechain; in their own
		// file that flag is the norm, not a duplicate marker, so keep them.
		accumulate(sub, false, seen, exclude, &s, sink)
	}
	return s
}

// Sink collects the per-day and per-attribution breakdowns while scanning.
// A nil *Sink, or nil maps within it, simply skip that breakdown.
type Sink struct {
	// Daily is deduplicated usage by UTC day and model.
	Daily map[string]ModelTokens
	// Rows is the undeduplicated per-day token total: what Claude Code's
	// /usage Stats tab reports.
	Rows map[string]int
	// Attr is deduplicated usage by UTC day, attribution key and model.
	Attr map[string]map[string]ModelTokens
	// IDs collects the message id of every response counted, in scan order.
	IDs []string
}

// NewSink returns a Sink with every breakdown enabled.
func NewSink() *Sink {
	return &Sink{
		Daily: map[string]ModelTokens{},
		Rows:  map[string]int{},
		Attr:  map[string]map[string]ModelTokens{},
	}
}

// addAttr records one response against an attribution key.
func (k *Sink) addAttr(day, key, model string, t Tokens) {
	if k == nil || k.Attr == nil || day == "" || key == "" {
		return
	}
	byKey := k.Attr[day]
	if byKey == nil {
		byKey = map[string]ModelTokens{}
		k.Attr[day] = byKey
	}
	mt := byKey[key]
	if mt == nil {
		mt = ModelTokens{}
		byKey[key] = mt
	}
	mt.Add(model, t)
}

// accumulate folds one transcript's records into s (and the optional per-day
// maps). dropSidechain must be true for main transcripts and false for
// subagent transcripts. seen carries message ids already counted, so a caller
// can dedupe across several files of one session.
func accumulate(records []*Record, dropSidechain bool, seen map[string]bool, exclude map[string]bool, s *Stats, sink *Sink) {
	for _, r := range records {
		if r.Type != "assistant" || r.Message == nil || r.Message.Usage == nil {
			continue
		}
		if dropSidechain && r.IsSidechain {
			continue
		}
		model := r.Message.Model
		if model == "" || model == syntheticModel {
			continue
		}
		// Context fill is a snapshot, not a sum, so it is read from every
		// record; only the main transcript's turns describe this session's
		// own context window.
		if dropSidechain {
			if cn := r.Message.Usage.ContextTokens(); cn > 0 {
				s.ContextNow = cn
				if cn > s.MaxContext {
					s.MaxContext = cn
				}
			}
			s.Model = model
		}
		day := dayOf(r.Timestamp)
		if sink != nil && sink.Rows != nil && day != "" {
			var raw Tokens
			raw.addResponse(r.Message.Usage)
			sink.Rows[day] += raw.Total()
		}
		if id := r.Message.ID; id != "" {
			if seen[id] {
				continue // another content block of a response already counted
			}
			seen[id] = true
			// IDs records every response the transcript holds, including any
			// billed to an earlier session, so the ownership pass stays stable
			// across runs.
			if sink != nil {
				sink.IDs = append(sink.IDs, id)
			}
			if exclude[id] {
				continue // replayed history, already billed to an earlier session
			}
		}
		var t Tokens
		t.addResponse(r.Message.Usage)
		s.Tokens.Add(t)
		s.ByModel.Add(model, t)
		if sink == nil || day == "" {
			continue
		}
		if sink.Daily != nil {
			if sink.Daily[day] == nil {
				sink.Daily[day] = ModelTokens{}
			}
			sink.Daily[day].Add(model, t)
		}
		surface := "main loop"
		if !dropSidechain {
			surface = "subagents"
		}
		sink.addAttr(day, AttrKey(AttrSurface, surface), model, t)
		for dim, name := range map[string]string{
			AttrAgent: r.AttributionAgent,
			AttrSkill: r.AttributionSkill,
			AttrMCP:   r.AttributionMcpServer,
			AttrTool:  r.AttributionMcpTool,
		} {
			if name != "" {
				sink.addAttr(day, AttrKey(dim, name), model, t)
			}
		}
	}
}

// dayOf returns the UTC calendar day of a transcript timestamp. Transcript
// timestamps are RFC3339 in UTC, so the date is its first ten bytes — the same
// bucketing Claude Code uses.
func dayOf(ts string) string {
	if len(ts) < 10 || ts[4] != '-' || ts[7] != '-' {
		return ""
	}
	return ts[:10]
}

// AutoWindow infers the context-window size from peak observed usage: a session
// whose context ever exceeded 200k must be on the 1M-context (beta) tier, since
// the model id in transcripts does not carry the [1m] flag.
func AutoWindow(maxContext int) int {
	for _, w := range []int{200_000, 1_000_000} {
		if maxContext <= w {
			return w
		}
	}
	return maxContext
}

// Pricing is per-million-token USD rates.
type Pricing struct {
	InputPerM      float64
	OutputPerM     float64
	CacheWritePerM float64
	CacheReadPerM  float64
}

// DefaultPricing is a placeholder used when the LiteLLM table has no match.
var DefaultPricing = Pricing{InputPerM: 5, OutputPerM: 25, CacheWritePerM: 6.25, CacheReadPerM: 0.5}

// modelPricing holds per-model rates (e.g. fetched from LiteLLM), keyed by the
// model id's base name. nil/empty means "use DefaultPricing".
var modelPricing map[string]Pricing

// SetModelPricing installs a dynamic model->rates table.
func SetModelPricing(m map[string]Pricing) { modelPricing = m }

// PricingLoaded reports whether a dynamic pricing table is installed.
func PricingLoaded() bool { return len(modelPricing) > 0 }

// PricingFor resolves rates for a model id: exact match, then date-stripped,
// then a same-family prefix match; falls back to DefaultPricing.
func PricingFor(model string) Pricing {
	if len(modelPricing) == 0 || model == "" {
		return DefaultPricing
	}
	if p, ok := modelPricing[model]; ok {
		return p
	}
	stripped := stripModelDate(model)
	if p, ok := modelPricing[stripped]; ok {
		return p
	}
	fam := familyModelKey(stripped)
	var best string
	var bestLen int
	for k := range modelPricing {
		kk := stripModelDate(k)
		if !strings.HasPrefix(kk, fam) {
			continue
		}
		// Longest family match wins, ties broken lexicographically: map order
		// is randomised, and without a stable rule the cost estimate shifts
		// from run to run on identical data.
		if best == "" || len(kk) > bestLen || (len(kk) == bestLen && k < best) {
			best, bestLen = k, len(kk)
		}
	}
	if best != "" {
		return modelPricing[best]
	}
	return DefaultPricing
}

// stripModelDate removes a trailing -YYYYMMDD suffix.
func stripModelDate(s string) string {
	if i := strings.LastIndexByte(s, '-'); i >= 0 && len(s)-i-1 == 8 {
		for _, c := range s[i+1:] {
			if c < '0' || c > '9' {
				return s
			}
		}
		return s[:i]
	}
	return s
}

// familyModelKey drops a trailing -<number> segment (claude-opus-4-8 -> claude-opus-4).
func familyModelKey(s string) string {
	if i := strings.LastIndexByte(s, '-'); i >= 0 {
		for _, c := range s[i+1:] {
			if c < '0' || c > '9' {
				return s
			}
		}
		return s[:i]
	}
	return s
}
