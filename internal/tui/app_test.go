package tui

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	tea "charm.land/bubbletea/v2"

	"github.com/lsinghkochava/skwad-cli/internal/models"
	"github.com/lsinghkochava/skwad-cli/internal/persistence"
	"github.com/lsinghkochava/skwad-cli/internal/agent"
)

// newTestManager creates a Manager backed by a temp store for testing.
func newTestManager(t *testing.T) *agent.Manager {
	t.Helper()
	dir := t.TempDir()
	store, err := persistence.NewStoreAt(dir)
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	mgr, err := agent.NewManager(store)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return mgr
}

// addTestAgent adds an agent to the manager and returns it.
func addTestAgent(t *testing.T, mgr *agent.Manager, name string, status models.AgentStatus, statusText string) *models.Agent {
	t.Helper()
	a := &models.Agent{
		ID:        uuid.New(),
		Name:      name,
		AgentType: models.AgentTypeClaude,
		Folder:    "/tmp",
		Status:    status,
		StatusText: statusText,
	}
	mgr.AddAgent(a, nil)
	return a
}

// simulateWindowSize sends a WindowSizeMsg to set up the model dimensions.
func simulateWindowSize(m Model, width, height int) Model {
	updated, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	return updated.(Model)
}

// findAgent returns the first agentState with the given name, or nil.
func findAgent(agents []agentState, name string) *agentState {
	for i := range agents {
		if agents[i].name == name {
			return &agents[i]
		}
	}
	return nil
}

// --- Constructor tests ---

func TestNew_InitialState(t *testing.T) {
	mgr := newTestManager(t)
	m := New(mgr, "http://localhost:8766")

	if m.mcpURL != "http://localhost:8766" {
		t.Errorf("mcpURL = %q, want %q", m.mcpURL, "http://localhost:8766")
	}
	if m.activityLog.ScrollPos() != 0 {
		t.Errorf("scrollPos = %d, want 0", m.activityLog.ScrollPos())
	}
	if m.ready {
		t.Error("ready = true, want false")
	}
	if len(m.agents) != 0 {
		t.Errorf("agents = %d, want 0", len(m.agents))
	}
	if len(m.activityLog.Lines()) != 0 {
		t.Errorf("logLines = %d, want 0", len(m.activityLog.Lines()))
	}
	if m.colorMap == nil {
		t.Error("colorMap is nil, want initialized map")
	}
}

func TestInit_ReturnsNil(t *testing.T) {
	mgr := newTestManager(t)
	m := New(mgr, "http://localhost:8766")
	cmd := m.Init()
	if cmd != nil {
		t.Error("Init() should return nil cmd")
	}
}

// --- View before WindowSizeMsg ---

func TestView_BeforeReady(t *testing.T) {
	mgr := newTestManager(t)
	m := New(mgr, "http://localhost:8766")

	v := m.View()
	if !strings.Contains(v.Content, "Initializing") {
		t.Errorf("View before ready should contain 'Initializing', got %q", v.Content)
	}
	if !v.AltScreen {
		t.Error("View should set AltScreen = true")
	}
}

// --- WindowSizeMsg ---

func TestUpdate_WindowSizeMsg(t *testing.T) {
	mgr := newTestManager(t)
	m := New(mgr, "http://localhost:8766")

	m = simulateWindowSize(m, 120, 40)

	if !m.ready {
		t.Error("ready should be true after WindowSizeMsg")
	}
	if m.width != 120 {
		t.Errorf("width = %d, want 120", m.width)
	}
	if m.height != 40 {
		t.Errorf("height = %d, want 40", m.height)
	}
}

// --- AgentChangedMsg ---

func TestUpdate_AgentChangedMsg_RefreshesAgents(t *testing.T) {
	mgr := newTestManager(t)
	a := addTestAgent(t, mgr, "Explorer", models.AgentStatusRunning, "searching codebase")

	m := New(mgr, "http://localhost:8766")
	m = simulateWindowSize(m, 120, 40)

	// Verify agents are populated after WindowSizeMsg (which calls refreshAgents).
	// Manager.Agents() returns entries based on workspace AgentIDs — find our agent.
	if len(m.agents) == 0 {
		t.Fatal("agents should not be empty")
	}
	found := findAgent(m.agents, "Explorer")
	if found == nil {
		t.Fatal("agent 'Explorer' not found")
	}
	if found.status != string(models.AgentStatusRunning) {
		t.Errorf("agent status = %q, want %q", found.status, models.AgentStatusRunning)
	}
	if found.statusText != "searching codebase" {
		t.Errorf("agent statusText = %q, want %q", found.statusText, "searching codebase")
	}

	// Update status via manager then send AgentChangedMsg.
	mgr.UpdateAgent(a.ID, func(ag *models.Agent) {
		ag.Status = models.AgentStatusIdle
		ag.StatusText = "done"
	})

	updated, _ := m.Update(AgentChangedMsg(a.ID))
	m = updated.(Model)

	found = findAgent(m.agents, "Explorer")
	if found == nil {
		t.Fatal("agent 'Explorer' not found after update")
	}
	if found.status != string(models.AgentStatusIdle) {
		t.Errorf("agent status after update = %q, want %q", found.status, models.AgentStatusIdle)
	}
}

func TestUpdate_MultipleAgentChangedMsg(t *testing.T) {
	mgr := newTestManager(t)
	addTestAgent(t, mgr, "Coder", models.AgentStatusRunning, "coding")
	addTestAgent(t, mgr, "Tester", models.AgentStatusIdle, "waiting")

	m := New(mgr, "http://localhost:8766")
	m = simulateWindowSize(m, 120, 40)

	// Both agents must be present.
	if findAgent(m.agents, "Coder") == nil {
		t.Error("agent 'Coder' not found")
	}
	if findAgent(m.agents, "Tester") == nil {
		t.Error("agent 'Tester' not found")
	}
	countBefore := len(m.agents)

	// Add a third agent and send change msg.
	a3 := addTestAgent(t, mgr, "Reviewer", models.AgentStatusRunning, "reviewing")
	updated, _ := m.Update(AgentChangedMsg(a3.ID))
	m = updated.(Model)

	if findAgent(m.agents, "Reviewer") == nil {
		t.Error("agent 'Reviewer' not found after AgentChangedMsg")
	}
	if len(m.agents) <= countBefore {
		t.Error("agent count should increase after adding third agent")
	}
}

// --- LogEntryMsg ---

func TestUpdate_LogEntryMsg_AppendsLog(t *testing.T) {
	mgr := newTestManager(t)
	m := New(mgr, "http://localhost:8766")
	m = simulateWindowSize(m, 120, 40)

	msg := LogEntryMsg{
		AgentID:   uuid.New(),
		AgentName: "Coder",
		Data:      []byte("implementing feature X"),
	}

	updated, _ := m.Update(msg)
	m = updated.(Model)

	lines := m.activityLog.Lines()
	if len(lines) != 1 {
		t.Fatalf("logLines = %d, want 1", len(lines))
	}
	if lines[0].agentName != "Coder" {
		t.Errorf("log agentName = %q, want %q", lines[0].agentName, "Coder")
	}
	if lines[0].text != "implementing feature X" {
		t.Errorf("log text = %q, want %q", lines[0].text, "implementing feature X")
	}
	if lines[0].timestamp.IsZero() {
		t.Error("log timestamp should not be zero")
	}
}

func TestUpdate_LogEntryMsg_MultiLineData(t *testing.T) {
	mgr := newTestManager(t)
	m := New(mgr, "http://localhost:8766")
	m = simulateWindowSize(m, 120, 40)

	msg := LogEntryMsg{
		AgentID:   uuid.New(),
		AgentName: "Coder",
		Data:      []byte("line one\nline two\nline three"),
	}

	updated, _ := m.Update(msg)
	m = updated.(Model)

	lines := m.activityLog.Lines()
	if len(lines) != 3 {
		t.Fatalf("logLines = %d, want 3 (multi-line split)", len(lines))
	}
	if lines[0].text != "line one" {
		t.Errorf("logLines[0].text = %q, want %q", lines[0].text, "line one")
	}
	if lines[2].text != "line three" {
		t.Errorf("logLines[2].text = %q, want %q", lines[2].text, "line three")
	}
}

func TestUpdate_LogEntryMsg_EmptyData(t *testing.T) {
	mgr := newTestManager(t)
	m := New(mgr, "http://localhost:8766")
	m = simulateWindowSize(m, 120, 40)

	msg := LogEntryMsg{
		AgentID:   uuid.New(),
		AgentName: "Coder",
		Data:      []byte(""),
	}

	updated, _ := m.Update(msg)
	m = updated.(Model)

	// Empty data splits into [""], which gets skipped by the empty-line filter.
	if len(m.activityLog.Lines()) != 0 {
		t.Errorf("logLines = %d, want 0 for empty data", len(m.activityLog.Lines()))
	}
}

func TestUpdate_LogEntryMsg_EmptyLines(t *testing.T) {
	mgr := newTestManager(t)
	m := New(mgr, "http://localhost:8766")
	m = simulateWindowSize(m, 120, 40)

	msg := LogEntryMsg{
		AgentID:   uuid.New(),
		AgentName: "Coder",
		Data:      []byte("hello\n\n\nworld"),
	}

	updated, _ := m.Update(msg)
	m = updated.(Model)

	// Empty lines are skipped.
	if len(m.activityLog.Lines()) != 2 {
		t.Errorf("logLines = %d, want 2 (empty lines filtered)", len(m.activityLog.Lines()))
	}
}

// --- Color assignment ---

func TestColorAssignment_Consistent(t *testing.T) {
	mgr := newTestManager(t)
	m := New(mgr, "http://localhost:8766")
	m = simulateWindowSize(m, 120, 40)

	// Send two log entries from same agent.
	msg1 := LogEntryMsg{AgentID: uuid.New(), AgentName: "Coder", Data: []byte("first")}
	msg2 := LogEntryMsg{AgentID: uuid.New(), AgentName: "Coder", Data: []byte("second")}

	updated, _ := m.Update(msg1)
	m = updated.(Model)
	updated, _ = m.Update(msg2)
	m = updated.(Model)

	lines := m.activityLog.Lines()
	if lines[0].color != lines[1].color {
		t.Error("same agent should get same color across log entries")
	}
}

func TestColorAssignment_DifferentAgents(t *testing.T) {
	mgr := newTestManager(t)
	m := New(mgr, "http://localhost:8766")
	m = simulateWindowSize(m, 120, 40)

	msg1 := LogEntryMsg{AgentID: uuid.New(), AgentName: "Coder", Data: []byte("one")}
	msg2 := LogEntryMsg{AgentID: uuid.New(), AgentName: "Tester", Data: []byte("two")}

	updated, _ := m.Update(msg1)
	m = updated.(Model)
	updated, _ = m.Update(msg2)
	m = updated.(Model)

	lines := m.activityLog.Lines()
	if lines[0].color == lines[1].color {
		t.Error("different agents should get different colors")
	}
}

func TestColorAssignment_WrapsAround(t *testing.T) {
	mgr := newTestManager(t)
	m := New(mgr, "http://localhost:8766")
	m = simulateWindowSize(m, 120, 40)

	// Add more agents than available colors to verify wrap-around doesn't panic.
	for i := 0; i < len(agentColors)+3; i++ {
		msg := LogEntryMsg{
			AgentID:   uuid.New(),
			AgentName: "Agent" + string(rune('A'+i)),
			Data:      []byte("log"),
		}
		updated, _ := m.Update(msg)
		m = updated.(Model)
	}

	if len(m.activityLog.Lines()) != len(agentColors)+3 {
		t.Errorf("logLines = %d, want %d", len(m.activityLog.Lines()), len(agentColors)+3)
	}
}

// --- Key handling ---

func TestUpdate_QuitKey(t *testing.T) {
	mgr := newTestManager(t)
	m := New(mgr, "http://localhost:8766")
	m = simulateWindowSize(m, 120, 40)

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'q'})
	if cmd == nil {
		t.Error("pressing 'q' should return a quit command")
	}
}

func TestUpdate_CtrlC(t *testing.T) {
	mgr := newTestManager(t)
	m := New(mgr, "http://localhost:8766")
	m = simulateWindowSize(m, 120, 40)

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Error("pressing ctrl+c should return a quit command")
	}
}

// --- Scroll ---

func TestScroll_Down(t *testing.T) {
	mgr := newTestManager(t)
	m := New(mgr, "http://localhost:8766")
	m = simulateWindowSize(m, 80, 20)

	// Add enough log entries to enable scrolling.
	for i := 0; i < 30; i++ {
		msg := LogEntryMsg{AgentID: uuid.New(), AgentName: "A", Data: []byte("line")}
		updated, _ := m.Update(msg)
		m = updated.(Model)
	}

	// scrollPos should have auto-scrolled to bottom.
	posBefore := m.activityLog.ScrollPos()

	// Scroll up first, then verify scroll down works.
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'k'})
	m = updated.(Model)
	if m.activityLog.ScrollPos() >= posBefore {
		t.Error("scroll up should decrease scrollPos")
	}

	updated, _ = m.Update(tea.KeyPressMsg{Code: 'j'})
	m = updated.(Model)
	if m.activityLog.ScrollPos() != posBefore {
		t.Errorf("scroll down should increase scrollPos back, got %d want %d", m.activityLog.ScrollPos(), posBefore)
	}
}

func TestScroll_UpAtZero(t *testing.T) {
	mgr := newTestManager(t)
	m := New(mgr, "http://localhost:8766")
	m = simulateWindowSize(m, 80, 20)

	// No log lines — scrollPos should stay at 0.
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'k'})
	m = updated.(Model)

	if m.activityLog.ScrollPos() != 0 {
		t.Errorf("scrollPos = %d, should not go below 0", m.activityLog.ScrollPos())
	}
}

// --- View rendering ---

func TestView_Ready_RendersPanels(t *testing.T) {
	mgr := newTestManager(t)
	addTestAgent(t, mgr, "Explorer", models.AgentStatusRunning, "exploring")

	m := New(mgr, "http://localhost:9999")
	m = simulateWindowSize(m, 100, 30)

	v := m.View()
	content := v.Content

	// Should contain agent table header.
	if !strings.Contains(content, "AGENT") {
		t.Error("View should contain 'AGENT' header")
	}
	if !strings.Contains(content, "STATUS") {
		t.Error("View should contain 'STATUS' header")
	}
	if !strings.Contains(content, "ACTIVITY") {
		t.Error("View should contain 'ACTIVITY' header")
	}

	// Should contain agent name.
	if !strings.Contains(content, "Explorer") {
		t.Error("View should contain agent name 'Explorer'")
	}

	// Should contain status bar with MCP URL.
	if !strings.Contains(content, "localhost:9999") {
		t.Error("View should contain MCP URL in status bar")
	}

	// Should contain quit hint.
	if !strings.Contains(content, "q: quit") {
		t.Error("View should contain 'q: quit' in status bar")
	}

	if !v.AltScreen {
		t.Error("View should set AltScreen = true")
	}
}

func TestView_EmptyAgents(t *testing.T) {
	mgr := newTestManager(t)
	m := New(mgr, "http://localhost:8766")
	m = simulateWindowSize(m, 100, 30)

	v := m.View()
	content := v.Content

	// Header should still render.
	if !strings.Contains(content, "AGENT") {
		t.Error("View with no agents should still show table header")
	}

	// Status bar should show 0/0.
	if !strings.Contains(content, "0/0") {
		t.Error("View with no agents should show '0/0' in status bar")
	}
}

func TestView_EmptyLog(t *testing.T) {
	mgr := newTestManager(t)
	m := New(mgr, "http://localhost:8766")
	m = simulateWindowSize(m, 80, 20)

	v := m.View()
	// No log lines, so the log section should just be empty lines.
	// View should still render without panic.
	if v.Content == "" {
		t.Error("View should not be empty when ready")
	}
}

func TestView_ActiveAgentCount(t *testing.T) {
	mgr := newTestManager(t)
	addTestAgent(t, mgr, "A1", models.AgentStatusRunning, "active")
	addTestAgent(t, mgr, "A2", models.AgentStatusIdle, "idle")
	addTestAgent(t, mgr, "A3", models.AgentStatusRunning, "also active")

	m := New(mgr, "http://localhost:8766")
	m = simulateWindowSize(m, 100, 30)

	// Count running vs non-running agents as the model sees them.
	activeCount := 0
	totalCount := len(m.agents)
	for _, a := range m.agents {
		if a.status == string(models.AgentStatusRunning) {
			activeCount++
		}
	}
	// Must have at least some active and some total.
	if activeCount == 0 {
		t.Error("should have at least one active agent")
	}
	if totalCount == 0 {
		t.Error("should have agents")
	}
	if activeCount >= totalCount {
		t.Errorf("not all agents should be active: active=%d, total=%d", activeCount, totalCount)
	}

	// Verify the view renders without panic and contains agent info.
	v := m.View()
	if v.Content == "" {
		t.Error("View should not be empty")
	}
	if !strings.Contains(v.Content, "active") {
		t.Error("View status bar should contain 'active'")
	}
}

// --- Filter cycling ---

func TestCycleFilter_DirectModel(t *testing.T) {
	// Test cycleFilter directly with manually set agent state (no duplicates).
	mgr := newTestManager(t)
	m := New(mgr, "http://localhost:8766")
	m.ready = true
	m.width = 100
	m.height = 30
	m.agents = []agentState{
		{id: uuid.New(), name: "Coder", status: "running"},
		{id: uuid.New(), name: "Tester", status: "idle"},
		{id: uuid.New(), name: "Reviewer", status: "running"},
	}

	// Start with empty filter.
	if m.filterAgent != "" {
		t.Fatalf("initial filterAgent = %q, want empty", m.filterAgent)
	}

	// First cycle → first agent.
	m.cycleFilter()
	if m.filterAgent != "Coder" {
		t.Errorf("after first cycle: filterAgent = %q, want %q", m.filterAgent, "Coder")
	}

	// Second cycle → second agent.
	m.cycleFilter()
	if m.filterAgent != "Tester" {
		t.Errorf("after second cycle: filterAgent = %q, want %q", m.filterAgent, "Tester")
	}

	// Third cycle → third agent.
	m.cycleFilter()
	if m.filterAgent != "Reviewer" {
		t.Errorf("after third cycle: filterAgent = %q, want %q", m.filterAgent, "Reviewer")
	}

	// Fourth cycle → wraps back to empty.
	m.cycleFilter()
	if m.filterAgent != "" {
		t.Errorf("after full cycle: filterAgent = %q, want empty", m.filterAgent)
	}
}

func TestCycleFilter_TabSetsFilter(t *testing.T) {
	mgr := newTestManager(t)
	addTestAgent(t, mgr, "Coder", models.AgentStatusRunning, "coding")

	m := New(mgr, "http://localhost:8766")
	m = simulateWindowSize(m, 100, 30)

	// Tab should set filter to a non-empty value.
	updated, _ := m.Update(tea.KeyPressMsg{Code: '\t'})
	m = updated.(Model)

	if m.filterAgent == "" {
		t.Error("filterAgent should be set after tab")
	}
}

func TestCycleFilter_RemovedAgentResets(t *testing.T) {
	// If filterAgent is set to an agent that no longer exists, cycleFilter resets.
	mgr := newTestManager(t)
	m := New(mgr, "http://localhost:8766")
	m.ready = true
	m.agents = []agentState{
		{id: uuid.New(), name: "Coder", status: "running"},
	}
	m.filterAgent = "RemovedAgent"

	m.cycleFilter()
	if m.filterAgent != "" {
		t.Errorf("filter for removed agent should reset to empty, got %q", m.filterAgent)
	}
}

func TestCycleFilter_ResetsScroll(t *testing.T) {
	// When tab cycles filter, scroll position should reset to 0.
	mgr := newTestManager(t)
	m := New(mgr, "http://localhost:8766")
	m.ready = true
	m.width = 100
	m.height = 30
	m.agents = []agentState{
		{id: uuid.New(), name: "Coder", status: "running"},
		{id: uuid.New(), name: "Tester", status: "idle"},
	}

	// Add log entries and scroll down.
	assign := m.assignColor
	for i := 0; i < 50; i++ {
		m.activityLog.Append(LogEntryMsg{AgentID: uuid.New(), AgentName: "Coder", Data: []byte("line")}, 10, assign)
	}

	// Verify scroll is non-zero.
	if m.activityLog.ScrollPos() == 0 {
		t.Fatal("scrollPos should be non-zero after many appends")
	}

	// Tab to cycle filter — should reset scroll.
	m.cycleFilter()
	if m.activityLog.ScrollPos() != 0 {
		t.Errorf("cycleFilter should reset scroll to 0, got %d", m.activityLog.ScrollPos())
	}
}

func TestRefreshAgents_FilterResetOnAgentRemoved(t *testing.T) {
	// When a filtered agent is removed and AgentChangedMsg arrives, filter and scroll reset.
	mgr := newTestManager(t)
	a := addTestAgent(t, mgr, "Coder", models.AgentStatusRunning, "coding")

	m := New(mgr, "http://localhost:8766")
	m = simulateWindowSize(m, 100, 30)

	// Set filter to "Coder".
	m.filterAgent = "Coder"

	// Add some log lines and scroll.
	for i := 0; i < 30; i++ {
		m.activityLog.Append(LogEntryMsg{AgentID: uuid.New(), AgentName: "Coder", Data: []byte("log")}, 10, m.assignColor)
	}

	// Remove the agent from the manager.
	mgr.RemoveAgent(a.ID)

	// Send AgentChangedMsg to trigger refreshAgents.
	updated, _ := m.Update(AgentChangedMsg(a.ID))
	m = updated.(Model)

	if m.filterAgent != "" {
		t.Errorf("filter should reset when filtered agent removed, got %q", m.filterAgent)
	}
	if m.activityLog.ScrollPos() != 0 {
		t.Errorf("scroll should reset when filtered agent removed, got %d", m.activityLog.ScrollPos())
	}
}

func TestCycleFilter_NoAgents(t *testing.T) {
	mgr := newTestManager(t)

	m := New(mgr, "http://localhost:8766")
	m = simulateWindowSize(m, 100, 30)

	updated, _ := m.Update(tea.KeyPressMsg{Code: '\t'})
	m = updated.(Model)

	if m.filterAgent != "" {
		t.Errorf("tab with no agents should keep filter empty, got %q", m.filterAgent)
	}
}

// --- Help overlay ---

func TestHelpOverlay_QuestionMarkToggles(t *testing.T) {
	mgr := newTestManager(t)
	m := New(mgr, "http://localhost:8766")
	m = simulateWindowSize(m, 100, 30)

	if m.showHelp {
		t.Error("showHelp should be false initially")
	}

	// Press '?' to show help.
	updated, _ := m.Update(tea.KeyPressMsg{Code: '?'})
	m = updated.(Model)

	if !m.showHelp {
		t.Error("showHelp should be true after pressing '?'")
	}

	// View should contain help text.
	v := m.View()
	if !strings.Contains(v.Content, "Keyboard Shortcuts") {
		t.Error("help overlay should contain 'Keyboard Shortcuts'")
	}
}

func TestHelpOverlay_AnyKeyDismisses(t *testing.T) {
	mgr := newTestManager(t)
	m := New(mgr, "http://localhost:8766")
	m = simulateWindowSize(m, 100, 30)

	// Show help.
	m.showHelp = true

	// Press 'j' to dismiss.
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'j'})
	m = updated.(Model)

	if m.showHelp {
		t.Error("showHelp should be false after pressing any key")
	}
}

func TestHelpOverlay_QuitStillWorks(t *testing.T) {
	mgr := newTestManager(t)
	m := New(mgr, "http://localhost:8766")
	m = simulateWindowSize(m, 100, 30)

	// Show help.
	m.showHelp = true

	// Press 'q' — should quit, not just dismiss.
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'q'})
	if cmd == nil {
		t.Error("pressing 'q' while help shown should still return quit command")
	}
}

// --- S key stub ---

func TestSKey_NoStateChange(t *testing.T) {
	mgr := newTestManager(t)
	m := New(mgr, "http://localhost:8766")
	m = simulateWindowSize(m, 100, 30)

	before := m
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 's'})
	after := updated.(Model)

	if cmd != nil {
		t.Error("'s' key should return nil cmd")
	}
	if after.filterAgent != before.filterAgent {
		t.Error("'s' key should not change filterAgent")
	}
	if after.showHelp != before.showHelp {
		t.Error("'s' key should not change showHelp")
	}
}

// --- Stress / many log entries ---

func TestLogBuffer_ManyEntries(t *testing.T) {
	mgr := newTestManager(t)
	m := New(mgr, "http://localhost:8766")
	m = simulateWindowSize(m, 80, 20)

	// Add 500 log entries — should not panic.
	for i := 0; i < 500; i++ {
		msg := LogEntryMsg{AgentID: uuid.New(), AgentName: "Agent", Data: []byte("log entry")}
		updated, _ := m.Update(msg)
		m = updated.(Model)
	}

	if len(m.activityLog.Lines()) != 500 {
		t.Errorf("logLines = %d, want 500", len(m.activityLog.Lines()))
	}

	// View should still render without panic.
	v := m.View()
	if v.Content == "" {
		t.Error("View should render with many log entries")
	}
}

// --- PageUp / PageDown key handling ---

func TestUpdate_PgUpKey_ScrollsActivityLog(t *testing.T) {
	mgr := newTestManager(t)
	m := New(mgr, "http://localhost:8766")
	m = simulateWindowSize(m, 80, 30)

	// Add enough lines to enable scrolling.
	for i := 0; i < 100; i++ {
		msg := LogEntryMsg{AgentID: uuid.New(), AgentName: "A", Data: []byte("line")}
		updated, _ := m.Update(msg)
		m = updated.(Model)
	}

	// Record scroll position (should be auto-scrolled to bottom).
	posBefore := m.activityLog.ScrollPos()
	if posBefore == 0 {
		t.Fatal("scrollPos should be non-zero after many appends")
	}

	// Press pgup.
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
	m = updated.(Model)

	if m.activityLog.ScrollPos() >= posBefore {
		t.Errorf("pgup should decrease scrollPos, got %d (was %d)", m.activityLog.ScrollPos(), posBefore)
	}
}

func TestUpdate_PgDownKey_ScrollsActivityLog(t *testing.T) {
	mgr := newTestManager(t)
	m := New(mgr, "http://localhost:8766")
	m = simulateWindowSize(m, 80, 30)

	// Add enough lines to enable scrolling.
	for i := 0; i < 100; i++ {
		msg := LogEntryMsg{AgentID: uuid.New(), AgentName: "A", Data: []byte("line")}
		updated, _ := m.Update(msg)
		m = updated.(Model)
	}

	// Scroll up first so we have room to scroll down.
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
	m = updated.(Model)
	posAfterUp := m.activityLog.ScrollPos()

	// Press pgdown.
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	m = updated.(Model)

	if m.activityLog.ScrollPos() <= posAfterUp {
		t.Errorf("pgdown should increase scrollPos, got %d (was %d)", m.activityLog.ScrollPos(), posAfterUp)
	}
}
