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

func TestBuildSystemPrompt_ContainsUUID(t *testing.T) {
	id := uuid.MustParse("12345678-aaaa-bbbb-cccc-000000000099")
	a := &models.Agent{ID: id, Name: "Agent"}
	result := BuildSystemPrompt(a, nil, nil)
	if !strings.Contains(result, id.String()) {
		t.Errorf("system prompt should contain agent UUID %q", id.String())
	}
	if !strings.Contains(result, "CRITICAL RULE") {
		t.Error("system prompt should contain CRITICAL RULE for set-status")
	}
}

// --- BuildArgs tests ---

func TestBuildArgs_ClaudeExecutable(t *testing.T) {
	b := defaultBuilder()
	a := &models.Agent{ID: uuid.New(), Name: "Agent", Folder: "/tmp", AgentType: models.AgentTypeClaude}
	args, err := b.BuildArgs(a, nil, defaultSettings(), nil)
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
	args, err := b.BuildArgs(a, nil, defaultSettings(), nil)
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
	args, err := b.BuildArgs(a, nil, defaultSettings(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !containsArg(args, "--mcp-config") {
		t.Error("missing --mcp-config")
	}
	if !containsArg(args, "--allowedTools") {
		t.Error("missing --allowedTools")
	}
	mcpIdx := argIndex(args, "--mcp-config")
	if mcpIdx < 0 || mcpIdx+1 >= len(args) {
		t.Fatal("--mcp-config flag missing value")
	}
	if !strings.Contains(args[mcpIdx+1], "http://127.0.0.1:8777/mcp") {
		t.Errorf("MCP config missing URL: %s", args[mcpIdx+1])
	}
}

func TestBuildArgs_MCPURLContainsAgentID(t *testing.T) {
	b := defaultBuilder()
	agentID := uuid.MustParse("dddddddd-1111-2222-3333-444444444444")
	a := &models.Agent{ID: agentID, Name: "Agent", Folder: "/tmp", AgentType: models.AgentTypeClaude}
	args, err := b.BuildArgs(a, nil, defaultSettings(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mcpIdx := argIndex(args, "--mcp-config")
	if mcpIdx < 0 || mcpIdx+1 >= len(args) {
		t.Fatal("--mcp-config flag missing value")
	}
	mcpConfig := args[mcpIdx+1]
	if !strings.Contains(mcpConfig, "?agent="+agentID.String()) {
		t.Errorf("MCP URL should contain agent ID query param, got: %s", mcpConfig)
	}
}

func TestBuildArgs_ClaudeNoMCP(t *testing.T) {
	b := &CommandBuilder{MCPServerURL: "", PluginDir: "/tmp/plugins"}
	a := &models.Agent{ID: uuid.New(), Name: "Agent", Folder: "/tmp", AgentType: models.AgentTypeClaude}
	args, err := b.BuildArgs(a, nil, defaultSettings(), nil)
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
	args, err := b.BuildArgs(a, persona, defaultSettings(), nil)
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
	if !strings.Contains(prompt, "Persona: Tester") {
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
	args, err := b.BuildArgs(a, nil, defaultSettings(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sysIdx := argIndex(args, "--append-system-prompt")
	if sysIdx < 0 || sysIdx+1 >= len(args) {
		t.Fatal("missing --append-system-prompt value")
	}
	prompt := args[sysIdx+1]
	if strings.Contains(prompt, "Persona:") {
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
	args, err := b.BuildArgs(a, nil, defaultSettings(), nil)
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
	args, err := b.BuildArgs(a, nil, settings, nil)
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
	args, err := b.BuildArgs(a, nil, defaultSettings(), nil)
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
	args, err := b.BuildArgs(a, nil, defaultSettings(), nil)
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
	args, err := b.BuildArgs(a, nil, defaultSettings(), nil)
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
	args, err := b.BuildArgs(a, nil, defaultSettings(), nil)
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
	args, err := b.BuildArgs(a, nil, defaultSettings(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, arg := range args {
		if strings.Contains(arg, "List other agents names and project") {
			t.Error("headless mode should not include registration user prompt")
		}
	}
}

func TestBuildArgs_ExploreModeFlags(t *testing.T) {
	b := defaultBuilder()
	a := &models.Agent{ID: uuid.New(), Name: "Explorer", Folder: "/tmp", AgentType: models.AgentTypeClaude, ExploreMode: true}
	args, err := b.BuildArgs(a, nil, defaultSettings(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have plan permission mode
	pmIdx := argIndex(args, "--permission-mode")
	if pmIdx < 0 || pmIdx+1 >= len(args) {
		t.Fatal("missing --permission-mode")
	}
	if args[pmIdx+1] != "plan" {
		t.Errorf("expected --permission-mode plan, got %s", args[pmIdx+1])
	}

	// Should NOT have Write, Edit, or Bash
	for _, tool := range []string{"Write", "Edit", "Bash(*)"} {
		if containsArg(args, tool) {
			t.Errorf("explore mode should not have tool %q", tool)
		}
	}

	// Should have read-only tools
	for _, tool := range []string{"Read", "Glob", "Grep", "WebSearch", "WebFetch"} {
		if !containsArg(args, tool) {
			t.Errorf("explore mode should have tool %q", tool)
		}
	}
}

func TestBuildArgs_NormalModeFlags(t *testing.T) {
	b := defaultBuilder()
	a := &models.Agent{ID: uuid.New(), Name: "Coder", Folder: "/tmp", AgentType: models.AgentTypeClaude}
	args, err := b.BuildArgs(a, nil, defaultSettings(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have auto permission mode
	pmIdx := argIndex(args, "--permission-mode")
	if pmIdx < 0 || pmIdx+1 >= len(args) {
		t.Fatal("missing --permission-mode")
	}
	if args[pmIdx+1] != "auto" {
		t.Errorf("expected --permission-mode auto, got %s", args[pmIdx+1])
	}

	// Should have Write, Edit, Bash
	for _, tool := range []string{"Write", "Edit", "Bash(*)"} {
		if !containsArg(args, tool) {
			t.Errorf("normal mode should have tool %q", tool)
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
		_, err := b.BuildArgs(a, nil, defaultSettings(), nil)
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

func TestBuildArgs_WithTeammates(t *testing.T) {
	b := defaultBuilder()
	a := &models.Agent{ID: uuid.New(), Name: "Coder", Folder: "/tmp", AgentType: models.AgentTypeClaude}
	teammates := []models.Agent{
		{ID: uuid.New(), Name: "Explorer"},
		{ID: uuid.New(), Name: "Tester"},
		{ID: uuid.New(), Name: "Reviewer"},
	}
	args, err := b.BuildArgs(a, nil, defaultSettings(), teammates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sysIdx := argIndex(args, "--append-system-prompt")
	if sysIdx < 0 || sysIdx+1 >= len(args) {
		t.Fatal("missing --append-system-prompt value")
	}
	prompt := args[sysIdx+1]
	if !strings.Contains(prompt, "Team Roster") {
		t.Error("system prompt should contain team roster when teammates present")
	}
	if !strings.Contains(prompt, "Explorer") {
		t.Error("system prompt should list Explorer teammate")
	}
}

func TestBuildArgs_NoTeammates(t *testing.T) {
	b := defaultBuilder()
	a := &models.Agent{ID: uuid.New(), Name: "Coder", Folder: "/tmp", AgentType: models.AgentTypeClaude}
	args, err := b.BuildArgs(a, nil, defaultSettings(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sysIdx := argIndex(args, "--append-system-prompt")
	if sysIdx < 0 || sysIdx+1 >= len(args) {
		t.Fatal("missing --append-system-prompt value")
	}
	prompt := args[sysIdx+1]
	if strings.Contains(prompt, "Team Roster") {
		t.Error("system prompt should NOT contain team roster when no teammates")
	}
}

func TestBuildArgs_RoleInstructionsInPrompt(t *testing.T) {
	b := defaultBuilder()
	a := &models.Agent{ID: uuid.New(), Name: "Coder", Folder: "/tmp", AgentType: models.AgentTypeClaude}
	args, err := b.BuildArgs(a, nil, defaultSettings(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sysIdx := argIndex(args, "--append-system-prompt")
	if sysIdx < 0 || sysIdx+1 >= len(args) {
		t.Fatal("missing --append-system-prompt value")
	}
	prompt := args[sysIdx+1]
	if !strings.Contains(prompt, "Role: Coder") {
		t.Error("system prompt should contain coder role instructions for agent named Coder")
	}
}

func TestBuildArgs_AllowedToolsFromConfig(t *testing.T) {
	b := defaultBuilder()
	a := &models.Agent{
		ID:           uuid.New(),
		Name:         "Coder",
		Folder:       "/tmp",
		AgentType:    models.AgentTypeClaude,
		AllowedTools: []string{"Read", "Grep"},
	}
	args, err := b.BuildArgs(a, nil, defaultSettings(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have custom tools + mcp__skwad__*
	for _, tool := range []string{"mcp__skwad__*", "Read", "Grep"} {
		if !containsArg(args, tool) {
			t.Errorf("expected tool %q in args", tool)
		}
	}
	// Should NOT have default tools that weren't specified
	for _, tool := range []string{"Write", "Edit", "Bash(*)"} {
		if containsArg(args, tool) {
			t.Errorf("should not have default tool %q when AllowedTools is set", tool)
		}
	}
	// Should still have auto permission mode
	pmIdx := argIndex(args, "--permission-mode")
	if pmIdx < 0 || pmIdx+1 >= len(args) {
		t.Fatal("missing --permission-mode")
	}
	if args[pmIdx+1] != "auto" {
		t.Errorf("expected --permission-mode auto, got %s", args[pmIdx+1])
	}
}

func TestBuildArgs_NoAllowedToolsUsesDefaults(t *testing.T) {
	b := defaultBuilder()
	a := &models.Agent{ID: uuid.New(), Name: "Coder", Folder: "/tmp", AgentType: models.AgentTypeClaude}
	args, err := b.BuildArgs(a, nil, defaultSettings(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have all default tools
	for _, tool := range []string{"mcp__skwad__*", "Read", "Write", "Edit", "Glob", "Grep", "Bash(*)", "Agent"} {
		if !containsArg(args, tool) {
			t.Errorf("expected default tool %q in args", tool)
		}
	}
}

func TestBuildArgs_ExploreModeOverridesAllowedTools(t *testing.T) {
	b := defaultBuilder()
	a := &models.Agent{
		ID:           uuid.New(),
		Name:         "Explorer",
		Folder:       "/tmp",
		AgentType:    models.AgentTypeClaude,
		ExploreMode:  true,
		AllowedTools: []string{"Write", "Edit", "Bash(*)"},
	}
	args, err := b.BuildArgs(a, nil, defaultSettings(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// ExploreMode should win — plan mode, explore tools
	pmIdx := argIndex(args, "--permission-mode")
	if pmIdx < 0 || pmIdx+1 >= len(args) {
		t.Fatal("missing --permission-mode")
	}
	if args[pmIdx+1] != "plan" {
		t.Errorf("expected --permission-mode plan (explore mode), got %s", args[pmIdx+1])
	}
	// Should NOT have the AllowedTools values since explore mode wins
	for _, tool := range []string{"Write", "Edit", "Bash(*)"} {
		if containsArg(args, tool) {
			t.Errorf("explore mode should override AllowedTools — should not have %q", tool)
		}
	}
	// Should have explore tools
	for _, tool := range []string{"Read", "Glob", "Grep", "WebSearch", "WebFetch"} {
		if !containsArg(args, tool) {
			t.Errorf("explore mode should have tool %q", tool)
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
