package agent

import (
	"fmt"
	"strings"

	"github.com/lsinghkochava/skwad-cli/internal/models"
)

// CommandBuilder constructs command-line arguments to launch headless agent processes.
type CommandBuilder struct {
	MCPServerURL string // e.g. "http://127.0.0.1:8777/mcp"
	PluginDir    string // path to hook scripts directory
}

// BuildArgs returns a slice of command-line arguments for launching a headless
// Claude agent. args[0] is the executable, args[1:] are flags.
// Only Claude agents are supported; other agent types return an error.
func (b *CommandBuilder) BuildArgs(a *models.Agent, persona *models.Persona, settings *models.AppSettings, teammates []models.Agent) ([]string, error) {
	if a.AgentType != models.AgentTypeClaude {
		return nil, fmt.Errorf("headless mode not supported for agent type: %s", a.AgentType)
	}

	args := []string{
		"claude", // executable — args[0] is used by exec.Command
		"-p",
		"--input-format", "stream-json",
		"--output-format", "stream-json",
		"--verbose",
	}

	// MCP config
	if b.MCPServerURL != "" {
		mcpConfig := fmt.Sprintf(`{"mcpServers":{"skwad":{"type":"http","url":"%s?agent=%s"}}}`, b.MCPServerURL, a.ID.String())
		args = append(args, "--mcp-config", mcpConfig)
		if a.ExploreMode {
			args = append(args, "--permission-mode", "plan")
			args = append(args, "--allowedTools", "mcp__skwad__*", "Read", "Glob", "Grep", "Agent", "WebSearch", "WebFetch")
		} else if len(a.AllowedTools) > 0 {
			// Custom allowed tools from team config — always include skwad MCP tools.
			args = append(args, "--permission-mode", "auto")
			toolArgs := []string{"mcp__skwad__*"}
			toolArgs = append(toolArgs, a.AllowedTools...)
			args = append(args, "--allowedTools")
			args = append(args, toolArgs...)
		} else {
			args = append(args, "--permission-mode", "auto")
			args = append(args, "--allowedTools", "mcp__skwad__*", "Read", "Write", "Edit", "Glob", "Grep", "Bash(*)", "Agent")
		}
	}

	// System prompt: preamble + team protocol + role instructions + persona
	systemPrompt := BuildSystemPrompt(a, persona, teammates)
	if persona != nil && len(persona.AllowedCategories) > 0 {
		systemPrompt += " When calling set-status, use one of these categories: " + strings.Join(persona.AllowedCategories, ", ") + "."
	}
	args = append(args, "--append-system-prompt", systemPrompt)

	// Model from ClaudeOptions (parse --model flag if present)
	if opts := settings.AgentTypeOptions.ClaudeOptions; opts != "" {
		if model := parseFlag(opts, "--model"); model != "" {
			args = append(args, "--model", model)
		}
	}

	// Resume session
	if a.ResumeSessionID != "" {
		args = append(args, "--session-id", a.ResumeSessionID)
	}

	// Working directory
	if a.Folder != "" {
		args = append(args, "--add-dir", a.Folder)
	}

	// Agent name
	args = append(args, "--name", a.Name)

	return args, nil
}

// parseFlag extracts the value of a named flag from a space-separated options string.
// Returns empty string if the flag is not found.
func parseFlag(opts, flag string) string {
	fields := strings.Fields(opts)
	for i, f := range fields {
		if f == flag && i+1 < len(fields) {
			return fields[i+1]
		}
	}
	return ""
}
