// Package daemon encapsulates the full Skwad lifecycle: persistence, agent
// management, MCP server, and terminal pool. Both the GUI binary and the
// CLI binary instantiate a Daemon to avoid duplicating initialization code.
package daemon

import (
	"fmt"
	"log"

	"github.com/google/uuid"
	"github.com/lsinghkochava/skwad-cli/internal/agent"
	"github.com/lsinghkochava/skwad-cli/internal/mcp"
	"github.com/lsinghkochava/skwad-cli/internal/models"
	"github.com/lsinghkochava/skwad-cli/internal/persistence"
	"github.com/lsinghkochava/skwad-cli/internal/terminal"
)

// Config holds the parameters needed to initialize a Daemon.
type Config struct {
	MCPPort   int    // MCP server port (0 = use settings default)
	DataDir   string // persistence directory (empty = ~/.config/skwad/)
	PluginDir string // path to plugin/ scripts
}

// Daemon owns the core services shared by GUI and CLI binaries.
type Daemon struct {
	Store       *persistence.Store
	Manager     *agent.Manager
	Coordinator *agent.Coordinator
	MCPServer   *mcp.Server
	Pool        *terminal.Pool
	Config      Config
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

	d := &Daemon{
		Store:       store,
		Manager:     mgr,
		Coordinator: coord,
		MCPServer:   mcpServer,
		Config:      cfg,
	}

	return d, nil
}

// Start starts the MCP server and creates the terminal pool.
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

	// Create terminal pool and wire hookBridge.
	d.Pool = terminal.NewPool(d.Manager, d.Coordinator, mcpURL, d.Config.PluginDir)
	d.MCPServer.StatusUpdater = &hookBridge{pool: d.Pool, manager: d.Manager}

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

// hookBridge implements mcp.AgentStatusUpdater by routing hook events to
// the terminal pool (for status state machine) and the agent manager (for metadata).
type hookBridge struct {
	pool    *terminal.Pool
	manager *agent.Manager
}

func (h *hookBridge) SetRunning(id uuid.UUID) { h.pool.OnHookRunning(id) }
func (h *hookBridge) SetIdle(id uuid.UUID)    { h.pool.OnHookIdle(id) }
func (h *hookBridge) SetBlocked(id uuid.UUID) { h.pool.OnHookBlocked(id) }
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
