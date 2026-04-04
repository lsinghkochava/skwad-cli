package models

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestAgent_ActivityMode(t *testing.T) {
	cases := []struct {
		name      string
		agentType AgentType
		sessionID string
		want      ActivityTracking
	}{
		{"shell", AgentTypeShell, "", ActivityTrackingNone},
		{"claude no session", AgentTypeClaude, "", ActivityTrackingAll},
		{"claude with session", AgentTypeClaude, "sess-123", ActivityTrackingUserInput},
		{"codex no session", AgentTypeCodex, "", ActivityTrackingAll},
		{"codex with session", AgentTypeCodex, "sess-456", ActivityTrackingUserInput},
		{"gemini", AgentTypeGemini, "", ActivityTrackingAll},
		{"opencode", AgentTypeOpenCode, "", ActivityTrackingAll},
	}
	for _, tc := range cases {
		a := &Agent{AgentType: tc.agentType, SessionID: tc.sessionID}
		if got := a.ActivityMode(); got != tc.want {
			t.Errorf("%s: got %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestAgent_SupportsHooks(t *testing.T) {
	yes := []AgentType{AgentTypeClaude, AgentTypeCodex}
	no := []AgentType{AgentTypeOpenCode, AgentTypeGemini, AgentTypeCopilot, AgentTypeShell}

	for _, at := range yes {
		a := &Agent{AgentType: at}
		if !a.SupportsHooks() {
			t.Errorf("%s should support hooks", at)
		}
	}
	for _, at := range no {
		a := &Agent{AgentType: at}
		if a.SupportsHooks() {
			t.Errorf("%s should not support hooks", at)
		}
	}
}

func TestAgent_SupportsResume(t *testing.T) {
	yes := []AgentType{AgentTypeClaude, AgentTypeCodex, AgentTypeGemini, AgentTypeCopilot}
	no := []AgentType{AgentTypeOpenCode, AgentTypeShell, AgentTypeCustom1}

	for _, at := range yes {
		a := &Agent{AgentType: at}
		if !a.SupportsResume() {
			t.Errorf("%s should support resume", at)
		}
	}
	for _, at := range no {
		a := &Agent{AgentType: at}
		if a.SupportsResume() {
			t.Errorf("%s should not support resume", at)
		}
	}
}

func TestAgent_SupportsSystemPrompt(t *testing.T) {
	a := &Agent{AgentType: AgentTypeClaude}
	if !a.SupportsSystemPrompt() {
		t.Error("claude should support system prompt")
	}
	a = &Agent{AgentType: AgentTypeGemini}
	if a.SupportsSystemPrompt() {
		t.Error("gemini should not support system prompt")
	}
}

func TestAgent_IsNewSession(t *testing.T) {
	cases := []struct {
		name            string
		resumeSessionID string
		isFork          bool
		want            bool
	}{
		{"new session", "", false, true},
		{"resumed session", "sess-123", false, false},
		{"forked session", "sess-456", true, false},
		{"fork without session ID", "", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := &Agent{
				ResumeSessionID: tc.resumeSessionID,
				IsFork:          tc.isFork,
			}
			if got := a.IsNewSession(); got != tc.want {
				t.Errorf("IsNewSession() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAgent_DefaultMetadata(t *testing.T) {
	// Metadata map should be initialized before use (not nil).
	a := &Agent{
		ID:       uuid.New(),
		Metadata: make(map[string]string),
	}
	a.Metadata["cwd"] = "/tmp"
	if a.Metadata["cwd"] != "/tmp" {
		t.Error("metadata map not writable")
	}
}

func TestAgent_JSON_ExploreMode_RoundTrip(t *testing.T) {
	a := Agent{
		ID:          uuid.MustParse("a1000001-0000-0000-0000-000000000001"),
		Name:        "Explorer",
		AgentType:   AgentTypeClaude,
		ExploreMode: true,
	}

	data, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded Agent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !decoded.ExploreMode {
		t.Error("ExploreMode should survive JSON round-trip")
	}
}

func TestAgent_JSON_WorktreeIsolation_RoundTrip(t *testing.T) {
	a := Agent{
		ID:                uuid.MustParse("a1000001-0000-0000-0000-000000000002"),
		Name:              "Isolated",
		AgentType:         AgentTypeCodex,
		WorktreeIsolation: true,
	}

	data, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded Agent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !decoded.WorktreeIsolation {
		t.Error("WorktreeIsolation should survive JSON round-trip")
	}
}

func TestAgent_JSON_RuntimeFields_Excluded(t *testing.T) {
	a := Agent{
		ID:             uuid.MustParse("a1000001-0000-0000-0000-000000000003"),
		Name:           "Worker",
		AgentType:      AgentTypeClaude,
		WorktreePath:   "/tmp/worktree-path",
		WorktreeBranch: "feature/test",
		Status:         AgentStatusRunning,
		StatusText:     "coding",
		SessionID:      "sess-999",
	}

	data, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	jsonStr := string(data)
	for _, excluded := range []string{"worktree-path", "feature/test", "running", "coding", "sess-999"} {
		if strings.Contains(jsonStr, excluded) {
			t.Errorf("runtime field value %q should not appear in JSON output", excluded)
		}
	}

	// Unmarshal back — runtime fields should be zero-valued
	var decoded Agent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.WorktreePath != "" {
		t.Error("WorktreePath should be empty after round-trip")
	}
	if decoded.WorktreeBranch != "" {
		t.Error("WorktreeBranch should be empty after round-trip")
	}
	if decoded.Status != "" {
		t.Error("Status should be empty after round-trip")
	}
}

func TestAgent_JSON_ExploreMode_DefaultFalse(t *testing.T) {
	a := Agent{
		ID:        uuid.MustParse("a1000001-0000-0000-0000-000000000004"),
		Name:      "Default",
		AgentType: AgentTypeClaude,
	}

	data, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded Agent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.ExploreMode {
		t.Error("ExploreMode should default to false")
	}
	if decoded.WorktreeIsolation {
		t.Error("WorktreeIsolation should default to false")
	}
}
