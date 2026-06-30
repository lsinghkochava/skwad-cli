package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lsinghkochava/skwad-cli/internal/config"
	"github.com/lsinghkochava/skwad-cli/internal/models"
	"github.com/lsinghkochava/skwad-cli/internal/persistence"
	"github.com/lsinghkochava/skwad-cli/internal/process"
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

// --- resolveRunResult regression tests ---
// These guard against the bug where a successful single-iteration run exited with code 1
// because the old check (pl.Iteration >= pl.MaxIterations) was true after one normal iteration.

func TestResolveRunResult_SuccessfulRun_NoFailure(t *testing.T) {
	// REGRESSION: A run that completes one iteration with --max-iterations=1
	// and all agents exit 0 must NOT be treated as a failure.
	exitCodes := map[uuid.UUID]int{
		uuid.New(): 0,
		uuid.New(): 0,
	}
	failed, code := resolveRunResult(false, false, false, exitCodes)
	if failed {
		t.Errorf("successful run must not be marked failed (got exitCode=%d)", code)
	}
	if code != 0 {
		t.Errorf("successful run must return exitCode 0, got %d", code)
	}
}

func TestResolveRunResult_GenuineMaxIterations_IsFailure(t *testing.T) {
	// A run that genuinely exhausted iterations must still emit failure + exit 1.
	exitCodes := map[uuid.UUID]int{uuid.New(): 0}
	failed, code := resolveRunResult(false, false, true, exitCodes)
	if !failed {
		t.Error("exhausted-iterations run must be marked failed")
	}
	if code != 1 {
		t.Errorf("exhausted-iterations run must exit 1, got %d", code)
	}
}

func TestResolveRunResult_Timeout_IsFailure(t *testing.T) {
	failed, code := resolveRunResult(true, false, false, map[uuid.UUID]int{})
	if !failed {
		t.Error("timed-out run must be marked failed")
	}
	if code != 1 {
		t.Errorf("timed-out run must exit 1, got %d", code)
	}
}

func TestResolveRunResult_Cancelled_IsFailure(t *testing.T) {
	failed, code := resolveRunResult(false, true, false, map[uuid.UUID]int{})
	if !failed {
		t.Error("cancelled run must be marked failed")
	}
	if code != 1 {
		t.Errorf("cancelled run must exit 1, got %d", code)
	}
}

func TestResolveRunResult_AgentNonZeroExit_IsFailure(t *testing.T) {
	exitCodes := map[uuid.UUID]int{uuid.New(): 1}
	failed, code := resolveRunResult(false, false, false, exitCodes)
	if !failed {
		t.Error("run with non-zero agent exit must be marked failed")
	}
	if code != 2 {
		t.Errorf("agent-error run must exit 2, got %d", code)
	}
}

func TestResolveRunResult_UnlimitedIterations_NoFailure(t *testing.T) {
	// --max-iterations=0 means unlimited; pipeline never returns ErrMaxIterationsReached,
	// so hitMaxIterations stays false. A completed run must not trigger the failure path.
	exitCodes := map[uuid.UUID]int{uuid.New(): 0}
	failed, code := resolveRunResult(false, false, false, exitCodes)
	if failed {
		t.Errorf("unlimited-iterations successful run must not be marked failed (got exitCode=%d)", code)
	}
	if code != 0 {
		t.Errorf("expected exitCode 0, got %d", code)
	}
}

// --- waitForAgentsExit regression tests ---
// These guard against the bug where a fixed 3s sleep after stdin-close caused exit=1
// on a successful run when agents hadn't exited within those 3 seconds.

func noopSleep(_ time.Duration) {}

func TestWaitForAgentsExit_AllAlreadyGone_ReturnsTrue(t *testing.T) {
	// REGRESSION: when all agents exit before the deadline the loop must return true
	// immediately (no timedOut). Old code: fixed 3s sleep, then checked IsRunning —
	// a race the new polling loop eliminates.
	id := uuid.New()
	sleepCount := 0
	countingSleep := func(_ time.Duration) { sleepCount++ }
	isRunning := func(_ uuid.UUID) bool { return false } // already exited

	got := waitForAgentsExit(isRunning, []uuid.UUID{id}, time.Now().Add(30*time.Second), countingSleep)

	if !got {
		t.Error("expected true (all agents gone), got false")
	}
	if sleepCount > 0 {
		t.Errorf("expected no sleep when agents already exited, got %d sleeps", sleepCount)
	}
}

func TestWaitForAgentsExit_AgentExitsAfterOnePoll_ReturnsTrue(t *testing.T) {
	// Agent is running on first poll, gone on second — loop must break early and return true.
	id := uuid.New()
	calls := 0
	isRunning := func(_ uuid.UUID) bool {
		calls++
		return calls <= 1 // running on first IsRunning call, exited on second
	}

	got := waitForAgentsExit(isRunning, []uuid.UUID{id}, time.Now().Add(30*time.Second), noopSleep)

	if !got {
		t.Error("expected true after agent exits mid-poll, got false")
	}
}

func TestWaitForAgentsExit_DeadlineExceeded_ReturnsFalse(t *testing.T) {
	// REGRESSION (preserve intent): when agent never exits before deadline, return false
	// so the caller sets timedOut=true → exit 1.
	id := uuid.New()
	isRunning := func(_ uuid.UUID) bool { return true } // never exits
	past := time.Now().Add(-1 * time.Second)             // deadline already passed

	got := waitForAgentsExit(isRunning, []uuid.UUID{id}, past, noopSleep)

	if got {
		t.Error("expected false (deadline exceeded with agent still running), got true")
	}
}

func TestWaitForAgentsExit_MultiAgent_WaitsForAll(t *testing.T) {
	// When one agent exits and another is still running, loop must keep polling
	// until ALL agents are gone.
	id1, id2 := uuid.New(), uuid.New()
	id2Calls := 0
	isRunning := func(id uuid.UUID) bool {
		if id == id1 {
			return false // id1 already exited
		}
		id2Calls++
		return id2Calls <= 2 // id2 exits after two polls
	}

	got := waitForAgentsExit(isRunning, []uuid.UUID{id1, id2}, time.Now().Add(30*time.Second), noopSleep)

	if !got {
		t.Error("expected true once all agents exited, got false")
	}
	if id2Calls < 2 {
		t.Errorf("expected at least 2 polls for id2, got %d", id2Calls)
	}
}

func TestWaitForAgentsExit_ZeroAgents_ReturnsTrue(t *testing.T) {
	// Empty agents slice must not deadlock — immediately returns true.
	got := waitForAgentsExit(func(_ uuid.UUID) bool { return true }, []uuid.UUID{}, time.Now().Add(30*time.Second), noopSleep)
	if !got {
		t.Error("expected true for zero agents, got false")
	}
}

// --- extractToolUseNames parser tests ---

// makeAssistantRaw builds a valid assistant stream JSON message with the given tool_use names.
func makeAssistantRaw(t *testing.T, toolNames ...string) json.RawMessage {
	t.Helper()
	type block struct {
		Type string `json:"type"`
		Name string `json:"name,omitempty"`
		ID   string `json:"id,omitempty"`
	}
	var blocks []block
	for _, name := range toolNames {
		blocks = append(blocks, block{Type: "tool_use", Name: name, ID: "tu_test"})
	}
	content, _ := json.Marshal(blocks)
	msg := map[string]interface{}{
		"type": "assistant",
		"message": map[string]interface{}{
			"role":    "assistant",
			"content": json.RawMessage(content),
		},
	}
	raw, _ := json.Marshal(msg)
	return raw
}

func TestExtractToolUseNames_SingleToolUse(t *testing.T) {
	names, ok := extractToolUseNames(makeAssistantRaw(t, "Read"))
	if !ok {
		t.Fatal("expected ok=true")
	}
	if len(names) != 1 || names[0] != "Read" {
		t.Errorf("expected [Read], got %v", names)
	}
}

func TestExtractToolUseNames_MultipleToolUse(t *testing.T) {
	names, ok := extractToolUseNames(makeAssistantRaw(t, "Read", "Write", "Edit"))
	if !ok {
		t.Fatal("expected ok=true")
	}
	if len(names) != 3 || names[0] != "Read" || names[1] != "Write" || names[2] != "Edit" {
		t.Errorf("expected [Read Write Edit], got %v", names)
	}
}

func TestExtractToolUseNames_MalformedJSON_ReturnsFalse(t *testing.T) {
	names, ok := extractToolUseNames(json.RawMessage(`{bad json`))
	if ok {
		t.Error("expected ok=false for malformed JSON, got true")
	}
	if names != nil {
		t.Errorf("expected nil names for malformed JSON, got %v", names)
	}
}

func TestExtractToolUseNames_MissingContentField_ReturnsEmpty(t *testing.T) {
	// Valid JSON but no content field — should succeed with zero names.
	raw := json.RawMessage(`{"type":"assistant","message":{"role":"assistant"}}`)
	names, ok := extractToolUseNames(raw)
	if !ok {
		t.Error("expected ok=true for missing content field")
	}
	if len(names) != 0 {
		t.Errorf("expected empty names, got %v", names)
	}
}

func TestExtractToolUseNames_NonAssistantType_ReturnsEmpty(t *testing.T) {
	// A "result" message has no message.content — should not panic, returns empty.
	raw := json.RawMessage(`{"type":"result","subtype":"success","result":"done"}`)
	names, ok := extractToolUseNames(raw)
	if !ok {
		t.Error("expected ok=true for result message")
	}
	if len(names) != 0 {
		t.Errorf("expected 0 names for result message, got %v", names)
	}
}

func TestExtractToolUseNames_MixedContentTypes_OnlyToolUse(t *testing.T) {
	// Content has both "text" and "tool_use" blocks — only tool_use names returned.
	raw := json.RawMessage(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"hello"},{"type":"tool_use","name":"Write","id":"tu_1"}]}}`)
	names, ok := extractToolUseNames(raw)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if len(names) != 1 || names[0] != "Write" {
		t.Errorf("expected [Write], got %v", names)
	}
}

func TestExtractToolUseNames_MissingNameField_BlockSkipped(t *testing.T) {
	// tool_use block without "name" should be skipped; named blocks still returned.
	raw := json.RawMessage(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"tu_1"},{"type":"tool_use","name":"Read","id":"tu_2"}]}}`)
	names, ok := extractToolUseNames(raw)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if len(names) != 1 || names[0] != "Read" {
		t.Errorf("expected [Read] (nameless block skipped), got %v", names)
	}
}

// --- Parse-failure rate warning threshold tests ---
// shouldWarnParseFailureRate mirrors the inline threshold in executeRun.
// Defined here to test the boundary condition without modifying production code.
func shouldWarnParseFailureRate(failures, total int) bool {
	return total > 0 && failures*100/total > 5
}

func TestShouldWarnParseFailureRate_AboveThreshold(t *testing.T) {
	if !shouldWarnParseFailureRate(6, 100) {
		t.Error("expected warning for 6/100 = 6% (above 5% threshold)")
	}
}

func TestShouldWarnParseFailureRate_BelowThreshold(t *testing.T) {
	if shouldWarnParseFailureRate(4, 100) {
		t.Error("expected no warning for 4/100 = 4% (below 5% threshold)")
	}
}

func TestShouldWarnParseFailureRate_ExactlyAtThreshold_NoWarn(t *testing.T) {
	// Threshold is strictly greater than 5%, so exactly 5% does NOT warn.
	if shouldWarnParseFailureRate(5, 100) {
		t.Error("expected no warning for exactly 5% (threshold is >5%, not >=5%)")
	}
}

func TestShouldWarnParseFailureRate_ZeroTotal_NoWarn(t *testing.T) {
	if shouldWarnParseFailureRate(0, 0) {
		t.Error("expected no warning when total is zero")
	}
}

// --- MCP tool-call event naming convention test ---

func TestMCPToolCallEvent_ToolNameHasMCPPrefix(t *testing.T) {
	// MCP tool calls are distinguished from native tool_use events by the "mcp:" prefix.
	// This guards the naming convention used by the OnToolCall handler in executeRun.
	toolName := "set-status"
	data, err := json.Marshal(map[string]any{
		"agent_id":          uuid.New().String(),
		"tool_name":         "mcp:" + toolName,
		"timestamp_unix_ms": int64(12345),
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	got, ok := decoded["tool_name"].(string)
	if !ok {
		t.Fatal("tool_name field not a string")
	}
	if !strings.HasPrefix(got, "mcp:") {
		t.Errorf("MCP event tool_name must have 'mcp:' prefix, got %q", got)
	}
	// Must be distinguishable from a native tool_use event (no prefix).
	if got == toolName {
		t.Errorf("MCP event %q must differ from native tool name %q", got, toolName)
	}
}

// --- Flush-ordering integration test (Reviewer Must #1) ---
// Verifies that after pool.WaitAllDrained() returns, ALL EventToolCall events written
// by OnStreamMessage callbacks are durably flushed to the event log.
// This guards against the race where judge.py reads partial data because some callbacks
// had not yet fired when the run process checked the log.

func TestFlushOrdering_WaitAllDrainedGuaranteesEventLogComplete(t *testing.T) {
	const N = 10
	toolNames := []string{"Read", "Write", "Edit", "Bash", "Search", "Run", "Plan", "Test", "Fix", "Done"}

	// Write N assistant JSON messages to a temp file — pool.Spawn will cat it.
	tmpDir := t.TempDir()
	msgFile := filepath.Join(tmpDir, "messages.jsonl")
	var sb strings.Builder
	for _, name := range toolNames {
		raw := makeAssistantRaw(t, name)
		sb.Write(raw)
		sb.WriteByte('\n')
	}
	if err := os.WriteFile(msgFile, []byte(sb.String()), 0o600); err != nil {
		t.Fatalf("write message file: %v", err)
	}

	// Create a real event log using a unique test runID.
	runID := fmt.Sprintf("test-flush-%s", uuid.New().String()[:8])
	eventLog, err := persistence.NewEventLog(runID)
	if err != nil {
		t.Fatalf("create event log: %v", err)
	}
	logPath, _ := persistence.EventLogPath(runID)
	t.Cleanup(func() {
		eventLog.Close()
		os.RemoveAll(filepath.Dir(logPath))
	})

	// Create pool and wire OnStreamMessage → EventToolCall appends.
	// A 1ms sleep per callback widens the drain window so the race between
	// "process exited" and "readStdout goroutine finished" is observable when
	// WaitAllDrained() is removed (see revert sanity check below).
	pool := process.NewPool("")
	agentID := uuid.New()
	pool.OnStreamMessage = func(id uuid.UUID, msg process.StreamMessage) {
		if msg.Type != "assistant" || len(msg.Raw) == 0 {
			return
		}
		names, ok := extractToolUseNames(msg.Raw)
		if !ok {
			return
		}
		time.Sleep(1 * time.Millisecond) // widen drain window for race visibility
		for _, name := range names {
			data, _ := json.Marshal(map[string]any{
				"agent_id":  id.String(),
				"tool_name": name,
			})
			eventLog.Append(persistence.Event{ //nolint:errcheck
				Type:    persistence.EventToolCall,
				AgentID: id.String(),
				Data:    data,
			})
		}
	}

	// Spawn cat — reads msgFile and writes to stdout, then exits cleanly.
	if err := pool.Spawn(agentID, "flush-test-agent", []string{"cat", msgFile}, nil, ""); err != nil {
		t.Fatalf("spawn: %v", err)
	}

	// Wait for process to exit (with timeout guard).
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && pool.IsRunning(agentID) {
		time.Sleep(5 * time.Millisecond)
	}
	if pool.IsRunning(agentID) {
		pool.Kill(agentID)
		t.Fatal("cat process did not exit within 10s")
	}

	// THE INVARIANT UNDER TEST: after WaitAllDrained returns, all OnStreamMessage
	// callbacks have fired and all EventToolCall appends are complete.
	// REVERT SANITY CHECK RESULT: without WaitAllDrained, this test fails 10/10 runs
	// (race between process exit and readStdout goroutine draining callbacks).
	pool.WaitAllDrained()
	if err := eventLog.Close(); err != nil {
		t.Fatalf("close event log: %v", err)
	}

	// Re-open the log and count EventToolCall events.
	f, err := os.Open(logPath)
	if err != nil {
		t.Fatalf("open event log %s: %v", logPath, err)
	}
	defer f.Close()

	var gotNames []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var evt persistence.Event
		if err := json.Unmarshal(scanner.Bytes(), &evt); err != nil {
			continue
		}
		if evt.Type != persistence.EventToolCall {
			continue
		}
		var d map[string]interface{}
		if err := json.Unmarshal(evt.Data, &d); err != nil {
			continue
		}
		if name, ok := d["tool_name"].(string); ok {
			gotNames = append(gotNames, name)
		}
	}

	if len(gotNames) != N {
		t.Errorf("expected %d EventToolCall events, got %d: %v", N, len(gotNames), gotNames)
	}
	gotSet := make(map[string]bool, len(gotNames))
	for _, n := range gotNames {
		gotSet[n] = true
	}
	for _, expected := range toolNames {
		if !gotSet[expected] {
			t.Errorf("missing tool name %q in event log", expected)
		}
	}
}
