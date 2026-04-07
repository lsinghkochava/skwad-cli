package cli

import (
	"strings"
	"testing"

	"github.com/lsinghkochava/skwad-cli/internal/config"
	"github.com/lsinghkochava/skwad-cli/internal/daemon"
	"github.com/lsinghkochava/skwad-cli/internal/models"
)

// newTestDaemon creates a minimal daemon for testing persona resolution.
func newTestDaemon(t *testing.T) *daemon.Daemon {
	t.Helper()
	d, err := daemon.New(daemon.Config{
		DataDir: t.TempDir(),
		MCPPort: 0,
	})
	if err != nil {
		t.Fatalf("daemon.New: %v", err)
	}
	return d
}

func TestResolvePersona_ByName(t *testing.T) {
	d := newTestDaemon(t)

	// Save a persona that matches by name.
	personas := models.DefaultPersonas()
	_ = d.Store.SavePersonas(personas)

	tc := &config.TeamConfig{
		Name: "Test",
		Repo: t.TempDir(),
		Agents: []config.AgentConfig{
			{Name: "Bot", AgentType: "claude", Persona: personas[0].Name},
		},
	}

	agents := createAgentsFromConfig(d, tc)
	if len(agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(agents))
	}
	if agents[0].PersonaID == nil {
		t.Error("expected PersonaID to be set for name-matched persona")
	}
}

func TestResolvePersona_InlineInstructions(t *testing.T) {
	d := newTestDaemon(t)

	tc := &config.TeamConfig{
		Name: "Test",
		Repo: t.TempDir(),
		Agents: []config.AgentConfig{
			{Name: "Bot", AgentType: "claude", PersonaInstructions: "You are a specialist."},
		},
	}

	agents := createAgentsFromConfig(d, tc)
	if agents[0].PersonaID == nil {
		t.Fatal("expected PersonaID to be set for inline instructions")
	}

	// Verify the persona is registered in-memory (transient, not persisted).
	p := d.Manager.Persona(*agents[0].PersonaID)
	if p == nil || !strings.Contains(p.Instructions, "You are a specialist.") {
		t.Error("expected inline instructions persona to be registered in-memory")
	}
}

func TestResolvePersona_NoPersona(t *testing.T) {
	d := newTestDaemon(t)

	tc := &config.TeamConfig{
		Name: "Test",
		Repo: t.TempDir(),
		Agents: []config.AgentConfig{
			{Name: "Bot", AgentType: "claude"},
		},
	}

	agents := createAgentsFromConfig(d, tc)
	if agents[0].PersonaID != nil {
		t.Error("expected nil PersonaID when no persona fields set")
	}
}

func TestResolvePersona_InlinePriorityOverName(t *testing.T) {
	d := newTestDaemon(t)

	// Save a persona with a known name.
	personas := models.DefaultPersonas()
	_ = d.Store.SavePersonas(personas)

	tc := &config.TeamConfig{
		Name: "Test",
		Repo: t.TempDir(),
		Agents: []config.AgentConfig{
			{
				Name:                "Bot",
				AgentType:           "claude",
				Persona:             personas[0].Name,
				PersonaInstructions: "Override instructions",
			},
		},
	}

	agents := createAgentsFromConfig(d, tc)
	if agents[0].PersonaID == nil {
		t.Fatal("expected PersonaID set")
	}

	// The inline instructions should win — verify via in-memory lookup.
	p := d.Manager.Persona(*agents[0].PersonaID)
	if p == nil || !strings.Contains(p.Instructions, "Override instructions") {
		t.Error("expected inline instructions to take priority over name match")
	}
}

// ---------------------------------------------------------------------------
// Tag merging tests
// ---------------------------------------------------------------------------

func TestTagMerge_ConfigAndPersonaTags(t *testing.T) {
	d := newTestDaemon(t)

	// Create a persona with AllowedCategories (acts as persona tags).
	tc := &config.TeamConfig{
		Name: "Test",
		Repo: t.TempDir(),
		Agents: []config.AgentConfig{
			{
				Name:                "Bot",
				AgentType:           "claude",
				Tags:                []string{"code"},
				PersonaInstructions: "You are a test bot.",
			},
		},
		Personas: []config.PersonaConfig{
			{Name: "Bot", Instructions: "You are a test bot.", Tags: []string{"test", "review"}},
		},
	}

	agents := createAgentsFromConfig(d, tc)
	if len(agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(agents))
	}

	a := agents[0]

	// Inline persona instructions take priority over team persona, so
	// AllowedCategories from the inline persona won't have the team persona tags.
	// The config tags should be present.
	found := make(map[string]bool)
	for _, tag := range a.Tags {
		found[tag] = true
	}
	if !found["code"] {
		t.Errorf("expected 'code' tag from AgentConfig.Tags, got %v", a.Tags)
	}
}

func TestTagMerge_PersonaCategoriesMerged(t *testing.T) {
	d := newTestDaemon(t)

	// Use team-level persona (matched by name) which has AllowedCategories.
	tc := &config.TeamConfig{
		Name: "Test",
		Repo: t.TempDir(),
		Agents: []config.AgentConfig{
			{
				Name:      "Bot",
				AgentType: "claude",
				Tags:      []string{"code"},
			},
		},
		Personas: []config.PersonaConfig{
			{Name: "Bot", Instructions: "You are a test bot.", Tags: []string{"test", "review"}},
		},
	}

	agents := createAgentsFromConfig(d, tc)
	a := agents[0]

	// Should have: code (from config) + test, review (from persona categories).
	found := make(map[string]bool)
	for _, tag := range a.Tags {
		found[tag] = true
	}
	if !found["code"] {
		t.Errorf("expected 'code' from config tags, got %v", a.Tags)
	}
	if !found["test"] {
		t.Errorf("expected 'test' from persona categories, got %v", a.Tags)
	}
	if !found["review"] {
		t.Errorf("expected 'review' from persona categories, got %v", a.Tags)
	}

	// Should be sorted.
	for i := 1; i < len(a.Tags); i++ {
		if a.Tags[i-1] > a.Tags[i] {
			t.Errorf("tags should be sorted, got %v", a.Tags)
			break
		}
	}
}

func TestTagMerge_Deduplication(t *testing.T) {
	d := newTestDaemon(t)

	// Both config and persona have "code" — should appear only once.
	tc := &config.TeamConfig{
		Name: "Test",
		Repo: t.TempDir(),
		Agents: []config.AgentConfig{
			{
				Name:      "Bot",
				AgentType: "claude",
				Tags:      []string{"code", "test"},
			},
		},
		Personas: []config.PersonaConfig{
			{Name: "Bot", Instructions: "Bot persona", Tags: []string{"Code", "review"}},
		},
	}

	agents := createAgentsFromConfig(d, tc)
	a := agents[0]

	// Count occurrences of "code" — should be exactly 1 (case-insensitive dedup).
	codeCount := 0
	for _, tag := range a.Tags {
		if tag == "code" {
			codeCount++
		}
	}
	if codeCount != 1 {
		t.Errorf("expected 'code' to appear once (deduplicated), got %d in %v", codeCount, a.Tags)
	}

	// Total should be 3: code, review, test (sorted).
	if len(a.Tags) != 3 {
		t.Errorf("expected 3 deduplicated tags, got %d: %v", len(a.Tags), a.Tags)
	}

	// Verify sorted: code, review, test.
	expected := []string{"code", "review", "test"}
	for i, want := range expected {
		if i >= len(a.Tags) || a.Tags[i] != want {
			t.Errorf("tags[%d] = %q, want %q (full: %v)", i, a.Tags[i], want, a.Tags)
		}
	}
}

func TestTagMerge_NoTags(t *testing.T) {
	d := newTestDaemon(t)

	tc := &config.TeamConfig{
		Name: "Test",
		Repo: t.TempDir(),
		Agents: []config.AgentConfig{
			{Name: "Bot", AgentType: "claude"},
		},
	}

	agents := createAgentsFromConfig(d, tc)
	if len(agents[0].Tags) != 0 {
		t.Errorf("expected no tags when none configured, got %v", agents[0].Tags)
	}
}
