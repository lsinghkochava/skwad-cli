package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/google/uuid"
	"github.com/lsinghkochava/skwad-cli/internal/models"
)

var (
	sidebarStyle = lipgloss.NewStyle().
			BorderRight(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("#444444"))

	selectedStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#333333")).
			Bold(true)

	agentNameStyle = lipgloss.NewStyle()

	statusTextStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#666666"))

	dotRunning = lipgloss.NewStyle().Foreground(lipgloss.Color("#00CC00")).Render("●")
	dotIdle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#CCCC00")).Render("●")
	dotError   = lipgloss.NewStyle().Foreground(lipgloss.Color("#CC0000")).Render("●")
	dotInput   = lipgloss.NewStyle().Foreground(lipgloss.Color("#0088CC")).Render("●")
	dotDefault = lipgloss.NewStyle().Foreground(lipgloss.Color("#666666")).Render("●")
)

// AgentEntry holds the display state for one agent in the sidebar.
type AgentEntry struct {
	ID         uuid.UUID
	Name       string
	Status     models.AgentStatus
	StatusText string
}

// Sidebar displays a navigable list of agents.
type Sidebar struct {
	agents           []AgentEntry
	selected         int
	selectionChanged bool
}

// NewSidebar creates a sidebar from agent models.
func NewSidebar(agents []*models.Agent) *Sidebar {
	entries := make([]AgentEntry, len(agents))
	for i, a := range agents {
		entries[i] = AgentEntry{
			ID:     a.ID,
			Name:   a.Name,
			Status: a.Status,
		}
	}
	return &Sidebar{agents: entries}
}

// Update processes navigation keys and status messages.
func (s *Sidebar) Update(msg tea.Msg) tea.Cmd {
	s.selectionChanged = false

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "j", "down":
			if s.selected < len(s.agents)-1 {
				s.selected++
				s.selectionChanged = true
			}
		case "k", "up":
			if s.selected > 0 {
				s.selected--
				s.selectionChanged = true
			}
		case "tab":
			s.selected = (s.selected + 1) % len(s.agents)
			s.selectionChanged = true
		case "shift+tab":
			s.selected--
			if s.selected < 0 {
				s.selected = len(s.agents) - 1
			}
			s.selectionChanged = true
		}
	case StatusMsg:
		for i := range s.agents {
			if s.agents[i].ID == msg.AgentID {
				s.agents[i].Status = msg.Status
				s.agents[i].StatusText = msg.Text
				break
			}
		}
	}
	return nil
}

// View renders the sidebar at the given dimensions.
func (s *Sidebar) View(width, height int) string {
	// Reserve 1 char for right border.
	contentWidth := width - 1
	if contentWidth < 5 {
		contentWidth = 5
	}

	var lines []string
	for i, a := range s.agents {
		// Each agent takes 2 lines: name + status text.
		if len(lines) >= height {
			break
		}

		dot := statusDot(a.Status)
		nameLine := " " + dot + " " + a.Name

		// Truncate or pad name line.
		nameLine = padOrTruncate(nameLine, contentWidth)

		if i == s.selected {
			nameLine = selectedStyle.Width(contentWidth).Render(nameLine)
		}
		lines = append(lines, nameLine)

		// Status text line (if room).
		if len(lines) < height {
			statusLine := ""
			if a.StatusText != "" {
				statusLine = "   " + a.StatusText
			}
			statusLine = padOrTruncate(statusLine, contentWidth)
			statusLine = statusTextStyle.Render(statusLine)
			lines = append(lines, statusLine)
		}
	}

	// Pad remaining height.
	for len(lines) < height {
		lines = append(lines, strings.Repeat(" ", contentWidth))
	}

	content := strings.Join(lines[:height], "\n")
	return sidebarStyle.Render(content)
}

// SelectedAgent returns the ID of the currently selected agent.
func (s *Sidebar) SelectedAgent() uuid.UUID {
	if s.selected >= 0 && s.selected < len(s.agents) {
		return s.agents[s.selected].ID
	}
	return uuid.UUID{}
}

// SelectedName returns the name of the currently selected agent.
func (s *Sidebar) SelectedName() string {
	if s.selected >= 0 && s.selected < len(s.agents) {
		return s.agents[s.selected].Name
	}
	return ""
}

// SelectionChanged reports whether the selection changed in the last Update.
func (s *Sidebar) SelectionChanged() bool {
	return s.selectionChanged
}

func statusDot(status models.AgentStatus) string {
	switch status {
	case models.AgentStatusRunning:
		return dotRunning
	case models.AgentStatusIdle:
		return dotIdle
	case models.AgentStatusError:
		return dotError
	case models.AgentStatusInput:
		return dotInput
	default:
		return dotDefault
	}
}

func padOrTruncate(s string, width int) string {
	if len(s) > width {
		return s[:width]
	}
	return s + strings.Repeat(" ", width-len(s))
}
