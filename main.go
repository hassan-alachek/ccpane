package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	"github.com/hassan-alachek/ccpane/internal/export"
	"github.com/hassan-alachek/ccpane/internal/transcript"
	"github.com/hassan-alachek/ccpane/internal/ui"
)

// Build info, injected via -ldflags at release time.
var (
	version = "dev"
	commit  = ""
	date    = ""
)

func versionString() string {
	s := version
	if commit != "" {
		s += " (" + commit + ")"
	}
	if date != "" {
		s += " " + date
	}
	return s
}

func main() {
	browse := flag.Bool("browse", false, "browse all Claude Code sessions on this machine")
	browseShort := flag.Bool("b", false, "shorthand for --browse")
	stat := flag.Bool("stat", false, "print to stdout instead of launching the TUI")
	file := flag.String("f", "", "open a specific transcript .jsonl file")
	limit := flag.Int("limit", 0, "context-window size for the bar (0 = auto-detect 200k/1M)")
	dir := flag.String("dir", "", "directory whose active session to open (default: cwd)")
	exportPath := flag.String("export", "", "export the session tree to an HTML file and exit")
	render := flag.String("render", "", "debug: render a view at WxH to stdout (e.g. 120x40)")
	query := flag.String("q", "", "debug: pre-fill the browser search filter")
	statsFlag := flag.Bool("stats", false, "show usage stats and graphs")
	memFlag := flag.Bool("memory", false, "browse project auto-memories")
	flag.BoolVar(memFlag, "m", false, "shorthand for --memory")
	showVer := flag.Bool("version", false, "print version and exit")
	flag.BoolVar(showVer, "v", false, "print version (shorthand)")
	flag.Parse()

	if *showVer {
		fmt.Println("ccpane", versionString())
		return
	}

	if *statsFlag {
		if *render != "" {
			w, h := parseWH(*render)
			rd, _ := strconv.Atoi(*query)
			ui.DebugStats(*limit, w, h, rd)
			return
		}
		fail(ui.RunStats(*limit))
		return
	}
	if *memFlag {
		if *render != "" {
			w, h := parseWH(*render)
			ui.DebugMemory(*limit, w, h, *query)
			return
		}
		fail(ui.RunMemory(*limit))
		return
	}

	if *browse || *browseShort {
		switch {
		case *render != "":
			w, h := parseWH(*render)
			ui.DebugBrowser(*limit, w, h, *query)
		case *stat:
			ui.PrintBrowserStat(*limit)
		default:
			target, err := ui.RunBrowser(*limit)
			fail(err)
			if target != nil {
				resume(target)
			}
		}
		return
	}

	path := *file
	if path == "" {
		cwd := *dir
		if cwd == "" {
			cwd, _ = os.Getwd()
		}
		path = transcript.FindActiveTranscript(cwd)
		if path == "" {
			fmt.Fprintln(os.Stderr, "ccpane: no Claude Code session found for", cwd)
			fmt.Fprintln(os.Stderr, "try:    ccpane --browse")
			os.Exit(1)
		}
	}

	switch {
	case *exportPath != "":
		out, err := export.WriteHTML(path, *limit, *exportPath)
		fail(err)
		fmt.Println("exported →", out)
	case *render != "":
		w, h := parseWH(*render)
		ui.DebugPane(path, *limit, w, h)
	case *stat:
		ui.PrintSessionStat(path, *limit)
	default:
		fail(ui.RunPane(path, *limit))
	}
}

// resume replaces this process with `claude --resume <id>` in the session's
// original directory, so a session can be resumed from anywhere.
func resume(s *transcript.Session) {
	bin, err := exec.LookPath("claude")
	if err != nil {
		fmt.Fprintln(os.Stderr, "ccpane: 'claude' not found on PATH")
		os.Exit(1)
	}
	if s.Project != "" {
		_ = os.Chdir(s.Project)
	}
	args := []string{"claude", "--resume", s.SessionID}
	if err := syscall.Exec(bin, args, os.Environ()); err != nil {
		fmt.Fprintln(os.Stderr, "ccpane: resume failed:", err)
		os.Exit(1)
	}
}

func parseWH(s string) (int, int) {
	parts := strings.Split(strings.ToLower(s), "x")
	if len(parts) != 2 {
		return 120, 40
	}
	w, _ := strconv.Atoi(strings.TrimSpace(parts[0]))
	h, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
	if w <= 0 {
		w = 120
	}
	if h <= 0 {
		h = 40
	}
	return w, h
}

func fail(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "ccpane:", err)
		os.Exit(1)
	}
}
