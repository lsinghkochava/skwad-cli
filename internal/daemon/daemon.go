// Package daemon encapsulates the full Skwad lifecycle: persistence, agent
// management, MCP server, and terminal pool. Both the GUI binary and the
// CLI binary instantiate a Daemon to avoid duplicating initialization code.
package daemon

import (
	"fmt"
	"log"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/lsinghkochava/skwad-cli/internal/agent"
	"github.com/lsinghkochava/skwad-cli/internal/mcp"
	"github.com/lsinghkochava/skwad-cli/internal/models"
	"github.com/lsinghkochava/skwad-cli/internal/persistence"
	"github.com/lsinghkochava/skwad-cli/internal/process"
	"github.com/lsinghkochava/skwad-cli/internal/runlog"
)

// Config holds the parameters needed to initialize a Daemon.
type Config struct {
	MCPPort    int    // MCP server port (0 = use settings default)
	DataDir    string // persistence directory (empty = ~/.config/skwad/)
	PluginDir  string // path to plugin/ scripts
	EntryAgent string // default message recipient when --to is omitted
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

	// activities tracks the ActivityController per agent for stream-based status.
	activities map[uuid.UUID]*agent.ActivityController

	// runLog is the structured JSONL logger for agent activity.
	runLog *runlog.RunLogger
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

// SetRunLogger sets the structured JSONL logger on the Daemon.
func (d *Daemon) SetRunLogger(rl *runlog.RunLogger) {
	d.runLog = rl
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

	// Create process pool and wire hookBridge.
	d.Pool = process.NewPool(mcpURL)
	d.Builder = &agent.CommandBuilder{MCPServerURL: mcpURL, PluginDir: d.Config.PluginDir}
	d.activities = make(map[uuid.UUID]*agent.ActivityController)
	d.MCPServer.StatusUpdater = &hookBridge{manager: d.Manager, runLog: d.runLog}

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

	// Wire run logging callbacks.
	d.MCPServer.OnToolCall = func(agentID, agentName, toolName string, args map[string]interface{}, result interface{}) {
		d.runLog.LogToolCall(agentID, agentName, toolName, args, result)
	}
	d.MCPServer.OnToolCallLog = func(agentName, toolName, argsPreview string) {
		if d.Pool.LogSubscriber != nil {
			line := fmt.Sprintf("→ %s(%s)", toolName, argsPreview)
			d.Pool.LogSubscriber(uuid.Nil, agentName, []byte(line))
		}
	}
	d.Coordinator.OnMessageSent = func(fromID, fromName, toID, content string) {
		d.runLog.LogMessage(fromID, fromName, toID, content)
	}
	d.Coordinator.OnBroadcast = func(fromID, fromName, content string) {
		d.runLog.LogBroadcast(fromID, fromName, content)
	}
	d.Coordinator.OnStatusChanged = func(agentID, agentName, status, category string) {
		d.runLog.LogStatus(agentID, agentName, status, "", category)
	}
	d.Pool.OnSpawn = func(agentID uuid.UUID, agentName string, args []string) {
		d.runLog.LogSpawn(agentID.String(), agentName, "", "", args)
	}
	d.Pool.OnExit = func(agentID uuid.UUID, exitCode int) {
		agentName := ""
		if a, ok := d.Manager.Agent(agentID); ok {
			agentName = a.Name
		}
		d.runLog.LogExit(agentID.String(), agentName, exitCode)
	}
	d.Pool.OnPromptSent = func(agentID uuid.UUID, agentName, promptType, prompt string) {
		d.runLog.LogPrompt(agentID.String(), agentName, promptType, prompt)
	}

	return nil
}

// Stop gracefully shuts down all services: terminal pool, MCP server.
func (d *Daemon) Stop() error {
	if d.Pool != nil {
		d.Pool.StopAll()
	}
	if d.MCPServer != nil {
		d.MCPServer.Stop()
	}
	d.runLog.Close()
	d.Manager.Shutdown()
	return nil
}

// SpawnAgent builds args and spawns a headless process for the given agent.
// Non-Claude agents are logged and skipped (headless not supported yet).
func (d *Daemon) SpawnAgent(a *models.Agent) {
	settings := d.Store.Settings()
	var persona *models.Persona
	if a.PersonaID != nil {
		persona = d.Manager.Persona(*a.PersonaID)
	}

	args, err := d.Builder.BuildArgs(a, persona, &settings)
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
	ac.OnTurnComplete = func() {
		// Agent finished a turn — ready for next prompt.
		slog.Debug("agent turn complete", "agentID", a.ID, "name", a.Name)
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

// hookBridge implements mcp.AgentStatusUpdater by routing hook events
// directly to the agent manager. In headless mode, Claude agent status
// comes from stream messages via ActivityController; hookBridge handles
// non-Claude agents and metadata updates.
type hookBridge struct {
	manager *agent.Manager
	runLog  *runlog.RunLogger
}

func (h *hookBridge) SetRunning(id uuid.UUID) {
	h.manager.UpdateAgent(id, func(a *models.Agent) { a.Status = models.AgentStatusRunning })
	h.logHook(id, "set_running", "running")
}
func (h *hookBridge) SetIdle(id uuid.UUID) {
	h.manager.UpdateAgent(id, func(a *models.Agent) { a.Status = models.AgentStatusIdle })
	h.logHook(id, "set_idle", "idle")
}
func (h *hookBridge) SetBlocked(id uuid.UUID) {
	h.manager.UpdateAgent(id, func(a *models.Agent) { a.Status = models.AgentStatusInput })
	h.logHook(id, "set_blocked", "input")
}
func (h *hookBridge) SetError(id uuid.UUID) {
	h.manager.UpdateAgent(id, func(a *models.Agent) { a.Status = models.AgentStatusError })
	h.logHook(id, "set_error", "error")
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
	h.logHook(id, "set_metadata", key+"="+value)
}
func (h *hookBridge) SetSessionID(id uuid.UUID, sessionID string) {
	h.manager.UpdateAgent(id, func(a *models.Agent) { a.SessionID = sessionID })
	h.logHook(id, "set_session_id", sessionID)
}
func (h *hookBridge) SetStatusText(id uuid.UUID, status, category string) {
	h.manager.UpdateAgent(id, func(a *models.Agent) {
		a.StatusText = status
		a.StatusCategory = category
	})
	h.logHook(id, "set_status_text", status)
}

func (h *hookBridge) logHook(id uuid.UUID, eventType, status string) {
	agentName := ""
	if a, ok := h.manager.Agent(id); ok {
		agentName = a.Name
	}
	h.runLog.LogHookEvent(id.String(), agentName, eventType, status)
}
