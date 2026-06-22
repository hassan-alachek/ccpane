package transcript

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Node is a display node in the session tree.
type Node struct {
	Kind     string // user | assistant | tool | text | subagent
	Icon     string // glyph shown before the label
	Label    string // primary text (already shortened for the row)
	Detail   string // secondary (dimmed) text
	Full     string // complete content (JSON pretty-printed), for reader / export
	Raw      string // original unparsed content, when it differs from Full
	Tokens   int
	Children []*Node
	Expanded bool
}

// Glyphs used across the UI and renderers.
const (
	IconUser     = "●"
	IconAssist   = "◆"
	IconSubagent = "◈"
	IconThought  = "▏"
	IconToolOK   = "✓"
	IconToolErr  = "✗"
	IconToolNone = "◦"
)

// FullTree builds the complete display forest for a transcript, including a
// collapsible group of subagent subtrees. Shared by the TUI and the exporter.
func FullTree(path string) []*Node {
	recs, err := ParseFile(path)
	if err != nil {
		return nil
	}
	roots := BuildDisplayTree(recs)
	if subs := SubagentTrees(path); len(subs) > 0 {
		roots = append(roots, &Node{
			Kind:     "subagent",
			Icon:     IconSubagent,
			Label:    fmt.Sprintf("subagents (%d)", len(subs)),
			Children: subs,
		})
	}
	return roots
}

// BuildDisplayTree turns a flat record list into a renderable timeline.
func BuildDisplayTree(records []*Record) []*Node {
	results := map[string]Block{}
	for _, r := range records {
		if r.Message == nil {
			continue
		}
		for _, b := range r.Message.Blocks() {
			if b.Type == "tool_result" && b.ToolUseID != "" {
				results[b.ToolUseID] = b
			}
		}
	}

	var nodes []*Node
	prevUUID := ""
	for _, r := range records {
		if r.Message == nil {
			continue
		}
		blocks := r.Message.Blocks()

		switch r.Type {
		case "user":
			var text string
			onlyResults := true
			for _, b := range blocks {
				if b.Type == "text" {
					text += b.Text
				}
				if b.Type != "tool_result" {
					onlyResults = false
				}
			}
			if onlyResults {
				prevUUID = r.UUID
				continue
			}
			label := firstLine(text, 140)
			if r.ParentUUID != "" && prevUUID != "" && r.ParentUUID != prevUUID {
				label = "⑂ " + label
			}
			nodes = append(nodes, &Node{Kind: "user", Icon: IconUser, Label: label, Full: text, Expanded: true})

		case "assistant":
			out := 0
			if r.Message.Usage != nil {
				out = r.Message.Usage.OutputTokens
			}
			n := &Node{Kind: "assistant", Icon: IconAssist, Label: shortModel(r.Message.Model), Tokens: out, Expanded: true}
			var thoughts string
			for _, b := range blocks {
				switch b.Type {
				case "text":
					thoughts += b.Text
				case "tool_use":
					res := results[b.ID]
					disp, raw := fullToolText(b.Input, res)
					tn := &Node{
						Kind:   "tool",
						Icon:   toolStatus(res),
						Label:  b.Name,
						Detail: summarizeInput(b.Name, b.Input),
						Full:   disp,
					}
					if raw != disp {
						tn.Raw = raw
					}
					n.Children = append(n.Children, tn)
				}
			}
			if t := strings.TrimSpace(thoughts); t != "" {
				n.Children = append([]*Node{{Kind: "text", Icon: IconThought, Detail: firstLine(t, 140), Full: t}}, n.Children...)
			}
			nodes = append(nodes, n)
		}
		prevUUID = r.UUID
	}
	return nodes
}

// SubagentTrees loads each subagent transcript into its own subtree.
func SubagentTrees(mainPath string) []*Node {
	dir := SubagentDir(mainPath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []*Node
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		recs, err := ParseFile(filepath.Join(dir, e.Name()))
		if err != nil || len(recs) == 0 {
			continue
		}
		st := Aggregate(recs)
		id := strings.TrimSuffix(strings.TrimPrefix(e.Name(), "agent-"), ".jsonl")
		prompt := firstUserText(recs)
		out = append(out, &Node{
			Kind:     "subagent",
			Icon:     IconSubagent,
			Label:    id,
			Detail:   firstLine(prompt, 80),
			Full:     prompt,
			Tokens:   st.OutputTokens,
			Children: BuildDisplayTree(recs),
		})
	}
	return out
}

// RenderTreeLines renders a tree as plain text with box-drawing connectors
// (used by --stat). All children shown regardless of Expanded.
func RenderTreeLines(nodes []*Node) []string {
	var out []string
	var walk func(ns []*Node, prefix string, depth int)
	walk = func(ns []*Node, prefix string, depth int) {
		for i, n := range ns {
			last := i == len(ns)-1
			branch := ""
			if depth > 0 {
				if last {
					branch = "└─ "
				} else {
					branch = "├─ "
				}
			}
			caret := "  "
			if len(n.Children) > 0 {
				caret = "▾ "
			}
			icon := n.Icon
			if icon == "" {
				icon = " "
			}
			line := prefix + branch + caret + icon
			if n.Label != "" {
				line += " " + n.Label
			}
			if n.Detail != "" {
				line += "  " + n.Detail
			}
			if n.Tokens > 0 {
				line += fmt.Sprintf("  +%dtok", n.Tokens)
			}
			out = append(out, strings.TrimRight(line, " "))

			childPrefix := prefix
			if depth > 0 {
				if last {
					childPrefix = prefix + "   "
				} else {
					childPrefix = prefix + "│  "
				}
			}
			walk(n.Children, childPrefix, depth+1)
		}
	}
	walk(nodes, "", 0)
	return out
}

func toolStatus(res Block) string {
	if res.ToolUseID == "" {
		return IconToolNone
	}
	if res.IsError {
		return IconToolErr
	}
	return IconToolOK
}

// fullToolText renders a tool's input and result twice: a display version with
// JSON pretty-printed, and a raw version. They differ only when JSON was found.
func fullToolText(input json.RawMessage, res Block) (display, raw string) {
	var disp, rawp []string
	if len(input) > 0 {
		r := strings.TrimSpace(string(input))
		rawp = append(rawp, r)
		if p, ok := prettyJSON(r); ok {
			disp = append(disp, p)
		} else {
			disp = append(disp, r)
		}
	}
	if rt := strings.TrimSpace(res.ResultText()); rt != "" {
		rt = capStr(rt, 50000)
		rawp = append(rawp, "── result ──\n"+rt)
		if p, ok := prettyJSON(rt); ok {
			disp = append(disp, "── result ──\n"+p)
		} else {
			disp = append(disp, "── result ──\n"+rt)
		}
	}
	return strings.Join(disp, "\n\n"), strings.Join(rawp, "\n\n")
}

// prettyJSON indents a JSON object/array string; ok is false if it isn't JSON.
func prettyJSON(s string) (string, bool) {
	t := strings.TrimSpace(s)
	if t == "" || (t[0] != '{' && t[0] != '[') {
		return s, false
	}
	var buf bytes.Buffer
	if json.Indent(&buf, []byte(t), "", "  ") != nil {
		return s, false
	}
	return buf.String(), true
}

func capStr(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "\n… (truncated)"
}

// summarizeInput picks the most informative field from a tool's input.
func summarizeInput(name string, raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return ""
	}
	pick := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := m[k]; ok {
				return fmt.Sprintf("%v", v)
			}
		}
		return ""
	}
	switch name {
	case "Bash":
		return firstLine(pick("command"), 90)
	case "Read", "Edit", "Write", "NotebookEdit":
		return pick("file_path", "notebook_path")
	case "Grep", "Glob":
		return pick("pattern")
	case "Task", "Agent":
		return pick("description", "subagent_type")
	case "WebFetch", "WebSearch":
		return pick("url", "query")
	}
	for _, v := range m {
		if s, ok := v.(string); ok && len(s) < 100 {
			return firstLine(s, 80)
		}
	}
	return ""
}
