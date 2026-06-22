package transcript

import "strings"

// SessionCwd returns the first non-empty cwd recorded in the transcript.
func SessionCwd(recs []*Record) string {
	for _, r := range recs {
		if r.Cwd != "" {
			return r.Cwd
		}
	}
	return ""
}

// SessionGitBranch returns the first non-empty git branch recorded.
func SessionGitBranch(recs []*Record) string {
	for _, r := range recs {
		if r.GitBranch != "" {
			return r.GitBranch
		}
	}
	return ""
}

// SessionTitle resolves a display title: custom title > AI title > first user
// prompt > "(untitled)".
func SessionTitle(recs []*Record) string {
	var custom, ai string
	for _, r := range recs {
		switch r.Type {
		case "custom-title":
			if r.CustomTitle != "" {
				custom = r.CustomTitle
			}
		case "ai-title":
			if r.AiTitle != "" {
				ai = r.AiTitle
			}
		}
	}
	if custom != "" {
		return custom
	}
	if ai != "" {
		return ai
	}
	if t := firstUserText(recs); t != "" {
		return firstLine(t, 60)
	}
	return "(untitled)"
}

func firstUserText(recs []*Record) string {
	for _, r := range recs {
		if r.Type != "user" || r.Message == nil {
			continue
		}
		for _, b := range r.Message.Blocks() {
			if b.Type == "text" && strings.TrimSpace(b.Text) != "" {
				return b.Text
			}
		}
	}
	return ""
}

// firstLine returns the first line of s, collapsed and truncated to n runes.
func firstLine(s string, n int) string {
	s = strings.ReplaceAll(s, "\t", " ")
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// shortModel trims the "claude-" prefix for compact display.
func shortModel(m string) string {
	return strings.TrimPrefix(m, "claude-")
}
