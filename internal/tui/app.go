// Package tui implements a Bubble Tea v2 terminal dashboard for monitoring
// Skwad agents in real time. It provides a 3-panel layout: agent status table,
// scrollable activity log, and a status bar.
package tui

import (
	"image/color"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/lsinghkochava/skwad-cli/internal/agent"
)

// agentColors are consistent colors assigned to agents by index.
var agentColors = []color.Color{
	lipgloss.Color("#00CCCC"), // cyan
	lipgloss.Color("#CCCC00"), // yellow
	lipgloss.Color("#00CC00"), // green
	lipgloss.Color("#CC00CC"), // magenta
	lipgloss.Color("#0000CC"), // blue
	lipgloss.Color("#FF3333"), // bright red
}

// AgentChangedMsg is sent when an agent's state changes.
type AgentChangedMsg uuid.UUID

// LogEntryMsg is sent when a new log line arrives from an agent.
type LogEntryMsg struct {
	AgentID   uuid.UUID
	AgentName string
	Data      []byte
}

// agentState is a snapshot of an agent's display state.
type agentState struct {
	id         uuid.UUID
	name       string
	status     string
	statusText string
}

// Model is the main Bubble Tea model for the TUI dashboard.
type Model struct {
	manager     *agent.Manager
	agents      []agentState
	activityLog *ActivityLog
	width       int
	height      int
	mcpURL      string
	ready       bool

	// colorMap maps agent name → color for consistent coloring.
	colorMap  map[string]color.Color
	nextColor int

	filterAgent string // empty = show all, non-empty = filter by agent name
	showHelp    bool   // toggle help overlay
}

// New creates a new TUI model wired to the given agent manager.
func New(manager *agent.Manager, mcpURL string) Model {
	return Model{
		manager:     manager,
		colorMap:    make(map[string]color.Color),
		activityLog: NewActivityLog(),
		mcpURL:      mcpURL,
	}
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ready = true
		m.refreshAgents()
		return m, nil

	case tea.KeyPressMsg:
		// Help overlay: any key dismisses it (except q/ctrl+c which quit).
		if m.showHelp {
			if msg.String() == "q" || msg.String() == "ctrl+c" {
				return m, tea.Quit
			}
			m.showHelp = false
			return m, nil
		}

		switch {
		case msg.String() == "q", msg.String() == "ctrl+c":
			return m, tea.Quit
		case msg.String() == "j", msg.String() == "down":
			m.activityLog.ScrollDown(m.logViewHeight())
		case msg.String() == "k", msg.String() == "up":
			m.activityLog.ScrollUp()
		case msg.String() == "pgup":
			m.activityLog.PageUp(m.logViewHeight())
		case msg.String() == "pgdown":
			m.activityLog.PageDown(m.logViewHeight())
		case msg.String() == "tab":
			m.cycleFilter()
		case msg.String() == "?":
			m.showHelp = true
		case msg.String() == "s":
			// Stub — send message (coming soon)
		}
		return m, nil

	case AgentChangedMsg:
		m.refreshAgents()
		return m, nil

	case LogEntryMsg:
		m.activityLog.Append(msg, m.logViewHeight(), m.assignColor)
		return m, nil
	}

	return m, nil
}

// View implements tea.Model.
func (m Model) View() tea.View {
	if !m.ready {
		v := tea.NewView("Initializing...")
		v.AltScreen = true
		return v
	}

	tableHeight := m.tableHeight()
	statusBarHeight := 1
	logHeight := m.height - tableHeight - statusBarHeight
	if logHeight < 1 {
		logHeight = 1
	}

	var b strings.Builder

	// Top panel: agent status table.
	b.WriteString(RenderStatusTable(m.agents, m.width, tableHeight, m.colorMap))

	// Middle panel: activity log or help overlay.
	if m.showHelp {
		b.WriteString(renderHelp(logHeight))
	} else {
		b.WriteString(m.activityLog.Render(m.width, logHeight, m.filterAgent))
	}

	// Bottom panel: status bar.
	b.WriteString(RenderStatusBar(m.agents, m.mcpURL, m.width, m.filterAgent))

	v := tea.NewView(b.String())
	v.AltScreen = true
	return v
}

// --- internal helpers ---

func (m *Model) refreshAgents() {
	agents := m.manager.Agents()
	names := make([]string, len(agents))
	for i, a := range agents {
		names[i] = a.Name
	}
	slog.Debug("tui.refreshAgents", "count", len(agents), "names", names)
	m.agents = make([]agentState, 0, len(agents))
	for _, a := range agents {
		m.agents = append(m.agents, agentState{
			id:         a.ID,
			name:       a.Name,
			status:     string(a.Status),
			statusText: a.StatusText,
		})
		// Assign color if not already assigned.
		m.assignColor(a.Name)
	}

	// If the filtered agent was removed, reset filter and scroll.
	if m.filterAgent != "" {
		found := false
		for _, a := range m.agents {
			if a.name == m.filterAgent {
				found = true
				break
			}
		}
		if !found {
			m.filterAgent = ""
			m.activityLog.ResetScroll()
		}
	}
}

func (m *Model) assignColor(name string) color.Color {
	if c, ok := m.colorMap[name]; ok {
		return c
	}
	c := agentColors[m.nextColor%len(agentColors)]
	m.nextColor++
	m.colorMap[name] = c
	return c
}

func (m *Model) logViewHeight() int {
	// Subtract 3 for border (top+bottom) and header line.
	h := m.height - m.tableHeight() - 1 - 3
	if h < 1 {
		h = 1
	}
	return h
}

func (m *Model) tableHeight() int {
	// Header + separator + one row per agent + 1 blank line, capped at 30% of height.
	rows := len(m.agents) + 3
	maxH := m.height * 30 / 100
	if maxH < 5 {
		maxH = 5
	}
	if rows > maxH {
		rows = maxH
	}
	return rows
}

func (m *Model) cycleFilter() {
	if len(m.agents) == 0 {
		m.filterAgent = ""
		m.activityLog.ResetScroll()
		return
	}

	if m.filterAgent == "" {
		m.filterAgent = m.agents[0].name
		m.activityLog.ResetScroll()
		return
	}

	// Find current agent in list and advance to next, or wrap to empty.
	for i, a := range m.agents {
		if a.name == m.filterAgent {
			if i+1 < len(m.agents) {
				m.filterAgent = m.agents[i+1].name
			} else {
				m.filterAgent = "" // wrap back to "show all"
			}
			m.activityLog.ResetScroll()
			return
		}
	}

	// Current filter agent not found (removed), reset.
	m.filterAgent = ""
	m.activityLog.ResetScroll()
}

// --- rendering ---

func renderHelp(height int) string {
	lines := []string{
		"",
		"  Keyboard Shortcuts",
		"  ──────────────────",
		"  q / ctrl+c    Quit",
		"  j / ↓         Scroll down",
		"  k / ↑         Scroll up",
		"  PgUp          Page up",
		"  PgDn          Page down",
		"  tab           Cycle agent filter",
		"  ?             Toggle this help",
		"  s             Send message (coming soon)",
		"",
		"  Press any key to close",
	}

	var b strings.Builder
	for i, line := range lines {
		if i >= height {
			break
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	// Fill remaining lines.
	for i := len(lines); i < height; i++ {
		b.WriteString("\n")
	}
	return b.String()
}

