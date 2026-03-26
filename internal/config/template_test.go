package config

import (
	"strings"
	"testing"
)

func TestListTemplates(t *testing.T) {
	templates := ListTemplates()
	if len(templates) < 2 {
		t.Fatalf("expected at least 2 templates, got %d: %v", len(templates), templates)
	}

	found := make(map[string]bool)
	for _, name := range templates {
		found[name] = true
	}
	if !found["review-team"] {
		t.Error("expected 'review-team' template")
	}
	if !found["dev-team"] {
		t.Error("expected 'dev-team' template")
	}
}

func TestLoadTemplate_ReviewTeam(t *testing.T) {
	repo := t.TempDir()
	tc, err := LoadTemplate("review-team", map[string]string{
		"repo":   repo,
		"prompt": "Review PR #42",
	})
	if err != nil {
		t.Fatalf("LoadTemplate: %v", err)
	}
	if tc.Name != "Code Reviews" {
		t.Errorf("expected name 'Code Reviews', got %q", tc.Name)
	}
	if tc.Repo != repo {
		t.Errorf("expected repo %q, got %q", repo, tc.Repo)
	}
	if tc.Prompt != "Review PR #42" {
		t.Errorf("expected prompt 'Review PR #42', got %q", tc.Prompt)
	}
	if len(tc.Agents) < 5 {
		t.Errorf("expected at least 5 agents in review-team, got %d", len(tc.Agents))
	}
}

func TestLoadTemplate_DevTeam(t *testing.T) {
	repo := t.TempDir()
	tc, err := LoadTemplate("dev-team", map[string]string{
		"repo":   repo,
		"prompt": "",
	})
	if err != nil {
		t.Fatalf("LoadTemplate: %v", err)
	}
	if len(tc.Agents) != 3 {
		t.Errorf("expected 3 agents in dev-team, got %d", len(tc.Agents))
	}
}

func TestLoadTemplate_UnknownTemplate(t *testing.T) {
	_, err := LoadTemplate("nonexistent-template", nil)
	if err == nil {
		t.Error("expected error for unknown template")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got %v", err)
	}
}

func TestLoadTemplate_VariableSubstitution(t *testing.T) {
	repo := t.TempDir()
	tc, err := LoadTemplate("review-team", map[string]string{
		"repo":   repo,
		"prompt": "custom prompt text",
	})
	if err != nil {
		t.Fatalf("LoadTemplate: %v", err)
	}
	// Verify ${repo} was substituted.
	if tc.Repo != repo {
		t.Errorf("${repo} not substituted: got %q", tc.Repo)
	}
	// Verify ${prompt} was substituted.
	if tc.Prompt != "custom prompt text" {
		t.Errorf("${prompt} not substituted: got %q", tc.Prompt)
	}
}

func TestLoadTemplate_MissingVar(t *testing.T) {
	// Don't provide ${prompt} — it should stay as "${prompt}" in the output.
	tc, err := LoadTemplate("dev-team", map[string]string{
		"repo": t.TempDir(),
	})
	if err != nil {
		t.Fatalf("LoadTemplate: %v", err)
	}
	// ${prompt} was not substituted — should remain as literal.
	if tc.Prompt != "${prompt}" {
		t.Errorf("expected unsubstituted '${prompt}', got %q", tc.Prompt)
	}
}

func TestParseSetFlags(t *testing.T) {
	vars := ParseSetFlags([]string{"repo=/tmp/test", "prompt=do the thing"})
	if vars["repo"] != "/tmp/test" {
		t.Errorf("expected repo=/tmp/test, got %q", vars["repo"])
	}
	if vars["prompt"] != "do the thing" {
		t.Errorf("expected prompt='do the thing', got %q", vars["prompt"])
	}
}

func TestParseSetFlags_Defaults(t *testing.T) {
	vars := ParseSetFlags(nil)
	if _, ok := vars["repo"]; !ok {
		t.Error("expected default repo to be set")
	}
	if _, ok := vars["prompt"]; !ok {
		t.Error("expected default prompt to be set")
	}
}
