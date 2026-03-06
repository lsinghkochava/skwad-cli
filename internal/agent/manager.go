// Package agent owns the core business logic for managing agents, workspaces,
// inter-agent messaging, terminal command construction, and status tracking.
// It has no dependency on the UI or terminal layers.
package agent

import (
	"fmt"
	"sync"

	"github.com/google/uuid"
	"github.com/Jared-Boschmann/skwad-linux/internal/models"
	"github.com/Jared-Boschmann/skwad-linux/internal/persistence"
)

// Manager is the central, thread-safe owner of all agent and workspace state.
type Manager struct {
	mu         sync.RWMutex
	agents     map[uuid.UUID]*models.Agent
	workspaces []*models.Workspace
	activeWS   uuid.UUID
	store      *persistence.Store

	// Callbacks — set by UI layer. Called OUTSIDE the manager lock.
	OnAgentChanged     func(id uuid.UUID)
	OnWorkspaceChanged func()
}

// NewManager loads persisted state and returns a ready Manager.
func NewManager(store *persistence.Store) (*Manager, error) {
	m := &Manager{
		agents: make(map[uuid.UUID]*models.Agent),
		store:  store,
	}
	if err := m.load(); err != nil {
		return nil, fmt.Errorf("agent manager load: %w", err)
	}
	return m, nil
}

func (m *Manager) load() error {
	data, err := m.store.LoadAgents()
	if err != nil {
		return err
	}
	for i := range data {
		a := &data[i]
		a.Metadata = make(map[string]string)
		m.agents[a.ID] = a
	}

	ws, err := m.store.LoadWorkspaces()
	if err != nil {
		return err
	}
	m.workspaces = make([]*models.Workspace, len(ws))
	for i := range ws {
		m.workspaces[i] = &ws[i]
	}

	if id, err := m.store.LoadActiveWorkspaceID(); err == nil {
		m.activeWS = id
	}

	// If agents exist but no workspaces, create default "Skwad" workspace.
	if len(m.agents) > 0 && len(m.workspaces) == 0 {
		m.createDefaultWorkspace()
	}

	return nil
}

func (m *Manager) save() {
	agents := m.agentSlice()
	_ = m.store.SaveAgents(agents)

	wss := make([]models.Workspace, len(m.workspaces))
	for i, w := range m.workspaces {
		wss[i] = *w
	}
	_ = m.store.SaveWorkspaces(wss)
	_ = m.store.SaveActiveWorkspaceID(m.activeWS)
}

// notifyAgentChanged calls OnAgentChanged outside any lock.
// Must be called after the lock is released.
func (m *Manager) notifyAgentChanged(id uuid.UUID) {
	m.mu.RLock()
	cb := m.OnAgentChanged
	m.mu.RUnlock()
	if cb != nil {
		cb(id)
	}
}

// notifyWorkspaceChanged calls OnWorkspaceChanged outside any lock.
func (m *Manager) notifyWorkspaceChanged() {
	m.mu.RLock()
	cb := m.OnWorkspaceChanged
	m.mu.RUnlock()
	if cb != nil {
		cb()
	}
}

// Agents returns a copy of all non-companion agents for the active workspace, in order.
func (m *Manager) Agents() []*models.Agent {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ws := m.activeWorkspace()
	if ws == nil {
		return nil
	}
	var result []*models.Agent
	for _, id := range ws.AgentIDs {
		if a, ok := m.agents[id]; ok && !a.IsCompanion {
			result = append(result, a)
		}
	}
	return result
}

// AllAgents returns all agents (including companions).
func (m *Manager) AllAgents() []*models.Agent {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*models.Agent, 0, len(m.agents))
	for _, a := range m.agents {
		result = append(result, a)
	}
	return result
}

func (m *Manager) agentSlice() []models.Agent {
	result := make([]models.Agent, 0, len(m.agents))
	for _, a := range m.agents {
		result = append(result, *a)
	}
	return result
}

// Agent returns the agent with the given ID.
func (m *Manager) Agent(id uuid.UUID) (*models.Agent, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	a, ok := m.agents[id]
	return a, ok
}

// AddAgent creates a new agent, optionally inserting it after afterID in the active workspace.
func (m *Manager) AddAgent(a *models.Agent, afterID *uuid.UUID) {
	m.mu.Lock()
	a.Metadata = make(map[string]string)
	m.agents[a.ID] = a

	ws := m.activeWorkspace()
	if ws == nil {
		ws = m.createDefaultWorkspace()
	}

	if afterID != nil {
		for i, id := range ws.AgentIDs {
			if id == *afterID {
				ws.AgentIDs = append(ws.AgentIDs[:i+1], append([]uuid.UUID{a.ID}, ws.AgentIDs[i+1:]...)...)
				goto saved
			}
		}
	}
	ws.AgentIDs = append(ws.AgentIDs, a.ID)

saved:
	m.save()
	agentID := a.ID
	m.mu.Unlock()

	m.notifyAgentChanged(agentID)
}

// RemoveAgent removes an agent (and its companions) and updates workspace layout.
func (m *Manager) RemoveAgent(id uuid.UUID) {
	m.mu.Lock()
	// Remove companions first.
	for _, a := range m.agents {
		if a.CreatedBy != nil && *a.CreatedBy == id {
			m.removeAgentFromWorkspaces(a.ID)
			delete(m.agents, a.ID)
		}
	}
	m.removeAgentFromWorkspaces(id)
	delete(m.agents, id)
	m.save()
	m.mu.Unlock()

	m.notifyWorkspaceChanged()
}

func (m *Manager) removeAgentFromWorkspaces(id uuid.UUID) {
	for _, ws := range m.workspaces {
		filtered := ws.AgentIDs[:0]
		for _, aid := range ws.AgentIDs {
			if aid != id {
				filtered = append(filtered, aid)
			}
		}
		ws.AgentIDs = filtered

		active := ws.ActiveAgentIDs[:0]
		for _, aid := range ws.ActiveAgentIDs {
			if aid != id {
				active = append(active, aid)
			}
		}
		ws.ActiveAgentIDs = active
	}
}

// UpdateAgent applies f to the agent and persists.
func (m *Manager) UpdateAgent(id uuid.UUID, f func(*models.Agent)) {
	m.mu.Lock()
	_, ok := m.agents[id]
	if ok {
		f(m.agents[id])
		m.save()
	}
	m.mu.Unlock()

	if ok {
		m.notifyAgentChanged(id)
	}
}

// RestartAgent increments the restart token, clearing session state.
func (m *Manager) RestartAgent(id uuid.UUID) {
	m.UpdateAgent(id, func(a *models.Agent) {
		a.RestartToken++
		a.SessionID = ""
		a.ResumeSessionID = ""
		a.IsRegistered = false
		a.Status = models.AgentStatusIdle
	})
}

// ResumeAgent sets the session to resume on next launch, then restarts.
func (m *Manager) ResumeAgent(id uuid.UUID, sessionID string) {
	m.UpdateAgent(id, func(a *models.Agent) {
		a.ResumeSessionID = sessionID
		a.IsFork = false
		a.RestartToken++
		a.SessionID = ""
		a.IsRegistered = false
		a.Status = models.AgentStatusIdle
	})
}

// ForkAgent creates a copy of an agent that resumes the given session as a fork.
func (m *Manager) ForkAgent(id uuid.UUID, sessionID string) *models.Agent {
	m.mu.RLock()
	src, ok := m.agents[id]
	if !ok {
		m.mu.RUnlock()
		return nil
	}
	forked := *src
	m.mu.RUnlock()

	forked.ID = uuid.New()
	forked.Name = src.Name + " (fork)"
	forked.ResumeSessionID = sessionID
	forked.IsFork = true
	forked.SessionID = ""
	forked.IsRegistered = false
	forked.RestartToken = 0
	forked.Metadata = make(map[string]string)

	m.AddAgent(&forked, &id)
	return &forked
}

// Companions returns all companion agents for the given creator ID.
func (m *Manager) Companions(creatorID uuid.UUID) []*models.Agent {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []*models.Agent
	for _, a := range m.agents {
		if a.IsCompanion && a.CreatedBy != nil && *a.CreatedBy == creatorID {
			result = append(result, a)
		}
	}
	return result
}

// DuplicateAgent creates a copy of an agent with " (copy)" name suffix.
func (m *Manager) DuplicateAgent(id uuid.UUID) *models.Agent {
	m.mu.RLock()
	src, ok := m.agents[id]
	if !ok {
		m.mu.RUnlock()
		return nil
	}
	dup := *src
	m.mu.RUnlock()

	dup.ID = uuid.New()
	dup.Name = src.Name + " (copy)"
	dup.IsRegistered = false
	dup.SessionID = ""
	dup.ResumeSessionID = ""
	dup.RestartToken = 0
	dup.Metadata = make(map[string]string)

	m.AddAgent(&dup, &id)
	return &dup
}

// MoveAgent moves an agent from its current workspace to targetWorkspace.
func (m *Manager) MoveAgent(agentID, targetWorkspaceID uuid.UUID) {
	m.mu.Lock()
	m.removeAgentFromWorkspaces(agentID)
	for _, ws := range m.workspaces {
		if ws.ID == targetWorkspaceID {
			ws.AgentIDs = append(ws.AgentIDs, agentID)
			break
		}
	}
	m.save()
	m.mu.Unlock()

	m.notifyWorkspaceChanged()
}

// Workspaces returns a snapshot of all workspaces.
func (m *Manager) Workspaces() []*models.Workspace {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*models.Workspace, len(m.workspaces))
	copy(result, m.workspaces)
	return result
}

// ActiveWorkspace returns the currently active workspace.
func (m *Manager) ActiveWorkspace() *models.Workspace {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.activeWorkspace()
}

func (m *Manager) activeWorkspace() *models.Workspace {
	for _, ws := range m.workspaces {
		if ws.ID == m.activeWS {
			return ws
		}
	}
	if len(m.workspaces) > 0 {
		return m.workspaces[0]
	}
	return nil
}

// SetActiveWorkspace switches to the given workspace, saving state first.
func (m *Manager) SetActiveWorkspace(id uuid.UUID) {
	m.mu.Lock()
	m.activeWS = id
	m.save()
	m.mu.Unlock()

	m.notifyWorkspaceChanged()
}

// AddWorkspace creates a new workspace and appends it.
func (m *Manager) AddWorkspace(ws *models.Workspace) {
	m.mu.Lock()
	m.workspaces = append(m.workspaces, ws)
	m.save()
	m.mu.Unlock()

	m.notifyWorkspaceChanged()
}

// RemoveWorkspace deletes a workspace by ID.
func (m *Manager) RemoveWorkspace(id uuid.UUID) {
	m.mu.Lock()
	filtered := m.workspaces[:0]
	for _, ws := range m.workspaces {
		if ws.ID != id {
			filtered = append(filtered, ws)
		}
	}
	m.workspaces = filtered
	if m.activeWS == id && len(m.workspaces) > 0 {
		m.activeWS = m.workspaces[0].ID
	}
	m.save()
	m.mu.Unlock()

	m.notifyWorkspaceChanged()
}

// UpdateWorkspace applies f to the workspace and persists.
func (m *Manager) UpdateWorkspace(id uuid.UUID, f func(*models.Workspace)) {
	m.mu.Lock()
	for _, ws := range m.workspaces {
		if ws.ID == id {
			f(ws)
			break
		}
	}
	m.save()
	m.mu.Unlock()

	m.notifyWorkspaceChanged()
}

func (m *Manager) createDefaultWorkspace() *models.Workspace {
	ws := &models.Workspace{
		ID:                  uuid.New(),
		Name:                "Skwad",
		ColorHex:            models.WorkspaceColors[0],
		LayoutMode:          models.LayoutModeSingle,
		SplitRatio:          0.5,
		SplitRatioSecondary: 0.5,
	}
	for id := range m.agents {
		ws.AgentIDs = append(ws.AgentIDs, id)
	}
	m.workspaces = append(m.workspaces, ws)
	m.activeWS = ws.ID
	return ws
}

// ActiveSettings returns the current app settings.
func (m *Manager) ActiveSettings() *models.AppSettings {
	s := m.store.Settings()
	return &s
}

// Persona returns the persona with the given ID, or nil if not found.
func (m *Manager) Persona(id uuid.UUID) *models.Persona {
	personas, _ := m.store.LoadPersonas()
	for i := range personas {
		if personas[i].ID == id && personas[i].State != models.PersonaStateDeleted {
			return &personas[i]
		}
	}
	return nil
}

// Shutdown persists final state.
func (m *Manager) Shutdown() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.save()
}
