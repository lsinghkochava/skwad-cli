package tui

import (
	"strings"
	"testing"
)

func TestStatusBar_ContainsMCPURL(t *testing.T) {
	sb := NewStatusBar("http://127.0.0.1:8777/mcp", 5)
	view := sb.View(120)

	if !strings.Contains(view, "http://127.0.0.1:8777/mcp") {
		t.Errorf("status bar should contain MCP URL, got: %q", view)
	}
}

func TestStatusBar_ContainsAgentCount(t *testing.T) {
	sb := NewStatusBar("http://localhost:8777/mcp", 3)
	view := sb.View(120)

	if !strings.Contains(view, "Agents: 3") {
		t.Errorf("status bar should contain agent count, got: %q", view)
	}
}

func TestStatusBar_ContainsActiveAgent(t *testing.T) {
	sb := NewStatusBar("http://localhost:8777/mcp", 2)
	sb.SetActive("Manager")
	view := sb.View(120)

	if !strings.Contains(view, "Active: Manager") {
		t.Errorf("status bar should contain active agent name, got: %q", view)
	}
}

func TestStatusBar_ModeNavigation(t *testing.T) {
	sb := NewStatusBar("", 1)
	view := sb.View(100)

	// The mode is rendered with ANSI styling, so check for the raw text "NAV".
	if !strings.Contains(view, "NAV") {
		t.Errorf("status bar should show NAV mode by default, got: %q", view)
	}
}

func TestStatusBar_ModeInsert(t *testing.T) {
	sb := NewStatusBar("", 1)
	sb.SetMode(ModeInsert)
	view := sb.View(100)

	if !strings.Contains(view, "INS") {
		t.Errorf("status bar should show INS mode, got: %q", view)
	}
}

func TestStatusBar_ModeTransition(t *testing.T) {
	sb := NewStatusBar("", 1)

	// Default is NAV.
	if !strings.Contains(sb.View(100), "NAV") {
		t.Error("should start in NAV mode")
	}

	// Switch to INS.
	sb.SetMode(ModeInsert)
	if !strings.Contains(sb.View(100), "INS") {
		t.Error("should switch to INS mode")
	}

	// Switch back to NAV.
	sb.SetMode(ModeNavigation)
	if !strings.Contains(sb.View(100), "NAV") {
		t.Error("should switch back to NAV mode")
	}
}

func TestStatusBar_UpdateActiveAgent(t *testing.T) {
	sb := NewStatusBar("", 2)

	sb.SetActive("Alice")
	if !strings.Contains(sb.View(100), "Active: Alice") {
		t.Error("should show Alice as active")
	}

	sb.SetActive("Bob")
	if !strings.Contains(sb.View(100), "Active: Bob") {
		t.Error("should update to Bob as active")
	}
}

func TestStatusBar_ShowsShortcuts_NavMode(t *testing.T) {
	sb := NewStatusBar("", 1)
	view := sb.View(120)

	if !strings.Contains(view, "q:quit") {
		t.Error("NAV mode should show q:quit shortcut")
	}
	if !strings.Contains(view, "i:insert") {
		t.Error("NAV mode should show i:insert shortcut")
	}
}

func TestStatusBar_ShowsShortcuts_InsertMode(t *testing.T) {
	sb := NewStatusBar("", 1)
	sb.SetMode(ModeInsert)
	view := sb.View(120)

	if !strings.Contains(view, "esc:navigate") {
		t.Error("INS mode should show esc:navigate shortcut")
	}
}
