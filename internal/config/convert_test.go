package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// macExportFixture returns a minimal macOS export JSON for testing.
func macExportFixture(t *testing.T, repoDir string) []byte {
	t.Helper()
	export := map[string]interface{}{
		"formatVersion": 1,
		"appVersion":    "1.8.0",
		"workspace":     map[string]interface{}{"name": "Review Team"},
		"agents": []interface{}{
			map[string]interface{}{
				"name": "Bug Hunter", "agentType": "claude", "folder": repoDir,
				"personaId": "UUID1", "isCompanion": false, "avatar": "🦄",
			},
			map[string]interface{}{
				"name": "Helper", "agentType": "claude", "folder": repoDir,
				"personaId": "", "isCompanion": true, "avatar": "",
			},
		},
		"personas": []interface{}{
			map[string]interface{}{
				"id": "UUID1", "name": "Bug Hunter", "instructions": "Find bugs.",
				"state": "enabled", "type": "user",
			},
		},
	}
	data, _ := json.Marshal(export)
	return data
}

func TestConvertMacOSExport(t *testing.T) {
	repo := t.TempDir()
	data := macExportFixture(t, repo)

	tc, err := ConvertMacOSExport(data)
	if err != nil {
		t.Fatalf("ConvertMacOSExport: %v", err)
	}

	if tc.Name != "Review Team" {
		t.Errorf("expected name 'Review Team', got %q", tc.Name)
	}
	if tc.Repo != repo {
		t.Errorf("expected repo %q, got %q", repo, tc.Repo)
	}
	// Should have 1 agent (companion skipped).
	if len(tc.Agents) != 1 {
		t.Fatalf("expected 1 agent (companion skipped), got %d", len(tc.Agents))
	}
	if tc.Agents[0].Name != "Bug Hunter" {
		t.Errorf("expected agent name 'Bug Hunter', got %q", tc.Agents[0].Name)
	}
	if tc.Agents[0].AgentType != "claude" {
		t.Errorf("expected agent type 'claude', got %q", tc.Agents[0].AgentType)
	}
}

func TestConvertMacOSExport_SkipsCompanions(t *testing.T) {
	repo := t.TempDir()
	data := macExportFixture(t, repo)

	tc, err := ConvertMacOSExport(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, a := range tc.Agents {
		if a.Name == "Helper" {
			t.Error("companion agent 'Helper' should have been skipped")
		}
	}
}

func TestConvertMacOSExport_PersonaLookup(t *testing.T) {
	repo := t.TempDir()
	data := macExportFixture(t, repo)

	tc, err := ConvertMacOSExport(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Bug Hunter has personaId=UUID1 which maps to "Find bugs." instructions.
	if tc.Agents[0].PersonaInstructions != "Find bugs." {
		t.Errorf("expected persona_instructions 'Find bugs.', got %q", tc.Agents[0].PersonaInstructions)
	}
}

func TestConvertMacOSExport_MissingPersona(t *testing.T) {
	repo := t.TempDir()
	export := map[string]interface{}{
		"formatVersion": 1,
		"workspace":     map[string]interface{}{"name": "Test"},
		"agents": []interface{}{
			map[string]interface{}{
				"name": "Bot", "agentType": "claude", "folder": repo,
				"personaId": "NONEXISTENT-UUID", "isCompanion": false,
			},
		},
		"personas": []interface{}{},
	}
	data, _ := json.Marshal(export)

	tc, err := ConvertMacOSExport(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Missing persona should result in empty persona_instructions, no error.
	if tc.Agents[0].PersonaInstructions != "" {
		t.Errorf("expected empty persona_instructions for missing persona, got %q", tc.Agents[0].PersonaInstructions)
	}
}

func TestIsMacOSExport_True(t *testing.T) {
	data := []byte(`{"formatVersion": 1, "agents": []}`)
	if !IsMacOSExport(data) {
		t.Error("expected true for JSON with formatVersion")
	}
}

func TestIsMacOSExport_False(t *testing.T) {
	data := []byte(`{"name": "My Team", "repo": "/tmp", "agents": []}`)
	if IsMacOSExport(data) {
		t.Error("expected false for normal team config")
	}
}

func TestLoadOrConvert_TeamConfig(t *testing.T) {
	repo := t.TempDir()
	cfg := `{"name": "Test", "repo": "` + repo + `", "agents": [{"name": "Bot", "agent_type": "claude"}]}`
	path := filepath.Join(t.TempDir(), "team.json")
	os.WriteFile(path, []byte(cfg), 0644)

	tc, err := LoadOrConvert(path)
	if err != nil {
		t.Fatalf("LoadOrConvert: %v", err)
	}
	if tc.Name != "Test" {
		t.Errorf("expected name 'Test', got %q", tc.Name)
	}
}

func TestLoadOrConvert_MacOSExport(t *testing.T) {
	repo := t.TempDir()
	data := macExportFixture(t, repo)
	path := filepath.Join(t.TempDir(), "export.json")
	os.WriteFile(path, data, 0644)

	tc, err := LoadOrConvert(path)
	if err != nil {
		t.Fatalf("LoadOrConvert: %v", err)
	}
	if tc.Name != "Review Team" {
		t.Errorf("expected name 'Review Team', got %q", tc.Name)
	}
}

func TestConvertMacOSExport_AllCompanions(t *testing.T) {
	repo := t.TempDir()
	export := map[string]interface{}{
		"formatVersion": 1,
		"workspace":     map[string]interface{}{"name": "Empty"},
		"agents": []interface{}{
			map[string]interface{}{
				"name": "Comp1", "agentType": "claude", "folder": repo,
				"isCompanion": true,
			},
		},
		"personas": []interface{}{},
	}
	data, _ := json.Marshal(export)

	_, err := ConvertMacOSExport(data)
	if err == nil || !strings.Contains(err.Error(), "no non-companion agents") {
		t.Errorf("expected 'no non-companion agents' error, got %v", err)
	}
}
