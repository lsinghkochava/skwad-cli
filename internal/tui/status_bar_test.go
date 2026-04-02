package tui

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/lsinghkochava/skwad-cli/internal/models"
)

func TestRenderStatusBar_ContainsMCPURL(t *testing.T) {
	out := RenderStatusBar(nil, "http://localhost:8766", 100, "")
	if !strings.Contains(out, "localhost:8766") {
		t.Error("status bar should contain MCP URL")
	}
}

func TestRenderStatusBar_AgentCounts(t *testing.T) {
	agents := []agentState{
		{id: uuid.New(), name: "A1", status: string(models.AgentStatusRunning)},
		{id: uuid.New(), name: "A2", status: string(models.AgentStatusIdle)},
		{id: uuid.New(), name: "A3", status: string(models.AgentStatusRunning)},
		{id: uuid.New(), name: "A4", status: string(models.AgentStatusError)},
		{id: uuid.New(), name: "A5", status: string(models.AgentStatusIdle)},
	}

	out := RenderStatusBar(agents, "http://localhost:8766", 120, "")
	if !strings.Contains(out, "2/5") {
		t.Errorf("status bar should show '2/5' active, got: %s", out)
	}
}

func TestRenderStatusBar_FilterActive(t *testing.T) {
	out := RenderStatusBar(nil, "http://localhost:8766", 120, "Coder")
	if !strings.Contains(out, "filter [Coder]") {
		t.Errorf("status bar with filter should show 'filter [Coder]', got: %s", out)
	}
}

func TestRenderStatusBar_FilterInactive(t *testing.T) {
	out := RenderStatusBar(nil, "http://localhost:8766", 120, "")
	if !strings.Contains(out, "tab: filter") {
		t.Error("status bar without filter should show 'tab: filter'")
	}
	if strings.Contains(out, "filter [") {
		t.Error("status bar without filter should NOT show 'filter [...]'")
	}
}

func TestRenderStatusBar_EmptyAgents(t *testing.T) {
	out := RenderStatusBar(nil, "http://localhost:8766", 100, "")
	if !strings.Contains(out, "0/0") {
		t.Error("status bar with no agents should show '0/0'")
	}
}

func TestRenderStatusBar_HelpHint(t *testing.T) {
	out := RenderStatusBar(nil, "http://localhost:8766", 100, "")
	if !strings.Contains(out, "?: help") {
		t.Error("status bar should contain '?: help'")
	}
}
