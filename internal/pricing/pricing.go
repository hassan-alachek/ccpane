// Package pricing loads per-model token pricing from LiteLLM's public price
// table (the same source ccusage uses) and installs it into the transcript
// package. It caches the data on disk for 24h and falls back to a stale cache,
// then to transcript.DefaultPricing, when offline.
package pricing

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hassan-alachek/ccpane/internal/transcript"
)

const (
	litellmURL = "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json"
	cacheTTL   = 24 * time.Hour
)

type entry struct {
	Input      float64 `json:"input_cost_per_token"`
	Output     float64 `json:"output_cost_per_token"`
	CacheWrite float64 `json:"cache_creation_input_token_cost"`
	CacheRead  float64 `json:"cache_read_input_token_cost"`
}

// Load installs dynamic pricing (best-effort). Safe to call on every startup;
// it only hits the network when the disk cache is missing or older than 24h.
func Load() {
	data := readCache(true)
	if data == nil {
		if fetched := fetch(); fetched != nil {
			_ = os.WriteFile(cachePath(), fetched, 0o644)
			data = fetched
		} else {
			data = readCache(false) // stale fallback
		}
	}
	if data == nil {
		return
	}
	if table := build(data); len(table) > 0 {
		transcript.SetModelPricing(table)
	}
}

func cachePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "ccpane-pricing.json")
}

func readCache(requireFresh bool) []byte {
	fi, err := os.Stat(cachePath())
	if err != nil {
		return nil
	}
	if requireFresh && time.Since(fi.ModTime()) > cacheTTL {
		return nil
	}
	b, err := os.ReadFile(cachePath())
	if err != nil {
		return nil
	}
	return b
}

func fetch() []byte {
	c := &http.Client{Timeout: 6 * time.Second}
	resp, err := c.Get(litellmURL)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil
	}
	return b
}

// build extracts Claude model rates (per-million tokens) keyed by base name.
func build(data []byte) map[string]transcript.Pricing {
	var raw map[string]entry
	if json.Unmarshal(data, &raw) != nil {
		return nil
	}
	out := map[string]transcript.Pricing{}
	for k, e := range raw {
		if !strings.Contains(k, "claude") || (e.Input == 0 && e.Output == 0) {
			continue
		}
		base := k
		if i := strings.LastIndex(k, "/"); i >= 0 {
			base = k[i+1:]
		}
		out[base] = transcript.Pricing{
			InputPerM:      e.Input * 1e6,
			OutputPerM:     e.Output * 1e6,
			CacheWritePerM: e.CacheWrite * 1e6,
			CacheReadPerM:  e.CacheRead * 1e6,
		}
	}
	return out
}
