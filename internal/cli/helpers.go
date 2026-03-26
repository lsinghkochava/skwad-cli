package cli

import (
	"strings"

	"github.com/google/uuid"
	"github.com/lsinghkochava/skwad-cli/internal/config"
	"github.com/lsinghkochava/skwad-cli/internal/daemon"
	"github.com/lsinghkochava/skwad-cli/internal/models"
)

// createAgentsFromConfig maps AgentConfig entries to models.Agent, adds them
// to the daemon's Manager, and returns the created agents.
func createAgentsFromConfig(d *daemon.Daemon, tc *config.TeamConfig) []*models.Agent {
	// Load available personas (defaults + any user-saved).
	personas, _ := d.Store.LoadPersonas()

	agents := make([]*models.Agent, 0, len(tc.Agents))
	for _, ac := range tc.Agents {
		avatar := ac.Avatar
		if avatar == "" {
			avatar = defaultAvatar(ac.AgentType)
		}
		a := &models.Agent{
			ID:        uuid.New(),
			Name:      ac.Name,
			AgentType: mapAgentType(ac.AgentType),
			Folder:    tc.Repo,
			Avatar:    avatar,
		}
		if ac.Command != "" {
			a.ShellCommand = ac.Command
		}

		// Resolve persona — priority order:
		// 1. persona_instructions (inline instructions, highest priority)
		// 2. persona_id (UUID reference)
		// 3. persona (name match)
		// 4. team-level personas[] matching agent name
		pid := resolveAgentPersona(ac, tc, personas, d)
		if pid != nil {
			a.PersonaID = pid
		}

		d.Manager.AddAgent(a, nil)
		agents = append(agents, a)
	}
	return agents
}

// resolveAgentPersona resolves a persona for an agent following the priority order.
func resolveAgentPersona(ac config.AgentConfig, tc *config.TeamConfig, personas []models.Persona, d *daemon.Daemon) *uuid.UUID {
	// 1. Inline instructions — highest priority.
	if ac.PersonaInstructions != "" {
		return createTransientPersona(ac.Name+" persona", ac.PersonaInstructions, d)
	}

	// 2. Explicit persona ID.
	if ac.PersonaID != "" {
		if pid, err := uuid.Parse(ac.PersonaID); err == nil {
			return &pid
		}
	}

	// 3. Persona name match.
	if ac.Persona != "" {
		return resolvePersonaByName(ac.Persona, personas, d)
	}

	// 4. Team-level inline personas matching agent name.
	for _, p := range tc.Personas {
		if strings.EqualFold(p.Name, ac.Name) {
			return createTransientPersona(p.Name, p.Instructions, d)
		}
	}

	return nil
}

// resolvePersonaByName finds a persona by name (case-insensitive) in the loaded list.
// If not found, creates an ad-hoc persona with the name as instructions.
func resolvePersonaByName(name string, personas []models.Persona, d *daemon.Daemon) *uuid.UUID {
	for i := range personas {
		if strings.EqualFold(personas[i].Name, name) && personas[i].State != models.PersonaStateDeleted {
			id := personas[i].ID
			return &id
		}
	}
	return createTransientPersona(name, name, d)
}

// createTransientPersona creates an in-memory persona and registers it with the Manager.
// It is NOT persisted to disk — only lives for the duration of this daemon process.
func createTransientPersona(name, instructions string, d *daemon.Daemon) *uuid.UUID {
	p := models.Persona{
		ID:           uuid.New(),
		Name:         name,
		Instructions: instructions,
		Type:         models.PersonaTypeUser,
		State:        models.PersonaStateEnabled,
	}
	d.Manager.RegisterTransientPersona(p)
	return &p.ID
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
