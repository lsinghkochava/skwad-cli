package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeConfig writes a JSON string to a temp file and returns its path.
func writeConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "team.json")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestLoadTeamConfig_Valid(t *testing.T) {
	repo := t.TempDir()
	cfg := writeConfig(t, `{
		"name": "My Team",
		"repo": "`+repo+`",
		"agents": [
			{"name": "Coder", "agent_type": "claude"},
			{"name": "Tester", "agent_type": "codex"}
		]
	}`)

	tc, err := LoadTeamConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tc.Name != "My Team" {
		t.Errorf("expected name 'My Team', got %q", tc.Name)
	}
	if tc.Repo != repo {
		t.Errorf("expected repo %q, got %q", repo, tc.Repo)
	}
	if len(tc.Agents) != 2 {
		t.Errorf("expected 2 agents, got %d", len(tc.Agents))
	}
}

func TestValidate_MissingName(t *testing.T) {
	repo := t.TempDir()
	tc := &TeamConfig{
		Repo:   repo,
		Agents: []AgentConfig{{Name: "A", AgentType: "claude"}},
	}
	err := tc.Validate()
	if err == nil || !strings.Contains(err.Error(), "team name is required") {
		t.Errorf("expected 'team name is required', got %v", err)
	}
}

func TestValidate_MissingRepo(t *testing.T) {
	tc := &TeamConfig{
		Name:   "Test",
		Agents: []AgentConfig{{Name: "A", AgentType: "claude"}},
	}
	err := tc.Validate()
	if err == nil || !strings.Contains(err.Error(), "repo path is required") {
		t.Errorf("expected 'repo path is required', got %v", err)
	}
}

func TestValidate_RepoPathDoesNotExist(t *testing.T) {
	tc := &TeamConfig{
		Name:   "Test",
		Repo:   "/nonexistent/path/abc123",
		Agents: []AgentConfig{{Name: "A", AgentType: "claude"}},
	}
	err := tc.Validate()
	if err == nil || !strings.Contains(err.Error(), "repo path does not exist") {
		t.Errorf("expected 'repo path does not exist', got %v", err)
	}
}

func TestValidate_NoAgents(t *testing.T) {
	repo := t.TempDir()
	tc := &TeamConfig{
		Name:   "Test",
		Repo:   repo,
		Agents: []AgentConfig{},
	}
	err := tc.Validate()
	if err == nil || !strings.Contains(err.Error(), "at least one agent is required") {
		t.Errorf("expected 'at least one agent is required', got %v", err)
	}
}

func TestValidate_AgentMissingName(t *testing.T) {
	repo := t.TempDir()
	tc := &TeamConfig{
		Name:   "Test",
		Repo:   repo,
		Agents: []AgentConfig{{AgentType: "claude"}},
	}
	err := tc.Validate()
	if err == nil || !strings.Contains(err.Error(), "agent[0].name is required") {
		t.Errorf("expected 'agent[0].name is required', got %v", err)
	}
}

func TestValidate_AgentUnknownType(t *testing.T) {
	repo := t.TempDir()
	tc := &TeamConfig{
		Name:   "Test",
		Repo:   repo,
		Agents: []AgentConfig{{Name: "Bot", AgentType: "gpt5"}},
	}
	err := tc.Validate()
	if err == nil || !strings.Contains(err.Error(), "unknown type 'gpt5'") {
		t.Errorf("expected 'unknown type' error, got %v", err)
	}
}

func TestValidate_DuplicateAgentNames(t *testing.T) {
	repo := t.TempDir()
	tc := &TeamConfig{
		Name: "Test",
		Repo: repo,
		Agents: []AgentConfig{
			{Name: "Bot", AgentType: "claude"},
			{Name: "Bot", AgentType: "codex"},
		},
	}
	err := tc.Validate()
	if err == nil || !strings.Contains(err.Error(), "duplicate agent name: 'Bot'") {
		t.Errorf("expected 'duplicate agent name' error, got %v", err)
	}
}

func TestValidate_AllValidAgentTypes(t *testing.T) {
	repo := t.TempDir()
	for _, agentType := range []string{"claude", "codex", "gemini", "copilot", "opencode", "custom"} {
		t.Run(agentType, func(t *testing.T) {
			tc := &TeamConfig{
				Name:   "Test",
				Repo:   repo,
				Agents: []AgentConfig{{Name: "Bot", AgentType: agentType}},
			}
			if err := tc.Validate(); err != nil {
				t.Errorf("type %q should be valid, got error: %v", agentType, err)
			}
		})
	}
}

func TestValidate_OptionalFieldsPresent(t *testing.T) {
	repo := t.TempDir()
	cfg := writeConfig(t, `{
		"name": "Full Team",
		"repo": "`+repo+`",
		"agents": [{
			"name": "FullBot",
			"agent_type": "claude",
			"persona": "Senior Engineer",
			"command": "claude --model opus",
			"allowed_tools": ["Read", "Write", "Bash"],
			"prompt": "You are a helpful assistant."
		}]
	}`)

	tc, err := LoadTeamConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	a := tc.Agents[0]
	if a.Persona != "Senior Engineer" {
		t.Errorf("expected persona 'Senior Engineer', got %q", a.Persona)
	}
	if a.Command != "claude --model opus" {
		t.Errorf("expected command 'claude --model opus', got %q", a.Command)
	}
	if len(a.AllowedTools) != 3 {
		t.Errorf("expected 3 allowed_tools, got %d", len(a.AllowedTools))
	}
	if a.Prompt != "You are a helpful assistant." {
		t.Errorf("expected prompt set, got %q", a.Prompt)
	}
}

func TestValidate_OptionalFieldsAbsent(t *testing.T) {
	repo := t.TempDir()
	cfg := writeConfig(t, `{
		"name": "Minimal",
		"repo": "`+repo+`",
		"agents": [{"name": "MinBot", "agent_type": "codex"}]
	}`)

	tc, err := LoadTeamConfig(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	a := tc.Agents[0]
	if a.Persona != "" {
		t.Errorf("expected empty persona, got %q", a.Persona)
	}
	if a.Command != "" {
		t.Errorf("expected empty command, got %q", a.Command)
	}
	if a.AllowedTools != nil {
		t.Errorf("expected nil allowed_tools, got %v", a.AllowedTools)
	}
	if a.Prompt != "" {
		t.Errorf("expected empty prompt, got %q", a.Prompt)
	}
}

func TestLoadTeamConfig_FileNotFound(t *testing.T) {
	_, err := LoadTeamConfig("/nonexistent/team.json")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestLoadTeamConfig_InvalidJSON(t *testing.T) {
	cfg := writeConfig(t, `{not valid json}`)
	_, err := LoadTeamConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "parse config") {
		t.Errorf("expected parse error, got %v", err)
	}
}
