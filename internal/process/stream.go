package process

import "encoding/json"

// StreamMessage is the top-level message from Claude CLI stdout stream.
// Every line is a JSON object with a "type" field.
type StreamMessage struct {
	Type    string          `json:"type"`
	Subtype string          `json:"subtype,omitempty"`
	Raw     json.RawMessage `json:"-"` // original JSON bytes for passthrough
}

// InitMessage is a "system" type with subtype "init".
type InitMessage struct {
	Type           string   `json:"type"`
	Subtype        string   `json:"subtype"`
	CWD            string   `json:"cwd"`
	SessionID      string   `json:"session_id"`
	Tools          []string `json:"tools"`
	Model          string   `json:"model"`
	PermissionMode string   `json:"permissionMode"`
	Version        string   `json:"claude_code_version"`
	UUID           string   `json:"uuid"`
}

// AssistantMessage is an "assistant" type.
type AssistantMessage struct {
	Type            string         `json:"type"`
	Message         MessageContent `json:"message"`
	ParentToolUseID *string        `json:"parent_tool_use_id"`
	SessionID       string         `json:"session_id"`
	UUID            string         `json:"uuid"`
}

// MessageContent holds the nested message structure inside an AssistantMessage.
type MessageContent struct {
	Model      string          `json:"model"`
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"` // []ContentBlock — can be text or tool_use
	StopReason *string         `json:"stop_reason"`
	Usage      *Usage          `json:"usage,omitempty"`
}

// ContentBlock represents a single block in the assistant message content array.
type ContentBlock struct {
	Type  string          `json:"type"` // "text" or "tool_use"
	Text  string          `json:"text,omitempty"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

// Usage tracks token consumption for a message.
type Usage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

// ResultMessage is a "result" type with subtype "success" or "error".
type ResultMessage struct {
	Type       string  `json:"type"`
	Subtype    string  `json:"subtype"`
	IsError    bool    `json:"is_error"`
	DurationMS int     `json:"duration_ms"`
	NumTurns   int     `json:"num_turns"`
	Result     string  `json:"result"`
	StopReason string  `json:"stop_reason"`
	SessionID  string  `json:"session_id"`
	TotalCost  float64 `json:"total_cost_usd"`
	UUID       string  `json:"uuid"`
}

// UserInputMessage is what we send to Claude via stdin.
type UserInputMessage struct {
	Type    string      `json:"type"`
	Message UserMessage `json:"message"`
}

// UserMessage holds the role and content for a user input.
type UserMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// NewUserInput creates a properly formatted stdin message.
func NewUserInput(prompt string) UserInputMessage {
	return UserInputMessage{
		Type: "user",
		Message: UserMessage{
			Role:    "user",
			Content: prompt,
		},
	}
}
