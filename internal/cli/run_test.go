package cli

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lsinghkochava/skwad-cli/internal/config"
	"github.com/lsinghkochava/skwad-cli/internal/models"
	"github.com/lsinghkochava/skwad-cli/internal/persistence"
	"github.com/lsinghkochava/skwad-cli/internal/report"
)

func TestIsRetryableExit(t *testing.T) {
	cases := []struct {
		code int
		want bool
	}{
		{0, false},     // success
		{1, true},      // general failure — retryable
		{2, false},     // permission denied
		{130, false},   // SIGINT
		{137, false},   // SIGKILL
		{42, true},     // unknown non-zero — retryable
		{255, true},    // high error code — retryable
	}
	for _, tc := range cases {
		got := isRetryableExit(tc.code)
		if got != tc.want {
			t.Errorf("isRetryableExit(%d) = %v, want %v", tc.code, got, tc.want)
		}
	}
}

func TestParseDurationSpec_Days(t *testing.T) {
	d, err := parseDurationSpec("7d")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d != 7*24*time.Hour {
		t.Errorf("expected 168h, got %v", d)
	}
}

func TestParseDurationSpec_Hours(t *testing.T) {
	d, err := parseDurationSpec("24h")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d != 24*time.Hour {
		t.Errorf("expected 24h, got %v", d)
	}
}

func TestParseDurationSpec_Minutes(t *testing.T) {
	d, err := parseDurationSpec("30m")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d != 30*time.Minute {
		t.Errorf("expected 30m, got %v", d)
	}
}

func TestParseDurationSpec_Invalid(t *testing.T) {
	_, err := parseDurationSpec("abc")
	if err == nil {
		t.Error("expected error for invalid duration spec")
	}
}

func TestParseDurationSpec_InvalidDays(t *testing.T) {
	_, err := parseDurationSpec("xd")
	if err == nil {
		t.Error("expected error for 'xd'")
	}
}

func TestResolveAgentPrompt_PerAgentFirst(t *testing.T) {
	ac := config.AgentConfig{Prompt: "agent-specific"}
	result := resolveAgentPrompt(ac, "flag-prompt", "team-prompt")
	if result != "agent-specific" {
		t.Errorf("expected per-agent prompt, got %q", result)
	}
}

func TestResolveAgentPrompt_FlagSecond(t *testing.T) {
	ac := config.AgentConfig{}
	result := resolveAgentPrompt(ac, "flag-prompt", "team-prompt")
	if result != "flag-prompt" {
		t.Errorf("expected flag prompt, got %q", result)
	}
}

func TestResolveAgentPrompt_TeamFallback(t *testing.T) {
	ac := config.AgentConfig{}
	result := resolveAgentPrompt(ac, "", "team-prompt")
	if result != "team-prompt" {
		t.Errorf("expected team prompt, got %q", result)
	}
}

func TestResolveAgentPrompt_Empty(t *testing.T) {
	ac := config.AgentConfig{}
	result := resolveAgentPrompt(ac, "", "")
	if result != "" {
		t.Errorf("expected empty prompt, got %q", result)
	}
}

func TestFilterRunOutput_EntryMode(t *testing.T) {
	agents := []report.AgentResult{
		{Name: "Builder", ResultText: "built it"},
		{Name: "QA", ResultText: "tested it"},
	}
	got := filterRunOutput("entry", agents, "Builder")
	if len(got) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(got))
	}
	if got[0].Name != "Builder" {
		t.Errorf("expected Builder, got %q", got[0].Name)
	}
}

func TestFilterRunOutput_EntryModeFallback(t *testing.T) {
	agents := []report.AgentResult{
		{Name: "Alpha", ResultText: "first"},
		{Name: "Beta", ResultText: "second"},
	}
	got := filterRunOutput("entry", agents, "")
	if len(got) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(got))
	}
	if got[0].Name != "Alpha" {
		t.Errorf("expected Alpha (first agent fallback), got %q", got[0].Name)
	}
}

func TestFilterRunOutput_EntryModeNotFound(t *testing.T) {
	agents := []report.AgentResult{
		{Name: "Alpha", ResultText: "first"},
	}
	got := filterRunOutput("entry", agents, "NonExistent")
	if len(got) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(got))
	}
	if got[0].Name != "Alpha" {
		t.Errorf("expected Alpha (fallback), got %q", got[0].Name)
	}
}

func TestFilterRunOutput_AllMode(t *testing.T) {
	agents := []report.AgentResult{
		{Name: "Builder", ResultText: "built it"},
		{Name: "QA", ResultText: "tested it"},
	}
	got := filterRunOutput("all", agents, "Builder")
	if len(got) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(got))
	}
}

func TestFilterRunOutput_RawMode(t *testing.T) {
	agents := []report.AgentResult{
		{Name: "Builder", ResultText: "built it"},
	}
	got := filterRunOutput("raw", agents, "Builder")
	if got != nil {
		t.Errorf("expected nil for raw mode, got %v", got)
	}
}

func TestFilterRunOutput_EmptyAgents(t *testing.T) {
	got := filterRunOutput("entry", nil, "")
	if got != nil {
		t.Errorf("expected nil for empty agents, got %v", got)
	}
}

func TestParseResultText(t *testing.T) {
	raw := json.RawMessage(`{"type":"result","subtype":"success","result":"All done!","num_turns":5,"total_cost_usd":0.12}`)
	got := parseResultText(raw)
	if got != "All done!" {
		t.Errorf("expected 'All done!', got %q", got)
	}
}

func TestParseResultText_EmptyResult(t *testing.T) {
	raw := json.RawMessage(`{"type":"result","subtype":"success","result":""}`)
	got := parseResultText(raw)
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestParseResultText_InvalidJSON(t *testing.T) {
	got := parseResultText(json.RawMessage(`{bad json`))
	if got != "" {
		t.Errorf("expected empty string for invalid JSON, got %q", got)
	}
}

func TestParseResultText_Nil(t *testing.T) {
	got := parseResultText(nil)
	if got != "" {
		t.Errorf("expected empty string for nil input, got %q", got)
	}
}

func TestParseResultText_NoResultField(t *testing.T) {
	raw := json.RawMessage(`{"type":"result","subtype":"success"}`)
	got := parseResultText(raw)
	if got != "" {
		t.Errorf("expected empty string when result field is absent, got %q", got)
	}
}

// --- Resume orchestration tests ---

func makeRunState(runID string, completed, failed bool, agents map[string]persistence.AgentRunState) *persistence.RunState {
	return &persistence.RunState{
		RunID:     runID,
		Agents:    agents,
		Completed: completed,
		Failed:    failed,
	}
}

func makeAgent(name string) *models.Agent {
	return &models.Agent{
		ID:   uuid.New(),
		Name: name,
	}
}

func TestResolveResumeAgents_CompletedRunReturnsError(t *testing.T) {
	state := makeRunState("run-1", true, false, map[string]persistence.AgentRunState{
		"a1": {AgentID: uuid.New().String(), AgentName: "Builder", Exited: true, ExitCode: 0},
	})
	agents := []*models.Agent{makeAgent("Builder")}

	_, err := resolveResumeAgents(state, agents)
	if err == nil {
		t.Fatal("expected error for completed run")
	}
	if !strings.Contains(err.Error(), "already completed") {
		t.Errorf("expected 'already completed' error, got: %v", err)
	}
}

func TestResolveResumeAgents_AllAgentsSucceeded(t *testing.T) {
	id1 := uuid.New()
	id2 := uuid.New()
	state := makeRunState("run-2", false, true, map[string]persistence.AgentRunState{
		id1.String(): {AgentID: id1.String(), AgentName: "Builder", Exited: true, ExitCode: 0},
		id2.String(): {AgentID: id2.String(), AgentName: "QA", Exited: true, ExitCode: 0},
	})
	agents := []*models.Agent{makeAgent("Builder"), makeAgent("QA")}

	_, err := resolveResumeAgents(state, agents)
	if err == nil {
		t.Fatal("expected error when all agents completed successfully")
	}
	if !strings.Contains(err.Error(), "nothing to resume") {
		t.Errorf("expected 'nothing to resume' error, got: %v", err)
	}
}

func TestResolveResumeAgents_SkipsCompletedAgents(t *testing.T) {
	builderID := uuid.New()
	qaID := uuid.New()
	state := makeRunState("run-3", false, true, map[string]persistence.AgentRunState{
		builderID.String(): {AgentID: builderID.String(), AgentName: "Builder", Exited: true, ExitCode: 0, SessionID: "sess-b"},
		qaID.String():      {AgentID: qaID.String(), AgentName: "QA", Exited: true, ExitCode: 1, SessionID: "sess-q"},
	})
	agents := []*models.Agent{makeAgent("Builder"), makeAgent("QA")}

	rr, err := resolveResumeAgents(state, agents)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rr.agents) != 1 {
		t.Fatalf("expected 1 agent to resume, got %d", len(rr.agents))
	}
	if rr.agents[0].Name != "QA" {
		t.Errorf("expected QA to resume, got %q", rr.agents[0].Name)
	}
	if rr.skippedCount != 1 {
		t.Errorf("expected 1 skipped, got %d", rr.skippedCount)
	}
}

func TestResolveResumeAgents_RestoresUUIDs(t *testing.T) {
	originalID := uuid.New()
	state := makeRunState("run-4", false, true, map[string]persistence.AgentRunState{
		originalID.String(): {AgentID: originalID.String(), AgentName: "Builder", Exited: true, ExitCode: 1, SessionID: "sess-123"},
	})
	agent := makeAgent("Builder")
	newID := agent.ID // capture the auto-generated ID

	rr, err := resolveResumeAgents(state, []*models.Agent{agent})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Agent ID should be restored to the original from event log.
	if rr.agents[0].ID != originalID {
		t.Errorf("expected restored ID %s, got %s", originalID, rr.agents[0].ID)
	}
	// idSwaps should map old → new.
	if rr.idSwaps[newID] != originalID {
		t.Errorf("expected idSwaps[%s] = %s, got %s", newID, originalID, rr.idSwaps[newID])
	}
}

func TestResolveResumeAgents_SetsResumeSessionID(t *testing.T) {
	agentID := uuid.New()
	state := makeRunState("run-5", false, true, map[string]persistence.AgentRunState{
		agentID.String(): {AgentID: agentID.String(), AgentName: "Builder", Exited: true, ExitCode: 1, SessionID: "sess-resume-me"},
	})
	agent := makeAgent("Builder")

	rr, err := resolveResumeAgents(state, []*models.Agent{agent})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rr.agents[0].ResumeSessionID != "sess-resume-me" {
		t.Errorf("expected ResumeSessionID 'sess-resume-me', got %q", rr.agents[0].ResumeSessionID)
	}
}

func TestResolveResumeAgents_CollectsLastPrompt(t *testing.T) {
	agentID := uuid.New()
	state := makeRunState("run-6", false, true, map[string]persistence.AgentRunState{
		agentID.String(): {AgentID: agentID.String(), AgentName: "Builder", Exited: true, ExitCode: 1, LastPrompt: "implement feature X"},
	})
	agent := makeAgent("Builder")

	rr, err := resolveResumeAgents(state, []*models.Agent{agent})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rr.resumePrompts[rr.agents[0].ID] != "implement feature X" {
		t.Errorf("expected resume prompt 'implement feature X', got %q", rr.resumePrompts[rr.agents[0].ID])
	}
}

func TestResolveResumeAgents_NewAgentNotInPriorRun(t *testing.T) {
	builderID := uuid.New()
	state := makeRunState("run-7", false, true, map[string]persistence.AgentRunState{
		builderID.String(): {AgentID: builderID.String(), AgentName: "Builder", Exited: true, ExitCode: 0},
	})
	// Team config has Builder (completed) + Reviewer (new).
	agents := []*models.Agent{makeAgent("Builder"), makeAgent("Reviewer")}

	rr, err := resolveResumeAgents(state, agents)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Builder completed → skipped. Reviewer is new → included.
	if len(rr.agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(rr.agents))
	}
	if rr.agents[0].Name != "Reviewer" {
		t.Errorf("expected Reviewer, got %q", rr.agents[0].Name)
	}
}

func TestResolveResumeAgents_AgentNotExited(t *testing.T) {
	agentID := uuid.New()
	state := makeRunState("run-8", false, false, map[string]persistence.AgentRunState{
		agentID.String(): {AgentID: agentID.String(), AgentName: "Builder", Exited: false, SessionID: "sess-interrupted"},
	})
	agent := makeAgent("Builder")

	rr, err := resolveResumeAgents(state, []*models.Agent{agent})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rr.agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(rr.agents))
	}
	if rr.agents[0].ResumeSessionID != "sess-interrupted" {
		t.Errorf("expected ResumeSessionID 'sess-interrupted', got %q", rr.agents[0].ResumeSessionID)
	}
	if rr.skippedCount != 0 {
		t.Errorf("non-exited agent should not be skipped, got skippedCount=%d", rr.skippedCount)
	}
}

func TestFilterRunOutput_UnrecognizedMode(t *testing.T) {
	agents := []report.AgentResult{
		{Name: "Builder", ResultText: "built it"},
	}
	got := filterRunOutput("unknown-mode", agents, "Builder")
	if got != nil {
		t.Error("unrecognized mode should return nil (same as raw)")
	}
}
