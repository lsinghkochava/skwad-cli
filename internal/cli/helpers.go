package cli

import (
	"github.com/google/uuid"
	"github.com/lsinghkochava/skwad-cli/internal/config"
	"github.com/lsinghkochava/skwad-cli/internal/daemon"
	"github.com/lsinghkochava/skwad-cli/internal/models"
)

// createAgentsFromConfig maps AgentConfig entries to models.Agent, adds them
// to the daemon's Manager, and returns the created agents.
func createAgentsFromConfig(d *daemon.Daemon, tc *config.TeamConfig) []*models.Agent {
	agents := make([]*models.Agent, 0, len(tc.Agents))
	for _, ac := range tc.Agents {
		a := &models.Agent{
			ID:        uuid.New(),
			Name:      ac.Name,
			AgentType: mapAgentType(ac.AgentType),
			Folder:    tc.Repo,
			Avatar:    defaultAvatar(ac.AgentType),
		}
		if ac.Command != "" {
			a.ShellCommand = ac.Command
		}
		d.Manager.AddAgent(a, nil)
		agents = append(agents, a)
	}
	return agents
}

// mapAgentType converts a config string to models.AgentType.
func mapAgentType(s string) models.AgentType {
	switch s {
	case "claude":
		return models.AgentTypeClaude
	case "codex":
		return models.AgentTypeCodex
	case "gemini":
		return models.AgentTypeGemini
	case "copilot":
		return models.AgentTypeCopilot
	case "opencode":
		return models.AgentTypeOpenCode
	case "custom":
		return models.AgentTypeCustom1
	default:
		return models.AgentTypeClaude
	}
}

// defaultAvatar returns a default emoji for an agent type.
func defaultAvatar(agentType string) string {
	switch agentType {
	case "claude":
		return "🟠"
	case "codex":
		return "🟢"
	case "gemini":
		return "🔵"
	case "copilot":
		return "⚪"
	default:
		return "🤖"
	}
}
