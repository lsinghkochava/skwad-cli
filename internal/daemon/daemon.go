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

	if err := d.Pool.Spawn(a.ID, a.Name, args, env, a.Folder); err != nil {
		slog.Error("failed to spawn agent", "name", a.Name, "error", err)
		return
	}
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
}
func (h *hookBridge) SetIdle(id uuid.UUID) {
	h.manager.UpdateAgent(id, func(a *models.Agent) { a.Status = models.AgentStatusIdle })
}
func (h *hookBridge) SetBlocked(id uuid.UUID) {
	h.manager.UpdateAgent(id, func(a *models.Agent) { a.Status = models.AgentStatusInput })
}
func (h *hookBridge) SetError(id uuid.UUID) {
	h.manager.UpdateAgent(id, func(a *models.Agent) { a.Status = models.AgentStatusError })
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
}
func (h *hookBridge) SetSessionID(id uuid.UUID, sessionID string) {
	h.manager.UpdateAgent(id, func(a *models.Agent) { a.SessionID = sessionID })
}
func (h *hookBridge) SetStatusText(id uuid.UUID, status, category string) {
	h.manager.UpdateAgent(id, func(a *models.Agent) {
		a.StatusText = status
		a.StatusCategory = category
	})
}
