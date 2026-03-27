package agent

import (
	"fmt"
	"strings"

	"github.com/lsinghkochava/skwad-cli/internal/models"
)

// CommandBuilder constructs the shell command string to launch an agent terminal.
type CommandBuilder struct {
	MCPServerURL string // e.g. "http://127.0.0.1:8777/mcp"
	PluginDir    string // path to hook scripts directory
}

// Build returns the full shell command to run for the given agent.
// The command is prefixed with a space to suppress shell history (HISTCONTROL=ignorespace).
func (b *CommandBuilder) Build(a *models.Agent, persona *models.Persona, settings *models.AppSettings) string {
	var parts []string

	// cd to folder
	parts = append(parts, fmt.Sprintf("cd %s", shellQuote(a.Folder)))
	parts = append(parts, "clear")

	// Inject agent ID into environment.
	envPrefix := fmt.Sprintf("SKWAD_AGENT_ID=%s", a.ID.String())

	cmd := b.agentCommand(a, persona, settings)
	parts = append(parts, envPrefix+" "+cmd)

	// Prefix the whole pipeline with a space to avoid history pollution.
	return " " + strings.Join(parts, " && ")
}

func (b *CommandBuilder) agentCommand(a *models.Agent, persona *models.Persona, settings *models.AppSettings) string {
	switch a.AgentType {
	case models.AgentTypeClaude:
		return b.claudeCommand(a, persona, settings)
	case models.AgentTypeCodex:
		return b.codexCommand(a, persona, settings)
	case models.AgentTypeOpenCode:
		return b.openCodeCommand(a, settings)
	case models.AgentTypeGemini:
		return b.geminiCommand(a, settings)
	case models.AgentTypeCopilot:
		return b.copilotCommand(a, settings)
	case models.AgentTypeCustom1:
		return b.customCommand(settings.AgentTypeOptions.Custom1Command, settings.AgentTypeOptions.Custom1Options)
	case models.AgentTypeCustom2:
		return b.customCommand(settings.AgentTypeOptions.Custom2Command, settings.AgentTypeOptions.Custom2Options)
	case models.AgentTypeShell:
		if a.ShellCommand != "" {
			return a.ShellCommand
		}
		return "$SHELL"
	default:
		return "$SHELL"
	}
}

func (b *CommandBuilder) claudeCommand(a *models.Agent, persona *models.Persona, settings *models.AppSettings) string {
	var sb strings.Builder
	sb.WriteString("claude")

	// MCP config
	if b.MCPServerURL != "" {
		mcpConfig := fmt.Sprintf(`{"mcpServers":{"skwad":{"type":"http","url":"%s"}}}`, b.MCPServerURL)
		sb.WriteString(fmt.Sprintf(" --mcp-config %s", shellQuote(mcpConfig)))
		sb.WriteString(" --allowed-tools 'mcp__skwad__*'")
	}

	// Hook plugin dir
	if b.PluginDir != "" {
		sb.WriteString(fmt.Sprintf(" --plugin-dir %s", shellQuote(b.PluginDir)))
	}

	// Resume / fork
	if a.ResumeSessionID != "" {
		if a.IsFork {
			sb.WriteString(fmt.Sprintf(" --resume %s --fork-session", a.ResumeSessionID))
		} else {
			sb.WriteString(fmt.Sprintf(" --resume %s", a.ResumeSessionID))
		}
	}

	// System prompt: skwad instructions + persona
	systemPrompt := skwadInstructions(a.ID.String())
	if persona != nil {
		systemPrompt += " " + personaPrompt(persona)
	}
	sb.WriteString(" --append-system-prompt " + shellEscapeDouble(systemPrompt))

	// Extra user options
	if opts := settings.AgentTypeOptions.ClaudeOptions; opts != "" {
		sb.WriteString(" " + opts)
	}

	// Initial user prompt for new sessions
	if a.IsNewSession() {
		sb.WriteString(" " + shellEscapeDouble(registrationUserPrompt))
	}

	return sb.String()
}

func (b *CommandBuilder) codexCommand(a *models.Agent, persona *models.Persona, settings *models.AppSettings) string {
	var sb strings.Builder
	sb.WriteString("codex")

	if a.ResumeSessionID != "" {
		if a.IsFork {
			sb.WriteString(fmt.Sprintf(" fork %s", a.ResumeSessionID))
		} else {
			sb.WriteString(fmt.Sprintf(" resume %s", a.ResumeSessionID))
		}
	}

	if persona != nil && persona.Instructions != "" {
		sb.WriteString(fmt.Sprintf(" -c %s", shellQuote("developer_instructions="+persona.Instructions)))
	}

	if b.PluginDir != "" {
		notifyScript := b.PluginDir + "/codex-notify.sh"
		sb.WriteString(fmt.Sprintf(` -c %s`, shellQuote(fmt.Sprintf(`notify=["bash","%s"]`, notifyScript))))
	}

	if opts := settings.AgentTypeOptions.CodexOptions; opts != "" {
		sb.WriteString(" " + opts)
	}

	return sb.String()
}

func (b *CommandBuilder) openCodeCommand(a *models.Agent, settings *models.AppSettings) string {
	cmd := "opencode"
	if opts := settings.AgentTypeOptions.OpenCodeOptions; opts != "" {
		cmd += " " + opts
	}
	return cmd
}

func (b *CommandBuilder) geminiCommand(a *models.Agent, settings *models.AppSettings) string {
	cmd := "gemini"
	if a.ResumeSessionID != "" {
		cmd += " --resume " + a.ResumeSessionID
	}
	if b.MCPServerURL != "" {
		cmd += " --allowed-mcp-server-names skwad"
	}
	if opts := settings.AgentTypeOptions.GeminiOptions; opts != "" {
		cmd += " " + opts
	}
	return cmd
}

func (b *CommandBuilder) copilotCommand(a *models.Agent, settings *models.AppSettings) string {
	cmd := "gh copilot"
	if a.ResumeSessionID != "" {
		cmd += " --interactive"
	}
	if opts := settings.AgentTypeOptions.CopilotOptions; opts != "" {
		cmd += " " + opts
	}
	return cmd
}

func (b *CommandBuilder) customCommand(command, options string) string {
	if command == "" {
		return "$SHELL"
	}
	if options != "" {
		return command + " " + options
	}
	return command
}

const registrationUserPrompt = `List other agents names and project (no ID) in a table based on context then set your status to indicate you are ready to get going!`

func skwadInstructions(agentID string) string {
	return "You are part of a team of agents called a skwad. A skwad is made of high-performing agents who collaborate to achieve complex goals so engage with them: ask for help and in return help them succeed. Your skwad agent ID: " + agentID + ". CRITICAL RULE: Before you start working on anything, your FIRST action must be calling set-status with what you are about to do. When you finish, call set-status again. When you change direction, call set-status. Other agents depend on your status to coordinate — if you do not update it, the team cannot function. This is not optional. When you need help with exploration, coding, testing, or review, prefer coordinating with your skwad agents over spinning up local subagents. Your teammates are already running and have shared context — use send-message to delegate work to them."
}

func personaPrompt(persona *models.Persona) string {
	return "You are asked to impersonate " + persona.Name + " based on the following instructions: " + persona.Instructions
}

// shellQuote wraps s in single quotes, escaping any single quotes within.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// shellEscapeDouble wraps s in double quotes, escaping special characters.
func shellEscapeDouble(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, `$`, `\$`)
	s = strings.ReplaceAll(s, "`", "\\`")
	s = strings.ReplaceAll(s, "!", `\!`)
	return `"` + s + `"`
}
