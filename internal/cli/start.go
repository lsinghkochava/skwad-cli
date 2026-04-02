package cli

import (
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	tea "charm.land/bubbletea/v2"
	"github.com/lsinghkochava/skwad-cli/internal/config"
	"github.com/lsinghkochava/skwad-cli/internal/daemon"
	"github.com/lsinghkochava/skwad-cli/internal/tui"
)

var (
	startFlagWatch   bool
	startFlagDataDir string
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
	}
	d, err := daemon.New(cfg)
	if err != nil {
		return fmt.Errorf("initialize daemon: %w", err)
	}

	// 3. Create agents from team config.
	agents := createAgentsFromConfig(d, tc)

	// 4. Start daemon (MCP server + pool).
	if err := d.Start(); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}

	// 5. Spawn all agents.
	for _, a := range agents {
		d.SpawnAgent(a)
	}

	// 6. Write PID file.
	dataDir := startFlagDataDir
	if dataDir == "" {
		dataDir = d.Store.Dir()
	}
	pidFile, err := daemon.WritePIDFile(dataDir)
	if err != nil {
		slog.Warn("failed to write PID file", "error", err)
	}

	// 7. Wire --watch mode.
	if startFlagWatch {
		// TUI dashboard mode — Bubble Tea v2.
		m := tui.New(d.Manager, d.Pool.MCPURL())
		p := tea.NewProgram(m)

		// Wire callbacks BEFORE agents start producing output.
		d.Manager.OnAgentChanged = func(id uuid.UUID) {
			p.Send(tui.AgentChangedMsg(id))
		}
		d.Pool.LogSubscriber = func(agentID uuid.UUID, agentName string, data []byte) {
			p.Send(tui.LogEntryMsg{AgentID: agentID, AgentName: agentName, Data: data})
		}

		// Run TUI (blocks until quit).
		if _, err := p.Run(); err != nil {
			slog.Error("tui error", "error", err)
		}

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
