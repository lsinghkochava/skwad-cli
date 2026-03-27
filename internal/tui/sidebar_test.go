package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/google/uuid"
	"github.com/lsinghkochava/skwad-cli/internal/models"
)

// keyPress creates a tea.KeyPressMsg for a simple key.
func keyPress(key rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: key, Text: string(key)}
}

// specialKey creates a tea.KeyPressMsg for a special key (tab, esc, up, down).
func specialKey(code rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: code}
}

func testAgents(n int) ([]*models.Agent, []uuid.UUID) {
	agents := make([]*models.Agent, n)
	ids := make([]uuid.UUID, n)
	names := []string{"Alice", "Bob", "Charlie", "Dave", "Eve"}
	for i := 0; i < n; i++ {
		id := uuid.New()
		ids[i] = id
		name := "Agent"
		if i < len(names) {
			name = names[i]
		}
		agents[i] = &models.Agent{
			ID:     id,
			Name:   name,
			Status: models.AgentStatusIdle,
		}
	}
	return agents, ids
}

func TestSidebar_InitialState(t *testing.T) {
	agents, ids := testAgents(3)
	sb := NewSidebar(agents)

	if got := sb.SelectedAgent(); got != ids[0] {
		t.Errorf("initial selection should be first agent, got %v", got)
	}
	if got := sb.SelectedName(); got != "Alice" {
		t.Errorf("initial name should be Alice, got %q", got)
	}
}

func TestSidebar_NavigateDown_J(t *testing.T) {
	agents, ids := testAgents(3)
	sb := NewSidebar(agents)

	sb.Update(keyPress('j'))
	if got := sb.SelectedAgent(); got != ids[1] {
		t.Errorf("after j, expected Bob, got agent %v", got)
	}
	if !sb.SelectionChanged() {
		t.Error("selection should have changed")
	}
}

func TestSidebar_NavigateDown_Arrow(t *testing.T) {
	agents, ids := testAgents(3)
	sb := NewSidebar(agents)

	sb.Update(specialKey(tea.KeyDown))
	if got := sb.SelectedAgent(); got != ids[1] {
		t.Errorf("after down arrow, expected Bob, got agent %v", got)
	}
}

func TestSidebar_NavigateUp_K(t *testing.T) {
	agents, ids := testAgents(3)
	sb := NewSidebar(agents)

	// Move down first, then up.
	sb.Update(keyPress('j'))
	sb.Update(keyPress('k'))
	if got := sb.SelectedAgent(); got != ids[0] {
		t.Errorf("after j then k, expected Alice, got agent %v", got)
	}
}

func TestSidebar_NavigateUp_Arrow(t *testing.T) {
	agents, ids := testAgents(3)
	sb := NewSidebar(agents)

	sb.Update(keyPress('j'))
	sb.Update(specialKey(tea.KeyUp))
	if got := sb.SelectedAgent(); got != ids[0] {
		t.Errorf("after j then up, expected Alice, got agent %v", got)
	}
}

func TestSidebar_BoundsTop(t *testing.T) {
	agents, ids := testAgents(3)
	sb := NewSidebar(agents)

	// At top, pressing k should stay at 0.
	sb.Update(keyPress('k'))
	if got := sb.SelectedAgent(); got != ids[0] {
		t.Errorf("should stay at first agent, got %v", got)
	}
	if sb.SelectionChanged() {
		t.Error("selection should NOT have changed at top bound")
	}
}

func TestSidebar_BoundsBottom(t *testing.T) {
	agents, ids := testAgents(3)
	sb := NewSidebar(agents)

	// Move to bottom.
	sb.Update(keyPress('j'))
	sb.Update(keyPress('j'))
	if got := sb.SelectedAgent(); got != ids[2] {
		t.Fatalf("should be at last agent, got %v", got)
	}

	// Another j should stay at bottom.
	sb.Update(keyPress('j'))
	if got := sb.SelectedAgent(); got != ids[2] {
		t.Errorf("should stay at last agent, got %v", got)
	}
	if sb.SelectionChanged() {
		t.Error("selection should NOT have changed at bottom bound")
	}
}

func TestSidebar_TabWraps(t *testing.T) {
	agents, ids := testAgents(3)
	sb := NewSidebar(agents)

	// Move to last, then tab should wrap to first.
	sb.Update(keyPress('j'))
	sb.Update(keyPress('j'))
	if got := sb.SelectedAgent(); got != ids[2] {
		t.Fatalf("should be at last agent")
	}

	sb.Update(specialKey(tea.KeyTab))
	if got := sb.SelectedAgent(); got != ids[0] {
		t.Errorf("tab from last should wrap to first, got agent %v", got)
	}
	if !sb.SelectionChanged() {
		t.Error("tab wrap should mark selection changed")
	}
}

func TestSidebar_TabCycles(t *testing.T) {
	agents, ids := testAgents(3)
	sb := NewSidebar(agents)

	// Tab 3 times should cycle through all and back to first.
	sb.Update(specialKey(tea.KeyTab))
	if got := sb.SelectedAgent(); got != ids[1] {
		t.Errorf("first tab: expected Bob, got %v", got)
	}
	sb.Update(specialKey(tea.KeyTab))
	if got := sb.SelectedAgent(); got != ids[2] {
		t.Errorf("second tab: expected Charlie, got %v", got)
	}
	sb.Update(specialKey(tea.KeyTab))
	if got := sb.SelectedAgent(); got != ids[0] {
		t.Errorf("third tab: expected Alice (wrap), got %v", got)
	}
}

func TestSidebar_StatusMsg_UpdatesCorrectAgent(t *testing.T) {
	agents, _ := testAgents(3)
	sb := NewSidebar(agents)

	targetID := agents[1].ID
	sb.Update(StatusMsg{
		AgentID: targetID,
		Status:  models.AgentStatusRunning,
		Text:    "Writing tests",
	})

	// Verify the status was updated.
	if sb.agents[1].Status != models.AgentStatusRunning {
		t.Errorf("expected running, got %v", sb.agents[1].Status)
	}
	if sb.agents[1].StatusText != "Writing tests" {
		t.Errorf("expected 'Writing tests', got %q", sb.agents[1].StatusText)
	}

	// Other agents should be unchanged.
	if sb.agents[0].Status != models.AgentStatusIdle {
		t.Errorf("agent 0 status should be unchanged, got %v", sb.agents[0].Status)
	}
}

func TestSidebar_StatusMsg_UnknownAgent(t *testing.T) {
	agents, _ := testAgents(2)
	sb := NewSidebar(agents)

	// Status for unknown agent should be silently ignored.
	sb.Update(StatusMsg{
		AgentID: uuid.New(),
		Status:  models.AgentStatusError,
		Text:    "something broke",
	})

	// Existing agents should be unchanged.
	if sb.agents[0].Status != models.AgentStatusIdle {
		t.Error("agent 0 should be unchanged after unknown status msg")
	}
	if sb.agents[1].Status != models.AgentStatusIdle {
		t.Error("agent 1 should be unchanged after unknown status msg")
	}
}

func TestSidebar_SelectedAgent_EmptyList(t *testing.T) {
	sb := NewSidebar(nil)
	if got := sb.SelectedAgent(); got != (uuid.UUID{}) {
		t.Errorf("expected zero UUID for empty sidebar, got %v", got)
	}
	if got := sb.SelectedName(); got != "" {
		t.Errorf("expected empty name for empty sidebar, got %q", got)
	}
}
