package models

import (
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
