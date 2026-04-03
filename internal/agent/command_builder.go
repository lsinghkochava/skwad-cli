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
func (b *CommandBuilder) BuildArgs(a *models.Agent, persona *models.Persona, settings *models.AppSettings) ([]string, error) {
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
		mcpConfig := fmt.Sprintf(`{"mcpServers":{"skwad":{"type":"http","url":"%s"}}}`, b.MCPServerURL)
		args = append(args, "--mcp-config", mcpConfig)
		args = append(args, "--allowed-tools", "mcp__skwad__*")
	}

	// System prompt: skwad instructions + persona
	systemPrompt := skwadInstructions(a.ID.String())
	if persona != nil {
		systemPrompt += " " + personaPrompt(persona)
	}
	args = append(args, "--append-system-prompt", systemPrompt)

	// Permission mode
	args = append(args, "--permission-mode", "auto")

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

func skwadInstructions(agentID string) string {
	return "You are part of a team of agents called a skwad. A skwad is made of high-performing agents who collaborate to achieve complex goals so engage with them: ask for help and in return help them succeed. Your skwad agent ID: " + agentID + ". CRITICAL RULE: Before you start working on anything, your FIRST action must be calling set-status with what you are about to do. When you finish, call set-status again. When you change direction, call set-status. Other agents depend on your status to coordinate — if you do not update it, the team cannot function. This is not optional. When you need help with exploration, coding, testing, or review, prefer coordinating with your skwad agents over spinning up local subagents. Your teammates are already running and have shared context — use send-message to delegate work to them."
}

func personaPrompt(persona *models.Persona) string {
	return "You are asked to impersonate " + persona.Name + " based on the following instructions: " + persona.Instructions
}
