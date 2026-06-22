package transcript

import "strings"

// Usage mirrors message.usage in the transcript.
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

// Stats aggregates token usage across a set of records.
type Stats struct {
	InputTokens   int
	OutputTokens  int
	CacheCreation int
	CacheRead     int
	ContextNow    int // context fill at the latest assistant turn
	MaxContext    int // peak context fill observed
	Turns         int
	Model         string
}

// Aggregate sums usage over records in chronological (file) order.
func Aggregate(records []*Record) Stats {
	var s Stats
	for _, r := range records {
		if r.Message == nil || r.Message.Usage == nil {
			continue
		}
		u := r.Message.Usage
		s.InputTokens += u.InputTokens
		s.OutputTokens += u.OutputTokens
		s.CacheCreation += u.CacheCreationInputTokens
		s.CacheRead += u.CacheReadInputTokens
		if r.Type == "assistant" {
			s.Turns++
			cn := u.ContextTokens()
			s.ContextNow = cn
			if cn > s.MaxContext {
				s.MaxContext = cn
			}
			if r.Message.Model != "" {
				s.Model = r.Message.Model
			}
		}
	}
	return s
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

// Pricing is per-million-token USD rates (placeholders — edit to match current
// pricing; cost is shown only as a labelled estimate).
type Pricing struct {
	InputPerM      float64
	OutputPerM     float64
	CacheWritePerM float64
	CacheReadPerM  float64
}

// DefaultPricing is a placeholder. Verify against current Anthropic pricing.
var DefaultPricing = Pricing{InputPerM: 5, OutputPerM: 25, CacheWritePerM: 6.25, CacheReadPerM: 0.5}

// EstCost returns a rough USD estimate for the aggregated usage.
func (s Stats) EstCost(p Pricing) float64 {
	return float64(s.InputTokens)/1e6*p.InputPerM +
		float64(s.OutputTokens)/1e6*p.OutputPerM +
		float64(s.CacheCreation)/1e6*p.CacheWritePerM +
		float64(s.CacheRead)/1e6*p.CacheReadPerM
}

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
	for k := range modelPricing {
		if kk := stripModelDate(k); strings.HasPrefix(kk, fam) && len(kk) > len(best) {
			best = k
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
