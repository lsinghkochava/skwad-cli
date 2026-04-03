package config

import (
	"encoding/json"
	"fmt"
	"os"
)

// macExport represents the macOS Skwad workspace export format.
type macExport struct {
	FormatVersion int              `json:"formatVersion"`
	AppVersion    string           `json:"appVersion"`
	Workspace     macWorkspace     `json:"workspace"`
	Agents        []macAgent       `json:"agents"`
	Personas      []macPersona     `json:"personas"`
}

type macWorkspace struct {
	Name     string `json:"name"`
	ColorHex string `json:"colorHex"`
}

type macAgent struct {
	Name        string `json:"name"`
	AgentType   string `json:"agentType"`
	Folder      string `json:"folder"`
	Avatar      string `json:"avatar"`
	PersonaID   string `json:"personaId"`
	IsCompanion bool   `json:"isCompanion"`
}

type macPersona struct {
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	Instructions      string   `json:"instructions"`
	State             string   `json:"state"`
	Type              string   `json:"type"`
	AllowedCategories []string `json:"allowedCategories"`
}

// IsMacOSExport checks if JSON data is a macOS workspace export
// by looking for "formatVersion" or "appVersion" keys.
func IsMacOSExport(data []byte) bool {
	var probe struct {
		FormatVersion *int    `json:"formatVersion"`
		AppVersion    *string `json:"appVersion"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return false
	}
	return probe.FormatVersion != nil || probe.AppVersion != nil
}

// ConvertMacOSExport converts a macOS Skwad workspace export to a TeamConfig.
func ConvertMacOSExport(data []byte) (*TeamConfig, error) {
	var export macExport
	if err := json.Unmarshal(data, &export); err != nil {
		return nil, fmt.Errorf("parse macOS export: %w", err)
	}

	// Build persona lookup by ID.
	personaMap := make(map[string]macPersona)
	for _, p := range export.Personas {
		personaMap[p.ID] = p
	}

	tc := &TeamConfig{
		Name: export.Workspace.Name,
	}

	for _, a := range export.Agents {
		if a.IsCompanion {
			continue
		}

		// Use first agent's folder as repo.
		if tc.Repo == "" {
			tc.Repo = a.Folder
		}

		ac := AgentConfig{
			Name:      a.Name,
			AgentType: a.AgentType,
			Avatar:    a.Avatar,
		}

		// Resolve persona from export's persona list.
		if a.PersonaID != "" {
			if p, ok := personaMap[a.PersonaID]; ok && p.Instructions != "" {
				if len(p.AllowedCategories) > 0 {
					// Emit as team-level persona (matched by agent name) to carry tags.
					tc.Personas = append(tc.Personas, PersonaConfig{
						Name:         a.Name,
						Instructions: p.Instructions,
						Tags:         p.AllowedCategories,
					})
				} else {
					ac.PersonaInstructions = p.Instructions
				}
			}
		}

		tc.Agents = append(tc.Agents, ac)
	}

	if len(tc.Agents) == 0 {
		return nil, fmt.Errorf("no non-companion agents found in export")
	}

	return tc, nil
}

// LoadOrConvert tries LoadTeamConfig first; if it detects macOS export format,
// auto-converts instead.
func LoadOrConvert(path string) (*TeamConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	if IsMacOSExport(data) {
		tc, err := ConvertMacOSExport(data)
		if err != nil {
			return nil, err
		}
		// Validate after conversion (repo path may not exist on this machine).
		// Skip validation for converted configs — the user may need to adjust paths.
		return tc, nil
	}

	var tc TeamConfig
	if err := json.Unmarshal(data, &tc); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	if err := tc.Validate(); err != nil {
		return nil, err
	}
	return &tc, nil
}
