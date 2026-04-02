package tui

import (
	"image/color"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/google/uuid"

	"github.com/lsinghkochava/skwad-cli/internal/models"
)

func TestRenderStatusTable_EmptyAgents(t *testing.T) {
	out := RenderStatusTable(nil, 80, 10, nil)

	if !strings.Contains(out, "AGENT") {
		t.Error("should contain 'AGENT' header")
	}
	if !strings.Contains(out, "STATUS") {
		t.Error("should contain 'STATUS' header")
	}
	if !strings.Contains(out, "ACTIVITY") {
		t.Error("should contain 'ACTIVITY' header")
	}
	// Should have separator line with box-drawing chars.
	if !strings.Contains(out, "─") {
		t.Error("should contain separator line")
	}
}

func TestRenderStatusTable_MultipleAgents(t *testing.T) {
	agents := []agentState{
		{id: uuid.New(), name: "Coder", status: string(models.AgentStatusRunning), statusText: "writing code"},
		{id: uuid.New(), name: "Tester", status: string(models.AgentStatusIdle), statusText: "waiting"},
	}
	colorMap := map[string]color.Color{
		"Coder":  lipgloss.Color("#00CCCC"),
		"Tester": lipgloss.Color("#CCCC00"),
	}

	out := RenderStatusTable(agents, 100, 10, colorMap)

	if !strings.Contains(out, "running") {
		t.Error("should contain 'running' status")
	}
	if !strings.Contains(out, "idle") {
		t.Error("should contain 'idle' status")
	}
	if !strings.Contains(out, "writing code") {
		t.Error("should contain activity text 'writing code'")
	}
	if !strings.Contains(out, "waiting") {
		t.Error("should contain activity text 'waiting'")
	}
}

func TestRenderStatusTable_LongActivityTruncated(t *testing.T) {
	longActivity := strings.Repeat("x", 200)
	agents := []agentState{
		{id: uuid.New(), name: "Agent", status: "running", statusText: longActivity},
	}

	out := RenderStatusTable(agents, 80, 10, nil)

	// The full 200-char activity should NOT appear — it gets truncated.
	if strings.Contains(out, longActivity) {
		t.Error("long activity text should be truncated")
	}
	// Should contain the truncation marker.
	if !strings.Contains(out, "…") {
		t.Error("truncated activity should end with '…'")
	}
}

func TestRenderStatusTable_RowsCappedByHeight(t *testing.T) {
	agents := make([]agentState, 20)
	for i := range agents {
		agents[i] = agentState{
			id:     uuid.New(),
			name:   "Agent",
			status: "idle",
		}
	}

	// Height of 5 means rowsAvailable = 5-3 = 2.
	out := RenderStatusTable(agents, 80, 5, nil)

	// Count rows by counting "idle" occurrences (each agent row has "idle").
	count := strings.Count(out, "idle")
	if count > 2 {
		t.Errorf("should render at most 2 rows with height=5, got %d", count)
	}
}

// --- statusDot tests ---

func TestStatusDot_Running(t *testing.T) {
	dot := statusDot(string(models.AgentStatusRunning))
	if !strings.Contains(dot, "●") {
		t.Error("running status should contain filled dot")
	}
}

func TestStatusDot_Idle(t *testing.T) {
	dot := statusDot(string(models.AgentStatusIdle))
	if !strings.Contains(dot, "●") {
		t.Error("idle status should contain filled dot")
	}
}

func TestStatusDot_Input(t *testing.T) {
	dot := statusDot(string(models.AgentStatusInput))
	if !strings.Contains(dot, "●") {
		t.Error("input status should contain filled dot")
	}
}

func TestStatusDot_Error(t *testing.T) {
	dot := statusDot(string(models.AgentStatusError))
	if !strings.Contains(dot, "●") {
		t.Error("error status should contain filled dot")
	}
}

func TestStatusDot_Unknown(t *testing.T) {
	dot := statusDot("unknown")
	if dot != "○" {
		t.Errorf("unknown status dot = %q, want %q", dot, "○")
	}
}

func TestStatusDot_AllVariantsDistinct(t *testing.T) {
	running := statusDot(string(models.AgentStatusRunning))
	idle := statusDot(string(models.AgentStatusIdle))
	input := statusDot(string(models.AgentStatusInput))
	errDot := statusDot(string(models.AgentStatusError))
	unknown := statusDot("unknown")

	// Each should be different (ANSI color codes differ).
	dots := []string{running, idle, input, errDot, unknown}
	seen := make(map[string]bool)
	for _, d := range dots {
		if seen[d] {
			t.Errorf("duplicate dot rendering found: %q", d)
		}
		seen[d] = true
	}
}

func TestRenderStatusTable_NoColorMap(t *testing.T) {
	agents := []agentState{
		{id: uuid.New(), name: "Agent", status: "running", statusText: "work"},
	}

	// nil colorMap — should not panic.
	out := RenderStatusTable(agents, 80, 10, nil)
	if out == "" {
		t.Error("should render even without colorMap")
	}
}
