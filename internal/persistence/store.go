// Package persistence handles JSON-file storage under ~/.config/skwad/.
// All reads include migration defaults so old config files remain compatible.
package persistence

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/lsinghkochava/skwad-cli/internal/models"
)

const (
	configDir      = "skwad"
	agentsFile     = "agents.json"
	workspacesFile = "workspaces.json"
	personasFile   = "personas.json"
	benchFile      = "bench.json"
	settingsFile   = "config.json"
	stateFile      = "state.json"
	recentReposFile = "recent-repos.json"
)

// Store handles all JSON-file persistence under ~/.config/skwad/.
type Store struct {
	dir string
}

// NewStore initialises the store under the XDG config directory.
func NewStore() (*Store, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	return NewStoreAt(filepath.Join(base, configDir))
}

// NewStoreAt initialises a store at an explicit directory (useful for tests).
func NewStoreAt(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

// Dir returns the config directory path.
func (s *Store) Dir() string { return s.dir }

// --- Agents ---

func (s *Store) LoadAgents() ([]models.Agent, error) {
	var agents []models.Agent
	if err := s.load(agentsFile, &agents); err != nil {
		return nil, err
	}
	// Migration defaults.
	for i := range agents {
		if agents[i].AgentType == "" {
			agents[i].AgentType = models.AgentTypeClaude
		}
	}
	return agents, nil
}

func (s *Store) SaveAgents(agents []models.Agent) error {
	return s.save(agentsFile, agents)
}

// --- Workspaces ---

func (s *Store) LoadWorkspaces() ([]models.Workspace, error) {
	var ws []models.Workspace
	if err := s.load(workspacesFile, &ws); err != nil {
		return nil, err
	}
	for i := range ws {
		if ws[i].SplitRatioSecondary == 0 {
			ws[i].SplitRatioSecondary = 0.5
		}
		if ws[i].SplitRatio == 0 {
			ws[i].SplitRatio = 0.5
		}
		if ws[i].LayoutMode == "" {
			ws[i].LayoutMode = models.LayoutModeSingle
		}
	}
	return ws, nil
}

func (s *Store) SaveWorkspaces(ws []models.Workspace) error {
	return s.save(workspacesFile, ws)
}

// --- Active workspace ID ---

type stateData struct {
	ActiveWorkspaceID  string  `json:"activeWorkspaceId"`
	SidebarSplitOffset float64 `json:"sidebarSplitOffset"`
}

func (s *Store) LoadActiveWorkspaceID() (uuid.UUID, error) {
	var state stateData
	if err := s.load(stateFile, &state); err != nil {
		return uuid.Nil, err
	}
	return uuid.Parse(state.ActiveWorkspaceID)
}

func (s *Store) SaveActiveWorkspaceID(id uuid.UUID) error {
	var state stateData
	_ = s.load(stateFile, &state)
	state.ActiveWorkspaceID = id.String()
	return s.save(stateFile, state)
}

// LoadSidebarSplitOffset returns the persisted sidebar split ratio (default 0.20).
func (s *Store) LoadSidebarSplitOffset() float64 {
	var state stateData
	_ = s.load(stateFile, &state)
	if state.SidebarSplitOffset <= 0 {
		return 0.20
	}
	return state.SidebarSplitOffset
}

// SaveSidebarSplitOffset persists the sidebar split ratio.
func (s *Store) SaveSidebarSplitOffset(offset float64) error {
	var state stateData
	_ = s.load(stateFile, &state)
	state.SidebarSplitOffset = offset
	return s.save(stateFile, state)
}

// --- Personas ---

func (s *Store) LoadPersonas() ([]models.Persona, error) {
	var personas []models.Persona
	if err := s.load(personasFile, &personas); err != nil {
		return models.DefaultPersonas(), nil
	}
	// File existed but was empty or had no entries — return defaults.
	if len(personas) == 0 {
		return models.DefaultPersonas(), nil
	}
	return personas, nil
}

func (s *Store) SavePersonas(personas []models.Persona) error {
	return s.save(personasFile, personas)
}

// --- Bench ---

func (s *Store) LoadBench() ([]models.BenchAgent, error) {
	var bench []models.BenchAgent
	if err := s.load(benchFile, &bench); err != nil {
		return nil, nil // empty bench is fine
	}
	return bench, nil
}

func (s *Store) SaveBench(bench []models.BenchAgent) error {
	return s.save(benchFile, bench)
}

// --- Settings ---

func (s *Store) Settings() models.AppSettings {
	settings := models.DefaultSettings()
	_ = s.load(settingsFile, &settings)
	return settings
}

func (s *Store) SaveSettings(settings models.AppSettings) error {
	return s.save(settingsFile, settings)
}

// --- Recent repos ---

func (s *Store) RecentRepos() ([]string, error) {
	var repos []string
	if err := s.load(recentReposFile, &repos); err != nil {
		return nil, nil
	}
	return repos, nil
}

func (s *Store) AddRecentRepo(path string) error {
	repos, _ := s.RecentRepos()

	// Remove if already present.
	filtered := repos[:0]
	for _, r := range repos {
		if r != path {
			filtered = append(filtered, r)
		}
	}

	// Prepend and cap at 5.
	filtered = append([]string{path}, filtered...)
	if len(filtered) > 5 {
		filtered = filtered[:5]
	}
	return s.save(recentReposFile, filtered)
}

// --- helpers ---

func (s *Store) load(name string, v interface{}) error {
	data, err := os.ReadFile(filepath.Join(s.dir, name))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return json.Unmarshal(data, v)
}

func (s *Store) save(name string, v interface{}) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(s.dir, name)
	return os.WriteFile(path, data, 0o600)
}
