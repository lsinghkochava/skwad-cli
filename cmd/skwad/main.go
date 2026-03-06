package main

import (
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/google/uuid"
	"github.com/Jared-Boschmann/skwad-linux/internal/agent"
	"github.com/Jared-Boschmann/skwad-linux/internal/mcp"
	"github.com/Jared-Boschmann/skwad-linux/internal/models"
	"github.com/Jared-Boschmann/skwad-linux/internal/notifications"
	"github.com/Jared-Boschmann/skwad-linux/internal/persistence"
	"github.com/Jared-Boschmann/skwad-linux/internal/terminal"
	"github.com/Jared-Boschmann/skwad-linux/internal/ui"
)

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

func main() {
	store, err := persistence.NewStore()
	if err != nil {
		log.Fatalf("failed to initialize persistence: %v", err)
	}

	agentMgr, err := agent.NewManager(store)
	if err != nil {
		log.Fatalf("failed to initialize agent manager: %v", err)
	}

	coordinator := agent.NewCoordinator(agentMgr)
	settings := store.Settings()
	pluginDir := pluginDirectory()

	// Start MCP server (non-fatal on port conflict).
	var mcpServer *mcp.Server
	mcpURL := ""
	if settings.MCPServerEnabled {
		mcpServer = mcp.NewServer(coordinator, store, settings.MCPServerPort)
		if err := mcpServer.Start(); err != nil {
			log.Printf("warning: MCP server could not start (port %d in use?): %v", settings.MCPServerPort, err)
			log.Printf("continuing without MCP server — agents will not be able to communicate")
		} else {
			mcpURL = mcpServer.URL()
			defer mcpServer.Stop()
		}
	}

	// Create terminal pool with the MCP URL (empty if server didn't start).
	pool := terminal.NewPool(agentMgr, coordinator, mcpURL, pluginDir)

	// Wire MCP hook events → pool status state machine.
	if mcpServer != nil {
		mcpServer.StatusUpdater = &hookBridge{pool: pool, manager: agentMgr}
	}

	// Wire desktop notifications for agent status changes.
	notifSvc := notifications.NewService("Skwad", settings.NotificationsEnabled)
	pool.OnStatusChanged = func(id uuid.UUID, status models.AgentStatus) {
		if status == models.AgentStatusInput {
			if ag, ok := agentMgr.Agent(id); ok {
				notifSvc.Notify(ag.Name+" needs input", "The agent is waiting for your response.")
			}
		}
	}

	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		pool.StopAll()
		agentMgr.Shutdown()
		os.Exit(0)
	}()

	skwadApp := ui.NewApp(agentMgr, coordinator, store, pool)

	// Wire MCP display callbacks to the UI layer.
	if mcpServer != nil {
		mcpServer.OnDisplayMarkdown = func(_ string, filePath string) {
			skwadApp.ShowMarkdownFile(filePath)
		}
		mcpServer.OnViewMermaid = func(_ string, source, title string) {
			skwadApp.ShowMermaid(source, title)
		}
	}

	skwadApp.Run()
}

// pluginDirectory returns the path to the plugin/ directory.
// It looks next to the running executable first, then falls back to the
// working directory (used when running with `go run` during development).
func pluginDirectory() string {
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Join(filepath.Dir(exe), "plugin")
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir
		}
	}
	wd, _ := os.Getwd()
	dir := filepath.Join(wd, "plugin")
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		return dir
	}
	return ""
}
