package mcp

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/lsinghkochava/skwad-cli/internal/git"
	"github.com/lsinghkochava/skwad-cli/internal/models"
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
			Name:        ToolMergeBranches,
			Description: "Merge agent worktree branches into a consolidation branch. Operates in its own worktree — main repo checkout is not modified.",
			InputSchema: schema(map[string]interface{}{
				"repoPath":          propString("Absolute path to the git repository"),
				"branches":          propString("Comma-separated branch names to merge (default: all skwad/* branches)"),
				"consolidateBranch": propString("Name for consolidation branch (default: skwad/consolidate)"),
			}, "repoPath"),
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
		{
			Name:        ToolCreateTask,
			Description: "Create a new task for the team. Tasks can have dependencies on other tasks.",
			InputSchema: schema(map[string]interface{}{
				"title":        propString("Task title (required)"),
				"description":  propString("Task description (required)"),
				"dependencies": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Optional list of task IDs this task depends on"},
			}, "title", "description"),
		},
		{
			Name:        ToolListTasks,
			Description: "List all tasks. Optionally filter by status or assignee.",
			InputSchema: schema(map[string]interface{}{
				"status":   propString("Filter by status: pending, in_progress, completed, blocked (optional)"),
				"assignee": propString("Filter by assignee name or ID (optional)"),
			}),
		},
		{
			Name:        ToolClaimTask,
			Description: "Claim an unassigned pending task. Sets you as the assignee and marks it in-progress.",
			InputSchema: schema(map[string]interface{}{
				"taskId": propString("The task ID to claim"),
			}, "taskId"),
		},
		{
			Name:        ToolCompleteTask,
			Description: "Mark a task as completed. You must be the assignee.",
			InputSchema: schema(map[string]interface{}{
				"taskId": propString("The task ID to complete"),
			}, "taskId"),
		},
		{
			Name:        ToolUpdateTask,
			Description: "Update a task's title or description. You must be the creator or assignee.",
			InputSchema: schema(map[string]interface{}{
				"taskId":      propString("The task ID to update"),
				"title":       propString("New title (optional)"),
				"description": propString("New description (optional)"),
			}, "taskId"),
		},
	}
}

func (h *toolHandler) call(params ToolCallParams, sess *session) (ToolResult, error) {
	var result ToolResult
	var err error

	switch params.Name {
	case ToolRegisterAgent:
		result, err = h.registerAgent(params.Arguments, sess)
	case ToolListAgents:
		result, err = h.listAgents()
	case ToolSendMessage:
		result, err = h.sendMessage(params.Arguments, sess)
	case ToolCheckMessages:
		result, err = h.checkMessages(params.Arguments, sess)
	case ToolBroadcast:
		result, err = h.broadcast(params.Arguments, sess)
	case ToolListRepos:
		result, err = h.listRepos()
	case ToolListWorktrees:
		result, err = h.listWorktrees(params.Arguments)
	case ToolCreateAgent:
		result, err = h.createAgent(params.Arguments, sess)
	case ToolCloseAgent:
		result, err = h.closeAgent(params.Arguments, sess)
	case ToolCreateWorktree:
		result, err = h.createWorktree(params.Arguments)
	case ToolDisplayMD:
		result, err = h.displayMarkdown(params.Arguments, sess)
	case ToolViewMermaid:
		result, err = h.viewMermaid(params.Arguments, sess)
	case ToolMergeBranches:
		result, err = h.mergeBranches(params.Arguments)
	case ToolSetStatus:
		result, err = h.setStatus(params.Arguments)
	case ToolCreateTask:
		result, err = h.createTask(params.Arguments, sess)
	case ToolListTasks:
		result, err = h.listTasks(params.Arguments)
	case ToolClaimTask:
		result, err = h.claimTask(params.Arguments, sess)
	case ToolCompleteTask:
		result, err = h.completeTask(params.Arguments, sess)
	case ToolUpdateTask:
		result, err = h.updateTask(params.Arguments, sess)
	default:
		return errorResult("unknown tool: " + params.Name), nil
	}

	agentID := sess.agentID.String()
	agentName := ""
	if a, ok := h.server.coordinator.Agent(sess.agentID); ok {
		agentName = a.Name
	}

	// Fallback: extract agent identity from tool arguments when session has zero agentID.
	if sess.agentID == uuid.Nil {
		if idStr, ok := params.Arguments["agentId"].(string); ok {
			agentID = idStr
			if parsed, err := uuid.Parse(idStr); err == nil {
				if a, ok := h.server.coordinator.Agent(parsed); ok {
					agentName = a.Name
				}
			}
		} else if fromStr, ok := params.Arguments["from"].(string); ok {
			agentID = fromStr
			if parsed, err := uuid.Parse(fromStr); err == nil {
				if a, ok := h.server.coordinator.Agent(parsed); ok {
					agentName = a.Name
				}
			}
		}
	}

	if h.server.OnToolCall != nil {
		h.server.OnToolCall(agentID, agentName, params.Name, params.Arguments, result)
	}

	if h.server.OnToolCallLog != nil {
		preview := formatArgsPreview(params.Arguments, 80)
		h.server.OnToolCallLog(agentName, params.Name, preview)
	}

	return result, err
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

	a, ok := h.server.coordinator.Agent(agentID)
	if !ok {
		return errorResult("agent not found: " + agentIDStr), nil
	}

	h.server.coordinator.SetStatusText(agentID, status, category)

	// Soft warning if category is outside the persona's allowed list.
	if category != "" && a.PersonaID != nil {
		if persona := h.server.coordinator.Persona(*a.PersonaID); persona != nil && len(persona.AllowedCategories) > 0 {
			allowed := false
			for _, c := range persona.AllowedCategories {
				if c == category {
					allowed = true
					break
				}
			}
			if !allowed {
				slog.Warn("agent status category outside persona scope",
					"agent", a.Name, "category", category, "allowed", persona.AllowedCategories)
				return textResult(fmt.Sprintf("Status updated (warning: category '%s' is outside this agent's scope)", category)), nil
			}
		}
	}

	return textResult("Status updated"), nil
}

func (h *toolHandler) mergeBranches(args map[string]interface{}) (ToolResult, error) {
	repoPath := strArg(args, "repoPath")
	if repoPath == "" {
		return errorResult("repoPath is required"), nil
	}

	consolidateBranch := strArg(args, "consolidateBranch")
	if consolidateBranch == "" {
		consolidateBranch = "skwad/consolidate"
	}

	branchesStr := strArg(args, "branches")
	var branches []string
	if branchesStr != "" {
		for _, b := range strings.Split(branchesStr, ",") {
			if trimmed := strings.TrimSpace(b); trimmed != "" {
				branches = append(branches, trimmed)
			}
		}
	} else {
		cli := &git.CLI{RepoPath: repoPath}
		lines, err := cli.RunLines("branch", "--list", "skwad/*")
		if err != nil {
			return errorResult(fmt.Sprintf("list branches: %v", err)), nil
		}
		for _, line := range lines {
			b := strings.TrimSpace(strings.TrimPrefix(line, "*"))
			b = strings.TrimSpace(b)
			if b != "" && !strings.HasSuffix(b, "/consolidate") {
				branches = append(branches, b)
			}
		}
	}

	if len(branches) == 0 {
		return textResult("No skwad agent branches found"), nil
	}

	cli := &git.CLI{RepoPath: repoPath}
	base, err := cli.Run("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return errorResult(fmt.Sprintf("detect base branch: %v", err)), nil
	}

	result, err := git.Consolidate(repoPath, strings.TrimSpace(base), branches, consolidateBranch)
	if err != nil {
		return errorResult(fmt.Sprintf("consolidation failed: %v", err)), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Consolidated into %s\n\n", result.Branch))
	for _, b := range result.MergedFrom {
		sb.WriteString(fmt.Sprintf("✓ %s — merged\n", b))
	}
	for _, b := range result.Skipped {
		sb.WriteString(fmt.Sprintf("✗ %s — SKIPPED (conflict: %s)\n", b, result.ConflictDetails[b]))
	}
	sb.WriteString(fmt.Sprintf("\n%d/%d branches merged", len(result.MergedFrom), len(branches)))

	return textResult(sb.String()), nil
}

func (h *toolHandler) createTask(args map[string]interface{}, sess *session) (ToolResult, error) {
	title := strArg(args, "title")
	description := strArg(args, "description")

	var deps []uuid.UUID
	if depsRaw, ok := args["dependencies"].([]interface{}); ok {
		for _, d := range depsRaw {
			if s, ok := d.(string); ok {
				id, err := uuid.Parse(s)
				if err != nil {
					return errorResult("invalid dependency ID: " + s), nil
				}
				deps = append(deps, id)
			}
		}
	}

	task, err := h.server.coordinator.CreateTask(sess.agentID, title, description, deps)
	if err != nil {
		return errorResult(err.Error()), nil
	}
	data, _ := json.Marshal(task)
	return textResult(string(data)), nil
}

func (h *toolHandler) listTasks(args map[string]interface{}) (ToolResult, error) {
	tasks := h.server.coordinator.ListTasks()

	status := strArg(args, "status")
	assignee := strArg(args, "assignee")

	if status != "" || assignee != "" {
		var filtered []*models.Task
		for _, t := range tasks {
			if status != "" && string(t.Status) != status {
				continue
			}
			if assignee != "" && t.AssigneeName != assignee {
				if t.AssigneeID == nil || t.AssigneeID.String() != assignee {
					continue
				}
			}
			filtered = append(filtered, t)
		}
		tasks = filtered
	}

	data, _ := json.Marshal(tasks)
	return textResult(string(data)), nil
}

func (h *toolHandler) claimTask(args map[string]interface{}, sess *session) (ToolResult, error) {
	taskIDStr := strArg(args, "taskId")
	taskID, err := uuid.Parse(taskIDStr)
	if err != nil {
		return errorResult("invalid task ID: " + taskIDStr), nil
	}
	if err := h.server.coordinator.ClaimTask(sess.agentID, taskID); err != nil {
		return errorResult(err.Error()), nil
	}
	return textResult("Task claimed"), nil
}

func (h *toolHandler) completeTask(args map[string]interface{}, sess *session) (ToolResult, error) {
	taskIDStr := strArg(args, "taskId")
	taskID, err := uuid.Parse(taskIDStr)
	if err != nil {
		return errorResult("invalid task ID: " + taskIDStr), nil
	}
	if err := h.server.coordinator.CompleteTask(sess.agentID, taskID); err != nil {
		return errorResult(err.Error()), nil
	}
	return textResult("Task completed"), nil
}

func (h *toolHandler) updateTask(args map[string]interface{}, sess *session) (ToolResult, error) {
	taskIDStr := strArg(args, "taskId")
	taskID, err := uuid.Parse(taskIDStr)
	if err != nil {
		return errorResult("invalid task ID: " + taskIDStr), nil
	}
	title := strArg(args, "title")
	description := strArg(args, "description")
	if err := h.server.coordinator.UpdateTask(sess.agentID, taskID, title, description); err != nil {
		return errorResult(err.Error()), nil
	}
	return textResult("Task updated"), nil
}

// formatArgsPreview builds a concise key=value summary of tool arguments for display.
// Skips agentId/from fields (agent name is shown separately). Sorted alphabetically.
func formatArgsPreview(args map[string]interface{}, maxLen int) string {
	skipKeys := map[string]bool{"agentId": true, "from": true}
	keys := make([]string, 0, len(args))
	for k := range args {
		if !skipKeys[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	var result string
	for i, k := range keys {
		v := fmt.Sprintf("%v", args[k])
		if len(v) > 30 {
			v = v[:30] + "…"
		}
		pair := fmt.Sprintf("%s=%q", k, v)
		if i > 0 {
			pair = ", " + pair
		}
		if len(result)+len(pair) > maxLen {
			result += "…"
			break
		}
		result += pair
	}
	return result
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
