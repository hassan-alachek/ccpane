package transcript

import "testing"

// resp builds the records Claude Code writes for one API response: one per
// content block, each carrying an identical copy of the whole response's usage.
func resp(id, model, ts string, blocks int, u Usage, sidechain bool) []*Record {
	var out []*Record
	for i := 0; i < blocks; i++ {
		cp := u
		out = append(out, &Record{
			Type:        "assistant",
			Timestamp:   ts,
			IsSidechain: sidechain,
			Message:     &Message{Role: "assistant", Model: model, ID: id, Usage: &cp},
		})
	}
	return out
}

func TestAggregateCountsEachResponseOnce(t *testing.T) {
	u := Usage{InputTokens: 2, OutputTokens: 100, CacheCreationInputTokens: 30, CacheReadInputTokens: 500}
	var recs []*Record
	recs = append(recs, resp("msg_a", "claude-opus-5", "2026-09-03T10:00:00.000Z", 4, u, false)...)
	recs = append(recs, resp("msg_b", "claude-opus-5", "2026-09-03T10:01:00.000Z", 1, u, false)...)

	got := Aggregate(recs)
	if got.Requests != 2 {
		t.Errorf("Requests = %d, want 2 (five rows, two API responses)", got.Requests)
	}
	if want := 2 * u.Total(); got.Total() != want {
		t.Errorf("Total = %d, want %d", got.Total(), want)
	}
	if got.Output != 200 {
		t.Errorf("Output = %d, want 200", got.Output)
	}
}

// Total is the sum of a Usage's four token fields, for test expectations.
func (u Usage) Total() int {
	return u.InputTokens + u.OutputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens
}

func TestAggregateSkipsSidechainAndSynthetic(t *testing.T) {
	u := Usage{OutputTokens: 10}
	var recs []*Record
	recs = append(recs, resp("msg_main", "claude-opus-5", "2026-09-03T10:00:00.000Z", 1, u, false)...)
	// A subagent turn echoed into the main transcript: counted in its own file.
	recs = append(recs, resp("msg_side", "claude-opus-5", "2026-09-03T10:00:01.000Z", 1, u, true)...)
	// A locally fabricated record: no API call happened.
	recs = append(recs, resp("msg_synth", syntheticModel, "2026-09-03T10:00:02.000Z", 1, u, false)...)

	got := Aggregate(recs)
	if got.Requests != 1 {
		t.Errorf("Requests = %d, want 1", got.Requests)
	}
	if got.Output != 10 {
		t.Errorf("Output = %d, want 10", got.Output)
	}
}

func TestSinkBucketsByDayModelAndAttribution(t *testing.T) {
	u := Usage{OutputTokens: 10, CacheReadInputTokens: 90}
	recs := resp("msg_a", "claude-opus-5", "2026-09-01T23:00:00.000Z", 3, u, false)
	recs[0].AttributionSkill = "artifact-design"
	recs[0].AttributionMcpServer = "datadog"
	recs = append(recs, resp("msg_b", "claude-haiku-4-5", "2026-09-02T01:00:00.000Z", 1, u, false)...)

	sink := NewSink()
	got := Stats{ByModel: ModelTokens{}}
	accumulate(recs, true, map[string]bool{}, nil, &got, sink)

	if len(sink.Daily) != 2 {
		t.Fatalf("Daily has %d days, want 2 (UTC dates 09-01 and 09-02)", len(sink.Daily))
	}
	if n := sink.Daily["2026-09-01"]["claude-opus-5"].Requests; n != 1 {
		t.Errorf("09-01 opus requests = %d, want 1", n)
	}
	if n := sink.Daily["2026-09-02"]["claude-haiku-4-5"].Requests; n != 1 {
		t.Errorf("09-02 haiku requests = %d, want 1", n)
	}
	// Rows is the undeduplicated total, i.e. what /usage reports.
	if sink.Rows["2026-09-01"] != 3*u.Total() {
		t.Errorf("Rows[09-01] = %d, want %d (three rows, uncollapsed)", sink.Rows["2026-09-01"], 3*u.Total())
	}
	skill := sink.Attr["2026-09-01"][AttrKey(AttrSkill, "artifact-design")].Total()
	if skill.Total() != u.Total() {
		t.Errorf("skill attribution = %d, want %d", skill.Total(), u.Total())
	}
	main := sink.Attr["2026-09-01"][AttrKey(AttrSurface, "main loop")].Total()
	if main.Total() != u.Total() {
		t.Errorf("surface attribution = %d, want %d", main.Total(), u.Total())
	}
	if len(sink.IDs) != 2 {
		t.Errorf("IDs = %v, want two response ids", sink.IDs)
	}
}

func TestExcludeDropsReplayedHistory(t *testing.T) {
	u := Usage{OutputTokens: 10}
	recs := resp("msg_old", "claude-opus-5", "2026-09-01T10:00:00.000Z", 2, u, false)
	recs = append(recs, resp("msg_new", "claude-opus-5", "2026-09-02T10:00:00.000Z", 1, u, false)...)

	sink := NewSink()
	got := Stats{ByModel: ModelTokens{}}
	accumulate(recs, true, map[string]bool{}, map[string]bool{"msg_old": true}, &got, sink)

	if got.Requests != 1 {
		t.Errorf("Requests = %d, want 1 (msg_old billed to the original session)", got.Requests)
	}
	// The id is still recorded, so the ownership pass stays stable across runs.
	if len(sink.IDs) != 2 {
		t.Errorf("IDs = %v, want both ids recorded", sink.IDs)
	}
}

func TestPricingForIsDeterministic(t *testing.T) {
	SetModelPricing(map[string]Pricing{
		"claude-opus-4-5":   {InputPerM: 1},
		"claude-opus-4-6":   {InputPerM: 2},
		"claude-haiku-4-5":  {InputPerM: 3},
		"claude-opus-4-1-x": {InputPerM: 4},
	})
	defer SetModelPricing(nil)

	first := PricingFor("claude-opus-5")
	for i := 0; i < 200; i++ {
		if got := PricingFor("claude-opus-5"); got != first {
			t.Fatalf("PricingFor drifted between calls: %v then %v", first, got)
		}
	}
}
