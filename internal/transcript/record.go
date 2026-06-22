package transcript

import "encoding/json"

// Record is one line of a Claude Code JSONL transcript. Different record
// "types" reuse the same struct; unused fields stay zero.
type Record struct {
	Type        string   `json:"type"`
	UUID        string   `json:"uuid"`
	ParentUUID  string   `json:"parentUuid"`
	SessionID   string   `json:"sessionId"`
	IsSidechain bool     `json:"isSidechain"`
	AgentID     string   `json:"agentId"`
	Cwd         string   `json:"cwd"`
	GitBranch   string   `json:"gitBranch"`
	Version     string   `json:"version"`
	Timestamp   string   `json:"timestamp"`
	Message     *Message `json:"message"`

	// metadata-record variants
	AiTitle     string `json:"aiTitle"`
	CustomTitle string `json:"customTitle"`
	LastPrompt  string `json:"lastPrompt"`
	Subtype     string `json:"subtype"`
}

// Message is the assistant/user message payload.
type Message struct {
	Role    string          `json:"role"`
	Model   string          `json:"model"`
	ID      string          `json:"id"`
	Content json.RawMessage `json:"content"`
	Usage   *Usage          `json:"usage"`
}

// Block is a single content block (text / tool_use / tool_result).
type Block struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	Content   json.RawMessage `json:"content"`
	ToolUseID string          `json:"tool_use_id"`
	IsError   bool            `json:"is_error"`
}

// Blocks decodes message.content, which may be a JSON array of blocks or a
// bare string (older / simple turns).
func (m *Message) Blocks() []Block {
	if m == nil || len(m.Content) == 0 {
		return nil
	}
	var blocks []Block
	if err := json.Unmarshal(m.Content, &blocks); err == nil {
		return blocks
	}
	var s string
	if err := json.Unmarshal(m.Content, &s); err == nil {
		return []Block{{Type: "text", Text: s}}
	}
	return nil
}

// ResultText extracts displayable text from a tool_result block, whose content
// may itself be a string or an array of {type,text} parts.
func (b Block) ResultText() string {
	if len(b.Content) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(b.Content, &s) == nil {
		return s
	}
	var parts []struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(b.Content, &parts) == nil {
		var out string
		for _, p := range parts {
			out += p.Text
		}
		return out
	}
	return ""
}
