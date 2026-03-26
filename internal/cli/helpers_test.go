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
