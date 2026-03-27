package persistence

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/lsinghkochava/skwad-cli/internal/models"
)

// newTempStore creates a Store backed by a temporary directory.
func newTempStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	return &Store{dir: dir}
}

func TestStore_AgentsRoundTrip(t *testing.T) {
	s := newTempStore(t)

	agents := []models.Agent{
		{
			ID:        uuid.New(),
			Name:      "Test Agent",
			AgentType: models.AgentTypeClaude,
			Folder:    "/tmp/project",
		},
	}

	if err := s.SaveAgents(agents); err != nil {
		t.Fatalf("SaveAgents: %v", err)
	}

	loaded, err := s.LoadAgents()
	if err != nil {
		t.Fatalf("LoadAgents: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(loaded))
	}
	if loaded[0].ID != agents[0].ID {
		t.Errorf("ID mismatch: got %s, want %s", loaded[0].ID, agents[0].ID)
	}
	if loaded[0].Name != "Test Agent" {
		t.Errorf("Name mismatch: got %q", loaded[0].Name)
	}
}

func TestStore_AgentMigration_DefaultsAgentType(t *testing.T) {
	s := newTempStore(t)

	// Write an agent without agentType to simulate old data.
	raw := `[{"id":"` + uuid.New().String() + `","name":"Legacy","folder":"/tmp"}]`
	if err := os.WriteFile(filepath.Join(s.dir, agentsFile), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}

	agents, err := s.LoadAgents()
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 1 {
		t.Fatalf("expected 1 agent")
	}
	if agents[0].AgentType != models.AgentTypeClaude {
		t.Errorf("expected default agentType claude, got %q", agents[0].AgentType)
	}
}

func TestStore_WorkspacesRoundTrip(t *testing.T) {
	s := newTempStore(t)

	ws := []models.Workspace{
		{
			ID:         uuid.New(),
			Name:       "Main",
			ColorHex:   "#4A90D9",
			LayoutMode: models.LayoutModeSingle,
			SplitRatio: 0.5,
		},
	}

	if err := s.SaveWorkspaces(ws); err != nil {
		t.Fatalf("SaveWorkspaces: %v", err)
	}

	loaded, err := s.LoadWorkspaces()
	if err != nil {
		t.Fatalf("LoadWorkspaces: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("expected 1 workspace, got %d", len(loaded))
	}
	if loaded[0].Name != "Main" {
		t.Errorf("Name mismatch: %q", loaded[0].Name)
	}
}

func TestStore_WorkspaceMigration_DefaultsSplitRatio(t *testing.T) {
	s := newTempStore(t)

	id := uuid.New()
	raw := `[{"id":"` + id.String() + `","name":"W"}]`
	if err := os.WriteFile(filepath.Join(s.dir, workspacesFile), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}

	loaded, err := s.LoadWorkspaces()
	if err != nil {
		t.Fatal(err)
	}
	if loaded[0].SplitRatio != 0.5 {
		t.Errorf("SplitRatio should default to 0.5, got %f", loaded[0].SplitRatio)
	}
	if loaded[0].SplitRatioSecondary != 0.5 {
		t.Errorf("SplitRatioSecondary should default to 0.5, got %f", loaded[0].SplitRatioSecondary)
	}
	if loaded[0].LayoutMode != models.LayoutModeSingle {
		t.Errorf("LayoutMode should default to single, got %q", loaded[0].LayoutMode)
	}
}

func TestStore_ActiveWorkspaceID(t *testing.T) {
	s := newTempStore(t)

	id := uuid.New()
	if err := s.SaveActiveWorkspaceID(id); err != nil {
		t.Fatalf("SaveActiveWorkspaceID: %v", err)
	}

	loaded, err := s.LoadActiveWorkspaceID()
	if err != nil {
		t.Fatalf("LoadActiveWorkspaceID: %v", err)
	}
	if loaded != id {
		t.Errorf("got %s, want %s", loaded, id)
	}
}

func TestStore_SettingsRoundTrip(t *testing.T) {
	s := newTempStore(t)

	settings := models.DefaultSettings()
	settings.MCPServerPort = 9999
	settings.TerminalFontName = "JetBrains Mono"

	if err := s.SaveSettings(settings); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}

	loaded := s.Settings()
	if loaded.MCPServerPort != 9999 {
		t.Errorf("MCPServerPort: got %d, want 9999", loaded.MCPServerPort)
	}
	if loaded.TerminalFontName != "JetBrains Mono" {
		t.Errorf("TerminalFontName: got %q", loaded.TerminalFontName)
	}
}

func TestStore_DefaultSettings_UsedWhenMissing(t *testing.T) {
	s := newTempStore(t)
	// No settings file written — should return defaults.
	settings := s.Settings()
	if settings.MCPServerPort != 8777 {
		t.Errorf("default MCPServerPort should be 8777, got %d", settings.MCPServerPort)
	}
	if !settings.MCPServerEnabled {
		t.Error("MCP server should be enabled by default")
	}
}

func TestStore_PersonasRoundTrip(t *testing.T) {
	s := newTempStore(t)

	personas := models.DefaultPersonas()
	if err := s.SavePersonas(personas); err != nil {
		t.Fatalf("SavePersonas: %v", err)
	}

	loaded, err := s.LoadPersonas()
	if err != nil {
		t.Fatalf("LoadPersonas: %v", err)
	}
	if len(loaded) != len(personas) {
		t.Errorf("expected %d personas, got %d", len(personas), len(loaded))
	}
}

func TestStore_LoadPersonas_ReturnsDefaultsWhenMissing(t *testing.T) {
	s := newTempStore(t)
	// No file — should return default personas.
	personas, err := s.LoadPersonas()
	if err != nil {
		t.Fatal(err)
	}
	if len(personas) != 6 {
		t.Errorf("expected 6 default personas, got %d", len(personas))
	}
}

func TestStore_RecentRepos(t *testing.T) {
	s := newTempStore(t)

	for _, p := range []string{"/a", "/b", "/c"} {
		if err := s.AddRecentRepo(p); err != nil {
			t.Fatalf("AddRecentRepo(%s): %v", p, err)
		}
	}

	repos, err := s.RecentRepos()
	if err != nil {
		t.Fatal(err)
	}
	// Most recently added should be first.
	if repos[0] != "/c" {
		t.Errorf("expected /c first, got %s", repos[0])
	}
}

func TestStore_RecentRepos_NoDuplicates(t *testing.T) {
	s := newTempStore(t)

	_ = s.AddRecentRepo("/a")
	_ = s.AddRecentRepo("/b")
	_ = s.AddRecentRepo("/a") // re-add /a

	repos, _ := s.RecentRepos()
	count := 0
	for _, r := range repos {
		if r == "/a" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("/a should appear exactly once, got %d occurrences", count)
	}
}

func TestStore_RecentRepos_CappedAtFive(t *testing.T) {
	s := newTempStore(t)
	for i := 0; i < 10; i++ {
		_ = s.AddRecentRepo(filepath.Join("/repo", string(rune('a'+i))))
	}
	repos, _ := s.RecentRepos()
	if len(repos) > 5 {
		t.Errorf("recent repos should be capped at 5, got %d", len(repos))
	}
}
