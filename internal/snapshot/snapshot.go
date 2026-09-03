// Package snapshot archives Claude Code's transcripts into a timestamped
// tar.gz.
//
// Claude Code deletes transcripts older than cleanupPeriodDays (30 by default),
// so a session's full record is temporary unless something copies it out. This
// only ever reads: nothing is moved or deleted, and the archive is written
// outside ~/.claude so a later sweep of that directory cannot reach it.
package snapshot

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/hassan-alachek/ccpane/internal/transcript"
)

// Result describes a completed snapshot.
type Result struct {
	Path       string // absolute path of the archive
	Files      int    // transcripts archived
	Raw        int64  // uncompressed bytes
	Size       int64  // archive size on disk
	Sessions   int    // main transcripts (excludes subagent files)
	Subagents  int    // subagent transcripts
	Oldest     string // earliest transcript day covered
	Newest     string // latest transcript day covered
	StatsCache bool   // whether Claude Code's cumulative stats cache was included
}

// manifest is written into the archive so its provenance survives with it.
type manifest struct {
	Tool      string `json:"tool"`
	Version   string `json:"ccpaneVersion"`
	Created   string `json:"created"`
	Source    string `json:"source"`
	Host      string `json:"host,omitempty"`
	OS        string `json:"os"`
	Files     int    `json:"files"`
	Sessions  int    `json:"sessions"`
	Subagents int    `json:"subagents"`
	RawBytes  int64  `json:"rawBytes"`
	Oldest    string `json:"oldestDay,omitempty"`
	Newest    string `json:"newestDay,omitempty"`
	Note      string `json:"note"`
}

// DefaultDir is where snapshots land when no output path is given: the
// platform's user data directory, deliberately outside ~/.claude so Claude
// Code's retention sweep can never remove them.
func DefaultDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "ccpane-snapshots"
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "ccpane", "snapshots")
	case "windows":
		if d := os.Getenv("LOCALAPPDATA"); d != "" {
			return filepath.Join(d, "ccpane", "snapshots")
		}
	default:
		if d := os.Getenv("XDG_DATA_HOME"); d != "" {
			return filepath.Join(d, "ccpane", "snapshots")
		}
	}
	return filepath.Join(home, ".local", "share", "ccpane", "snapshots")
}

// Create archives every transcript into a tar.gz and returns where it landed.
//
// out may be empty (use DefaultDir), a directory, or an explicit *.tar.gz path.
// version is stamped into the archive manifest.
func Create(out, version string) (Result, error) {
	var r Result
	src := transcript.ProjectsDir()
	if _, err := os.Stat(src); err != nil {
		return r, fmt.Errorf("no transcripts at %s: %w", src, err)
	}

	dest, err := resolveDest(out)
	if err != nil {
		return r, err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return r, err
	}

	// Write to a temp file first so an interrupted run leaves no half archive
	// that looks like a usable backup.
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".ccpane-snapshot-*.tmp")
	if err != nil {
		return r, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once renamed

	gz := gzip.NewWriter(tmp)
	tw := tar.NewWriter(gz)

	files, err := collect(src)
	if err != nil {
		tw.Close()
		gz.Close()
		tmp.Close()
		return r, err
	}
	for _, f := range files {
		rel, err := filepath.Rel(src, f)
		if err != nil {
			continue
		}
		n, err := addFile(tw, f, path.Join("projects", filepath.ToSlash(rel)))
		if err != nil {
			continue // a transcript deleted mid-run is expected, not fatal
		}
		r.Files++
		r.Raw += n
		if strings.Contains(filepath.ToSlash(rel), "/subagents/") {
			r.Subagents++
		} else {
			r.Sessions++
		}
	}
	r.Oldest, r.Newest = span(files)

	// Claude Code's cumulative stats outlive the transcripts it deletes, so the
	// archive is more complete with them alongside.
	if home, err := os.UserHomeDir(); err == nil {
		cache := filepath.Join(home, ".claude", "stats-cache.json")
		if _, err := addFile(tw, cache, "claude/stats-cache.json"); err == nil {
			r.StatsCache = true
		}
	}

	m := manifest{
		Tool: "ccpane", Version: version,
		Created: time.Now().Format(time.RFC3339), Source: src,
		OS: runtime.GOOS, Files: r.Files, Sessions: r.Sessions,
		Subagents: r.Subagents, RawBytes: r.Raw,
		Oldest: r.Oldest, Newest: r.Newest,
		Note: "Read-only snapshot of Claude Code transcripts. Nothing was deleted from disk.",
	}
	m.Host, _ = os.Hostname()
	if b, err := json.MarshalIndent(m, "", "  "); err == nil {
		writeBytes(tw, "ccpane-snapshot.json", b)
	}

	if err := tw.Close(); err != nil {
		tmp.Close()
		return r, err
	}
	if err := gz.Close(); err != nil {
		tmp.Close()
		return r, err
	}
	if err := tmp.Close(); err != nil {
		return r, err
	}
	if err := os.Rename(tmpName, dest); err != nil {
		return r, err
	}
	if fi, err := os.Stat(dest); err == nil {
		r.Size = fi.Size()
	}
	r.Path = dest
	return r, nil
}

// resolveDest turns the -o value into an absolute archive path.
func resolveDest(out string) (string, error) {
	name := "ccpane-transcripts-" + time.Now().Format("20060102-150405") + ".tar.gz"
	switch {
	case out == "":
		return filepath.Join(DefaultDir(), name), nil
	case strings.HasSuffix(out, ".tar.gz") || strings.HasSuffix(out, ".tgz"):
		return filepath.Abs(out)
	default:
		return filepath.Abs(filepath.Join(out, name))
	}
}

// collect lists every transcript under the projects directory.
func collect(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".jsonl") {
			return nil
		}
		out = append(out, p)
		return nil
	})
	return out, err
}

// span returns the first and last day covered, read from the file names' mtimes
// rather than parsing every transcript.
func span(files []string) (oldest, newest string) {
	for _, f := range files {
		fi, err := os.Stat(f)
		if err != nil {
			continue
		}
		d := fi.ModTime().UTC().Format("2006-01-02")
		if oldest == "" || d < oldest {
			oldest = d
		}
		if newest == "" || d > newest {
			newest = d
		}
	}
	return oldest, newest
}

// addFile copies one file into the archive under name, returning its size.
func addFile(tw *tar.Writer, src, name string) (int64, error) {
	fi, err := os.Stat(src)
	if err != nil || !fi.Mode().IsRegular() {
		return 0, fmt.Errorf("skip %s", src)
	}
	f, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	hdr := &tar.Header{
		Name: name, Mode: 0o600, Size: fi.Size(),
		ModTime: fi.ModTime(), Typeflag: tar.TypeReg,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return 0, err
	}
	n, err := io.Copy(tw, f)
	if err != nil {
		return 0, err
	}
	return n, nil
}

// writeBytes adds an in-memory file to the archive.
func writeBytes(tw *tar.Writer, name string, b []byte) {
	hdr := &tar.Header{
		Name: name, Mode: 0o600, Size: int64(len(b)),
		ModTime: time.Now(), Typeflag: tar.TypeReg,
	}
	if tw.WriteHeader(hdr) == nil {
		tw.Write(b)
	}
}
