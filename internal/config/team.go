// Package config handles loading and validating team configuration files.
package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	"github.com/google/uuid"
)

// validAgentTypes are the agent types accepted in team config files.
var validAgentTypes = map[string]bool{
	"claude":   true,
	"codex":    true,
	"gemini":   true,
	"copilot":  true,
	"opencode": true,
	"custom":   true,
}

// TeamConfig defines a team of agents to spawn together.
type TeamConfig struct {
	Name     string          `json:"name"`
	Repo     string          `json:"repo"`
	Prompt   string          `json:"prompt,omitempty"`
	EntryAgent string          `json:"entry_agent,omitempty"`
	Agents     []AgentConfig   `json:"agents"`
	Personas   []PersonaConfig `json:"personas,omitempty"`
}

// PersonaConfig defines an inline persona within a team config.
type PersonaConfig struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Instructions string `json:"instructions"`
}

// AgentConfig defines a single agent within a team.
type AgentConfig struct {
	Name                string   `json:"name"`
	AgentType           string   `json:"agent_type"`
	Persona             string   `json:"persona,omitempty"`
	PersonaInstructions string   `json:"persona_instructions,omitempty"`
	PersonaID           string   `json:"persona_id,omitempty"`
	Avatar              string   `json:"avatar,omitempty"`
	Command             string   `json:"command,omitempty"`
	AllowedTools        []string `json:"allowed_tools,omitempty"`
	Prompt              string   `json:"prompt,omitempty"`
}

// LoadTeamConfig reads a JSON file and returns a validated TeamConfig.
func LoadTeamConfig(path string) (*TeamConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var tc TeamConfig
	if err := json.Unmarshal(data, &tc); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	if err := tc.Validate(); err != nil {
		return nil, err
	}

	// Debug: log parsed team config.
	agentNames := make([]string, len(tc.Agents))
	for i, a := range tc.Agents {
		agentNames[i] = a.Name
	}
	slog.Debug("team config loaded", "team", tc.Name, "agentCount", len(tc.Agents), "agents", agentNames)

	return &tc, nil
}

// Validate checks that the TeamConfig is well-formed.
func (tc *TeamConfig) Validate() error {
	if tc.Name == "" {
		return fmt.Errorf("team name is required")
	}
	if tc.Repo == "" {
		return fmt.Errorf("repo path is required")
	}

	info, err := os.Stat(tc.Repo)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("repo path does not exist: %s", tc.Repo)
	}

	if len(tc.Agents) == 0 {
		return fmt.Errorf("at least one agent is required")
	}

	seen := make(map[string]bool)
	for i, a := range tc.Agents {
		if a.Name == "" {
			return fmt.Errorf("agent[%d].name is required", i)
		}
		if a.AgentType == "" || !validAgentTypes[a.AgentType] {
			return fmt.Errorf("agent[%d].agent_type: unknown type '%s'", i, a.AgentType)
		}
		if seen[a.Name] {
			return fmt.Errorf("duplicate agent name: '%s'", a.Name)
		}
		seen[a.Name] = true

		if a.PersonaID != "" {
			if _, err := uuid.Parse(a.PersonaID); err != nil {
				return fmt.Errorf("agent[%d].persona_id: invalid UUID '%s'", i, a.PersonaID)
			}
		}
	}

	if tc.EntryAgent != "" && !seen[tc.EntryAgent] {
		return fmt.Errorf("entry_agent %q does not match any agent name", tc.EntryAgent)
	}

	// Validate inline personas.
	seenPersona := make(map[string]bool)
	for i, p := range tc.Personas {
		if p.Name == "" {
			return fmt.Errorf("personas[%d].name is required", i)
		}
		if p.Instructions == "" {
			return fmt.Errorf("personas[%d].instructions is required", i)
		}
		if seenPersona[p.Name] {
			return fmt.Errorf("duplicate persona name: '%s'", p.Name)
		}
		seenPersona[p.Name] = true
	}

	return nil
}
