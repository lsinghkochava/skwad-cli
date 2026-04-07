package persistence

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func setupTestEventLog(t *testing.T, runID string) *EventLog {
	t.Helper()
	dir := t.TempDir()
	runDir := filepath.Join(dir, "runs", runID)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(runDir, "events.jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	return &EventLog{
		file:  f,
		runID: runID,
		enc:   json.NewEncoder(f),
	}
}

func replayTestLog(t *testing.T, logFile string) *RunState {
	t.Helper()
	f, err := os.Open(logFile)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	state := &RunState{
		Agents: make(map[string]AgentRunState),
	}

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var event Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			continue
		}
		state.RunID = event.RunID
		state.LastEvent = event
		applyEvent(state, event)
	}
	return state
}

func TestEventLog_AppendAndReplay(t *testing.T) {
	el := setupTestEventLog(t, "test-run-1")

	el.Append(Event{Type: EventRunStarted})
	el.Append(Event{Type: EventAgentSpawned, AgentID: "a1", AgentName: "Coder"})
	el.Append(Event{Type: EventAgentSpawned, AgentID: "a2", AgentName: "Tester"})
	exitData, _ := json.Marshal(0)
	el.Append(Event{Type: EventAgentExited, AgentID: "a1", Data: exitData})
	el.Append(Event{Type: EventRunCompleted})
	el.Close()

	state := replayTestLog(t, el.file.Name())
	if state.RunID != "test-run-1" {
		t.Errorf("expected runID test-run-1, got %s", state.RunID)
	}
	if len(state.Agents) != 2 {
		t.Errorf("expected 2 agents, got %d", len(state.Agents))
	}
	if !state.Agents["a1"].Exited {
		t.Error("agent a1 should be exited")
	}
	if state.Agents["a2"].Exited {
		t.Error("agent a2 should NOT be exited")
	}
	if !state.Completed {
		t.Error("run should be completed")
	}
}

func TestEventLog_ConcurrentAppends(t *testing.T) {
	el := setupTestEventLog(t, "test-concurrent")
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			el.Append(Event{Type: EventAgentSpawned, AgentID: "a1"})
		}()
	}
	wg.Wait()
	el.Close()

	// Verify file is not corrupted — all lines should parse.
	state := replayTestLog(t, el.file.Name())
	if !state.Agents["a1"].Spawned {
		t.Error("agent a1 should be spawned")
	}
}

func TestReplay_EmptyLog(t *testing.T) {
	el := setupTestEventLog(t, "test-empty")
	el.Close()

	state := replayTestLog(t, el.file.Name())
	if len(state.Agents) != 0 {
		t.Errorf("expected 0 agents, got %d", len(state.Agents))
	}
	if state.Completed {
		t.Error("should not be completed")
	}
}

func TestReplay_PartialRun(t *testing.T) {
	el := setupTestEventLog(t, "test-partial")
	el.Append(Event{Type: EventRunStarted})
	el.Append(Event{Type: EventAgentSpawned, AgentID: "a1", AgentName: "Coder"})
	// No completed event — simulates crash
	el.Close()

	state := replayTestLog(t, el.file.Name())
	if state.Completed {
		t.Error("should NOT be completed without EventRunCompleted")
	}
	if state.Failed {
		t.Error("should NOT be failed without EventRunFailed")
	}
	if !state.Agents["a1"].Spawned {
		t.Error("a1 should be spawned")
	}
}

func TestReplay_AgentNotExited(t *testing.T) {
	el := setupTestEventLog(t, "test-not-exited")
	el.Append(Event{Type: EventAgentSpawned, AgentID: "a1", AgentName: "Coder"})
	el.Close()

	state := replayTestLog(t, el.file.Name())
	if state.Agents["a1"].Exited {
		t.Error("agent should NOT be exited")
	}
}

func TestReplay_FullLifecycle(t *testing.T) {
	el := setupTestEventLog(t, "test-lifecycle")
	el.Append(Event{Type: EventRunStarted})
	el.Append(Event{Type: EventAgentSpawned, AgentID: "a1", AgentName: "Coder"})

	promptData, _ := json.Marshal("implement feature X")
	el.Append(Event{Type: EventPromptSent, AgentID: "a1", Data: promptData})

	phaseData, _ := json.Marshal("execute")
	el.Append(Event{Type: EventPhaseTransition, Data: phaseData})

	iterData, _ := json.Marshal(1)
	el.Append(Event{Type: EventIteration, Data: iterData})

	exitData, _ := json.Marshal(0)
	el.Append(Event{Type: EventAgentExited, AgentID: "a1", Data: exitData})
	el.Append(Event{Type: EventRunCompleted})
	el.Close()

	state := replayTestLog(t, el.file.Name())
	if !state.Completed {
		t.Error("run should be completed")
	}
	if state.CurrentPhase != "execute" {
		t.Errorf("expected phase 'execute', got %q", state.CurrentPhase)
	}
	if state.CurrentIteration != 1 {
		t.Errorf("expected iteration 1, got %d", state.CurrentIteration)
	}
	a1 := state.Agents["a1"]
	if a1.PromptsSent != 1 {
		t.Errorf("expected 1 prompt sent, got %d", a1.PromptsSent)
	}
	if a1.LastPrompt != "implement feature X" {
		t.Errorf("expected last prompt 'implement feature X', got %q", a1.LastPrompt)
	}
	if !a1.Exited || a1.ExitCode != 0 {
		t.Errorf("expected exited with code 0, got exited=%v code=%d", a1.Exited, a1.ExitCode)
	}
}

func TestReplay_MalformedLines_Skipped(t *testing.T) {
	el := setupTestEventLog(t, "test-malformed")
	// Write valid events
	el.Append(Event{Type: EventRunStarted})
	el.Append(Event{Type: EventAgentSpawned, AgentID: "a1", AgentName: "Coder"})
	el.Close()

	// Inject a malformed line between valid lines
	path := el.file.Name()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("{this is not valid json}\n")
	// Write another valid event after the malformed one
	validEvent, _ := json.Marshal(Event{Type: EventRunCompleted, RunID: "test-malformed"})
	f.Write(validEvent)
	f.WriteString("\n")
	f.Close()

	state := replayTestLog(t, path)
	// Should have processed the valid events and skipped the malformed one
	if !state.Agents["a1"].Spawned {
		t.Error("valid events before malformed line should be processed")
	}
	if !state.Completed {
		t.Error("valid events after malformed line should be processed (lenient parsing)")
	}
}

func TestReplay_FailedRun(t *testing.T) {
	el := setupTestEventLog(t, "test-failed")
	el.Append(Event{Type: EventRunStarted})
	el.Append(Event{Type: EventAgentSpawned, AgentID: "a1", AgentName: "Coder"})
	exitData, _ := json.Marshal(1)
	el.Append(Event{Type: EventAgentExited, AgentID: "a1", Data: exitData})
	el.Append(Event{Type: EventRunFailed})
	el.Close()

	state := replayTestLog(t, el.file.Name())
	if !state.Failed {
		t.Error("run should be marked as failed")
	}
	if state.Completed {
		t.Error("failed run should NOT be marked as completed")
	}
	if state.Agents["a1"].ExitCode != 1 {
		t.Errorf("expected exit code 1, got %d", state.Agents["a1"].ExitCode)
	}
}

func TestEventLog_ConcurrentAppends_AllParseable(t *testing.T) {
	el := setupTestEventLog(t, "test-concurrent-verify")
	var wg sync.WaitGroup
	const n = 50
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			el.Append(Event{
				Type:      EventAgentSpawned,
				AgentID:   fmt.Sprintf("a%d", idx),
				AgentName: fmt.Sprintf("Agent-%d", idx),
			})
		}(i)
	}
	wg.Wait()
	el.Close()

	// Read back and count valid lines
	f, err := os.Open(el.file.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	validCount := 0
	for scanner.Scan() {
		var event Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err == nil {
			validCount++
		}
	}

	if validCount != n {
		t.Errorf("expected %d valid JSONL lines from concurrent writes, got %d", n, validCount)
	}
}

func TestEventLog_Append_SetsRunIDAndTime(t *testing.T) {
	el := setupTestEventLog(t, "test-runid-time")
	el.Append(Event{Type: EventRunStarted})
	el.Close()

	f, err := os.Open(el.file.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Scan()
	var event Event
	if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if event.RunID != "test-runid-time" {
		t.Errorf("expected RunID='test-runid-time', got %q", event.RunID)
	}
	if event.Time.IsZero() {
		t.Error("expected non-zero Time")
	}
}

func TestReplay_AgentRegistered_SessionID(t *testing.T) {
	el := setupTestEventLog(t, "test-registered-session")
	el.Append(Event{Type: EventRunStarted})
	el.Append(Event{Type: EventAgentSpawned, AgentID: "a1", AgentName: "Coder"})

	regData, _ := json.Marshal(map[string]string{"session_id": "abc-123-def"})
	el.Append(Event{Type: EventAgentRegistered, AgentID: "a1", AgentName: "Coder", Data: regData})
	el.Append(Event{Type: EventRunCompleted})
	el.Close()

	state := replayTestLog(t, el.file.Name())
	a1 := state.Agents["a1"]
	if !a1.Registered {
		t.Error("agent a1 should be registered")
	}
	if a1.SessionID != "abc-123-def" {
		t.Errorf("expected session_id 'abc-123-def', got %q", a1.SessionID)
	}
}

func TestReplay_AgentRegistered_NoData(t *testing.T) {
	el := setupTestEventLog(t, "test-registered-nodata")
	el.Append(Event{Type: EventAgentSpawned, AgentID: "a1", AgentName: "Coder"})
	el.Append(Event{Type: EventAgentRegistered, AgentID: "a1"})
	el.Close()

	state := replayTestLog(t, el.file.Name())
	a1 := state.Agents["a1"]
	if !a1.Registered {
		t.Error("agent a1 should be registered")
	}
	if a1.SessionID != "" {
		t.Errorf("expected empty session_id, got %q", a1.SessionID)
	}
}

func TestReplay_FullResumeTrail(t *testing.T) {
	el := setupTestEventLog(t, "test-resume-trail")

	// Full event trail as emitted by run.go for resume support.
	el.Append(Event{Type: EventRunStarted})
	el.Append(Event{Type: EventAgentSpawned, AgentID: "a1", AgentName: "Builder"})
	el.Append(Event{Type: EventAgentSpawned, AgentID: "a2", AgentName: "QA"})

	// Session IDs captured from stream.
	regData1, _ := json.Marshal(map[string]string{"session_id": "sess-builder-001"})
	el.Append(Event{Type: EventAgentRegistered, AgentID: "a1", AgentName: "Builder", Data: regData1})
	regData2, _ := json.Marshal(map[string]string{"session_id": "sess-qa-002"})
	el.Append(Event{Type: EventAgentRegistered, AgentID: "a2", AgentName: "QA", Data: regData2})

	// Phase transition.
	phaseData, _ := json.Marshal("execute")
	el.Append(Event{Type: EventPhaseTransition, Data: phaseData})

	// Iteration.
	iterData, _ := json.Marshal(1)
	el.Append(Event{Type: EventIteration, Data: iterData})

	// Prompts sent.
	promptData, _ := json.Marshal("implement the feature")
	el.Append(Event{Type: EventPromptSent, AgentID: "a1", Data: promptData})

	// Builder exits successfully, QA exits with error (simulates interrupted run).
	exitData0, _ := json.Marshal(0)
	el.Append(Event{Type: EventAgentExited, AgentID: "a1", Data: exitData0})
	exitData1, _ := json.Marshal(1)
	el.Append(Event{Type: EventAgentExited, AgentID: "a2", Data: exitData1})
	el.Append(Event{Type: EventRunFailed})
	el.Close()

	state := replayTestLog(t, el.file.Name())

	// Verify overall state.
	if !state.Failed {
		t.Error("run should be marked as failed")
	}
	if state.Completed {
		t.Error("failed run should not be completed")
	}
	if state.CurrentPhase != "execute" {
		t.Errorf("expected phase 'execute', got %q", state.CurrentPhase)
	}
	if state.CurrentIteration != 1 {
		t.Errorf("expected iteration 1, got %d", state.CurrentIteration)
	}

	// Verify Builder agent.
	a1 := state.Agents["a1"]
	if !a1.Registered {
		t.Error("a1 should be registered")
	}
	if a1.SessionID != "sess-builder-001" {
		t.Errorf("a1 session_id = %q, want %q", a1.SessionID, "sess-builder-001")
	}
	if a1.PromptsSent != 1 {
		t.Errorf("a1 prompts sent = %d, want 1", a1.PromptsSent)
	}
	if a1.LastPrompt != "implement the feature" {
		t.Errorf("a1 last prompt = %q, want %q", a1.LastPrompt, "implement the feature")
	}
	if !a1.Exited || a1.ExitCode != 0 {
		t.Errorf("a1 should have exited with code 0, got exited=%v code=%d", a1.Exited, a1.ExitCode)
	}

	// Verify QA agent.
	a2 := state.Agents["a2"]
	if !a2.Registered {
		t.Error("a2 should be registered")
	}
	if a2.SessionID != "sess-qa-002" {
		t.Errorf("a2 session_id = %q, want %q", a2.SessionID, "sess-qa-002")
	}
	if !a2.Exited || a2.ExitCode != 1 {
		t.Errorf("a2 should have exited with code 1, got exited=%v code=%d", a2.Exited, a2.ExitCode)
	}
}

func TestReplay_PhaseTransition_ClearsOnEmpty(t *testing.T) {
	el := setupTestEventLog(t, "test-phase-clear")
	phaseData, _ := json.Marshal("execute")
	el.Append(Event{Type: EventPhaseTransition, Data: phaseData})
	emptyPhase, _ := json.Marshal("")
	el.Append(Event{Type: EventPhaseTransition, Data: emptyPhase})
	el.Close()

	state := replayTestLog(t, el.file.Name())
	if state.CurrentPhase != "" {
		t.Errorf("expected empty phase after clear, got %q", state.CurrentPhase)
	}
}

func TestReplay_MultipleIterations(t *testing.T) {
	el := setupTestEventLog(t, "test-multi-iter")
	for i := 1; i <= 3; i++ {
		iterData, _ := json.Marshal(i)
		el.Append(Event{Type: EventIteration, Data: iterData})
	}
	el.Close()

	state := replayTestLog(t, el.file.Name())
	if state.CurrentIteration != 3 {
		t.Errorf("expected iteration 3, got %d", state.CurrentIteration)
	}
}

func TestReplay_PromptSent_AccumulatesCount(t *testing.T) {
	el := setupTestEventLog(t, "test-prompt-count")
	el.Append(Event{Type: EventAgentSpawned, AgentID: "a1", AgentName: "Worker"})

	p1, _ := json.Marshal("first prompt")
	el.Append(Event{Type: EventPromptSent, AgentID: "a1", Data: p1})
	p2, _ := json.Marshal("second prompt")
	el.Append(Event{Type: EventPromptSent, AgentID: "a1", Data: p2})
	p3, _ := json.Marshal("third prompt")
	el.Append(Event{Type: EventPromptSent, AgentID: "a1", Data: p3})
	el.Close()

	state := replayTestLog(t, el.file.Name())
	a1 := state.Agents["a1"]
	if a1.PromptsSent != 3 {
		t.Errorf("expected 3 prompts sent, got %d", a1.PromptsSent)
	}
	if a1.LastPrompt != "third prompt" {
		t.Errorf("expected last prompt 'third prompt', got %q", a1.LastPrompt)
	}
}

func TestEventLog_CriticalEventsSync(t *testing.T) {
	el := setupTestEventLog(t, "test-critical")
	// Critical events should not error (fsync is called)
	for _, eventType := range []EventType{EventRunStarted, EventRunCompleted, EventRunFailed, EventAgentExited} {
		err := el.Append(Event{Type: eventType})
		if err != nil {
			t.Errorf("critical event %s should not error: %v", eventType, err)
		}
	}
	el.Close()
}
