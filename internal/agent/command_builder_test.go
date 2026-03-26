package agent

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/lsinghkochava/skwad-cli/internal/models"
)

func defaultBuilder() *CommandBuilder {
	return &CommandBuilder{
		MCPServerURL: "http://127.0.0.1:8766/mcp",
		PluginDir:    "/tmp/plugins",
	}
}

func defaultSettings() *models.AppSettings {
	s := models.DefaultSettings()
	return &s
}

func TestCommandBuilder_ContainsCdAndClear(t *testing.T) {
	b := defaultBuilder()
	a := &models.Agent{
		ID:        uuid.New(),
		Folder:    "/home/user/project",
		AgentType: models.AgentTypeClaude,
	}
	cmd := b.Build(a, nil, defaultSettings())
	if !strings.Contains(cmd, "cd '/home/user/project'") {
		t.Errorf("command missing cd: %s", cmd)
	}
	if !strings.Contains(cmd, "clear") {
		t.Errorf("command missing clear: %s", cmd)
	}
}

func TestCommandBuilder_InjectsAgentID(t *testing.T) {
	b := defaultBuilder()
	a := &models.Agent{
		ID:        uuid.MustParse("12345678-0000-0000-0000-000000000000"),
		Folder:    "/tmp",
		AgentType: models.AgentTypeClaude,
	}
	cmd := b.Build(a, nil, defaultSettings())
	if !strings.Contains(cmd, "SKWAD_AGENT_ID=12345678-0000-0000-0000-000000000000") {
		t.Errorf("command missing SKWAD_AGENT_ID: %s", cmd)
	}
}

func TestCommandBuilder_HistoryPrefix(t *testing.T) {
	b := defaultBuilder()
	a := &models.Agent{ID: uuid.New(), Folder: "/tmp", AgentType: models.AgentTypeShell}
	cmd := b.Build(a, nil, defaultSettings())
	if !strings.HasPrefix(cmd, " ") {
		t.Error("command must start with a space to suppress shell history")
	}
}

func TestCommandBuilder_ClaudeMCPFlags(t *testing.T) {
	b := defaultBuilder()
	a := &models.Agent{ID: uuid.New(), Folder: "/tmp", AgentType: models.AgentTypeClaude}
	cmd := b.Build(a, nil, defaultSettings())
	if !strings.Contains(cmd, "--mcp-config") {
		t.Errorf("claude command missing --mcp-config: %s", cmd)
	}
	if !strings.Contains(cmd, "--allowed-tools") {
		t.Errorf("claude command missing --allowed-tools: %s", cmd)
	}
}

func TestCommandBuilder_ClaudeResume(t *testing.T) {
	b := defaultBuilder()
	a := &models.Agent{
		ID:              uuid.New(),
		Folder:          "/tmp",
		AgentType:       models.AgentTypeClaude,
		ResumeSessionID: "sess-abc",
	}
	cmd := b.Build(a, nil, defaultSettings())
	if !strings.Contains(cmd, "--resume sess-abc") {
		t.Errorf("claude resume flag missing: %s", cmd)
	}
}

func TestCommandBuilder_ClaudePersonaInjected(t *testing.T) {
	b := defaultBuilder()
	a := &models.Agent{ID: uuid.New(), Folder: "/tmp", AgentType: models.AgentTypeClaude}
	persona := &models.Persona{Instructions: "Always write tests first."}
	cmd := b.Build(a, persona, defaultSettings())
	if !strings.Contains(cmd, "--append-system-prompt") {
		t.Errorf("claude persona injection missing: %s", cmd)
	}
	if !strings.Contains(cmd, "Always write tests first.") {
		t.Errorf("persona instructions not present in command: %s", cmd)
	}
}

func TestCommandBuilder_ShellAgent(t *testing.T) {
	b := defaultBuilder()
	a := &models.Agent{
		ID:           uuid.New(),
		Folder:       "/tmp",
		AgentType:    models.AgentTypeShell,
		ShellCommand: "bash",
	}
	cmd := b.Build(a, nil, defaultSettings())
	if !strings.Contains(cmd, "bash") {
		t.Errorf("shell agent command missing 'bash': %s", cmd)
	}
}

func TestCommandBuilder_ShellAgent_DefaultsToSHELL(t *testing.T) {
	b := defaultBuilder()
	a := &models.Agent{ID: uuid.New(), Folder: "/tmp", AgentType: models.AgentTypeShell}
	cmd := b.Build(a, nil, defaultSettings())
	if !strings.Contains(cmd, "$SHELL") {
		t.Errorf("shell agent without ShellCommand should use $SHELL: %s", cmd)
	}
}

func TestCommandBuilder_CodexCommand(t *testing.T) {
	b := defaultBuilder()
	a := &models.Agent{ID: uuid.New(), Folder: "/tmp", AgentType: models.AgentTypeCodex}
	cmd := b.Build(a, nil, defaultSettings())
	if !strings.Contains(cmd, "codex") {
		t.Errorf("codex command missing 'codex': %s", cmd)
	}
}

func TestShellQuote(t *testing.T) {
	cases := []struct{ in, want string }{
		{"hello", "'hello'"},
		{"it's", "'it'\\''s'"},
		{"/home/user/my project", "'/home/user/my project'"},
	}
	for _, tc := range cases {
		if got := shellQuote(tc.in); got != tc.want {
			t.Errorf("shellQuote(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestShellEscapeDouble(t *testing.T) {
	// Dollar signs and backticks must be escaped inside double quotes.
	result := shellEscapeDouble("hello $world `test` \"quoted\"")
	if strings.Contains(result, " $world") {
		t.Error("unescaped $ found in double-quoted string")
	}
	if strings.Contains(result, " `test`") {
		t.Error("unescaped backtick found in double-quoted string")
	}
}
