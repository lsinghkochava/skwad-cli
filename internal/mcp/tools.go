package mcp

import (
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/Jared-Boschmann/skwad-linux/internal/git"
	"github.com/Jared-Boschmann/skwad-linux/internal/models"
)

// CreateAgentRequest is the argument schema for the create-agent tool.
type CreateAgentRequest struct {
	Name         string `json:"name"`
	Folder       string `json:"folder"`
	AgentType    string `json:"agentType"`
	IsCompanion  bool   `json:"isCompanion"`
	CreatedByID  string `json:"createdById,omitempty"`
	NewWorktree  bool   `json:"newWorktree,omitempty"`
	BranchName   string `json:"branchName,omitempty"`
}

type toolDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

type toolHandler struct {
	server *Server
}

func newToolHandler(s *Server) *toolHandler { return &toolHandler{server: s} }

func (h *toolHandler) list() []toolDefinition {
	return []toolDefinition{
		{
			Name:        ToolRegisterAgent,
			Description: "Register this agent with Skwad. Returns member list and unread message count.",
			InputSchema: schema(map[string]interface{}{
				"agentId":   propString("The agent's UUID (from SKWAD_AGENT_ID env var)"),
				"name":      propString("Display name of this agent"),
				"folder":    propString("Working directory of this agent"),
				"sessionId": propString("Optional: current session ID"),
			}, "agentId", "name", "folder"),
		},
		{
			Name:        ToolListAgents,
			Description: "List all registered agents in the Skwad crew.",
			InputSchema: schema(nil),
		},
		{
			Name:        ToolSendMessage,
			Description: "Send a message to another agent by name or ID.",
			InputSchema: schema(map[string]interface{}{
				"to":      propString("Target agent name or ID"),
				"message": propString("Message content"),
			}, "to", "message"),
		},
		{
			Name:        ToolCheckMessages,
			Description: "Read inbox messages. Pass markRead=true to clear them.",
			InputSchema: schema(map[string]interface{}{
				"markRead": map[string]interface{}{"type": "boolean", "description": "Mark messages as read"},
			}),
		},
		{
			Name:        ToolBroadcast,
			Description: "Send a message to all registered agents.",
			InputSchema: schema(map[string]interface{}{
				"message": propString("Message content"),
			}, "message"),
		},
		{
			Name:        ToolListRepos,
			Description: "List all git repositories in the configured source folder.",
			InputSchema: schema(nil),
		},
		{
			Name:        ToolListWorktrees,
			Description: "List git worktrees for a given repo path.",
			InputSchema: schema(map[string]interface{}{
				"repoPath": propString("Absolute path to the git repository"),
			}, "repoPath"),
		},
		{
			Name:        ToolCreateAgent,
			Description: "Create a new agent in Skwad, optionally with a new git worktree.",
			InputSchema: schema(map[string]interface{}{
				"name":        propString("Display name"),
				"folder":      propString("Working directory"),
				"agentType":   propString("One of: claude, codex, opencode, gemini, copilot, shell"),
				"isCompanion": map[string]interface{}{"type": "boolean", "description": "Whether to create as companion"},
				"newWorktree": map[string]interface{}{"type": "boolean", "description": "Create a new git worktree"},
				"branchName":  propString("Branch name for new worktree"),
			}, "name", "folder"),
		},
		{
			Name:        ToolCloseAgent,
			Description: "Close an agent that was created by this caller.",
			InputSchema: schema(map[string]interface{}{
				"agentId": propString("ID of the agent to close"),
			}, "agentId"),
		},
		{
			Name:        ToolCreateWorktree,
			Description: "Create a new git worktree from a repository.",
			InputSchema: schema(map[string]interface{}{
				"repoPath":   propString("Absolute path to the git repository"),
				"branchName": propString("New branch name"),
				"destPath":   propString("Destination path for the worktree"),
			}, "repoPath", "branchName", "destPath"),
		},
		{
			Name:        ToolDisplayMD,
			Description: "Display a markdown file in the preview panel.",
			InputSchema: schema(map[string]interface{}{
				"filePath": propString("Absolute path to the markdown file"),
			}, "filePath"),
		},
		{
			Name:        ToolViewMermaid,
			Description: "Render a Mermaid diagram in the preview panel.",
			InputSchema: schema(map[string]interface{}{
				"source": propString("Mermaid diagram source"),
				"title":  propString("Optional diagram title"),
			}, "source"),
		},
		{
			Name:        ToolSetStatus,
			Description: "MANDATORY: Set your status so other agents know what you are doing. Call before starting any task, after completing it, and when changing direction. Keep it short and specific (e.g. 'Implementing auth module', 'Running tests', 'Done — PR ready'). Use empty string to clear.",
			InputSchema: schema(map[string]interface{}{
				"agentId":  propString("Your agent ID"),
				"status":   propString("Short status text describing what you are currently doing. Use empty string to clear."),
				"category": propString("The category of action you are about to perform. Predefined categories: code, test, explore, review, plan, delegate, coordinate. Custom categories are also accepted."),
			}, "agentId", "status"),
		},
	}
}

func (h *toolHandler) call(params ToolCallParams, sess *session) (ToolResult, error) {
	switch params.Name {
	case ToolRegisterAgent:
		return h.registerAgent(params.Arguments, sess)
	case ToolListAgents:
		return h.listAgents()
	case ToolSendMessage:
		return h.sendMessage(params.Arguments, sess)
	case ToolCheckMessages:
		return h.checkMessages(params.Arguments, sess)
	case ToolBroadcast:
		return h.broadcast(params.Arguments, sess)
	case ToolListRepos:
		return h.listRepos()
	case ToolListWorktrees:
		return h.listWorktrees(params.Arguments)
	case ToolCreateAgent:
		return h.createAgent(params.Arguments, sess)
	case ToolCloseAgent:
		return h.closeAgent(params.Arguments, sess)
	case ToolCreateWorktree:
		return h.createWorktree(params.Arguments)
	case ToolDisplayMD:
		return h.displayMarkdown(params.Arguments, sess)
	case ToolViewMermaid:
		return h.viewMermaid(params.Arguments, sess)
	case ToolSetStatus:
		return h.setStatus(params.Arguments)
	default:
		return errorResult("unknown tool: " + params.Name), nil
	}
}

func (h *toolHandler) registerAgent(args map[string]interface{}, sess *session) (ToolResult, error) {
	agentIDStr, _ := args["agentId"].(string)
	name, _ := args["name"].(string)
	folder, _ := args["folder"].(string)
	sessionID, _ := args["sessionId"].(string)

	agentID, err := uuid.Parse(agentIDStr)
	if err != nil {
		return errorResult("invalid agentId"), nil
	}

	sess.agentID = agentID
	members, unread, err := h.server.coordinator.RegisterAgent(agentID, name, folder, sessionID)
	if err != nil {
		return errorResult(err.Error()), nil
	}

	result := map[string]interface{}{
		"members":      members,
		"unreadCount":  unread,
		"message":      "Registered successfully",
	}
	data, _ := json.Marshal(result)
	return textResult(string(data)), nil
}

func (h *toolHandler) listAgents() (ToolResult, error) {
	agents := h.server.coordinator.ListAgents()
	data, _ := json.Marshal(agents)
	return textResult(string(data)), nil
}

func (h *toolHandler) sendMessage(args map[string]interface{}, sess *session) (ToolResult, error) {
	to, _ := args["to"].(string)
	msg, _ := args["message"].(string)
	if err := h.server.coordinator.SendMessage(sess.agentID, to, msg); err != nil {
		return errorResult(err.Error()), nil
	}
	return textResult("Message sent"), nil
}

func (h *toolHandler) checkMessages(args map[string]interface{}, sess *session) (ToolResult, error) {
	markRead, _ := args["markRead"].(bool)
	messages := h.server.coordinator.CheckMessages(sess.agentID, markRead)
	data, _ := json.Marshal(messages)
	return textResult(string(data)), nil
}

func (h *toolHandler) broadcast(args map[string]interface{}, sess *session) (ToolResult, error) {
	msg, _ := args["message"].(string)
	h.server.coordinator.BroadcastMessage(sess.agentID, msg)
	return textResult("Broadcast sent"), nil
}

func (h *toolHandler) listRepos() (ToolResult, error) {
	repos, err := h.server.store.RecentRepos()
	if err != nil {
		return errorResult(err.Error()), nil
	}
	data, _ := json.Marshal(repos)
	return textResult(string(data)), nil
}

func (h *toolHandler) listWorktrees(args map[string]interface{}) (ToolResult, error) {
	repoPath, _ := args["repoPath"].(string)
	if repoPath == "" {
		return errorResult("repoPath is required"), nil
	}
	wm := git.NewWorktreeManager(repoPath)
	trees, err := wm.List()
	if err != nil {
		return errorResult(fmt.Sprintf("git worktree list: %v", err)), nil
	}
	data, _ := json.Marshal(trees)
	return textResult(string(data)), nil
}

func (h *toolHandler) createAgent(args map[string]interface{}, sess *session) (ToolResult, error) {
	if h.server.OnCreateAgent == nil {
		return errorResult("create-agent not supported"), nil
	}
	req := CreateAgentRequest{
		Name:        strArg(args, "name"),
		Folder:      strArg(args, "folder"),
		AgentType:   strArg(args, "agentType"),
		BranchName:  strArg(args, "branchName"),
	}
	if v, ok := args["isCompanion"].(bool); ok {
		req.IsCompanion = v
	}
	if v, ok := args["newWorktree"].(bool); ok {
		req.NewWorktree = v
	}
	req.CreatedByID = sess.agentID.String()

	if err := h.server.OnCreateAgent(req); err != nil {
		return errorResult(err.Error()), nil
	}
	return textResult("Agent created"), nil
}

func (h *toolHandler) closeAgent(args map[string]interface{}, sess *session) (ToolResult, error) {
	targetID := strArg(args, "agentId")
	if h.server.OnCloseAgent == nil {
		return errorResult("close-agent not supported"), nil
	}
	if err := h.server.OnCloseAgent(sess.agentID.String(), targetID); err != nil {
		return errorResult(err.Error()), nil
	}
	return textResult("Agent closed"), nil
}

func (h *toolHandler) createWorktree(args map[string]interface{}) (ToolResult, error) {
	repoPath := strArg(args, "repoPath")
	branchName := strArg(args, "branchName")
	destPath := strArg(args, "destPath")
	if repoPath == "" || branchName == "" || destPath == "" {
		return errorResult("repoPath, branchName, and destPath are required"), nil
	}
	wm := git.NewWorktreeManager(repoPath)
	if err := wm.Create(branchName, destPath); err != nil {
		return errorResult(fmt.Sprintf("git worktree add: %v", err)), nil
	}
	result := map[string]string{"path": destPath, "branch": branchName}
	data, _ := json.Marshal(result)
	return textResult(string(data)), nil
}

func (h *toolHandler) displayMarkdown(args map[string]interface{}, sess *session) (ToolResult, error) {
	filePath := strArg(args, "filePath")
	if h.server.OnDisplayMarkdown != nil {
		h.server.OnDisplayMarkdown(sess.agentID.String(), filePath)
	}
	return textResult("Markdown displayed"), nil
}

func (h *toolHandler) viewMermaid(args map[string]interface{}, sess *session) (ToolResult, error) {
	source := strArg(args, "source")
	title := strArg(args, "title")
	if h.server.OnViewMermaid != nil {
		h.server.OnViewMermaid(sess.agentID.String(), source, title)
	}
	return textResult("Mermaid diagram displayed"), nil
}

func (h *toolHandler) setStatus(args map[string]interface{}) (ToolResult, error) {
	agentIDStr := strArg(args, "agentId")
	status := strArg(args, "status")
	category := strArg(args, "category")

	agentID, err := uuid.Parse(agentIDStr)
	if err != nil {
		return errorResult("invalid agentId"), nil
	}

	if _, ok := h.server.coordinator.Agent(agentID); !ok {
		return errorResult("agent not found: " + agentIDStr), nil
	}

	h.server.coordinator.SetStatusText(agentID, status, category)
	return textResult("Status updated"), nil
}

// --- schema helpers ---

func schema(props map[string]interface{}, required ...string) map[string]interface{} {
	s := map[string]interface{}{
		"type": "object",
	}
	if len(props) > 0 {
		s["properties"] = props
	}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

func propString(desc string) map[string]interface{} {
	return map[string]interface{}{"type": "string", "description": desc}
}

func strArg(args map[string]interface{}, key string) string {
	v, _ := args[key].(string)
	return v
}

// Ensure models import is used (AgentType validation).
var _ = models.AgentTypeClaude
