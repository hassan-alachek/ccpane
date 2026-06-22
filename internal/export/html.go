// Package export renders a session transcript to a self-contained HTML file,
// styled to match the Hash-Sec admin dashboard (sharp, Sora/Source Code Pro,
// Lucide icons, blue accent).
package export

import (
	"encoding/json"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strings"

	"github.com/hassan-alachek/ccpane/internal/transcript"
)

// WriteHTML renders the transcript at path to a single self-contained HTML file
// and returns its absolute path. If outPath is empty a name is derived from the
// session title.
func WriteHTML(path string, window int, outPath string) (string, error) {
	recs, err := transcript.ParseFile(path)
	if err != nil {
		return "", err
	}
	st := transcript.Aggregate(recs)
	if window <= 0 {
		window = transcript.AutoWindow(st.MaxContext)
	}
	if outPath == "" {
		base := sanitize(transcript.SessionTitle(recs))
		if base == "" {
			base = "ccpane-export"
		}
		outPath = base + ".html"
	}
	abs, err := filepath.Abs(outPath)
	if err != nil {
		abs = outPath
	}
	doc := renderDoc(meta{
		Title:     transcript.SessionTitle(recs),
		Model:     st.Model,
		Cwd:       transcript.SessionCwd(recs),
		Branch:    transcript.SessionGitBranch(recs),
		Context:   st.ContextNow,
		Window:    window,
		OutTokens: st.OutputTokens,
		Cost:      st.EstCost(transcript.PricingFor(st.Model)),
		Turns:     st.Turns,
	}, transcript.FullTree(path))
	if err := os.WriteFile(abs, []byte(doc), 0o644); err != nil {
		return "", err
	}
	return abs, nil
}

type meta struct {
	Title, Model, Cwd, Branch         string
	Context, Window, OutTokens, Turns int
	Cost                              float64
}

func renderDoc(m meta, roots []*transcript.Node) string {
	var tree strings.Builder
	tree.WriteString(`<ul class="tree">`)
	for _, n := range roots {
		renderNode(&tree, n)
	}
	tree.WriteString(`</ul>`)

	fill := 0.0
	if m.Window > 0 {
		fill = float64(m.Context) / float64(m.Window) * 100
	}
	if fill > 100 {
		fill = 100
	}
	barColor := "var(--positive)"
	switch {
	case fill >= 85:
		barColor = "var(--negative)"
	case fill >= 60:
		barColor = "var(--warning)"
	}

	cwd := `<span class="ico"><i data-lucide="folder"></i></span>` + html.EscapeString(m.Cwd)
	if m.Branch != "" {
		cwd += `<span class="sepdot">·</span><span class="ico"><i data-lucide="git-branch"></i></span>` + html.EscapeString(m.Branch)
	}

	var d strings.Builder
	d.WriteString(`<!doctype html><html lang="en" data-theme="dark"><head><meta charset="utf-8">`)
	d.WriteString(`<meta name="viewport" content="width=device-width, initial-scale=1">`)
	fmt.Fprintf(&d, `<title>%s — ccpane</title>`, html.EscapeString(m.Title))
	d.WriteString(`<link rel="preconnect" href="https://fonts.googleapis.com"><link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>`)
	d.WriteString(`<link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=Sora:wght@300;400;600;700&family=Source+Code+Pro:wght@400;600;700&family=Space+Grotesk:wght@300;400;500&display=swap">`)
	d.WriteString(`<style>` + css + `</style></head><body><div class="page">`)

	// page header
	fmt.Fprintf(&d, `<div class="pagehead"><div class="phl"><div class="tagline">Claude Code · Session</div><h1>%s</h1><div class="cwd">%s</div></div>`,
		html.EscapeString(m.Title), cwd)
	fmt.Fprintf(&d, `<div class="headactions"><span class="badge model"><i data-lucide="sparkles"></i>%s</span><button class="iconbtn" onclick="toggleTheme()" title="Toggle theme"><i data-lucide="moon" id="themeicon"></i></button></div></div>`,
		html.EscapeString(shortModel(m.Model)))

	// KPI cards
	d.WriteString(`<div class="kpis">`)
	fmt.Fprintf(&d, `<div class="kpi"><div class="kpi-l">Context</div><div class="kpi-v" style="color:%s">%.0f%%</div><div class="kpibar"><div style="width:%.1f%%;background:%s"></div></div><div class="kpi-s">%s / %s</div></div>`,
		barColor, fill, fill, barColor, ftok(m.Context), ftok(m.Window))
	fmt.Fprintf(&d, `<div class="kpi"><div class="kpi-l">Output</div><div class="kpi-v">%s</div><div class="kpi-s">tokens generated</div></div>`, ftok(m.OutTokens))
	fmt.Fprintf(&d, `<div class="kpi"><div class="kpi-l">Est. cost</div><div class="kpi-v" style="color:var(--warning)">~$%.2f</div><div class="kpi-s">placeholder rates</div></div>`, m.Cost)
	fmt.Fprintf(&d, `<div class="kpi"><div class="kpi-l">Turns</div><div class="kpi-v">%d</div><div class="kpi-s">assistant turns</div></div>`, m.Turns)
	d.WriteString(`</div>`)

	// conversation panel
	d.WriteString(`<div class="panel"><div class="panelhead"><div class="paneltitle">Conversation</div><div class="toolbar">`)
	d.WriteString(`<div class="searchbox"><i data-lucide="search"></i><input id="q" type="search" placeholder="Search…" autocomplete="off"></div>`)
	d.WriteString(`<button class="btn" onclick="setAll(true)"><i data-lucide="unfold-vertical"></i>Expand</button>`)
	d.WriteString(`<button class="btn" onclick="setAll(false)"><i data-lucide="fold-vertical"></i>Collapse</button>`)
	d.WriteString(`</div></div><div class="panelbody custom-scrollbar">`)
	d.WriteString(tree.String())
	d.WriteString(`</div></div>`)

	d.WriteString(`<footer><span class="tagline">Generated by ccpane</span></footer></div>`)
	d.WriteString(`<script src="https://unpkg.com/lucide@latest/dist/umd/lucide.min.js"></script>`)
	d.WriteString(`<script>` + js + `</script></body></html>`)
	return d.String()
}

func renderNode(b *strings.Builder, n *transcript.Node) {
	hasKids := len(n.Children) > 0
	full := strings.TrimSpace(n.Full)

	fmt.Fprintf(b, `<li class="n-%s">`, n.Kind)
	if hasKids || full != "" {
		b.WriteString(`<details open><summary>` + nodeRow(n) + `</summary>`)
		if full != "" {
			renderContent(b, full, strings.TrimSpace(n.Raw))
		}
		if hasKids {
			b.WriteString(`<ul>`)
			for _, c := range n.Children {
				renderNode(b, c)
			}
			b.WriteString(`</ul>`)
		}
		b.WriteString(`</details>`)
	} else {
		b.WriteString(`<div class="row">` + nodeRow(n) + `</div>`)
	}
	b.WriteString(`</li>`)
}

func renderContent(b *strings.Builder, full, raw string) {
	jsonCls := ""
	if isJSONDoc(full) {
		jsonCls = " json"
	}
	if raw != "" && raw != full {
		b.WriteString(`<div class="content">`)
		b.WriteString(`<button class="rawtoggle" onclick="toggleRaw(this)"><i data-lucide="eye"></i><span>raw</span></button>`)
		fmt.Fprintf(b, `<pre class="full parsed%s">%s</pre>`, jsonCls, html.EscapeString(full))
		fmt.Fprintf(b, `<pre class="full raw" hidden>%s</pre>`, html.EscapeString(raw))
		b.WriteString(`</div>`)
		return
	}
	fmt.Fprintf(b, `<pre class="full%s">%s</pre>`, jsonCls, html.EscapeString(full))
}

func nodeRow(n *transcript.Node) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<span class="ic %s"><i data-lucide="%s"></i></span>`, iconClass(n), lucideIcon(n))
	if n.Label != "" {
		fmt.Fprintf(&b, `<span class="lbl">%s</span>`, html.EscapeString(n.Label))
	}
	if n.Detail != "" {
		fmt.Fprintf(&b, `<span class="dt">%s</span>`, html.EscapeString(n.Detail))
	}
	if n.Tokens > 0 {
		fmt.Fprintf(&b, `<span class="tk">+%s</span>`, ftok(n.Tokens))
	}
	return b.String()
}

func iconClass(n *transcript.Node) string {
	if n.Kind == "tool" {
		switch n.Icon {
		case transcript.IconToolErr:
			return "ic-err"
		case transcript.IconToolNone:
			return "ic-none"
		default:
			return "ic-tool"
		}
	}
	return "ic-" + n.Kind
}

// lucideIcon maps a node to the same Lucide icons used in the admin dashboard.
func lucideIcon(n *transcript.Node) string {
	switch n.Kind {
	case "user":
		return "user"
	case "assistant":
		return "sparkles"
	case "text":
		return "quote"
	case "subagent":
		return "bot"
	case "tool":
		switch n.Label {
		case "Bash":
			return "terminal"
		case "Read":
			return "file-text"
		case "Edit", "MultiEdit":
			return "file-pen-line"
		case "Write":
			return "file-plus"
		case "NotebookEdit":
			return "notebook-pen"
		case "Grep", "Glob":
			return "search"
		case "Task", "Agent":
			return "bot"
		case "WebFetch", "WebSearch":
			return "globe"
		case "TodoWrite":
			return "list-checks"
		case "Skill":
			return "wand-sparkles"
		default:
			return "wrench"
		}
	}
	return "circle"
}

func isJSONDoc(s string) bool {
	t := strings.TrimSpace(s)
	if t == "" || (t[0] != '{' && t[0] != '[') {
		return false
	}
	return json.Valid([]byte(t))
}

func shortModel(m string) string { return strings.TrimPrefix(m, "claude-") }

func ftok(n int) string {
	switch {
	case n >= 1_000_000_000:
		return fmt.Sprintf("%.1fB", float64(n)/1e9)
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1_000:
		return fmt.Sprintf("%dk", n/1000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

func sanitize(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 60 {
		out = out[:60]
	}
	return out
}

// css mirrors the Hash-Sec admin dark/light tokens (real app values) and sharp style.
const css = `
*{box-sizing:border-box}
:root,[data-theme="dark"]{color-scheme:dark;
--app-bg:#000;--shell-bg:#070707;--surface:#0a0a0a;
--surface-hover:rgba(255,255,255,.06);--surface-active:rgba(255,255,255,.10);
--line:rgba(255,255,255,.10);--line-strong:rgba(255,255,255,.16);
--fg-1:#fff;--fg-2:rgba(255,255,255,.78);--fg-3:rgba(255,255,255,.55);--fg-4:rgba(255,255,255,.38);--fg-5:rgba(255,255,255,.20);
--track:rgba(255,255,255,.08);
--positive:#56bb81;--negative:#f0899b;--warning:#ff8636;--info:#7badda;--purple:#c8b0e4;--lime:#9ac900}
[data-theme="light"]{color-scheme:light;
--app-bg:#f1f1f3;--shell-bg:#fafafb;--surface:#fff;
--surface-hover:rgba(17,24,39,.04);--surface-active:rgba(17,24,39,.07);
--line:rgba(17,24,39,.08);--line-strong:rgba(17,24,39,.16);
--fg-1:#1f2937;--fg-2:rgba(17,24,39,.72);--fg-3:rgba(17,24,39,.55);--fg-4:rgba(17,24,39,.40);--fg-5:rgba(17,24,39,.22);
--track:rgba(17,24,39,.06);
--positive:#0c7847;--negative:#a54c5e;--warning:#ba3b00;--info:#426d95;--purple:#76608f;--lime:#4d7400}
:root{--sans:'Sora',ui-sans-serif,system-ui,sans-serif;--mono:'Source Code Pro',ui-monospace,Menlo,monospace;--grotesk:'Space Grotesk',var(--sans)}
body{margin:0;background:var(--app-bg);color:var(--fg-2);font-family:var(--sans);line-height:1.55;-webkit-font-smoothing:antialiased}
.page{max-width:1120px;margin:0 auto;padding:28px clamp(16px,4vw,44px) 64px}
svg{width:1em;height:1em;stroke-width:2;display:block}
.tagline{font-family:var(--grotesk);font-weight:300;font-size:11px;letter-spacing:.15em;text-transform:uppercase;color:var(--fg-4)}
.pagehead{display:flex;justify-content:space-between;align-items:flex-start;gap:16px;padding-bottom:18px;border-bottom:1px solid var(--line);margin-bottom:20px}
h1{font-family:var(--sans);font-weight:600;font-size:23px;line-height:1.25;letter-spacing:-.01em;color:var(--fg-1);margin:6px 0 0}
.cwd{margin-top:10px;font-family:var(--mono);font-size:12px;color:var(--fg-4);display:flex;align-items:center;gap:6px;flex-wrap:wrap}
.cwd .ico,.badge i,.btn i,.searchbox i,.rawtoggle i,.iconbtn i{display:inline-flex;font-size:13px}
.sepdot{color:var(--fg-5);margin:0 2px}
.headactions{display:flex;align-items:center;gap:10px;flex:none}
.badge{display:inline-flex;align-items:center;gap:6px;font-family:var(--mono);font-size:11px;font-weight:600;letter-spacing:.03em;padding:5px 11px;border-radius:999px;border:1px solid var(--line-strong);background:var(--surface-active);color:var(--fg-1)}
.badge.model{color:var(--info);border-color:color-mix(in srgb,var(--info) 45%,transparent);background:color-mix(in srgb,var(--info) 12%,transparent)}
.iconbtn{display:inline-flex;align-items:center;justify-content:center;width:34px;height:34px;background:var(--surface);border:1px solid var(--line);color:var(--fg-2);cursor:pointer;font-size:15px}
.iconbtn:hover{border-color:var(--info);color:var(--info)}
.kpis{display:grid;grid-template-columns:repeat(4,1fr);gap:12px;margin-bottom:20px}
@media(max-width:720px){.kpis{grid-template-columns:repeat(2,1fr)}}
.kpi{background:var(--surface);border:1px solid var(--line);padding:14px 16px}
.kpi-l{font-family:var(--grotesk);font-size:10px;letter-spacing:.12em;text-transform:uppercase;color:var(--fg-4)}
.kpi-v{font-family:var(--sans);font-weight:700;font-size:25px;line-height:1.1;color:var(--fg-1);margin:7px 0}
.kpi-s{font-family:var(--mono);font-size:11px;color:var(--fg-4)}
.kpibar{height:6px;background:var(--track);margin:0 0 8px;overflow:hidden}
.kpibar>div{height:100%}
.panel{background:var(--surface);border:1px solid var(--line)}
.panelhead{display:flex;justify-content:space-between;align-items:center;gap:12px;padding:12px 14px;border-bottom:1px solid var(--line);flex-wrap:wrap}
.paneltitle{font-family:var(--grotesk);font-size:11px;letter-spacing:.14em;text-transform:uppercase;color:var(--fg-3)}
.toolbar{display:flex;align-items:center;gap:8px;flex-wrap:wrap}
.searchbox{display:flex;align-items:center;gap:7px;background:var(--app-bg);border:1px solid var(--line);padding:7px 10px;color:var(--fg-4)}
.searchbox:focus-within{border-color:var(--info)}
.searchbox input{background:none;border:0;outline:0;color:var(--fg-1);font-family:var(--mono);font-size:12px;width:170px}
.searchbox input::placeholder{color:var(--fg-5)}
.btn{display:inline-flex;align-items:center;gap:6px;font-family:var(--mono);font-size:11px;font-weight:700;letter-spacing:.06em;text-transform:uppercase;background:var(--app-bg);border:1px solid var(--line);color:var(--fg-2);padding:7px 11px;cursor:pointer}
.btn:hover{border-color:var(--info);color:var(--info)}
.panelbody{padding:10px 12px}
ul.tree,ul.tree ul{list-style:none;margin:0;padding:0}
ul.tree ul{margin-left:9px;padding-left:14px;border-left:1px solid var(--line)}
li{margin:1px 0}
summary,.row{display:flex;align-items:center;gap:8px;padding:4px 8px;cursor:pointer;white-space:nowrap;overflow:hidden;font-family:var(--mono);font-size:13px;color:var(--fg-2)}
summary{list-style:none}summary::-webkit-details-marker{display:none}
summary::before{content:"";border:solid var(--fg-4);border-width:0 1.5px 1.5px 0;padding:2.5px;transform:rotate(-45deg);transition:transform .12s;flex:none;margin-right:2px}
details[open]>summary::before{transform:rotate(45deg)}
.row::before{content:"";width:7px;flex:none}
summary:hover,.row:hover{background:var(--surface-hover)}
.ic{display:inline-flex;flex:none;font-size:15px}
.ic-user{color:var(--info)}.ic-assistant{color:var(--purple)}.ic-tool{color:var(--positive)}.ic-err{color:var(--negative)}.ic-none{color:var(--fg-4)}.ic-subagent{color:var(--lime)}.ic-text{color:var(--fg-4)}
.lbl{color:var(--fg-1);font-weight:600}
.n-assistant>details>summary .lbl,.n-text .lbl{color:var(--fg-4);font-weight:400}
.n-subagent .lbl{color:var(--lime)}
.dt{color:var(--fg-4);overflow:hidden;text-overflow:ellipsis}
.tk{color:var(--info);margin-left:auto;font-size:11px;flex:none}
.content{position:relative;margin:4px 0 8px 22px}
.content>pre.full{margin:0}
.rawtoggle{position:absolute;top:7px;right:7px;display:inline-flex;align-items:center;gap:5px;font-family:var(--mono);font-size:10px;text-transform:uppercase;letter-spacing:.06em;background:var(--surface);border:1px solid var(--line);color:var(--fg-3);padding:3px 7px;cursor:pointer;z-index:2}
.rawtoggle:hover{border-color:var(--info);color:var(--info)}
pre.full{font-family:var(--mono);font-size:12.5px;line-height:1.6;background:var(--shell-bg);border:1px solid var(--line);border-left:2px solid var(--info);margin:4px 0 8px 22px;padding:12px 14px;white-space:pre-wrap;word-break:break-word;max-height:460px;overflow:auto;color:var(--fg-2)}
.j-key{color:var(--info)}.j-str{color:var(--positive)}.j-num{color:var(--warning)}.j-bool{color:var(--purple)}.j-null{color:var(--fg-4)}
footer{margin-top:22px;text-align:center}
.hidden{display:none}
.custom-scrollbar{scrollbar-width:thin;scrollbar-color:var(--line-strong) transparent}
.custom-scrollbar::-webkit-scrollbar{width:10px;height:10px}
.custom-scrollbar::-webkit-scrollbar-thumb{background:var(--line-strong);border:3px solid transparent;background-clip:padding-box}
`

const js = `
function setAll(o){document.querySelectorAll('details').forEach(d=>d.open=o)}
var q=document.getElementById('q');
if(q)q.addEventListener('input',function(){var t=q.value.trim().toLowerCase();
 document.querySelectorAll('li').forEach(function(li){li.classList.toggle('hidden',!(!t||li.textContent.toLowerCase().includes(t)))});
 if(t)setAll(true)});
function toggleRaw(btn){var c=btn.parentElement,p=c.querySelector('.parsed'),r=c.querySelector('.raw'),s=btn.querySelector('span'),i=btn.querySelector('i');
 if(r.hasAttribute('hidden')){r.removeAttribute('hidden');p.setAttribute('hidden','');s.textContent='parsed';i.setAttribute('data-lucide','eye-off')}
 else{r.setAttribute('hidden','');p.removeAttribute('hidden');s.textContent='raw';i.setAttribute('data-lucide','eye')}
 lucide.createIcons()}
function toggleTheme(){var h=document.documentElement,l=h.getAttribute('data-theme')==='light';
 h.setAttribute('data-theme',l?'dark':'light');var ti=document.getElementById('themeicon');if(ti)ti.setAttribute('data-lucide',l?'moon':'sun');lucide.createIcons()}
function hl(j){j=j.replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;');
 return j.replace(/("(\\u[a-zA-Z0-9]{4}|\\[^u]|[^\\"])*"(\s*:)?|\b(true|false|null)\b|-?\d+(?:\.\d+)?(?:[eE][+\-]?\d+)?)/g,function(m){var c='j-num';if(/^"/.test(m)){c=/:$/.test(m)?'j-key':'j-str'}else if(/true|false/.test(m)){c='j-bool'}else if(/null/.test(m)){c='j-null'}return '<span class="'+c+'">'+m+'</span>'})}
document.querySelectorAll('pre.json').forEach(function(p){p.innerHTML=hl(p.textContent)});
if(window.lucide)lucide.createIcons();
`
