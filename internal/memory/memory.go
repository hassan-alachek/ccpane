// Package memory discovers and parses Claude Code's per-project auto-memory
// (https://code.claude.com/docs/en/memory.md): a `memory/` directory under each
// project in ~/.claude/projects, containing a MEMORY.md index plus topic files.
// Topic files are plain markdown; in practice they carry YAML frontmatter
// (name/description/metadata.type) which we parse when present and ignore when not.
package memory

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hassan-alachek/ccpane/internal/transcript"
)

// Memory is a single parsed topic file.
type Memory struct {
	Path        string
	Name        string // frontmatter name, else filename
	Description string
	Type        string // feedback | project | reference | user | "" (unknown)
	Body        string // markdown after frontmatter
	Raw         string // full file content
}

// Project groups the memory files for one project directory.
type Project struct {
	Dir        string // the memory/ directory
	ProjectDir string // its parent (mangled project dir)
	Cwd        string // real cwd, for display
	Index      string // MEMORY.md contents (raw)
	Memories   []*Memory
	Mtime      int64
}

// Projects scans ~/.claude/projects for memory directories, newest first.
func Projects() []*Project {
	root := projectsDir()
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []*Project
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pdir := filepath.Join(root, e.Name())
		mdir := filepath.Join(pdir, "memory")
		fi, err := os.Stat(mdir)
		if err != nil || !fi.IsDir() {
			continue
		}
		p := &Project{Dir: mdir, ProjectDir: pdir, Cwd: transcript.CwdForProjectDir(pdir), Mtime: fi.ModTime().UnixNano()}
		files, _ := os.ReadDir(mdir)
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".md") {
				continue
			}
			full := filepath.Join(mdir, f.Name())
			if f.Name() == "MEMORY.md" {
				if b, err := os.ReadFile(full); err == nil {
					p.Index = string(b)
				}
				continue
			}
			if mem := parseFile(full); mem != nil {
				p.Memories = append(p.Memories, mem)
			}
			if info, err := f.Info(); err == nil && info.ModTime().UnixNano() > p.Mtime {
				p.Mtime = info.ModTime().UnixNano()
			}
		}
		sort.SliceStable(p.Memories, func(i, j int) bool {
			if p.Memories[i].Type != p.Memories[j].Type {
				return typeRank(p.Memories[i].Type) < typeRank(p.Memories[j].Type)
			}
			return p.Memories[i].Name < p.Memories[j].Name
		})
		if len(p.Memories) > 0 || strings.TrimSpace(p.Index) != "" {
			out = append(out, p)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Mtime > out[j].Mtime })
	return out
}

// Label returns a human-readable project name (real cwd, else the dir name).
func (p *Project) Label() string {
	if p.Cwd != "" {
		return p.Cwd
	}
	return filepath.Base(p.ProjectDir)
}

// TypeCounts returns how many memories of each type the project has.
func (p *Project) TypeCounts() map[string]int {
	m := map[string]int{}
	for _, x := range p.Memories {
		t := x.Type
		if t == "" {
			t = "other"
		}
		m[t]++
	}
	return m
}

func parseFile(path string) *Memory {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	raw := string(data)
	m := &Memory{Path: path, Raw: raw, Name: strings.TrimSuffix(filepath.Base(path), ".md")}
	body := raw

	if strings.HasPrefix(raw, "---") {
		rest := raw[len("---"):]
		if i := strings.Index(rest, "\n---"); i >= 0 {
			fm := rest[:i]
			body = strings.TrimPrefix(rest[i+len("\n---"):], "\n")
			for _, ln := range strings.Split(fm, "\n") {
				t := strings.TrimSpace(ln)
				switch {
				case strings.HasPrefix(t, "name:"):
					if v := unquote(strings.TrimSpace(t[len("name:"):])); v != "" {
						m.Name = v
					}
				case strings.HasPrefix(t, "description:"):
					m.Description = unquote(strings.TrimSpace(t[len("description:"):]))
				case strings.HasPrefix(t, "type:"): // metadata.type (node_type: won't match)
					m.Type = unquote(strings.TrimSpace(t[len("type:"):]))
				}
			}
		}
	}
	m.Body = strings.TrimSpace(body)
	return m
}

func unquote(s string) string { return strings.Trim(s, "\"'") }

func typeRank(t string) int {
	switch t {
	case "project":
		return 0
	case "feedback":
		return 1
	case "reference":
		return 2
	case "user":
		return 3
	}
	return 4
}

func projectsDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "projects")
}
