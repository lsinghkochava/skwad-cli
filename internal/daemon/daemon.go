// Package daemon encapsulates the full Skwad lifecycle: persistence, agent
// management, MCP server, and terminal pool. Both the GUI binary and the
// CLI binary instantiate a Daemon to avoid duplicating initialization code.
package daemon

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/lsinghkochava/skwad-cli/internal/agent"
	"github.com/lsinghkochava/skwad-cli/internal/config"
	"github.com/lsinghkochava/skwad-cli/internal/git"
	"github.com/lsinghkochava/skwad-cli/internal/mcp"
	"github.com/lsinghkochava/skwad-cli/internal/models"
	"github.com/lsinghkochava/skwad-cli/internal/persistence"
	"github.com/lsinghkochava/skwad-cli/internal/process"
)

// Config holds the parameters needed to initialize a Daemon.
type Config struct {
	MCPPort    int    // MCP server port (0 = use settings default)
	DataDir    string // persistence directory (empty = ~/.config/skwad/)
	PluginDir  string // path to plugin/ scripts
	EntryAgent string // default message recipient when --to is omitted
	RepoPath   string // root repo path for worktree creation
}

// Daemon owns the core services shared by GUI and CLI binaries.
type Daemon struct {
	Store       *persistence.Store
	Manager     *agent.Manager
	Coordinator *agent.Coordinator
	MCPServer   *mcp.Server
	Pool        *process.Pool
	Builder     *agent.CommandBuilder
	Config      Config
	SessionID   string // unique per daemon run

	// CoordinationMode is "managed" (default) or "autonomous".
	CoordinationMode string

	// activities tracks the ActivityController per agent for stream-based status.
	activities map[uuid.UUID]*agent.ActivityController

	// worktrees tracks agentID → worktree path for isolated agents.
	worktrees map[uuid.UUID]string

	// Team readiness gate.
	readyMu     sync.Mutex
	readyAgents map[uuid.UUID]bool
	teamReady   chan struct{}
	teamSize    int
}

// New initializes all core services and wires the hookBridge.
// The MCP server is created but NOT started — call Start() to begin serving.
func New(cfg Config) (*Daemon, error) {
	var store *persistence.Store
	var err error
	if cfg.DataDir != "" {
		store, err = persistence.NewStoreAt(cfg.DataDir)
	} else {
		store, err = persistence.NewStore()
	}
	if err != nil {
		return nil, fmt.Errorf("initialize persistence: %w", err)
	}

	mgr, err := agent.NewManager(store)
	if err != nil {
		return nil, fmt.Errorf("initialize agent manager: %w", err)
	}

	coord := agent.NewCoordinator(mgr)
	settings := store.Settings()

	// Determine MCP port: explicit config overrides settings.
	mcpPort := cfg.MCPPort
	if mcpPort == 0 {
		mcpPort = settings.MCPServerPort
	}

	mcpServer := mcp.NewServer(coord, store, mcpPort)
	mcpServer.EntryAgent = cfg.EntryAgent

	d := &Daemon{
		Store:       store,
		Manager:     mgr,
		Coordinator: coord,
		MCPServer:   mcpServer,
		Config:      cfg,
	}

	// Debug: log persisted agent state.
	persistedAgents := mgr.AllAgents()
	if len(persistedAgents) > 0 {
		names := make([]string, len(persistedAgents))
		for i, a := range persistedAgents {
			names[i] = a.Name
		}
		slog.Debug("loaded persisted agents", "count", len(persistedAgents), "names", names)
	} else {
		slog.Debug("no persisted agents found")
	}

	return d, nil
}

// Start starts the MCP server and creates the process pool.
// The MCP server's UI callbacks (OnDisplayMarkdown, etc.) should be set
// before calling Start if the caller needs them.
func (d *Daemon) Start() error {
	settings := d.Store.Settings()

	// Start MCP server (non-fatal on port conflict).
	mcpURL := ""
	if settings.MCPServerEnabled || d.Config.MCPPort != 0 {
		if err := d.MCPServer.Start(); err != nil {
			log.Printf("warning: MCP server could not start: %v", err)
			log.Printf("continuing without MCP server — agents will not be able to communicate")
		} else {
			mcpURL = d.MCPServer.URL()
		}
	}

	d.SessionID = uuid.New().String()[:8]
	d.worktrees = make(map[uuid.UUID]string)

	// Prune stale worktree refs once at startup, and ensure .gitignore has entry.
	if d.Config.RepoPath != "" {
		wm := git.NewWorktreeManager(d.Config.RepoPath)
		_ = wm.Prune()
		ensureGitignoreEntry(d.Config.RepoPath, ".skwad-worktrees/")
	}

	// Create process pool and wire hookBridge.
	d.Pool = process.NewPool(mcpURL)
	d.Builder = &agent.CommandBuilder{MCPServerURL: mcpURL, PluginDir: d.Config.PluginDir}
	d.activities = make(map[uuid.UUID]*agent.ActivityController)
	d.MCPServer.StatusUpdater = &hookBridge{manager: d.Manager}

	// Wire message delivery: when a message arrives, send via stream-json stdin.
	d.Coordinator.OnDeliverMessage = func(agentID uuid.UUID, text string) {
		if err := d.Pool.SendPrompt(agentID, text); err != nil {
			slog.Error("failed to deliver message", "agentID", agentID, "error", err)
		}
	}

	// Wire stream messages through ActivityController for centralized status tracking.
	d.Pool.OnStreamMessage = func(agentID uuid.UUID, msg process.StreamMessage) {
		if ac, ok := d.activities[agentID]; ok {
			ac.OnStreamMessage(msg.Type, msg.Subtype)
		}
	}

	// Wire tool call log for TUI display.
	d.MCPServer.OnToolCallLog = func(agentName, toolName, argsPreview string) {
		if d.Pool.LogSubscriber != nil {
			line := fmt.Sprintf("→ %s(%s)", toolName, argsPreview)
			d.Pool.LogSubscriber(uuid.Nil, agentName, []byte(line))
		}
	}

	return nil
}

// ApplyTeamConfig applies team-level settings from the config: coordination mode,
// max tasks, and task persistence.
func (d *Daemon) ApplyTeamConfig(tc *config.TeamConfig) {
	d.CoordinationMode = tc.Coordination
	if d.CoordinationMode == "" {
		d.CoordinationMode = "managed"
	}

	if tc.MaxTasks > 0 {
		d.Coordinator.SetMaxTasks(tc.MaxTasks)
	}

	if tc.PersistTasks {
		if tasks, err := d.Store.LoadTasks(); err == nil && len(tasks) > 0 {
			d.Coordinator.LoadTasks(tasks)
			slog.Info("restored tasks from disk", "count", len(tasks))
		}

		var taskSaveMu sync.Mutex
		saveTasks := func() {
			taskSaveMu.Lock()
			defer taskSaveMu.Unlock()
			tasks := d.Coordinator.ListTasks()
			if err := d.Store.SaveTasks(tasks); err != nil {
				slog.Error("failed to save tasks", "error", err)
			}
		}
		d.Coordinator.OnTaskCreated = func(task *models.Task) { saveTasks() }
		d.Coordinator.OnTaskCompleted = func(task *models.Task) { saveTasks() }
	}

	// Auto-claim: when an agent goes idle in autonomous mode, assign the next
	// pending task and deliver it as a prompt.
	d.Coordinator.OnAgentIdle = func(agentID uuid.UUID) {
		if d.CoordinationMode != "autonomous" {
			return
		}
		tasks := d.Coordinator.ListTasks()
		for _, t := range tasks {
			if t.Status != models.TaskStatusPending || t.AssigneeID != nil {
				continue
			}
			if err := d.Coordinator.ClaimTask(agentID, t.ID); err != nil {
				continue // another agent beat us, try next
			}
			prompt := fmt.Sprintf("Task assigned to you:\n\nTitle: %s\nDescription: %s\nTask ID: %s\n\nWhen complete, call complete-task with this task ID.",
				t.Title, t.Description, t.ID)
			d.Pool.SendPrompt(agentID, prompt)
			return
		}
	}
}

// Stop gracefully shuts down all services: terminal pool, MCP server.
func (d *Daemon) Stop() error {
	if d.Pool != nil {
		d.Pool.StopAll()
	}
	if d.MCPServer != nil {
		d.MCPServer.Stop()
	}
	if len(d.worktrees) > 0 {
		slog.Info(fmt.Sprintf("%d agent worktrees remain in .skwad-worktrees/%s/", len(d.worktrees), d.SessionID))
		slog.Info("run 'skwad merge' to consolidate or 'skwad clean' to remove")
	}
	d.Manager.Shutdown()
	return nil
}

// SetTeamSize sets the expected number of agents for the readiness gate.
// Must be called before spawning agents.
func (d *Daemon) SetTeamSize(n int) {
	d.readyMu.Lock()
	defer d.readyMu.Unlock()
	d.teamSize = n
	d.readyAgents = make(map[uuid.UUID]bool, n)
	d.teamReady = make(chan struct{})
}

// MarkAgentReady marks an agent as having completed its init turn.
// When all agents are ready, the teamReady channel is closed.
func (d *Daemon) MarkAgentReady(agentID uuid.UUID) {
	d.readyMu.Lock()
	defer d.readyMu.Unlock()

	if d.teamReady == nil {
		return
	}

	d.readyAgents[agentID] = true
	slog.Info("agent ready", "agentID", agentID, "ready", len(d.readyAgents), "total", d.teamSize)

	if len(d.readyAgents) >= d.teamSize {
		select {
		case <-d.teamReady:
			// already closed
		default:
			close(d.teamReady)
		}
	}
}

// WaitForTeamReady blocks until all agents have completed their init turn,
// or the context is cancelled.
func (d *Daemon) WaitForTeamReady(ctx context.Context) error {
	d.readyMu.Lock()
	ch := d.teamReady
	d.readyMu.Unlock()

	if ch == nil {
		return nil
	}

	select {
	case <-ch:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// TeamReady returns the teamReady channel (nil if SetTeamSize was not called).
func (d *Daemon) TeamReady() <-chan struct{} {
	d.readyMu.Lock()
	defer d.readyMu.Unlock()
	return d.teamReady
}

// CleanupWorktrees removes all worktrees created during this session.
func (d *Daemon) CleanupWorktrees() error {
	if d.Config.RepoPath == "" {
		return nil
	}
	wm := git.NewWorktreeManager(d.Config.RepoPath)
	for agentID, path := range d.worktrees {
		if err := wm.Remove(path); err != nil {
			slog.Warn("failed to remove worktree", "agentID", agentID, "path", path, "error", err)
		}
	}
	return wm.Prune()
}

// SpawnAgent builds args and spawns a headless process for the given agent.
// Non-Claude agents are logged and skipped (headless not supported yet).
func (d *Daemon) SpawnAgent(a *models.Agent) {
	settings := d.Store.Settings()
	var persona *models.Persona
	if a.PersonaID != nil {
		persona = d.Manager.Persona(*a.PersonaID)
	}

	// Create worktree for isolated agents.
	if a.WorktreeIsolation && d.Config.RepoPath != "" {
		branchName := fmt.Sprintf("skwad/%s/%s", d.SessionID, sanitizeName(a.Name))
		worktreeDir := filepath.Join(d.Config.RepoPath, ".skwad-worktrees", d.SessionID, sanitizeName(a.Name))

		wm := git.NewWorktreeManager(d.Config.RepoPath)

		if wm.BranchExists(branchName) {
			if err := wm.CreateFromExisting(branchName, worktreeDir); err != nil {
				slog.Error("failed to create worktree from existing branch", "agent", a.Name, "branch", branchName, "error", err)
				return
			}
		} else {
			if err := wm.Create(branchName, worktreeDir); err != nil {
				slog.Error("failed to create worktree", "agent", a.Name, "branch", branchName, "error", err)
				return
			}
		}

		a.WorktreePath = worktreeDir
		a.WorktreeBranch = branchName
		a.Folder = worktreeDir
		d.worktrees[a.ID] = worktreeDir
		slog.Info("created worktree for agent", "agent", a.Name, "branch", branchName, "path", worktreeDir)
	}

	// Gather teammates for system prompt team protocol.
	allAgents := d.Manager.AllAgents()
	var teammates []models.Agent
	for _, ta := range allAgents {
		if ta.ID != a.ID {
			teammates = append(teammates, *ta)
		}
	}

	args, err := d.Builder.BuildArgs(a, persona, &settings, teammates)
	if err != nil {
		slog.Warn("skipping agent — headless not supported", "name", a.Name, "type", a.AgentType, "error", err)
		return
	}

	env := []string{
		"SKWAD_AGENT_ID=" + a.ID.String(),
		"SKWAD_URL=" + strings.TrimSuffix(d.Pool.MCPURL(), "/mcp"),
	}

	// Create activity controller for stream-based status detection.
	ac := agent.NewActivityController(a.ID, a.ActivityMode(), d.Manager)
	ac.OnStatusChanged = func(id uuid.UUID, status models.AgentStatus) {
		if status == models.AgentStatusIdle {
			d.Coordinator.NotifyIdleAgent(id)
		}
		if d.Pool.OnStatusChanged != nil {
			d.Pool.OnStatusChanged(id, status)
		}
	}
	agentID := a.ID
	ac.OnTurnComplete = func() {
		slog.Debug("agent turn complete", "agentID", agentID, "name", a.Name)
		d.MarkAgentReady(agentID)
	}
	d.activities[a.ID] = ac

	slog.Debug("pool.Spawn", "name", a.Name, "id", a.ID, "args", strings.Join(args, " "), "dir", a.Folder)
	if err := d.Pool.Spawn(a.ID, a.Name, args, env, a.Folder); err != nil {
		slog.Error("failed to spawn agent", "name", a.Name, "error", err)
		return
	}

	// Pre-register agent with coordinator so all agents are visible immediately
	// via list-agents, even before the agent calls register-agent itself.
	if _, _, err := d.Coordinator.RegisterAgent(a.ID, a.Name, a.Folder, ""); err != nil {
		slog.Warn("failed to pre-register agent", "name", a.Name, "error", err)
	}

	slog.Debug("agent spawned successfully", "name", a.Name, "id", a.ID)
}

// ensureGitignoreEntry adds an entry to .gitignore if not already present.
func ensureGitignoreEntry(repoPath, entry string) {
	gitignorePath := filepath.Join(repoPath, ".gitignore")
	data, _ := os.ReadFile(gitignorePath)
	if strings.Contains(string(data), entry) {
		return
	}
	f, err := os.OpenFile(gitignorePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	if len(data) > 0 && data[len(data)-1] != '\n' {
		f.WriteString("\n")
	}
	f.WriteString(entry + "\n")
}

// sanitizeName converts a name to a filesystem/branch-safe string.
func sanitizeName(name string) string {
	return strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(name, " ", "-"), "/", "-"))
}

// hookBridge implements mcp.AgentStatusUpdater by routing hook events
// directly to the agent manager. In headless mode, Claude agent status
// comes from stream messages via ActivityController; hookBridge handles
// non-Claude agents and metadata updates.
type hookBridge struct {
	manager *agent.Manager
}

func (h *hookBridge) SetRunning(id uuid.UUID) {
	h.manager.UpdateAgent(id, func(a *models.Agent) { a.Status = models.AgentStatusRunning })
	slog.Debug("hook: set_running", "agentID", id)
}
func (h *hookBridge) SetIdle(id uuid.UUID) {
	h.manager.UpdateAgent(id, func(a *models.Agent) { a.Status = models.AgentStatusIdle })
	slog.Debug("hook: set_idle", "agentID", id)
}
func (h *hookBridge) SetBlocked(id uuid.UUID) {
	h.manager.UpdateAgent(id, func(a *models.Agent) { a.Status = models.AgentStatusInput })
	slog.Debug("hook: set_blocked", "agentID", id)
}
func (h *hookBridge) SetError(id uuid.UUID) {
	h.manager.UpdateAgent(id, func(a *models.Agent) { a.Status = models.AgentStatusError })
	slog.Debug("hook: set_error", "agentID", id)
}
func (h *hookBridge) SetMetadata(id uuid.UUID, key, value string) {
	h.manager.UpdateAgent(id, func(a *models.Agent) {
		if key == "cwd" {
			a.WorkingFolder = value
		}
		if a.Metadata != nil {
			a.Metadata[key] = value
		}
	})
	slog.Debug("hook: set_metadata", "agentID", id, "key", key)
}
func (h *hookBridge) SetSessionID(id uuid.UUID, sessionID string) {
	h.manager.UpdateAgent(id, func(a *models.Agent) { a.SessionID = sessionID })
	slog.Debug("hook: set_session_id", "agentID", id, "sessionID", sessionID)
}
func (h *hookBridge) SetStatusText(id uuid.UUID, status, category string) {
	h.manager.UpdateAgent(id, func(a *models.Agent) {
		a.StatusText = status
		a.StatusCategory = category
	})
	slog.Debug("hook: set_status_text", "agentID", id, "status", status)
}
