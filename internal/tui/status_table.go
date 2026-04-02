package tui

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/lsinghkochava/skwad-cli/internal/models"
)

// RenderStatusTable renders the agent status table panel.
// Takes agent states, available width, available height, and the color map.
func RenderStatusTable(agents []agentState, width, height int, colorMap map[string]color.Color) string {
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF"))
	sepStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#555555"))

	nameW := 20
	statusW := 10
	activityW := width - nameW - statusW - 6
	if activityW < 10 {
		activityW = 10
	}

	header := fmt.Sprintf(" %-*s  %-*s  %-*s", nameW, "AGENT", statusW, "STATUS", activityW, "ACTIVITY")
	sep := strings.Repeat("─", width)

	var b strings.Builder
	b.WriteString(headerStyle.Render(header))
	b.WriteString("\n")
	b.WriteString(sepStyle.Render(sep))
	b.WriteString("\n")

	rowsAvailable := height - 3 // header + sep + trailing blank
	for i, a := range agents {
		if i >= rowsAvailable {
			break
		}
		dot := statusDot(a.status)

		// Color-code agent name using assigned color.
		nameStr := truncate(a.name, nameW)
		if c, ok := colorMap[a.name]; ok {
			nameStr = lipgloss.NewStyle().Foreground(c).Render(nameStr)
		}

		activity := truncate(a.statusText, activityW)
		row := fmt.Sprintf(" %-*s  %s %-*s  %-*s", nameW, nameStr, dot, statusW-2, a.status, activityW, activity)
		b.WriteString(row)
		b.WriteString("\n")
	}

	b.WriteString("\n")
	return b.String()
}

// statusDot returns a colored dot string for the given agent status.
func statusDot(status string) string {
	switch models.AgentStatus(status) {
	case models.AgentStatusRunning:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#00FF00")).Render("●")
	case models.AgentStatusIdle:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#888888")).Render("●")
	case models.AgentStatusInput:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFF00")).Render("●")
	case models.AgentStatusError:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000")).Render("●")
	default:
		return "○"
	}
}
