package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/google/uuid"
	"github.com/Jared-Boschmann/skwad-linux/internal/agent"
	"github.com/Jared-Boschmann/skwad-linux/internal/autopilot"
	"github.com/Jared-Boschmann/skwad-linux/internal/git"
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
func (h *hookBridge) SetStatusText(id uuid.UUID, status, category string) {
	h.manager.UpdateAgent(id, func(a *models.Agent) {
		a.StatusText = status
		a.StatusCategory = category
	})
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

	// showAutopilotDecision is set after skwadApp is created so the UI can display
	// the decision sheet. We use an indirect pointer to avoid a forward reference.
	var showAutopilotDecision func(uuid.UUID, string, func(string))

	// Wire desktop notifications and autopilot for agent status changes.
	notifSvc := notifications.NewService("Skwad", settings.NotificationsEnabled)
	pool.OnStatusChanged = func(id uuid.UUID, status models.AgentStatus) {
		// Desktop notification when agent needs input.
		if status == models.AgentStatusInput {
			if ag, ok := agentMgr.Agent(id); ok {
				notifSvc.Notify(ag.Name+" needs input", "The agent is waiting for your response.")
			}
		}

		// Autopilot: analyze last output when a hook-managed agent goes idle.
		if status == models.AgentStatusIdle {
			ag, ok := agentMgr.Agent(id)
			if !ok || !ag.SupportsHooks() {
				return
			}
			s := store.Settings()
			if !s.Autopilot.Enabled {
				return
			}
			lastOut := pool.LastOutput(id)
			if lastOut == "" {
				return
			}
			go func() {
				svc := autopilot.NewService(&s.Autopilot)
				cls, err := svc.Analyze(lastOut)
				if err != nil {
					return
				}
				switch s.Autopilot.Action {
				case models.AutopilotActionMark:
					if cls != autopilot.ClassificationCompleted {
						agentMgr.UpdateAgent(id, func(a *models.Agent) {
							a.Status = models.AgentStatusInput
						})
					}
				case models.AutopilotActionAsk:
					if cls != autopilot.ClassificationCompleted {
						// Mark as input so the sidebar shows it needs attention.
						agentMgr.UpdateAgent(id, func(a *models.Agent) {
							a.Status = models.AgentStatusInput
						})
						if showAutopilotDecision != nil {
							out := lastOut // capture for goroutine
							showAutopilotDecision(id, out, func(reply string) {
								if reply != "" {
									pool.InjectText(id, reply+"\n")
								}
							})
						}
					}
				case models.AutopilotActionContinue:
					if cls == autopilot.ClassificationBinary {
						pool.InjectText(id, "yes, continue\n")
					} else if cls == autopilot.ClassificationOpen {
						agentMgr.UpdateAgent(id, func(a *models.Agent) {
							a.Status = models.AgentStatusInput
						})
					}
				case models.AutopilotActionCustom:
					if cls != autopilot.ClassificationCompleted {
						resp, err := svc.CustomResponse(lastOut)
						if err == nil && resp != "" {
							pool.InjectText(id, resp+"\n")
						}
					}
				}
			}()
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

	// Wire autopilot decision sheet to the UI layer.
	showAutopilotDecision = skwadApp.ShowAutopilotDecision

	// Wire MCP display callbacks to the UI layer.
	if mcpServer != nil {
		mcpServer.OnDisplayMarkdown = func(_ string, filePath string) {
			skwadApp.ShowMarkdownFile(filePath)
		}
		mcpServer.OnViewMermaid = func(_ string, source, title string) {
			skwadApp.ShowMermaid(source, title)
		}
		mcpServer.OnCreateAgent = func(req mcp.CreateAgentRequest) error {
			folder := req.Folder
			if req.NewWorktree && req.BranchName != "" {
				destPath := git.SuggestedPath(folder, req.BranchName)
				wm := git.NewWorktreeManager(folder)
				if err := wm.Create(req.BranchName, destPath); err != nil {
					return fmt.Errorf("create worktree: %w", err)
				}
				folder = destPath
			}
			at := models.AgentType(req.AgentType)
			if at == "" {
				at = models.AgentTypeClaude
			}
			newAg := &models.Agent{
				ID:          uuid.New(),
				Name:        req.Name,
				Avatar:      "🤖",
				Folder:      folder,
				AgentType:   at,
				IsCompanion: req.IsCompanion,
			}
			if req.CreatedByID != "" {
				if cid, err := uuid.Parse(req.CreatedByID); err == nil {
					newAg.CreatedBy = &cid
				}
			}
			agentMgr.AddAgent(newAg, nil)
			pool.Spawn(newAg)
			return nil
		}
		mcpServer.OnCloseAgent = func(callerID, targetID string) error {
			tid, err := uuid.Parse(targetID)
			if err != nil {
				return fmt.Errorf("invalid agent ID: %w", err)
			}
			target, ok := agentMgr.Agent(tid)
			if !ok {
				return fmt.Errorf("agent not found: %s", targetID)
			}
			// Only allow closing agents created by the caller.
			if target.CreatedBy != nil {
				if cid, err := uuid.Parse(callerID); err == nil && *target.CreatedBy == cid {
					pool.Kill(tid)
					agentMgr.RemoveAgent(tid)
					return nil
				}
			}
			return fmt.Errorf("not authorized to close agent %s", targetID)
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
