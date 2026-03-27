package tui

import (
	"fmt"

	"charm.land/lipgloss/v2"
)

var (
	statusBarStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#AAAAAA")).
			Background(lipgloss.Color("#1A1A2E"))

	modeNavStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#888888")).
			Background(lipgloss.Color("#1A1A2E"))

	modeInsStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#00CC00")).
			Background(lipgloss.Color("#1A1A2E")).
			Bold(true)

	separatorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#444444")).
			Background(lipgloss.Color("#1A1A2E"))
)

// StatusBar renders a single-line status bar at the bottom of the TUI.
type StatusBar struct {
	mcpURL      string
	agentCount  int
	activeAgent string
	mode        Mode
}

// NewStatusBar creates a status bar.
func NewStatusBar(mcpURL string, agentCount int) *StatusBar {
	return &StatusBar{
		mcpURL:     mcpURL,
		agentCount: agentCount,
		mode:       ModeNavigation,
	}
}

// SetActive updates the displayed active agent name.
func (sb *StatusBar) SetActive(name string) {
	sb.activeAgent = name
}

// SetMode updates the displayed mode.
func (sb *StatusBar) SetMode(m Mode) {
	sb.mode = m
}

// View renders the status bar at the given width.
func (sb *StatusBar) View(width int) string {
	sep := separatorStyle.Render(" │ ")

	modeStr := modeNavStyle.Render("NAV")
	shortcuts := "q:quit i:insert tab:cycle r:restart ?:help"
	if sb.mode == ModeInsert {
		modeStr = modeInsStyle.Render("INS")
		shortcuts = "esc:navigate"
	}

	left := statusBarStyle.Render(fmt.Sprintf(" MCP: %s", sb.mcpURL)) +
		sep +
		statusBarStyle.Render(fmt.Sprintf("Agents: %d", sb.agentCount)) +
		sep +
		statusBarStyle.Render(fmt.Sprintf("Active: %s", sb.activeAgent)) +
		sep +
		modeStr +
		sep +
		statusBarStyle.Render(shortcuts)

	// Pad to full width with background.
	padding := width - lipgloss.Width(left)
	if padding > 0 {
		left += statusBarStyle.Render(fmt.Sprintf("%*s", padding, ""))
	}

	return left
}
