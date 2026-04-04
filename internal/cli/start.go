package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	tea "charm.land/bubbletea/v2"
	"github.com/lsinghkochava/skwad-cli/internal/config"
	"github.com/lsinghkochava/skwad-cli/internal/daemon"
	"github.com/lsinghkochava/skwad-cli/internal/models"
	"github.com/lsinghkochava/skwad-cli/internal/tui"
)

const defaultBootstrapPrompt = "List other agents names and project (no ID) in a table based on context then set your status to indicate you are ready to get going!"

var (
	startFlagWatch   bool
	startFlagDataDir string
	startFlagExplore bool
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the Skwad daemon with agents from a team config",
	Long:  "Starts the MCP server, spawns agents defined in the team config, and blocks until SIGINT/SIGTERM.",
	RunE:  runStart,
}

func init() {
	startCmd.Flags().BoolVar(&startFlagWatch, "watch", false, "stream agent output to stdout")
	startCmd.Flags().StringVar(&startFlagDataDir, "data-dir", "", "data directory (default ~/.config/skwad/)")
	startCmd.Flags().BoolVar(&startFlagExplore, "explore", false, "Start all agents in read-only explore mode")
	rootCmd.AddCommand(startCmd)
}

func runStart(cmd *cobra.Command, args []string) error {
	// 1. Load team config (from file or template).
	vars := config.ParseSetFlags(flagSet)
	tc, err := config.LoadConfigOrTemplate(flagConfig, flagTeam, vars)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// 2. Initialize daemon.
	cfg := daemon.Config{
		MCPPort:    flagPort,
		DataDir:    startFlagDataDir,
		PluginDir:  findPluginDir(),
		EntryAgent: tc.EntryAgent,
		RepoPath:   tc.Repo,
	}
	d, err := daemon.New(cfg)
	if err != nil {
		return fmt.Errorf("initialize daemon: %w", err)
	}

	// 3. Create agents from team config.
	agents := createAgentsFromConfig(d, tc)

	if startFlagExplore {
		for _, a := range agents {
			a.ExploreMode = true
		}
	}

	// 4. Start daemon (MCP server + pool).
	if err := d.Start(); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}

	// 5. Wire TUI callbacks BEFORE spawning agents (so early messages aren't dropped).
	var p *tea.Program
	if startFlagWatch {
		slog.Debug("entering TUI watch mode")
		m := tui.New(d.Manager, d.Pool.MCPURL())
		p = tea.NewProgram(m)

		d.Manager.OnAgentChanged = func(id uuid.UUID) {
			go p.Send(tui.AgentChangedMsg(id))
		}
		d.Pool.LogSubscriber = func(agentID uuid.UUID, agentName string, data []byte) {
			go p.Send(tui.LogEntryMsg{AgentID: agentID, AgentName: agentName, Data: data})
		}
	}

	// 6. Spawn all agents.
	slog.Debug("spawning agents from config", "count", len(agents))
	for _, a := range agents {
		slog.Debug("spawning agent", "name", a.Name, "id", a.ID, "type", a.AgentType)
		d.SpawnAgent(a)
	}

	// 7. Set team size and fire off bootstrap prompts — each goroutine independently
	// waits for its agent to become ready then sends. Fire-and-forget so the TUI starts immediately.
	d.SetTeamSize(len(agents))
	for i, a := range agents {
		agentPrompt := tc.Prompt
		if tc.Agents[i].Prompt != "" {
			agentPrompt = tc.Agents[i].Prompt
		}
		if agentPrompt == "" {
			agentPrompt = defaultBootstrapPrompt
		}
		go func(a *models.Agent, prompt string) {
			slog.Debug("sending prompt to agent", "name", a.Name, "promptLen", len(prompt))
			if err := d.Pool.SendBootstrapPrompt(a.ID, prompt); err != nil {
				slog.Error("failed to send prompt", "agent", a.Name, "error", err)
			}
		}(a, agentPrompt)
	}

	// Wait for team readiness in a background goroutine (non-blocking for start mode).
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		if err := d.WaitForTeamReady(ctx); err != nil {
			slog.Warn("team readiness timeout", "error", err)
		} else {
			slog.Info("team ready — all agents initialized")
		}
	}()

	// 8. Write PID file.
	dataDir := startFlagDataDir
	if dataDir == "" {
		dataDir = d.Store.Dir()
	}
	pidFile, err := daemon.WritePIDFile(dataDir)
	if err != nil {
		slog.Warn("failed to write PID file", "error", err)
	}

	// 9. Block on TUI or signals.
	if p != nil {
		slog.Debug("starting TUI program")
		if _, err := p.Run(); err != nil {
			slog.Error("tui error", "error", err)
		}
		slog.Debug("TUI program exited")

		d.Stop()
		if pidFile != nil {
			pidFile.Close()
		}
		daemon.RemovePIDFile(dataDir)
		return nil
	}

	// Headless mode — print banner and block on signals.
	slog.Info("daemon started", "port", flagPort, "agents", len(agents))
	fmt.Printf("skwad started on port %d\n", flagPort)
	fmt.Printf("Agents: %d\n", len(agents))
	for _, a := range agents {
		slog.Debug("spawning agent", "name", a.Name, "type", a.AgentType)
		fmt.Printf("  - %s (%s)\n", a.Name, a.AgentType)
	}

	// Block on signals — double signal = force kill.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	slog.Info("shutting down...")
	fmt.Println("\nShutting down...")
	go func() {
		<-sig
		slog.Warn("force killing...")
		os.Exit(1)
	}()
	d.Stop()
	if pidFile != nil {
		pidFile.Close()
	}
	daemon.RemovePIDFile(dataDir)
	return nil
}

// findPluginDir locates the plugin/ directory.
func findPluginDir() string {
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
