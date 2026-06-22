# ccpane

A terminal companion for Claude Code. One Go binary, three things:

1. **Live pane** — context-usage gauge + a collapsible, connector-drawn tree of
   the current session (turns, tool calls, nested subagents). Auto-tracks the
   active session and refreshes live. Built for a tmux split pane.
2. **Session browser** — every session on the machine, **grouped by directory**,
   collapsible, **searchable**, with token + **cost** columns. **Resume** any
   session from anywhere with one key.
3. **HTML export** — render a session's tree to a self-contained, aesthetically
   designed, searchable HTML file you can share.

It reads Claude Code's JSONL transcripts under `~/.claude/projects/`; it never
modifies Claude Code (whose TUI is not extensible).

## Install

**macOS / Linux** (prebuilt binary):

```sh
curl -fsSL https://raw.githubusercontent.com/hassan-alachek/ccpane/main/install.sh | sh
```

**Windows** (PowerShell):

```powershell
irm https://raw.githubusercontent.com/hassan-alachek/ccpane/main/install.ps1 | iex
```

The installer detects your OS/arch, downloads the matching release asset,
verifies its checksum, and puts `ccpane` on your PATH. Overrides:
`CCPANE_VERSION=v1.2.3` (pin a version), `CCPANE_INSTALL_DIR=~/bin` (custom dir).

**From source** (needs Go 1.24+):

```sh
go install github.com/hassan-alachek/ccpane@latest   # or, in a clone: go install .
```

## Usage

```sh
ccpane                 # live pane for the active session in the current dir
ccpane -b              # browse every session, grouped by directory
ccpane -stats          # usage stats & graphs (filter: 7d / 30d / 60d / all)
ccpane -m              # browse project auto-memories and inspect them
ccpane -f FILE.jsonl   # open a specific transcript
ccpane -export out.html [-f FILE]   # write the shareable HTML tree and exit
ccpane -stat           # print the session (summary + tree) as text; no TUI
ccpane -b -stat        # print the grouped session index as text

# flags
-limit N   context-window size for the bar. 0 = auto-detect 200k vs 1M (default)
-dir PATH  directory whose active session to open (default: current dir)
```

### Open your current session

Run `ccpane` from the same directory Claude Code is in — it locks onto the
live session automatically. As a tmux split pane beside Claude Code:

```sh
tmux split-window -h -l 40% -c "#{pane_current_path}" "ccpane"
```

Bind it to **prefix + Ctrl-p** in `~/.tmux.conf`:

```tmux
bind C-p split-window -h -l 40% -c "#{pane_current_path}" "ccpane"
```

### Keys

**Pane / session detail**
- `↑/↓` `j/k` move · `←/→` collapse/expand · `space` fold · `g/G` ends
- `enter` **read full text** of the selected node (scrollable; nothing is cut off);
  in the reader, `t` toggles **parsed ⇄ raw** for JSON content
- `e` **export** this session to HTML · `r` reload (pane) · `q` quit

**Browser**
- `↑/↓` move · `enter` open a session / toggle a directory group
- `r` **resume** the selected session in Claude Code (cd's into its original dir first)
- `e` **export** the selected session to HTML
- `s` **stats** · `m` **memories** · `/` **search** · `esc` clear · `q` quit

**Stats** (`ccpane -stats`, or `s` from the browser)
- `1` 7d · `2` 30d · `3` 60d · `4` all time · `tab` cycle · `↑/↓` scroll · `q` back
- Token counts use k/M/**B** notation; overview, token composition, a 30/60/90-day
  activity sparkline, and top-projects + models bar charts.

**Memory** (`ccpane -m`, or `m` from the browser)
- Browses Claude Code's per-project auto-memory (`~/.claude/projects/<project>/memory/`).
- `↑/↓` move · `enter` drill in (project → memory file → rendered markdown)
- `r` toggle **rendered ⇄ raw** · `esc` up a level · `q` back

## How features work

- **Context fill** = the latest assistant turn's `input + cache_read +
  cache_creation` tokens. The **window auto-detects**: if a session ever
  exceeded 200k it's treated as a 1M-context (beta) session, since the model id
  in transcripts doesn't carry the `[1m]` flag. Override with `-limit`.
- **Resume from anywhere**: `r` exits ccpane and `exec`s `claude --resume <id>`
  after `cd`-ing to the session's recorded directory — so it resumes in the
  right project even if you started ccpane elsewhere.
- **Cost** is an **estimate**. Rates are in `internal/transcript/tokens.go`
  (`DefaultPricing`); edit them to match current model pricing.
- **Index cache** at `~/.claude/ccpane-index.v2.json`, keyed by file
  mtime+size. First scan of all sessions takes a few seconds; after that it's
  ~instant. (Kept as JSON rather than SQLite: at this scale it's already
  <20ms and keeps the binary cgo-free and portable.)
- **HTML export** is one file styled like the Hash-Sec admin dashboard: sharp
  surface cards, Sora / Source Code Pro / Space Grotesk, **Lucide** icons (per
  tool), a KPI header (context / output / cost / turns), collapsible `<details>`
  tree, live search, expand/collapse-all, and a **light/dark** toggle. JSON
  content (e.g. tool telemetry) is **pretty-printed + syntax-highlighted**, with
  a per-block **raw ⇄ parsed** toggle. CSS+JS are inline; fonts and the Lucide
  script load from a CDN (needs internet for those two).

## Limitations

- Branches (from rewinds) render as a timeline with a `⑂` marker, not as
  separate visual branches.
- Subagents are loaded from `<session>/subagents/` and shown as their own
  subtrees; not yet linked to the exact `Task` call that spawned them.
- The live pane re-reads the whole transcript on change (fine at these sizes).

## Layout

```
main.go                       flags + dispatch + resume exec
internal/transcript/          parsing core (no UI deps)
  record, parse, tokens, tree, session, locate, util
internal/ui/                  Bubble Tea layer
  pane, browser, sessionview, treeview, report, styles, uiutil, debug
internal/export/html.go       self-contained HTML exporter
```
