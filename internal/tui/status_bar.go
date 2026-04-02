package tui

import (
	"fmt"

	"charm.land/lipgloss/v2"

	"github.com/lsinghkochava/skwad-cli/internal/models"
)

// RenderStatusBar renders the bottom status bar with MCP URL, agent counts, and key hints.
func RenderStatusBar(agents []agentState, mcpURL string, width int, filterAgent string) string {
	style := lipgloss.NewStyle().
		Background(lipgloss.Color("#333333")).
		Foreground(lipgloss.Color("#CCCCCC")).
		Width(width)

	activeCount := 0
	for _, a := range agents {
		if a.status == string(models.AgentStatusRunning) {
			activeCount++
		}
	}

	filterHint := "tab: filter"
	if filterAgent != "" {
		filterHint = fmt.Sprintf("tab: filter [%s]", filterAgent)
	}

	bar := fmt.Sprintf(" MCP: %s  |  Agents: %d/%d active  |  q: quit  j/k: scroll  %s  ?: help",
		mcpURL, activeCount, len(agents), filterHint)
	return style.Render(bar)
}
