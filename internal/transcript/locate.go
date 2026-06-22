package transcript

import (
	"bufio"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ProjectsDir returns ~/.claude/projects.
func ProjectsDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "projects")
}

// MangleCwd converts an absolute path to Claude Code's project-dir name.
// Claude Code replaces path separators and dots with '-'; this mapping is
// lossy, so it is only a fast-path hint — callers verify via the cwd field.
func MangleCwd(cwd string) string {
	var b strings.Builder
	for _, r := range cwd {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return b.String()
}

// AllTranscripts returns every top-level session transcript path, excluding
// subagent sidechain files (which are children of a session).
func AllTranscripts() []string {
	var out []string
	root := ProjectsDir()
	sep := string(os.PathSeparator)
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(p, ".jsonl") {
			return nil
		}
		if strings.Contains(p, sep+"subagents"+sep) {
			return nil
		}
		out = append(out, p)
		return nil
	})
	return out
}

// FindActiveTranscript returns the most-recently-modified main transcript for
// the given cwd, walking up to the nearest ancestor project (so it works from a
// subdirectory like ./ccpane). Fallback: scan all transcripts for an exact cwd.
func FindActiveTranscript(cwd string) string {
	dir := cwd
	for i := 0; i < 12; i++ {
		if p := newestJSONL(filepath.Join(ProjectsDir(), MangleCwd(dir))); p != "" {
			return p
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	var best string
	var bestMod int64
	for _, p := range AllTranscripts() {
		if peekCwd(p) != cwd {
			continue
		}
		if fi, err := os.Stat(p); err == nil && fi.ModTime().UnixNano() > bestMod {
			bestMod = fi.ModTime().UnixNano()
			best = p
		}
	}
	return best
}

// SubagentDir returns the directory holding a session's subagent transcripts.
func SubagentDir(mainPath string) string {
	return strings.TrimSuffix(mainPath, ".jsonl") + string(os.PathSeparator) + "subagents"
}

func newestJSONL(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var best string
	var bestMod int64
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().UnixNano() > bestMod {
			bestMod = info.ModTime().UnixNano()
			best = filepath.Join(dir, e.Name())
		}
	}
	return best
}

// peekCwd reads only the head of a file to discover its cwd cheaply.
func peekCwd(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	for n := 0; sc.Scan() && n < 200; n++ {
		var r Record
		if json.Unmarshal(sc.Bytes(), &r) == nil && r.Cwd != "" {
			return r.Cwd
		}
	}
	return ""
}
