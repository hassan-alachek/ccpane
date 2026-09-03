package snapshot

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeHome builds a ~/.claude/projects tree and points HOME at it.
func fakeHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // windows
	proj := filepath.Join(home, ".claude", "projects")
	write := func(rel, body string) {
		p := filepath.Join(proj, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("-tmp-a/sess1.jsonl", `{"type":"user"}`+"\n")
	write("-tmp-a/sess2.jsonl", `{"type":"assistant"}`+"\n")
	write("-tmp-a/sess1/subagents/agent-abc.jsonl", `{"type":"assistant"}`+"\n")
	write("-tmp-b/sess3.jsonl", `{"type":"user"}`+"\n")
	write("-tmp-b/notes.txt", "not a transcript")
	if err := os.WriteFile(filepath.Join(home, ".claude", "stats-cache.json"), []byte(`{"totalSessions":7}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return home
}

// entries reads the archive's member names.
func entries(t *testing.T, archive string) map[string]int64 {
	t.Helper()
	f, err := os.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("archive is not valid gzip: %v", err)
	}
	defer gz.Close()
	out := map[string]int64{}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("archive is not a valid tar: %v", err)
		}
		out[h.Name] = h.Size
	}
	return out
}

func TestCreateArchivesEveryTranscript(t *testing.T) {
	home := fakeHome(t)
	dest := filepath.Join(t.TempDir(), "out")

	r, err := Create(dest, "test")
	if err != nil {
		t.Fatal(err)
	}
	if r.Files != 4 {
		t.Errorf("Files = %d, want 4 transcripts", r.Files)
	}
	if r.Sessions != 3 || r.Subagents != 1 {
		t.Errorf("Sessions/Subagents = %d/%d, want 3/1", r.Sessions, r.Subagents)
	}
	if !r.StatsCache {
		t.Error("stats-cache.json was not included")
	}

	got := entries(t, r.Path)
	for _, want := range []string{
		"projects/-tmp-a/sess1.jsonl",
		"projects/-tmp-a/sess2.jsonl",
		"projects/-tmp-a/sess1/subagents/agent-abc.jsonl",
		"projects/-tmp-b/sess3.jsonl",
		"claude/stats-cache.json",
		"ccpane-snapshot.json",
	} {
		if _, ok := got[want]; !ok {
			t.Errorf("archive is missing %q", want)
		}
	}
	if _, ok := got["projects/-tmp-b/notes.txt"]; ok {
		t.Error("archive included a non-transcript file")
	}

	// The whole point is that it is a copy: the originals must survive.
	for _, rel := range []string{"-tmp-a/sess1.jsonl", "-tmp-b/sess3.jsonl"} {
		p := filepath.Join(home, ".claude", "projects", rel)
		if _, err := os.Stat(p); err != nil {
			t.Errorf("snapshot removed %s from disk: %v", rel, err)
		}
	}
}

func TestManifestDescribesTheArchive(t *testing.T) {
	fakeHome(t)
	r, err := Create(filepath.Join(t.TempDir(), "out"), "v9.9.9")
	if err != nil {
		t.Fatal(err)
	}
	f, _ := os.Open(r.Path)
	defer f.Close()
	gz, _ := gzip.NewReader(f)
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err != nil {
			t.Fatal("manifest not found in archive")
		}
		if h.Name != "ccpane-snapshot.json" {
			continue
		}
		var m manifest
		if err := json.NewDecoder(tr).Decode(&m); err != nil {
			t.Fatalf("manifest is not valid JSON: %v", err)
		}
		if m.Version != "v9.9.9" {
			t.Errorf("manifest version = %q, want v9.9.9", m.Version)
		}
		if m.Files != 4 {
			t.Errorf("manifest files = %d, want 4", m.Files)
		}
		return
	}
}

func TestResolveDest(t *testing.T) {
	dir := t.TempDir()
	explicit := filepath.Join(dir, "mine.tar.gz")
	if got, err := resolveDest(explicit); err != nil || got != explicit {
		t.Errorf("explicit path: got %q (%v), want %q", got, err, explicit)
	}
	got, err := resolveDest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(got) != dir {
		t.Errorf("directory form landed in %q, want inside %q", filepath.Dir(got), dir)
	}
	base := filepath.Base(got)
	if !strings.HasPrefix(base, "ccpane-transcripts-") || !strings.HasSuffix(base, ".tar.gz") {
		t.Errorf("generated name %q is not the timestamped form", base)
	}
}

func TestCreateLeavesNoTempFileBehind(t *testing.T) {
	fakeHome(t)
	dest := t.TempDir()
	if _, err := Create(dest, "test"); err != nil {
		t.Fatal(err)
	}
	ents, err := os.ReadDir(dest)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("left a partial archive behind: %s", e.Name())
		}
	}
}
