package transcript

import "testing"

func TestDeflateUsesPerModelRatios(t *testing.T) {
	// Surviving transcripts: opus inflated 4x, haiku not at all.
	raw := ModelTokens{
		"opus":  {Output: 400, CacheRead: 4000},
		"haiku": {Output: 100, CacheRead: 1000},
	}
	deduped := ModelTokens{
		"opus":  {Output: 100, CacheRead: 1000},
		"haiku": {Output: 100, CacheRead: 1000},
	}
	lt := &Lifetime{Raw: ModelTokens{
		"opus":  {Output: 800, CacheRead: 8000},
		"haiku": {Output: 200, CacheRead: 2000},
	}}

	got := lt.Deflate(raw, deduped)
	if o := got["opus"]; o.Output != 200 || o.CacheRead != 2000 {
		t.Errorf("opus deflated to %+v, want Output 200 / CacheRead 2000 (÷4)", o)
	}
	if h := got["haiku"]; h.Output != 200 || h.CacheRead != 2000 {
		t.Errorf("haiku deflated to %+v, want it unchanged (ratio 1)", h)
	}
}

func TestDeflateFallsBackForUnseenModels(t *testing.T) {
	// Overall ratio is 2x; "ancient" never appears in surviving transcripts.
	raw := ModelTokens{"opus": {Output: 200}}
	deduped := ModelTokens{"opus": {Output: 100}}
	lt := &Lifetime{Raw: ModelTokens{"ancient": {Output: 500}}}

	if got := lt.Deflate(raw, deduped)["ancient"].Output; got != 250 {
		t.Errorf("unseen model deflated to %d, want 250 (overall ÷2)", got)
	}
}

func TestDeflateWithoutAnyRatioIsIdentity(t *testing.T) {
	lt := &Lifetime{Raw: ModelTokens{"opus": {Output: 700}}}
	if got := lt.Deflate(ModelTokens{}, ModelTokens{})["opus"].Output; got != 700 {
		t.Errorf("with no measurable ratio got %d, want the raw 700 unchanged", got)
	}
}

func TestDeflateOnNilLifetimeIsEmpty(t *testing.T) {
	var lt *Lifetime
	if got := lt.Deflate(ModelTokens{}, ModelTokens{}); len(got) != 0 {
		t.Errorf("nil Lifetime deflated to %v, want empty", got)
	}
}
