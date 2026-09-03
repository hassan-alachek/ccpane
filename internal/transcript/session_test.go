package transcript

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPruneOldCachesKeepsOnlyTheCurrentOne(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Every generation ccpane has ever written, plus an unrelated neighbour.
	stale := []string{
		"ccpane-index.json",
		"ccpane-index.v2.json",
		"ccpane-index.v3.json",
		"ccpane-index.v4.json",
	}
	bystander := []string{"ccpane-pricing.json", "stats-cache.json", "settings.json"}
	for _, n := range append(append([]string{}, stale...), bystander...) {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	current := cachePath()
	if err := os.WriteFile(current, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	pruneOldCaches()

	for _, n := range stale {
		if _, err := os.Stat(filepath.Join(dir, n)); err == nil {
			t.Errorf("%s survived pruning", n)
		}
	}
	if _, err := os.Stat(current); err != nil {
		t.Errorf("pruning removed the current cache %s: %v", filepath.Base(current), err)
	}
	for _, n := range bystander {
		if _, err := os.Stat(filepath.Join(dir, n)); err != nil {
			t.Errorf("pruning removed unrelated file %s", n)
		}
	}
}

func TestPruneOldCachesToleratesMissingDir(t *testing.T) {
	t.Setenv("HOME", filepath.Join(t.TempDir(), "nope"))
	pruneOldCaches() // must not panic when ~/.claude does not exist
}
