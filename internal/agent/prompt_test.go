package agent

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/lsinghkochava/skwad-cli/internal/models"
)

func TestBuildSystemPrompt_AllLayers(t *testing.T) {
	agent := &models.Agent{
		ID:   uuid.New(),
		Name: "Coder",
	}
	persona := &models.Persona{
		Name:         "Senior Dev",
		Instructions: "You are a senior developer.",
	}
	teammates := []models.Agent{
		{ID: uuid.New(), Name: "Explorer"},
		{ID: uuid.New(), Name: "Tester"},
	}

	prompt := BuildSystemPrompt(agent, persona, teammates)

	// Layer 1: preamble
	if !strings.Contains(prompt, agent.ID.String()) {
		t.Error("prompt should contain agent ID")
	}
	if !strings.Contains(prompt, "skwad agent ID") {
		t.Error("prompt should contain preamble")
	}

	// Layer 2: team protocol
	if !strings.Contains(prompt, "Team Roster") {
		t.Error("prompt should contain team roster")
	}
	if !strings.Contains(prompt, "Explorer") {
		t.Error("prompt should contain teammate Explorer")
	}

	// Layer 3: role instructions
	if !strings.Contains(prompt, "Role: Coder") {
		t.Error("prompt should contain coder role instructions")
	}

	// Layer 4: persona
	if !strings.Contains(prompt, "Senior Dev") {
		t.Error("prompt should contain persona name")
	}
	if !strings.Contains(prompt, "You are a senior developer.") {
		t.Error("prompt should contain persona instructions")
	}
}

func TestBuildSystemPrompt_SoloAgent(t *testing.T) {
	agent := &models.Agent{
		ID:   uuid.New(),
		Name: "Coder",
	}
	prompt := BuildSystemPrompt(agent, nil, nil)

	if !strings.Contains(prompt, "skwad agent ID") {
		t.Error("prompt should contain preamble")
	}
	if strings.Contains(prompt, "Team Roster") {
		t.Error("prompt should NOT contain team protocol when no teammates")
	}
}

func TestPreamble_ContainsAgentID(t *testing.T) {
	id := uuid.New().String()
	preamble := buildPreamble(id)
	if !strings.Contains(preamble, id) {
		t.Error("preamble should contain the agent ID")
	}
}

func TestMatchRoleInstructions_AgentName(t *testing.T) {
	agent := &models.Agent{Name: "Explorer"}
	result := matchRoleInstructions(agent, nil)
	if !strings.Contains(result, "Role: Explorer") {
		t.Errorf("expected explorer instructions, got %q", result)
	}
}

func TestMatchRoleInstructions_NoMatch(t *testing.T) {
	agent := &models.Agent{Name: "My Custom Agent"}
	result := matchRoleInstructions(agent, nil)
	if result != "" {
		t.Errorf("expected no role instructions, got %q", result)
	}
}

func TestMatchRoleInstructions_CaseInsensitive(t *testing.T) {
	agent := &models.Agent{Name: "CODER"}
	result := matchRoleInstructions(agent, nil)
	if !strings.Contains(result, "Role: Coder") {
		t.Errorf("expected coder instructions for uppercase name, got %q", result)
	}
}

func TestMatchRoleInstructions_PersonaFallback(t *testing.T) {
	agent := &models.Agent{Name: "Agent-007"}
	persona := &models.Persona{Name: "Reviewer"}
	result := matchRoleInstructions(agent, persona)
	if !strings.Contains(result, "Role: Reviewer") {
		t.Errorf("expected reviewer instructions from persona name, got %q", result)
	}
}

func TestBuildSystemPrompt_ReasonableLength(t *testing.T) {
	agent := &models.Agent{
		ID:   uuid.New(),
		Name: "Coder",
	}
	persona := &models.Persona{
		Name:         "Senior Dev",
		Instructions: "Build great code.",
	}
	teammates := make([]models.Agent, 5)
	for i := range teammates {
		teammates[i] = models.Agent{ID: uuid.New(), Name: "Agent-" + string(rune('A'+i))}
	}

	prompt := BuildSystemPrompt(agent, persona, teammates)
	if len(prompt) > 12000 {
		t.Errorf("prompt too long: %d chars (max 12000)", len(prompt))
	}
}

func TestMatchRoleInstructions_SubstringMatch(t *testing.T) {
	// "Lead Coder" should match "coder" role via substring
	agent := &models.Agent{Name: "Lead Coder"}
	result := matchRoleInstructions(agent, nil)
	if !strings.Contains(result, "Role: Coder") {
		t.Errorf("expected 'Lead Coder' to match coder role, got %q", result)
	}
}

func TestMatchRoleInstructions_PersonaMatchesWhenAgentDoesNot(t *testing.T) {
	// Agent name "Bot" doesn't match any role, but persona "Reviewer" does
	agent := &models.Agent{Name: "Bot"}
	persona := &models.Persona{Name: "Reviewer"}
	result := matchRoleInstructions(agent, persona)
	if !strings.Contains(result, "Role: Reviewer") {
		t.Errorf("expected persona 'Reviewer' to match reviewer role, got %q", result)
	}
}

func TestMatchRoleInstructions_AgentTakesPriorityOverPersona(t *testing.T) {
	// Agent name "Coder" matches coder, persona "Reviewer" matches reviewer
	// Agent name should win
	agent := &models.Agent{Name: "Coder"}
	persona := &models.Persona{Name: "Reviewer"}
	result := matchRoleInstructions(agent, persona)
	if !strings.Contains(result, "Role: Coder") {
		t.Errorf("expected agent name 'Coder' to take priority, got %q", result)
	}
	if strings.Contains(result, "Role: Reviewer") {
		t.Error("reviewer role should NOT be included when agent name matches coder")
	}
}

func TestMatchRoleInstructions_AllRoles(t *testing.T) {
	// Verify all 5 known roles are matchable
	roles := map[string]string{
		"explorer": "Role: Explorer",
		"coder":    "Role: Coder",
		"tester":   "Role: Tester",
		"reviewer": "Role: Reviewer",
		"manager":  "Role: Manager",
	}
	for name, expected := range roles {
		agent := &models.Agent{Name: name}
		result := matchRoleInstructions(agent, nil)
		if !strings.Contains(result, expected) {
			t.Errorf("agent name %q should match %q", name, expected)
		}
	}
}

func TestBuildSystemPrompt_NoPersonaNoRole(t *testing.T) {
	agent := &models.Agent{
		ID:   uuid.New(),
		Name: "MyCustomBot",
	}
	prompt := BuildSystemPrompt(agent, nil, nil)

	// Should have preamble but no role or persona sections
	if !strings.Contains(prompt, "skwad agent ID") {
		t.Error("should contain preamble")
	}
	if strings.Contains(prompt, "## Role:") {
		t.Error("should NOT contain role instructions for unmatched name")
	}
	if strings.Contains(prompt, "## Persona:") {
		t.Error("should NOT contain persona section when nil")
	}
}

func TestBuildSystemPrompt_PersonaNoInstructions(t *testing.T) {
	agent := &models.Agent{
		ID:   uuid.New(),
		Name: "Bot",
	}
	persona := &models.Persona{
		Name:         "Empty Persona",
		Instructions: "",
	}
	prompt := BuildSystemPrompt(agent, persona, nil)

	// Persona section should be omitted when instructions are empty
	if strings.Contains(prompt, "Persona: Empty Persona") {
		t.Error("should NOT include persona section when instructions are empty")
	}
}

func TestBuildTeamProtocol_ContainsAllTeammates(t *testing.T) {
	agent := &models.Agent{
		ID:   uuid.New(),
		Name: "Manager",
	}
	teammates := []models.Agent{
		{ID: uuid.New(), Name: "Coder"},
		{ID: uuid.New(), Name: "Tester"},
		{ID: uuid.New(), Name: "Reviewer"},
	}

	protocol := buildTeamProtocol(agent, teammates)

	for _, t2 := range teammates {
		if !strings.Contains(protocol, t2.Name) {
			t.Errorf("team protocol should list teammate %q", t2.Name)
		}
		if !strings.Contains(protocol, t2.ID.String()) {
			t.Errorf("team protocol should list teammate ID %q", t2.ID.String())
		}
	}

	// Should contain agent's own name
	if !strings.Contains(protocol, "Manager") {
		t.Error("team protocol should contain agent's own name")
	}
}

func TestBuildSystemPrompt_WorktreeIsolation(t *testing.T) {
	agent := &models.Agent{
		ID:             uuid.New(),
		Name:           "Coder",
		WorktreePath:   "/tmp/repo-coder",
		WorktreeBranch: "skwad/abc123/coder",
	}
	prompt := BuildSystemPrompt(agent, nil, nil)

	if !strings.Contains(prompt, "Git Worktree Isolation") {
		t.Error("prompt should contain worktree isolation section")
	}
	if !strings.Contains(prompt, "skwad/abc123/coder") {
		t.Error("prompt should contain the branch name")
	}
	if !strings.Contains(prompt, "/tmp/repo-coder") {
		t.Error("prompt should contain the worktree path")
	}
	if !strings.Contains(prompt, "git checkout") {
		t.Error("prompt should warn against checkout")
	}
}

func TestBuildCoordinationPrompt_Managed(t *testing.T) {
	prompt := buildCoordinationPrompt("managed")
	if !strings.Contains(prompt, "Managed Mode") {
		t.Error("expected 'Managed Mode' in prompt")
	}
	if !strings.Contains(prompt, "Wait for task assignments") {
		t.Error("expected 'Wait for task assignments' in managed prompt")
	}
	if !strings.Contains(prompt, "claim-task") {
		t.Error("managed mode should mention claim-task")
	}
}

func TestBuildCoordinationPrompt_Autonomous(t *testing.T) {
	prompt := buildCoordinationPrompt("autonomous")
	if !strings.Contains(prompt, "Autonomous Mode") {
		t.Error("expected 'Autonomous Mode' in prompt")
	}
	if !strings.Contains(prompt, "claim-task") {
		t.Error("autonomous mode should mention claim-task")
	}
	if !strings.Contains(prompt, "create-task") {
		t.Error("autonomous mode should mention create-task")
	}
}

func TestBuildSystemPrompt_WithCoordinationMode(t *testing.T) {
	agent := &models.Agent{
		ID:               uuid.New(),
		Name:             "Coder",
		CoordinationMode: "autonomous",
	}
	prompt := BuildSystemPrompt(agent, nil, nil)
	if !strings.Contains(prompt, "Autonomous Mode") {
		t.Error("prompt should contain coordination section for autonomous mode")
	}
}

func TestBuildSystemPrompt_EmptyCoordinationMode(t *testing.T) {
	agent := &models.Agent{
		ID:   uuid.New(),
		Name: "Coder",
	}
	prompt := BuildSystemPrompt(agent, nil, nil)
	if strings.Contains(prompt, "Task Coordination") {
		t.Error("prompt should NOT contain coordination section when mode is empty")
	}
}

func TestBuildSystemPrompt_NoWorktree(t *testing.T) {
	agent := &models.Agent{
		ID:   uuid.New(),
		Name: "Coder",
	}
	prompt := BuildSystemPrompt(agent, nil, nil)

	if strings.Contains(prompt, "Git Worktree Isolation") {
		t.Error("prompt should NOT contain worktree section when no worktree")
	}
}

// ---------------------------------------------------------------------------
// Verify-before-implement preamble tests
// ---------------------------------------------------------------------------

func TestPreamble_ContainsVerifyBeforeImplement(t *testing.T) {
	preamble := buildPreamble(uuid.New().String())
	if !strings.Contains(preamble, "Before implementing any change, verify the current state") {
		t.Error("preamble should contain 'verify before implement' guidance")
	}
}

func TestBuildSystemPrompt_AllRoles_ContainVerifyBeforeImplement(t *testing.T) {
	// The preamble is universal — every agent type should get it.
	roles := []string{"Coder", "Tester", "Explorer", "Reviewer", "Manager", "CustomBot"}
	for _, name := range roles {
		t.Run(name, func(t *testing.T) {
			agent := &models.Agent{ID: uuid.New(), Name: name}
			prompt := BuildSystemPrompt(agent, nil, nil)
			if !strings.Contains(prompt, "Before implementing any change, verify the current state") {
				t.Errorf("agent %q prompt should contain verify-before-implement text", name)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Completion message format tests
// ---------------------------------------------------------------------------

func TestBuildCoordinationPrompt_Autonomous_ContainsCompletionFormat(t *testing.T) {
	prompt := buildCoordinationPrompt("autonomous")
	if !strings.Contains(prompt, "Completion Message Format") {
		t.Error("autonomous prompt should contain 'Completion Message Format' section")
	}
	if !strings.Contains(prompt, "What you changed") {
		t.Error("autonomous prompt should contain completion format details")
	}
}

func TestBuildCoordinationPrompt_Managed_NoCompletionFormat(t *testing.T) {
	prompt := buildCoordinationPrompt("managed")
	if strings.Contains(prompt, "Completion Message Format") {
		t.Error("managed prompt should NOT contain 'Completion Message Format' — this is CRITICAL")
	}
}

func TestBuildCoordinationPrompt_Default_NoCompletionFormat(t *testing.T) {
	// Empty string defaults to managed mode.
	prompt := buildCoordinationPrompt("")
	if strings.Contains(prompt, "Completion Message Format") {
		t.Error("default (empty) mode prompt should NOT contain completion format")
	}
}

func TestBuildSystemPrompt_AutonomousAgent_HasCompletionFormat(t *testing.T) {
	agent := &models.Agent{
		ID:               uuid.New(),
		Name:             "Coder",
		CoordinationMode: "autonomous",
	}
	prompt := BuildSystemPrompt(agent, nil, nil)
	if !strings.Contains(prompt, "Completion Message Format") {
		t.Error("full prompt for autonomous agent should contain completion format")
	}
}

func TestBuildSystemPrompt_ManagedAgent_NoCompletionFormat(t *testing.T) {
	agent := &models.Agent{
		ID:               uuid.New(),
		Name:             "Coder",
		CoordinationMode: "managed",
	}
	prompt := BuildSystemPrompt(agent, nil, nil)
	if strings.Contains(prompt, "Completion Message Format") {
		t.Error("full prompt for managed agent should NOT contain completion format")
	}
}

// ---------------------------------------------------------------------------
// Tag display tests
// ---------------------------------------------------------------------------

func TestBuildTeamProtocol_IncludesTagsColumn(t *testing.T) {
	agent := &models.Agent{
		ID:   uuid.New(),
		Name: "Manager",
	}
	teammates := []models.Agent{
		{ID: uuid.New(), Name: "Coder", Tags: []string{"code", "backend"}},
		{ID: uuid.New(), Name: "Tester", Tags: []string{"test"}},
	}

	protocol := buildTeamProtocol(agent, teammates)

	if !strings.Contains(protocol, "Tags") {
		t.Error("team roster should include a Tags column header")
	}
	if !strings.Contains(protocol, "code") {
		t.Error("team roster should show Coder's 'code' tag")
	}
	if !strings.Contains(protocol, "backend") {
		t.Error("team roster should show Coder's 'backend' tag")
	}
	if !strings.Contains(protocol, "test") {
		t.Error("team roster should show Tester's 'test' tag")
	}
}

func TestBuildTeamProtocol_NoTags_EmptyTagColumn(t *testing.T) {
	agent := &models.Agent{
		ID:   uuid.New(),
		Name: "Manager",
	}
	teammates := []models.Agent{
		{ID: uuid.New(), Name: "Coder"},
	}

	protocol := buildTeamProtocol(agent, teammates)

	// Should still have the Tags column header even if agents have no tags.
	if !strings.Contains(protocol, "Tags") {
		t.Error("team roster should include Tags column header even when agents have no tags")
	}
}

func TestBuildCoordinationPrompt_Autonomous_MentionsTags(t *testing.T) {
	prompt := buildCoordinationPrompt("autonomous")
	if !strings.Contains(prompt, "tags") && !strings.Contains(prompt, "Tags") {
		t.Error("autonomous mode prompt should mention tags for task claiming")
	}
}

func TestBuildCoordinationPrompt_Managed_MentionsTags(t *testing.T) {
	prompt := buildCoordinationPrompt("managed")
	if !strings.Contains(prompt, "tags") && !strings.Contains(prompt, "Tags") {
		t.Error("managed mode prompt should mention tags for task creation")
	}
}
