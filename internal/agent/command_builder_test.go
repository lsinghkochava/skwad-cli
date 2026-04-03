package agent

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/lsinghkochava/skwad-cli/internal/models"
)

func defaultBuilder() *CommandBuilder {
	return &CommandBuilder{
		MCPServerURL: "http://127.0.0.1:8777/mcp",
		PluginDir:    "/tmp/plugins",
	}
}

func defaultSettings() *models.AppSettings {
	s := models.DefaultSettings()
	return &s
}

func TestCommandBuilder_SkwadInstructions_ContainsUUID(t *testing.T) {
	id := "12345678-aaaa-bbbb-cccc-000000000099"
	result := skwadInstructions(id)
	if !strings.Contains(result, id) {
		t.Errorf("skwad instructions should contain agent UUID %q, got: %s", id, result)
	}
	if !strings.Contains(result, "CRITICAL RULE") {
		t.Error("skwad instructions should contain CRITICAL RULE for set-status")
	}
}

// --- BuildArgs tests ---

func TestBuildArgs_ClaudeExecutable(t *testing.T) {
	b := defaultBuilder()
	a := &models.Agent{ID: uuid.New(), Name: "Agent", Folder: "/tmp", AgentType: models.AgentTypeClaude}
	args, err := b.BuildArgs(a, nil, defaultSettings())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(args) == 0 {
		t.Fatal("args is empty")
	}
	if args[0] != "claude" {
		t.Errorf("args[0] = %q, want %q", args[0], "claude")
	}
}

func TestBuildArgs_ClaudeBasicFlags(t *testing.T) {
	b := defaultBuilder()
	agentID := uuid.MustParse("aaaaaaaa-1111-2222-3333-444444444444")
	a := &models.Agent{
		ID:        agentID,
		Name:      "TestAgent",
		Folder:    "/home/user/project",
		AgentType: models.AgentTypeClaude,
	}
	args, err := b.BuildArgs(a, nil, defaultSettings())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	required := []string{"-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose", "--permission-mode", "auto"}
	for _, flag := range required {
		if !containsArg(args, flag) {
			t.Errorf("missing required flag %q in args: %v", flag, args)
		}
	}
}

func TestBuildArgs_ClaudeMCPConfig(t *testing.T) {
	b := defaultBuilder()
	a := &models.Agent{ID: uuid.New(), Name: "Agent", Folder: "/tmp", AgentType: models.AgentTypeClaude}
	args, err := b.BuildArgs(a, nil, defaultSettings())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !containsArg(args, "--mcp-config") {
		t.Error("missing --mcp-config")
	}
	if !containsArg(args, "--allowed-tools") {
		t.Error("missing --allowed-tools")
	}
	mcpIdx := argIndex(args, "--mcp-config")
	if mcpIdx < 0 || mcpIdx+1 >= len(args) {
		t.Fatal("--mcp-config flag missing value")
	}
	if !strings.Contains(args[mcpIdx+1], "http://127.0.0.1:8777/mcp") {
		t.Errorf("MCP config missing URL: %s", args[mcpIdx+1])
	}
}

func TestBuildArgs_ClaudeNoMCP(t *testing.T) {
	b := &CommandBuilder{MCPServerURL: "", PluginDir: "/tmp/plugins"}
	a := &models.Agent{ID: uuid.New(), Name: "Agent", Folder: "/tmp", AgentType: models.AgentTypeClaude}
	args, err := b.BuildArgs(a, nil, defaultSettings())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if containsArg(args, "--mcp-config") {
		t.Error("should not have --mcp-config when MCPServerURL is empty")
	}
}

func TestBuildArgs_ClaudePersona(t *testing.T) {
	b := defaultBuilder()
	agentID := uuid.MustParse("bbbbbbbb-1111-2222-3333-444444444444")
	a := &models.Agent{ID: agentID, Name: "Agent", Folder: "/tmp", AgentType: models.AgentTypeClaude}
	persona := &models.Persona{Name: "Tester", Instructions: "Write tests for all code."}
	args, err := b.BuildArgs(a, persona, defaultSettings())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sysIdx := argIndex(args, "--append-system-prompt")
	if sysIdx < 0 || sysIdx+1 >= len(args) {
		t.Fatal("missing --append-system-prompt value")
	}
	prompt := args[sysIdx+1]
	if !strings.Contains(prompt, "Your skwad agent ID: "+agentID.String()) {
		t.Error("system prompt missing skwad agent ID")
	}
	if !strings.Contains(prompt, "You are asked to impersonate Tester") {
		t.Error("system prompt missing persona name")
	}
	if !strings.Contains(prompt, "Write tests for all code.") {
		t.Error("system prompt missing persona instructions")
	}
}

func TestBuildArgs_ClaudeNoPersona(t *testing.T) {
	b := defaultBuilder()
	agentID := uuid.MustParse("cccccccc-1111-2222-3333-444444444444")
	a := &models.Agent{ID: agentID, Name: "Agent", Folder: "/tmp", AgentType: models.AgentTypeClaude}
	args, err := b.BuildArgs(a, nil, defaultSettings())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sysIdx := argIndex(args, "--append-system-prompt")
	if sysIdx < 0 || sysIdx+1 >= len(args) {
		t.Fatal("missing --append-system-prompt value")
	}
	prompt := args[sysIdx+1]
	if strings.Contains(prompt, "You are asked to impersonate") {
		t.Error("should not contain persona prompt when no persona given")
	}
}

func TestBuildArgs_ClaudeResumeSession(t *testing.T) {
	b := defaultBuilder()
	a := &models.Agent{
		ID:              uuid.New(),
		Name:            "Agent",
		Folder:          "/tmp",
		AgentType:       models.AgentTypeClaude,
		ResumeSessionID: "sess-headless-123",
	}
	args, err := b.BuildArgs(a, nil, defaultSettings())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	idx := argIndex(args, "--session-id")
	if idx < 0 || idx+1 >= len(args) {
		t.Fatal("missing --session-id")
	}
	if args[idx+1] != "sess-headless-123" {
		t.Errorf("wrong session ID: got %s, want sess-headless-123", args[idx+1])
	}
}

func TestBuildArgs_ClaudeModelFromOptions(t *testing.T) {
	b := defaultBuilder()
	a := &models.Agent{ID: uuid.New(), Name: "Agent", Folder: "/tmp", AgentType: models.AgentTypeClaude}
	settings := defaultSettings()
	settings.AgentTypeOptions.ClaudeOptions = "--model claude-sonnet-4-20250514 --some-other-flag"
	args, err := b.BuildArgs(a, nil, settings)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	idx := argIndex(args, "--model")
	if idx < 0 || idx+1 >= len(args) {
		t.Fatal("missing --model flag")
	}
	if args[idx+1] != "claude-sonnet-4-20250514" {
		t.Errorf("wrong model: got %s", args[idx+1])
	}
}

func TestBuildArgs_ClaudeNoModel(t *testing.T) {
	b := defaultBuilder()
	a := &models.Agent{ID: uuid.New(), Name: "Agent", Folder: "/tmp", AgentType: models.AgentTypeClaude}
	args, err := b.BuildArgs(a, nil, defaultSettings())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if containsArg(args, "--model") {
		t.Error("should not have --model when ClaudeOptions has no model")
	}
}

func TestBuildArgs_ClaudeWorkingDir(t *testing.T) {
	b := defaultBuilder()
	a := &models.Agent{ID: uuid.New(), Name: "Agent", Folder: "/home/user/project", AgentType: models.AgentTypeClaude}
	args, err := b.BuildArgs(a, nil, defaultSettings())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	idx := argIndex(args, "--add-dir")
	if idx < 0 || idx+1 >= len(args) {
		t.Fatal("missing --add-dir")
	}
	if args[idx+1] != "/home/user/project" {
		t.Errorf("wrong dir: got %s", args[idx+1])
	}
}

func TestBuildArgs_ClaudeAgentName(t *testing.T) {
	b := defaultBuilder()
	a := &models.Agent{ID: uuid.New(), Name: "MyAgent", Folder: "/tmp", AgentType: models.AgentTypeClaude}
	args, err := b.BuildArgs(a, nil, defaultSettings())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	idx := argIndex(args, "--name")
	if idx < 0 || idx+1 >= len(args) {
		t.Fatal("missing --name")
	}
	if args[idx+1] != "MyAgent" {
		t.Errorf("wrong name: got %s, want MyAgent", args[idx+1])
	}
}

func TestBuildArgs_NoPluginDir(t *testing.T) {
	b := defaultBuilder()
	a := &models.Agent{ID: uuid.New(), Name: "Agent", Folder: "/tmp", AgentType: models.AgentTypeClaude}
	args, err := b.BuildArgs(a, nil, defaultSettings())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if containsArg(args, "--plugin-dir") {
		t.Error("headless mode should not include --plugin-dir")
	}
}

func TestBuildArgs_NoRegistrationPrompt(t *testing.T) {
	b := defaultBuilder()
	a := &models.Agent{ID: uuid.New(), Name: "Agent", Folder: "/tmp", AgentType: models.AgentTypeClaude}
	args, err := b.BuildArgs(a, nil, defaultSettings())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, arg := range args {
		if strings.Contains(arg, "List other agents names and project") {
			t.Error("headless mode should not include registration user prompt")
		}
	}
}

func TestBuildArgs_UnsupportedAgentTypes(t *testing.T) {
	unsupported := []models.AgentType{
		models.AgentTypeCodex,
		models.AgentTypeOpenCode,
		models.AgentTypeGemini,
		models.AgentTypeCopilot,
		models.AgentTypeCustom1,
		models.AgentTypeCustom2,
		models.AgentTypeShell,
	}
	b := defaultBuilder()
	for _, agentType := range unsupported {
		a := &models.Agent{ID: uuid.New(), Name: "Agent", Folder: "/tmp", AgentType: agentType}
		_, err := b.BuildArgs(a, nil, defaultSettings())
		if err == nil {
			t.Errorf("expected error for agent type %s, got nil", agentType)
		}
		if err != nil && !strings.Contains(err.Error(), "headless mode not supported") {
			t.Errorf("unexpected error message for %s: %v", agentType, err)
		}
	}
}

func TestParseFlag(t *testing.T) {
	cases := []struct {
		opts, flag, want string
	}{
		{"--model claude-sonnet-4-20250514", "--model", "claude-sonnet-4-20250514"},
		{"--some-flag value --model opus", "--model", "opus"},
		{"--other stuff", "--model", ""},
		{"--model", "--model", ""}, // flag at end with no value
		{"", "--model", ""},
	}
	for _, tc := range cases {
		got := parseFlag(tc.opts, tc.flag)
		if got != tc.want {
			t.Errorf("parseFlag(%q, %q) = %q, want %q", tc.opts, tc.flag, got, tc.want)
		}
	}
}

// helpers for BuildArgs tests

func containsArg(args []string, target string) bool {
	for _, a := range args {
		if a == target {
			return true
		}
	}
	return false
}

func argIndex(args []string, target string) int {
	for i, a := range args {
		if a == target {
			return i
		}
	}
	return -1
}
