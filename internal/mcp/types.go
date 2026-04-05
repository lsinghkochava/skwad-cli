package mcp

// JSON-RPC 2.0 envelope types.

// Request is an inbound JSON-RPC 2.0 call from an MCP client.
type Request struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// Response is an outbound JSON-RPC 2.0 reply.
type Response struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *RPCError   `json:"error,omitempty"`
}

// RPCError carries a JSON-RPC error code and message.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// MCP initialize / tools/list / tools/call method names.
const (
	MethodInitialize  = "initialize"
	MethodToolsList   = "tools/list"
	MethodToolsCall   = "tools/call"
	MethodPing        = "ping"
)

// Tool names exposed by the MCP server.
const (
	ToolRegisterAgent  = "register-agent"
	ToolListAgents     = "list-agents"
	ToolSendMessage    = "send-message"
	ToolCheckMessages  = "check-messages"
	ToolBroadcast      = "broadcast-message"
	ToolListRepos      = "list-repos"
	ToolListWorktrees  = "list-worktrees"
	ToolCreateAgent    = "create-agent"
	ToolCloseAgent     = "close-agent"
	ToolCreateWorktree = "create-worktree"
	ToolDisplayMD      = "display-markdown"
	ToolViewMermaid    = "view-mermaid"
	ToolSetStatus        = "set-status"
	ToolMergeBranches    = "merge-branches"
	ToolCreateTask       = "create-task"
	ToolListTasks        = "list-tasks"
	ToolClaimTask        = "claim-task"
	ToolCompleteTask     = "complete-task"
	ToolUpdateTask       = "update-task"
)

// ToolCallParams is the params block for a tools/call request.
type ToolCallParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

// ToolResult wraps the result of a tool call.
type ToolResult struct {
	Content []ContentBlock `json:"content"`
}

// ContentBlock is a text or image block in a tool result.
type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

func textResult(text string) ToolResult {
	return ToolResult{Content: []ContentBlock{{Type: "text", Text: text}}}
}

func errorResult(msg string) ToolResult {
	return ToolResult{Content: []ContentBlock{{Type: "text", Text: "Error: " + msg}}}
}

// --- Swift-compatible HTTP API types ---

// AgentInfoResponse is the JSON representation of an agent in API responses.
// It mirrors agent.AgentInfo but lives in the mcp package to avoid import cycles.
type AgentInfoResponse struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Folder       string `json:"folder"`
	Status       string `json:"status"`
	IsRegistered bool   `json:"isRegistered"`
}

// RegisterHookRequest is the payload for POST /api/v1/agent/register.
type RegisterHookRequest struct {
	AgentID   string                 `json:"agent_id"`
	Agent     string                 `json:"agent"`
	Source    string                 `json:"source"`
	SessionID string                `json:"session_id"`
	Payload   map[string]interface{} `json:"payload"`
}

// RegisterHookResponse is the response for POST /api/v1/agent/register.
type RegisterHookResponse struct {
	Success            bool                `json:"success"`
	Message            string              `json:"message"`
	UnreadMessageCount int                 `json:"unreadMessageCount"`
	SkwadMembers       []AgentInfoResponse `json:"skwadMembers"`
}

// StatusHookRequest is the payload for POST /api/v1/agent/status.
type StatusHookRequest struct {
	AgentID string                 `json:"agent_id"`
	Agent   string                 `json:"agent"`
	Hook    string                 `json:"hook"`
	Status  string                 `json:"status"`
	Payload map[string]interface{} `json:"payload"`
}

// --- REST API types for CLI client commands ---

// SendMessageRequest is the payload for POST /api/v1/agent/send.
type SendMessageRequest struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Content string `json:"content"`
}

// BroadcastMessageRequest is the payload for POST /api/v1/agent/broadcast.
type BroadcastMessageRequest struct {
	From    string `json:"from"`
	Content string `json:"content"`
}

// MessageResponse is the response for send/broadcast REST endpoints.
type MessageResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}
