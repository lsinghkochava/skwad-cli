package cli

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/lsinghkochava/skwad-cli/internal/config"
	"github.com/lsinghkochava/skwad-cli/internal/report"
)

func TestIsRetryableExit(t *testing.T) {
	cases := []struct {
		code int
		want bool
	}{
		{0, false},     // success
		{1, true},      // general failure — retryable
		{2, false},     // permission denied
		{130, false},   // SIGINT
		{137, false},   // SIGKILL
		{42, true},     // unknown non-zero — retryable
		{255, true},    // high error code — retryable
	}
	for _, tc := range cases {
		got := isRetryableExit(tc.code)
		if got != tc.want {
			t.Errorf("isRetryableExit(%d) = %v, want %v", tc.code, got, tc.want)
		}
	}
}

func TestParseDurationSpec_Days(t *testing.T) {
	d, err := parseDurationSpec("7d")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d != 7*24*time.Hour {
		t.Errorf("expected 168h, got %v", d)
	}
}

func TestParseDurationSpec_Hours(t *testing.T) {
	d, err := parseDurationSpec("24h")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d != 24*time.Hour {
		t.Errorf("expected 24h, got %v", d)
	}
}

func TestParseDurationSpec_Minutes(t *testing.T) {
	d, err := parseDurationSpec("30m")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d != 30*time.Minute {
		t.Errorf("expected 30m, got %v", d)
	}
}

func TestParseDurationSpec_Invalid(t *testing.T) {
	_, err := parseDurationSpec("abc")
	if err == nil {
		t.Error("expected error for invalid duration spec")
	}
}

func TestParseDurationSpec_InvalidDays(t *testing.T) {
	_, err := parseDurationSpec("xd")
	if err == nil {
		t.Error("expected error for 'xd'")
	}
}

func TestResolveAgentPrompt_PerAgentFirst(t *testing.T) {
	ac := config.AgentConfig{Prompt: "agent-specific"}
	result := resolveAgentPrompt(ac, "flag-prompt", "team-prompt")
	if result != "agent-specific" {
		t.Errorf("expected per-agent prompt, got %q", result)
	}
}

func TestResolveAgentPrompt_FlagSecond(t *testing.T) {
	ac := config.AgentConfig{}
	result := resolveAgentPrompt(ac, "flag-prompt", "team-prompt")
	if result != "flag-prompt" {
		t.Errorf("expected flag prompt, got %q", result)
	}
}

func TestResolveAgentPrompt_TeamFallback(t *testing.T) {
	ac := config.AgentConfig{}
	result := resolveAgentPrompt(ac, "", "team-prompt")
	if result != "team-prompt" {
		t.Errorf("expected team prompt, got %q", result)
	}
}

func TestResolveAgentPrompt_Empty(t *testing.T) {
	ac := config.AgentConfig{}
	result := resolveAgentPrompt(ac, "", "")
	if result != "" {
		t.Errorf("expected empty prompt, got %q", result)
	}
}

func TestFilterRunOutput_EntryMode(t *testing.T) {
	agents := []report.AgentResult{
		{Name: "Builder", ResultText: "built it"},
		{Name: "QA", ResultText: "tested it"},
	}
	got := filterRunOutput("entry", agents, "Builder")
	if len(got) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(got))
	}
	if got[0].Name != "Builder" {
		t.Errorf("expected Builder, got %q", got[0].Name)
	}
}

func TestFilterRunOutput_EntryModeFallback(t *testing.T) {
	agents := []report.AgentResult{
		{Name: "Alpha", ResultText: "first"},
		{Name: "Beta", ResultText: "second"},
	}
	got := filterRunOutput("entry", agents, "")
	if len(got) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(got))
	}
	if got[0].Name != "Alpha" {
		t.Errorf("expected Alpha (first agent fallback), got %q", got[0].Name)
	}
}

func TestFilterRunOutput_EntryModeNotFound(t *testing.T) {
	agents := []report.AgentResult{
		{Name: "Alpha", ResultText: "first"},
	}
	got := filterRunOutput("entry", agents, "NonExistent")
	if len(got) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(got))
	}
	if got[0].Name != "Alpha" {
		t.Errorf("expected Alpha (fallback), got %q", got[0].Name)
	}
}

func TestFilterRunOutput_AllMode(t *testing.T) {
	agents := []report.AgentResult{
		{Name: "Builder", ResultText: "built it"},
		{Name: "QA", ResultText: "tested it"},
	}
	got := filterRunOutput("all", agents, "Builder")
	if len(got) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(got))
	}
}

func TestFilterRunOutput_RawMode(t *testing.T) {
	agents := []report.AgentResult{
		{Name: "Builder", ResultText: "built it"},
	}
	got := filterRunOutput("raw", agents, "Builder")
	if got != nil {
		t.Errorf("expected nil for raw mode, got %v", got)
	}
}

func TestFilterRunOutput_EmptyAgents(t *testing.T) {
	got := filterRunOutput("entry", nil, "")
	if got != nil {
		t.Errorf("expected nil for empty agents, got %v", got)
	}
}

func TestParseResultText(t *testing.T) {
	raw := json.RawMessage(`{"type":"result","subtype":"success","result":"All done!","num_turns":5,"total_cost_usd":0.12}`)
	got := parseResultText(raw)
	if got != "All done!" {
		t.Errorf("expected 'All done!', got %q", got)
	}
}

func TestParseResultText_EmptyResult(t *testing.T) {
	raw := json.RawMessage(`{"type":"result","subtype":"success","result":""}`)
	got := parseResultText(raw)
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestParseResultText_InvalidJSON(t *testing.T) {
	got := parseResultText(json.RawMessage(`{bad json`))
	if got != "" {
		t.Errorf("expected empty string for invalid JSON, got %q", got)
	}
}

func TestParseResultText_Nil(t *testing.T) {
	got := parseResultText(nil)
	if got != "" {
		t.Errorf("expected empty string for nil input, got %q", got)
	}
}

func TestParseResultText_NoResultField(t *testing.T) {
	raw := json.RawMessage(`{"type":"result","subtype":"success"}`)
	got := parseResultText(raw)
	if got != "" {
		t.Errorf("expected empty string when result field is absent, got %q", got)
	}
}

func TestFilterRunOutput_UnrecognizedMode(t *testing.T) {
	agents := []report.AgentResult{
		{Name: "Builder", ResultText: "built it"},
	}
	got := filterRunOutput("unknown-mode", agents, "Builder")
	if got != nil {
		t.Error("unrecognized mode should return nil (same as raw)")
	}
}
