package cli

import (
	"testing"
	"time"

	"github.com/lsinghkochava/skwad-cli/internal/config"
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
