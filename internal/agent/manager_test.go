package agent

import (
	"testing"

	"github.com/google/uuid"
	"github.com/lsinghkochava/skwad-cli/internal/models"
	"github.com/lsinghkochava/skwad-cli/internal/persistence"
)

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	store := &persistence.Store{}
	// Use a temp dir store so tests don't touch ~/.config/skwad
	dir := t.TempDir()
	store = mustTempStore(t, dir)
	mgr, err := NewManager(store)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return mgr
}

func mustTempStore(t *testing.T, dir string) *persistence.Store {
	t.Helper()
	s, err := persistence.NewStoreAt(dir)
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	return s
}

func addAgent(mgr *Manager, agentType models.AgentType) *models.Agent {
	a := &models.Agent{
		ID:        uuid.New(),
		Name:      "Test",
		AgentType: agentType,
		Folder:    "/tmp",
	}
	mgr.AddAgent(a, nil)
	return a
}

// --- Companion rules ---

func TestManager_CompanionHiddenInSidebar(t *testing.T) {
	mgr := newTestManager(t)

	creator := addAgent(mgr, models.AgentTypeClaude)
	creatorID := creator.ID

	companion := &models.Agent{
		ID:          uuid.New(),
		Name:        "Companion",
		AgentType:   models.AgentTypeShell,
		Folder:      "/tmp",
		IsCompanion: true,
		CreatedBy:   &creatorID,
	}
	mgr.AddAgent(companion, nil)

	// Agents() only returns non-companion agents.
	visible := mgr.Agents()
	for _, a := range visible {
		if a.IsCompanion {
			t.Errorf("companion agent %q should not appear in sidebar list", a.Name)
		}
	}
}

func TestManager_RemoveCreator_RemovesCompanions(t *testing.T) {
	mgr := newTestManager(t)

	creator := addAgent(mgr, models.AgentTypeClaude)
	creatorID := creator.ID

	companion := &models.Agent{
		ID:          uuid.New(),
		Name:        "Shell companion",
		AgentType:   models.AgentTypeShell,
		Folder:      "/tmp",
		IsCompanion: true,
		CreatedBy:   &creatorID,
	}
	mgr.AddAgent(companion, nil)
	companionID := companion.ID

	// Both should exist.
	if _, ok := mgr.Agent(creatorID); !ok {
		t.Fatal("creator should exist")
	}
	if _, ok := mgr.Agent(companionID); !ok {
		t.Fatal("companion should exist before creator removed")
	}

	mgr.RemoveAgent(creatorID)

	if _, ok := mgr.Agent(creatorID); ok {
		t.Error("creator should be removed")
	}
	if _, ok := mgr.Agent(companionID); ok {
		t.Error("companion should be removed when creator is removed")
	}
}

func TestManager_Companions(t *testing.T) {
	mgr := newTestManager(t)
	creator := addAgent(mgr, models.AgentTypeClaude)
	creatorID := creator.ID

	for i := 0; i < 2; i++ {
		c := &models.Agent{
			ID:          uuid.New(),
			Name:        "C",
			AgentType:   models.AgentTypeShell,
			Folder:      "/tmp",
			IsCompanion: true,
			CreatedBy:   &creatorID,
		}
		mgr.AddAgent(c, nil)
	}

	companions := mgr.Companions(creatorID)
	if len(companions) != 2 {
		t.Errorf("expected 2 companions, got %d", len(companions))
	}
}

// --- Fork / Resume ---

func TestManager_ForkAgent(t *testing.T) {
	mgr := newTestManager(t)
	src := addAgent(mgr, models.AgentTypeClaude)

	forked := mgr.ForkAgent(src.ID, "sess-123")
	if forked == nil {
		t.Fatal("ForkAgent returned nil")
	}
	if forked.ID == src.ID {
		t.Error("forked agent should have a new ID")
	}
	if forked.ResumeSessionID != "sess-123" {
		t.Errorf("expected ResumeSessionID sess-123, got %q", forked.ResumeSessionID)
	}
	if !containsString(forked.Name, "(fork)") {
		t.Errorf("forked agent name should contain '(fork)', got %q", forked.Name)
	}
}

func TestManager_ResumeAgent(t *testing.T) {
	mgr := newTestManager(t)
	a := addAgent(mgr, models.AgentTypeClaude)
	origToken := a.RestartToken

	mgr.ResumeAgent(a.ID, "sess-xyz")

	updated, _ := mgr.Agent(a.ID)
	if updated.ResumeSessionID != "sess-xyz" {
		t.Errorf("expected ResumeSessionID sess-xyz, got %q", updated.ResumeSessionID)
	}
	if updated.RestartToken <= origToken {
		t.Error("RestartToken should be incremented on resume")
	}
}

// --- Duplicate ---

func TestManager_DuplicateAgent(t *testing.T) {
	mgr := newTestManager(t)
	src := addAgent(mgr, models.AgentTypeCodex)

	dup := mgr.DuplicateAgent(src.ID)
	if dup == nil {
		t.Fatal("DuplicateAgent returned nil")
	}
	if dup.ID == src.ID {
		t.Error("duplicate should have a new ID")
	}
	if !containsString(dup.Name, "(copy)") {
		t.Errorf("duplicate name should contain '(copy)', got %q", dup.Name)
	}
	if dup.AgentType != src.AgentType {
		t.Errorf("duplicate should have same agent type")
	}
}

// --- Default workspace auto-create ---

func TestManager_AutoCreatesDefaultWorkspace(t *testing.T) {
	mgr := newTestManager(t)
	if len(mgr.Workspaces()) != 0 {
		t.Skip("workspace already exists")
	}
	addAgent(mgr, models.AgentTypeClaude)
	if len(mgr.Workspaces()) == 0 {
		t.Error("default workspace should be created when first agent is added")
	}
}

func TestManager_RestartAgent_ClearsSession(t *testing.T) {
	mgr := newTestManager(t)
	a := addAgent(mgr, models.AgentTypeClaude)

	mgr.UpdateAgent(a.ID, func(ag *models.Agent) {
		ag.SessionID = "live-session"
		ag.IsRegistered = true
	})

	mgr.RestartAgent(a.ID)

	updated, _ := mgr.Agent(a.ID)
	if updated.SessionID != "" {
		t.Error("SessionID should be cleared on restart")
	}
	if updated.IsRegistered {
		t.Error("IsRegistered should be false after restart")
	}
}

func containsString(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && stringContains(s, sub))
}

func stringContains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
